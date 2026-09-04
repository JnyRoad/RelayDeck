package modeltrace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/JnyRoad/RelayDeck/internal/pkg/errors"
)

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
			p.stored_bytes, p.sha256, p.created_at
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
			&payload.OriginalBytes, &payload.StoredBytes, &payload.SHA256, &payload.CreatedAt); err != nil {
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
			p.stored_bytes, p.sha256, p.ciphertext, p.created_at
		FROM model_call_payloads p
		JOIN model_call_traces t ON t.id=p.model_call_trace_id
		WHERE t.trace_id=$1 AND p.kind=$2 AND p.attempt_no=$3
	`, strings.TrimSpace(traceID), string(kind), attemptNo).Scan(
		&payload.Kind, &payload.AttemptNo, &payload.CaptureStatus, &payload.ContentType,
		&payload.OriginalBytes, &payload.StoredBytes, &payload.SHA256, &payload.Ciphertext, &payload.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TracePayload{}, infraerrors.NotFound("MODEL_TRACE_PAYLOAD_NOT_FOUND", "model trace payload not found")
	}
	if err != nil {
		return TracePayload{}, fmt.Errorf("get model trace payload: %w", err)
	}
	return payload, nil
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

// GetConversation loads the current trace plus only explicitly related turns.
// It resolves a stable session first, then falls back to exact response lineage;
// no user, Key, model, IP, or time fields take part in the query.
func (r *PostgresRepository) GetConversation(ctx context.Context, traceID string) (TraceConversation, error) {
	current, err := r.GetTrace(ctx, traceID)
	if err != nil {
		return TraceConversation{}, err
	}
	conversation := TraceConversation{
		CurrentTraceID: current.Trace.TraceID,
		Turns:          []TraceDetail{current},
	}
	if current.Trace.SessionID != "" {
		traceIDs, listErr := r.listConversationTraceIDsBySession(ctx, current.Trace.SessionID)
		if listErr != nil {
			return TraceConversation{}, listErr
		}
		turns, loadErr := r.loadConversationTurns(ctx, traceIDs)
		if loadErr != nil {
			return TraceConversation{}, loadErr
		}
		conversation.Linked = true
		conversation.LinkSource = "session_id"
		conversation.Turns = turns
		return conversation, nil
	}
	if current.Trace.PreviousResponseID == "" && current.Trace.ResponseID == "" {
		return conversation, nil
	}
	traceIDs, listErr := r.listConversationTraceIDsByLineage(ctx, current.Trace.TraceID)
	if listErr != nil {
		return TraceConversation{}, listErr
	}
	turns, loadErr := r.loadConversationTurns(ctx, traceIDs)
	if loadErr != nil {
		return TraceConversation{}, loadErr
	}
	conversation.Linked = true
	conversation.LinkSource = "response_lineage"
	conversation.Turns = turns
	return conversation, nil
}

// listConversationTraceIDsBySession selects calls sharing exactly one explicit
// stable session identifier in chronological order without touching payload rows.
func (r *PostgresRepository) listConversationTraceIDsBySession(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT trace_id
		FROM model_call_traces
		WHERE session_id=$1
		ORDER BY created_at ASC, id ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list model trace session: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTraceIDRows(rows)
}

// listConversationTraceIDsByLineage recursively walks exact parent-response
// links while preventing malformed cyclic identifiers from revisiting a trace.
func (r *PostgresRepository) listConversationTraceIDsByLineage(ctx context.Context, traceID string) ([]string, error) {
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
		SELECT trace.trace_id
		FROM chain
		JOIN model_call_traces trace ON trace.id=chain.id
		ORDER BY trace.created_at ASC, trace.id ASC
	`, traceID)
	if err != nil {
		return nil, fmt.Errorf("list model trace response lineage: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTraceIDRows(rows)
}

// scanTraceIDRows converts ordered trace ID rows into a closed result slice.
func scanTraceIDRows(rows *sql.Rows) ([]string, error) {
	traceIDs := make([]string, 0)
	for rows.Next() {
		var traceID string
		if err := rows.Scan(&traceID); err != nil {
			return nil, fmt.Errorf("scan model trace id: %w", err)
		}
		traceIDs = append(traceIDs, traceID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model trace ids: %w", err)
	}
	return traceIDs, nil
}

// loadConversationTurns reads each selected trace header and payload metadata
// in chronological order. GetTrace never reads payload ciphertext.
func (r *PostgresRepository) loadConversationTurns(ctx context.Context, traceIDs []string) ([]TraceDetail, error) {
	turns := make([]TraceDetail, 0, len(traceIDs))
	for _, traceID := range traceIDs {
		turn, err := r.GetTrace(ctx, traceID)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	return turns, nil
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
