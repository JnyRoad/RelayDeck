package modeltrace

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/JnyRoad/RelayDeck/internal/pkg/errors"
	"github.com/lib/pq"
)

// conversationCursor captures the stable chronological position of one trace
// without exposing database identifiers through the administrator API.
type conversationCursor struct {
	CreatedAt time.Time `json:"created_at"`
	TraceID   string    `json:"trace_id"`
}

// encodeConversationCursor serializes one stable trace position as a URL-safe
// opaque token for a subsequent bounded conversation request.
func encodeConversationCursor(cursor conversationCursor) (string, error) {
	if cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.TraceID) == "" {
		return "", fmt.Errorf("model trace conversation cursor is invalid")
	}
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode model trace conversation cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// decodeConversationCursor validates and restores a URL-safe cursor returned
// by this service before it becomes a parameterized SQL comparison value.
func decodeConversationCursor(value string) (conversationCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return conversationCursor{}, fmt.Errorf("model trace conversation cursor is invalid")
	}
	var cursor conversationCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.TraceID) == "" {
		return conversationCursor{}, fmt.Errorf("model trace conversation cursor is invalid")
	}
	return cursor, nil
}

// ListTraces 查询模型调用的轻量索引；查询不连接正文表，也不读取密文。
func (r *PostgresRepository) ListTraces(ctx context.Context, filter TraceFilter, page, pageSize int) ([]TraceSummary, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, fmt.Errorf("model trace database is unavailable")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	where, arguments := modelTraceWhere(filter)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_call_traces t WHERE `+where, arguments...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count model traces: %w", err)
	}
	limitPlaceholder := len(arguments) + 1
	offsetPlaceholder := len(arguments) + 2
	arguments = append(arguments, pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, traceSummarySelect+` WHERE `+where+` ORDER BY t.created_at DESC, t.id DESC LIMIT $`+fmt.Sprint(limitPlaceholder)+` OFFSET $`+fmt.Sprint(offsetPlaceholder), arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("list model traces: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]TraceSummary, 0)
	for rows.Next() {
		item, scanErr := scanTraceSummary(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate model traces: %w", err)
	}
	return items, total, nil
}

// GetTrace 返回一条调用的索引和正文元数据，不读取任何正文密文。
func (r *PostgresRepository) GetTrace(ctx context.Context, traceID string) (TraceDetail, error) {
	if r == nil || r.db == nil {
		return TraceDetail{}, fmt.Errorf("model trace database is unavailable")
	}
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return TraceDetail{}, infraerrors.NotFound("MODEL_TRACE_NOT_FOUND", "model trace not found")
	}
	trace, err := scanTraceSummary(r.db.QueryRowContext(ctx, traceSummarySelect+` WHERE t.trace_id=$1`, traceID))
	if errors.Is(err, sql.ErrNoRows) {
		return TraceDetail{}, infraerrors.NotFound("MODEL_TRACE_NOT_FOUND", "model trace not found")
	}
	if err != nil {
		return TraceDetail{}, err
	}
	detail := TraceDetail{Trace: trace, Attempts: make([]TraceAttempt, 0), Payloads: make([]TracePayload, 0)}
	attemptRows, err := r.db.QueryContext(ctx, `
		SELECT attempt.attempt_no, attempt.account_id, attempt.account_snapshot,
			attempt.upstream_route, attempt.upstream_model, attempt.outcome,
			attempt.status_code, attempt.error_stage, attempt.error_code,
			attempt.duration_ms, attempt.started_at, attempt.completed_at
		FROM model_call_trace_attempts attempt
		JOIN model_call_traces trace ON trace.id=attempt.model_call_trace_id
		WHERE trace.trace_id=$1
		ORDER BY attempt.attempt_no ASC, attempt.id ASC
	`, traceID)
	if err != nil {
		return TraceDetail{}, fmt.Errorf("list model trace upstream attempts: %w", err)
	}
	defer func() { _ = attemptRows.Close() }()
	for attemptRows.Next() {
		attempt, scanErr := scanTraceAttempt(attemptRows)
		if scanErr != nil {
			return TraceDetail{}, scanErr
		}
		detail.Attempts = append(detail.Attempts, attempt)
	}
	if err := attemptRows.Err(); err != nil {
		return TraceDetail{}, fmt.Errorf("iterate model trace upstream attempts: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT p.kind, p.attempt_no, p.capture_status, p.mime_type, p.original_bytes,
			p.stored_bytes, p.sha256, p.storage_mode, p.created_at
		FROM model_call_payloads p
		JOIN model_call_traces t ON t.id=p.model_call_trace_id
		WHERE t.trace_id=$1
		ORDER BY p.created_at ASC, p.id ASC
	`, traceID)
	if err != nil {
		return TraceDetail{}, fmt.Errorf("list model trace payloads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var payload TracePayload
		if err := rows.Scan(&payload.Kind, &payload.AttemptNo, &payload.CaptureStatus, &payload.ContentType,
			&payload.OriginalBytes, &payload.StoredBytes, &payload.SHA256, &payload.StorageMode, &payload.CreatedAt); err != nil {
			return TraceDetail{}, fmt.Errorf("scan model trace payload: %w", err)
		}
		detail.Payloads = append(detail.Payloads, payload)
	}
	if err := rows.Err(); err != nil {
		return TraceDetail{}, fmt.Errorf("iterate model trace payloads: %w", err)
	}
	return detail, nil
}

// scanTraceAttempt maps nullable transport terminal values to explicit API
// nulls while keeping the attempt order chosen by the repository query.
func scanTraceAttempt(scanner traceRowScanner) (TraceAttempt, error) {
	var attempt TraceAttempt
	var accountID sql.NullInt64
	var statusCode, durationMS sql.NullInt32
	var completedAt sql.NullTime
	err := scanner.Scan(
		&attempt.AttemptNo, &accountID, &attempt.AccountSnapshot,
		&attempt.UpstreamRoute, &attempt.UpstreamModel, &attempt.Outcome,
		&statusCode, &attempt.ErrorStage, &attempt.ErrorCode,
		&durationMS, &attempt.StartedAt, &completedAt,
	)
	if err != nil {
		return TraceAttempt{}, fmt.Errorf("scan model trace upstream attempt: %w", err)
	}
	attempt.AccountID = optionalInt64(accountID)
	attempt.StatusCode = optionalInt(statusCode)
	attempt.DurationMS = optionalInt(durationMS)
	attempt.CompletedAt = optionalTime(completedAt)
	return attempt, nil
}

// GetPayload reads one selected encrypted body after an administrator has
// explicitly opened that payload tab; it never widens the query to siblings.
func (r *PostgresRepository) GetPayload(ctx context.Context, traceID string, kind PayloadKind, attemptNo int) (TracePayload, error) {
	if r == nil || r.db == nil {
		return TracePayload{}, fmt.Errorf("model trace database is unavailable")
	}
	var payload TracePayload
	err := r.db.QueryRowContext(ctx, `
		SELECT p.kind, p.attempt_no, p.capture_status, p.mime_type, p.original_bytes,
			p.stored_bytes, p.sha256, p.storage_mode, p.ciphertext, p.created_at
		FROM model_call_payloads p
		JOIN model_call_traces t ON t.id=p.model_call_trace_id
		WHERE t.trace_id=$1 AND p.kind=$2 AND p.attempt_no=$3
	`, strings.TrimSpace(traceID), string(kind), attemptNo).Scan(
		&payload.Kind, &payload.AttemptNo, &payload.CaptureStatus, &payload.ContentType,
		&payload.OriginalBytes, &payload.StoredBytes, &payload.SHA256, &payload.StorageMode, &payload.Ciphertext, &payload.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TracePayload{}, infraerrors.NotFound("MODEL_TRACE_PAYLOAD_NOT_FOUND", "model trace payload not found")
	}
	if err != nil {
		return TracePayload{}, fmt.Errorf("get model trace payload: %w", err)
	}
	return payload, nil
}

// GetPayloadPage reads only the selected parent header and the fixed number of
// encrypted chunks that can produce one bounded plaintext response. Legacy
// inline rows retain their existing single-ciphertext behavior.
func (r *PostgresRepository) GetPayloadPage(ctx context.Context, traceID string, kind PayloadKind, attemptNo, chunkNo, maxPlaintextBytes int) (TracePayloadPage, error) {
	payload, err := r.GetPayload(ctx, traceID, kind, attemptNo)
	if err != nil {
		return TracePayloadPage{}, err
	}
	page := TracePayloadPage{Payload: payload}
	if payload.StorageMode != "chunked" {
		if payload.Ciphertext != "" {
			page.Ciphertexts = []string{payload.Ciphertext}
		}
		page.Payload.Ciphertext = ""
		return page, nil
	}
	if chunkNo < 0 {
		return TracePayloadPage{}, fmt.Errorf("model trace payload chunk number is invalid")
	}
	if maxPlaintextBytes < 1 || maxPlaintextBytes > maxPayloadPagePlaintextBytes {
		maxPlaintextBytes = maxPayloadPagePlaintextBytes
	}
	maxChunks := (maxPlaintextBytes + payloadChunkPlaintextBytes - 1) / payloadChunkPlaintextBytes
	rows, err := r.db.QueryContext(ctx, `
		SELECT chunk_no, ciphertext, stored_bytes
		FROM model_call_payload_chunks
		WHERE model_call_payload_id=(
			SELECT p.id
			FROM model_call_payloads p
			JOIN model_call_traces t ON t.id=p.model_call_trace_id
			WHERE t.trace_id=$1 AND p.kind=$2 AND p.attempt_no=$3
		)
		AND chunk_no >= $4
		ORDER BY chunk_no ASC
		LIMIT $5
	`, strings.TrimSpace(traceID), string(kind), attemptNo, chunkNo, maxChunks+1)
	if err != nil {
		return TracePayloadPage{}, fmt.Errorf("list model trace payload chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var currentChunkNo int
		var ciphertext string
		var storedBytes int64
		if err := rows.Scan(&currentChunkNo, &ciphertext, &storedBytes); err != nil {
			return TracePayloadPage{}, fmt.Errorf("scan model trace payload chunk: %w", err)
		}
		if len(page.Ciphertexts) == maxChunks {
			page.NextChunkNo = &currentChunkNo
			break
		}
		if storedBytes < 0 || storedBytes > int64(payloadChunkPlaintextBytes) {
			return TracePayloadPage{}, fmt.Errorf("model trace payload chunk size is invalid")
		}
		page.Ciphertexts = append(page.Ciphertexts, ciphertext)
	}
	if err := rows.Err(); err != nil {
		return TracePayloadPage{}, fmt.Errorf("iterate model trace payload chunks: %w", err)
	}
	return page, nil
}

const traceSummarySelect = `
	SELECT t.trace_id, t.request_id, t.user_id, t.api_key_id, t.group_id, t.account_id,
		t.user_snapshot, t.api_key_snapshot, t.group_snapshot, t.account_snapshot,
		t.session_id, t.previous_response_id, t.response_id,
		t.route, t.protocol, t.requested_model, t.upstream_model, t.response_model, t.outcome,
		t.status_code, t.stream, t.duration_ms, t.first_byte_ms,
		t.request_capture_status, t.response_capture_status, t.request_bytes, t.response_bytes,
		t.expires_at, t.created_at, t.completed_at
	FROM model_call_traces t`

// traceRowScanner 抽象 sql.Row 和 sql.Rows 共同的 Scan 能力，统一索引字段映射。
type traceRowScanner interface {
	Scan(dest ...any) error
}

// scanTraceSummary 将可空的 PostgreSQL 字段映射为显式指针，保持 API 的 null 语义。
func scanTraceSummary(scanner traceRowScanner) (TraceSummary, error) {
	var item TraceSummary
	var userID, apiKeyID, groupID, accountID sql.NullInt64
	var statusCode, durationMS, firstByteMS sql.NullInt32
	var completedAt sql.NullTime
	err := scanner.Scan(
		&item.TraceID, &item.RequestID, &userID, &apiKeyID, &groupID, &accountID,
		&item.UserSnapshot, &item.APIKeySnapshot, &item.GroupSnapshot, &item.AccountSnapshot,
		&item.SessionID, &item.PreviousResponseID, &item.ResponseID,
		&item.Route, &item.Protocol, &item.RequestedModel, &item.UpstreamModel, &item.ResponseModel, &item.Outcome,
		&statusCode, &item.Stream, &durationMS, &firstByteMS,
		&item.RequestCaptureStatus, &item.ResponseCaptureStatus, &item.RequestBytes, &item.ResponseBytes,
		&item.ExpiresAt, &item.CreatedAt, &completedAt,
	)
	if err != nil {
		return TraceSummary{}, err
	}
	item.UserID = optionalInt64(userID)
	item.APIKeyID = optionalInt64(apiKeyID)
	item.GroupID = optionalInt64(groupID)
	item.AccountID = optionalInt64(accountID)
	item.StatusCode = optionalInt(statusCode)
	item.DurationMS = optionalInt(durationMS)
	item.FirstByteMS = optionalInt(firstByteMS)
	item.CompletedAt = optionalTime(completedAt)
	return item, nil
}

// GetConversation keeps the legacy repository capability by returning the
// initial bounded page instead of loading every linked turn at once.
func (r *PostgresRepository) GetConversation(ctx context.Context, traceID string) (TraceConversation, error) {
	return r.GetConversationPage(ctx, traceID, ConversationPageRequest{Limit: defaultConversationTurnPageSize})
}

// GetConversationPage reads one centered or cursor-directed replay window. It
// relies only on an explicit session or response lineage and hydrates no more
// than the requested page of headers, attempts, and payload metadata.
func (r *PostgresRepository) GetConversationPage(ctx context.Context, traceID string, page ConversationPageRequest) (TraceConversation, error) {
	current, err := r.GetTrace(ctx, traceID)
	if err != nil {
		return TraceConversation{}, err
	}
	page, err = normalizeConversationPageRequest(page)
	if err != nil {
		return TraceConversation{}, err
	}
	conversation := TraceConversation{
		CurrentTraceID: current.Trace.TraceID,
		Turns:          []TraceDetail{current},
	}
	currentPosition := conversationCursor{CreatedAt: current.Trace.CreatedAt, TraceID: current.Trace.TraceID}
	var listOlder, listNewer conversationSegmentLoader
	if current.Trace.SessionID != "" {
		conversation.Linked = true
		conversation.LinkSource = "session_id"
		listOlder = func(pageCtx context.Context, cursor conversationCursor, limit int) ([]conversationCursor, bool, error) {
			return r.listConversationTraceIDsBySessionSegment(pageCtx, current.Trace.SessionID, cursor, "older", limit)
		}
		listNewer = func(pageCtx context.Context, cursor conversationCursor, limit int) ([]conversationCursor, bool, error) {
			return r.listConversationTraceIDsBySessionSegment(pageCtx, current.Trace.SessionID, cursor, "newer", limit)
		}
	} else if current.Trace.PreviousResponseID != "" || current.Trace.ResponseID != "" {
		conversation.Linked = true
		conversation.LinkSource = "response_lineage"
		listOlder = func(pageCtx context.Context, cursor conversationCursor, limit int) ([]conversationCursor, bool, error) {
			return r.listConversationTraceIDsByLineageSegment(pageCtx, current.Trace.TraceID, cursor, "older", limit)
		}
		listNewer = func(pageCtx context.Context, cursor conversationCursor, limit int) ([]conversationCursor, bool, error) {
			return r.listConversationTraceIDsByLineageSegment(pageCtx, current.Trace.TraceID, cursor, "newer", limit)
		}
	} else {
		return conversation, nil
	}

	positions, hasOlder, hasNewer, pageErr := loadConversationPagePositions(ctx, currentPosition, page, listOlder, listNewer)
	if pageErr != nil {
		return TraceConversation{}, pageErr
	}
	if len(positions) == 0 {
		conversation.Turns = []TraceDetail{}
		return conversation, nil
	}
	traceIDs := make([]string, 0, len(positions))
	for _, position := range positions {
		traceIDs = append(traceIDs, position.TraceID)
	}
	turns, loadErr := r.loadConversationTurns(ctx, traceIDs)
	if loadErr != nil {
		return TraceConversation{}, loadErr
	}
	turns = retainLoadedCurrentConversationTurn(turns, positions, current)
	conversation.Turns = turns
	conversation.OlderCursor, conversation.NewerCursor = conversationPageCursors(turns, positions, hasOlder, hasNewer)
	return conversation, nil
}

// conversationSegmentLoader reads one ordered side of a replay around a stable
// cursor and reports whether the same direction has another page.
type conversationSegmentLoader func(context.Context, conversationCursor, int) ([]conversationCursor, bool, error)

// loadConversationPagePositions selects a centered initial window or one
// cursor-directed window, keeping all query result slices bounded by the page.
func loadConversationPagePositions(ctx context.Context, current conversationCursor, page ConversationPageRequest, listOlder, listNewer conversationSegmentLoader) ([]conversationCursor, bool, bool, error) {
	if page.Direction != "" {
		cursor, err := decodeConversationCursor(page.Cursor)
		if err != nil {
			return nil, false, false, err
		}
		if page.Direction == "older" {
			positions, hasMore, loadErr := listOlder(ctx, cursor, page.Limit)
			return positions, hasMore, len(positions) > 0, loadErr
		}
		positions, hasMore, loadErr := listNewer(ctx, cursor, page.Limit)
		return positions, len(positions) > 0, hasMore, loadErr
	}

	// Read bounded candidates from both sides so the initial window keeps the
	// selected trace near the center while filling available adjacent turns.
	olderCandidates, olderMore, err := listOlder(ctx, current, page.Limit-1)
	if err != nil {
		return nil, false, false, err
	}
	newerCandidates, newerMore, err := listNewer(ctx, current, page.Limit-1)
	if err != nil {
		return nil, false, false, err
	}
	older, newer := chooseInitialConversationPositions(olderCandidates, newerCandidates, page.Limit)
	positions := make([]conversationCursor, 0, len(older)+1+len(newer))
	positions = append(positions, older...)
	positions = append(positions, current)
	positions = append(positions, newer...)
	return positions, olderMore || len(olderCandidates) > len(older), newerMore || len(newerCandidates) > len(newer), nil
}

// chooseInitialConversationPositions balances bounded preceding and following
// candidates, then uses any spare capacity so a sparse side does not waste a page.
func chooseInitialConversationPositions(olderCandidates, newerCandidates []conversationCursor, limit int) ([]conversationCursor, []conversationCursor) {
	capacity := limit - 1
	olderCount := minConversationCount(len(olderCandidates), capacity/2)
	newerCount := minConversationCount(len(newerCandidates), capacity-olderCount)
	olderCount += minConversationCount(len(olderCandidates)-olderCount, capacity-olderCount-newerCount)
	newerCount += minConversationCount(len(newerCandidates)-newerCount, capacity-olderCount-newerCount)
	olderStart := len(olderCandidates) - olderCount
	return olderCandidates[olderStart:], newerCandidates[:newerCount]
}

// minConversationCount returns the smaller non-negative page allocation.
func minConversationCount(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// conversationPageCursors emits only cursors for adjacent pages proven to
// exist. It uses the first and last rendered turns, or the selected SQL
// positions when concurrent deletion leaves a page with no surviving headers.
func conversationPageCursors(turns []TraceDetail, positions []conversationCursor, hasOlder, hasNewer bool) (string, string) {
	if len(turns) == 0 && len(positions) == 0 {
		return "", ""
	}
	var olderAnchor, newerAnchor conversationCursor
	if len(turns) > 0 {
		olderAnchor = conversationCursor{CreatedAt: turns[0].Trace.CreatedAt, TraceID: turns[0].Trace.TraceID}
		newerAnchor = conversationCursor{CreatedAt: turns[len(turns)-1].Trace.CreatedAt, TraceID: turns[len(turns)-1].Trace.TraceID}
	} else {
		olderAnchor = positions[0]
		newerAnchor = positions[len(positions)-1]
	}
	var olderCursor, newerCursor string
	if hasOlder {
		olderCursor, _ = encodeConversationCursor(olderAnchor)
	}
	if hasNewer {
		newerCursor, _ = encodeConversationCursor(newerAnchor)
	}
	return olderCursor, newerCursor
}

// listConversationTraceIDsBySessionSegment selects one chronological bounded
// session side without touching payload rows or materializing the full session.
func (r *PostgresRepository) listConversationTraceIDsBySessionSegment(ctx context.Context, sessionID string, cursor conversationCursor, direction string, limit int) ([]conversationCursor, bool, error) {
	comparison, order := conversationPageSQLDirection(direction)
	if comparison == "" {
		return nil, false, fmt.Errorf("model trace conversation direction is invalid")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT trace_id, created_at
		FROM model_call_traces
		WHERE session_id=$1 AND (created_at, trace_id) `+comparison+` ($2, $3)
		ORDER BY created_at `+order+`, trace_id `+order+`
		LIMIT $4
	`, sessionID, cursor.CreatedAt, cursor.TraceID, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list model trace session page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanConversationPositions(rows, limit, direction == "older")
}

// listConversationTraceIDsByLineageSegment recursively walks only one bounded
// chronological side of exact response lineage while preventing cyclic links.
func (r *PostgresRepository) listConversationTraceIDsByLineageSegment(ctx context.Context, traceID string, cursor conversationCursor, direction string, limit int) ([]conversationCursor, bool, error) {
	comparison, order := conversationPageSQLDirection(direction)
	if comparison == "" {
		return nil, false, fmt.Errorf("model trace conversation direction is invalid")
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH RECURSIVE ancestors AS (
			SELECT t.id, t.response_id, t.previous_response_id, ARRAY[t.id] AS path
			FROM model_call_traces t
			WHERE t.trace_id=$1
			UNION ALL
			SELECT parent.id, parent.response_id, parent.previous_response_id, ancestors.path || parent.id
			FROM model_call_traces parent
			JOIN ancestors ON ancestors.previous_response_id <> '' AND parent.response_id=ancestors.previous_response_id
			WHERE NOT parent.id = ANY(ancestors.path)
		), roots AS (
			SELECT ancestor.id, ancestor.response_id, ARRAY[ancestor.id] AS path
			FROM ancestors ancestor
			WHERE ancestor.previous_response_id = ''
				OR NOT EXISTS (
					SELECT 1 FROM model_call_traces possible_parent
					WHERE possible_parent.response_id=ancestor.previous_response_id
				)
			ORDER BY ancestor.id ASC
			LIMIT 1
		), chain AS (
			SELECT root.id, root.response_id, root.path
			FROM roots root
			UNION ALL
			SELECT child.id, child.response_id, chain.path || child.id
			FROM model_call_traces child
			JOIN chain ON chain.response_id <> '' AND child.previous_response_id=chain.response_id
			WHERE NOT child.id = ANY(chain.path)
		)
		SELECT trace.trace_id, trace.created_at
		FROM chain
		JOIN model_call_traces trace ON trace.id=chain.id
		WHERE (trace.created_at, trace.trace_id) `+comparison+` ($2, $3)
		ORDER BY trace.created_at `+order+`, trace.trace_id `+order+`
		LIMIT $4
	`, traceID, cursor.CreatedAt, cursor.TraceID, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list model trace response lineage page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanConversationPositions(rows, limit, direction == "older")
}

// conversationPageSQLDirection returns only fixed SQL syntax for a validated
// direction, keeping client values out of the query text.
func conversationPageSQLDirection(direction string) (string, string) {
	if direction == "older" {
		return "<", "DESC"
	}
	if direction == "newer" {
		return ">", "ASC"
	}
	return "", ""
}

// scanConversationPositions converts one bounded SQL segment to chronological
// order and keeps the extra row only as the has-more signal.
func scanConversationPositions(rows *sql.Rows, limit int, reverse bool) ([]conversationCursor, bool, error) {
	positions := make([]conversationCursor, 0, limit)
	for rows.Next() {
		var position conversationCursor
		if err := rows.Scan(&position.TraceID, &position.CreatedAt); err != nil {
			return nil, false, fmt.Errorf("scan model trace conversation position: %w", err)
		}
		positions = append(positions, position)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate model trace conversation positions: %w", err)
	}
	hasMore := len(positions) > limit
	if hasMore {
		positions = positions[:limit]
	}
	if reverse {
		for left, right := 0, len(positions)-1; left < right; left, right = left+1, right-1 {
			positions[left], positions[right] = positions[right], positions[left]
		}
	}
	return positions, hasMore, nil
}

// loadConversationTurns batch-hydrates a bounded page in three fixed queries:
// headers, ordered attempts, then payload metadata; no query count scales with
// the number of turns and no ciphertext is selected.
func (r *PostgresRepository) loadConversationTurns(ctx context.Context, traceIDs []string) ([]TraceDetail, error) {
	if len(traceIDs) == 0 {
		return []TraceDetail{}, nil
	}
	rows, err := r.db.QueryContext(ctx, traceSummarySelect+` WHERE t.trace_id = ANY($1)`, pq.Array(traceIDs))
	if err != nil {
		return nil, fmt.Errorf("list model trace conversation headers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byTraceID := make(map[string]*TraceDetail, len(traceIDs))
	for rows.Next() {
		trace, scanErr := scanTraceSummary(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		byTraceID[trace.TraceID] = &TraceDetail{Trace: trace, Attempts: []TraceAttempt{}, Payloads: []TracePayload{}}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model trace conversation headers: %w", err)
	}
	attemptRows, err := r.db.QueryContext(ctx, `
		SELECT trace.trace_id, attempt.attempt_no, attempt.account_id, attempt.account_snapshot,
			attempt.upstream_route, attempt.upstream_model, attempt.outcome,
			attempt.status_code, attempt.error_stage, attempt.error_code,
			attempt.duration_ms, attempt.started_at, attempt.completed_at
		FROM model_call_trace_attempts attempt
		JOIN model_call_traces trace ON trace.id=attempt.model_call_trace_id
		WHERE trace.trace_id = ANY($1)
		ORDER BY trace.created_at ASC, trace.trace_id ASC, attempt.attempt_no ASC, attempt.id ASC
	`, pq.Array(traceIDs))
	if err != nil {
		return nil, fmt.Errorf("list model trace conversation attempts: %w", err)
	}
	defer func() { _ = attemptRows.Close() }()
	for attemptRows.Next() {
		var traceID string
		var attempt TraceAttempt
		var accountID sql.NullInt64
		var statusCode, durationMS sql.NullInt32
		var completedAt sql.NullTime
		if err := attemptRows.Scan(&traceID, &attempt.AttemptNo, &accountID, &attempt.AccountSnapshot,
			&attempt.UpstreamRoute, &attempt.UpstreamModel, &attempt.Outcome, &statusCode,
			&attempt.ErrorStage, &attempt.ErrorCode, &durationMS, &attempt.StartedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("scan model trace conversation attempt: %w", err)
		}
		attempt.AccountID = optionalInt64(accountID)
		attempt.StatusCode = optionalInt(statusCode)
		attempt.DurationMS = optionalInt(durationMS)
		attempt.CompletedAt = optionalTime(completedAt)
		if detail := byTraceID[traceID]; detail != nil {
			detail.Attempts = append(detail.Attempts, attempt)
		}
	}
	if err := attemptRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model trace conversation attempts: %w", err)
	}
	payloadRows, err := r.db.QueryContext(ctx, `
		SELECT trace.trace_id, p.kind, p.attempt_no, p.capture_status, p.mime_type,
			p.original_bytes, p.stored_bytes, p.sha256, p.storage_mode, p.created_at
		FROM model_call_payloads p
		JOIN model_call_traces trace ON trace.id=p.model_call_trace_id
		WHERE trace.trace_id = ANY($1)
		ORDER BY trace.created_at ASC, trace.trace_id ASC, p.created_at ASC, p.id ASC
	`, pq.Array(traceIDs))
	if err != nil {
		return nil, fmt.Errorf("list model trace conversation payloads: %w", err)
	}
	defer func() { _ = payloadRows.Close() }()
	for payloadRows.Next() {
		var traceID string
		var payload TracePayload
		if err := payloadRows.Scan(&traceID, &payload.Kind, &payload.AttemptNo, &payload.CaptureStatus,
			&payload.ContentType, &payload.OriginalBytes, &payload.StoredBytes, &payload.SHA256,
			&payload.StorageMode, &payload.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan model trace conversation payload: %w", err)
		}
		if detail := byTraceID[traceID]; detail != nil {
			detail.Payloads = append(detail.Payloads, payload)
		}
	}
	if err := payloadRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model trace conversation payloads: %w", err)
	}
	return orderedConversationTurns(traceIDs, byTraceID), nil
}

// orderedConversationTurns preserves the selected chronological order while
// skipping traces deleted between the bounded position query and hydration.
func orderedConversationTurns(traceIDs []string, byTraceID map[string]*TraceDetail) []TraceDetail {
	turns := make([]TraceDetail, 0, len(traceIDs))
	for _, traceID := range traceIDs {
		if detail := byTraceID[traceID]; detail != nil {
			turns = append(turns, *detail)
		}
	}
	return turns
}

// retainLoadedCurrentConversationTurn preserves a root detail already read by
// GetTrace when it belongs to this page but is deleted before batch hydration.
// Directional pages that do not select the root remain unchanged.
func retainLoadedCurrentConversationTurn(turns []TraceDetail, positions []conversationCursor, current TraceDetail) []TraceDetail {
	currentPosition := conversationCursor{CreatedAt: current.Trace.CreatedAt, TraceID: current.Trace.TraceID}
	selected := false
	for _, position := range positions {
		if position == currentPosition {
			selected = true
			break
		}
	}
	if !selected {
		return turns
	}
	for _, turn := range turns {
		if turn.Trace.TraceID == current.Trace.TraceID {
			return turns
		}
	}
	insertAt := len(turns)
	for index, turn := range turns {
		if turn.Trace.CreatedAt.After(current.Trace.CreatedAt) || (turn.Trace.CreatedAt.Equal(current.Trace.CreatedAt) && turn.Trace.TraceID > current.Trace.TraceID) {
			insertAt = index
			break
		}
	}
	retained := make([]TraceDetail, 0, len(turns)+1)
	retained = append(retained, turns[:insertAt]...)
	retained = append(retained, current)
	retained = append(retained, turns[insertAt:]...)
	return retained
}

// modelTraceWhere 将允许的筛选字段转换为参数化 SQL，调用方永不拼接客户端输入。
func modelTraceWhere(filter TraceFilter) (string, []any) {
	clauses := []string{"1=1"}
	arguments := make([]any, 0, 9)
	add := func(clause string, value any) {
		arguments = append(arguments, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(arguments)))
	}
	if filter.UserID != nil {
		add("t.user_id=$%d", *filter.UserID)
	}
	if filter.APIKeyID != nil {
		add("t.api_key_id=$%d", *filter.APIKeyID)
	}
	if filter.GroupID != nil {
		add("t.group_id=$%d", *filter.GroupID)
	}
	if filter.AccountID != nil {
		add("t.account_id=$%d", *filter.AccountID)
	}
	if value := strings.TrimSpace(filter.User); value != "" {
		add("t.user_snapshot ILIKE $%d", "%"+value+"%")
	}
	if value := strings.TrimSpace(filter.APIKey); value != "" {
		add("t.api_key_snapshot ILIKE $%d", "%"+value+"%")
	}
	if value := strings.TrimSpace(filter.TraceID); value != "" {
		add("t.trace_id=$%d", value)
	}
	if value := strings.TrimSpace(filter.RequestID); value != "" {
		add("t.request_id=$%d", value)
	}
	if value := strings.TrimSpace(filter.SessionID); value != "" {
		add("t.session_id=$%d", value)
	}
	if value := strings.TrimSpace(filter.Route); value != "" {
		add("t.route=$%d", value)
	}
	if value := strings.TrimSpace(filter.RequestedModel); value != "" {
		add("t.requested_model ILIKE $%d", "%"+value+"%")
	}
	if value := strings.TrimSpace(filter.UpstreamModel); value != "" {
		add("t.upstream_model ILIKE $%d", "%"+value+"%")
	}
	if value := strings.TrimSpace(filter.Protocol); value != "" {
		add("t.protocol=$%d", value)
	}
	if filter.Outcome != "" {
		add("t.outcome=$%d", string(filter.Outcome))
	}
	if filter.CaptureStatus != "" {
		arguments = append(arguments, string(filter.CaptureStatus))
		placeholder := len(arguments)
		clauses = append(clauses, fmt.Sprintf("(t.request_capture_status=$%d OR t.response_capture_status=$%d)", placeholder, placeholder))
	}
	if filter.StartAt != nil {
		add("t.created_at >= $%d", filter.StartAt.UTC())
	}
	if filter.EndAt != nil {
		add("t.created_at < $%d", filter.EndAt.UTC())
	}
	return strings.Join(clauses, " AND "), arguments
}

// optionalInt64 将 SQL NULL 映射为 API 层的空指针。
func optionalInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

// optionalInt 将 SQL NULL 映射为 API 层的空指针。
func optionalInt(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int32)
	return &converted
}

// optionalTime 将 SQL NULL 映射为 API 层的空指针。
func optionalTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
