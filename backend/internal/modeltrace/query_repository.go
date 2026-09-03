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
	defer rows.Close()
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
	defer rows.Close()
	detail := TraceDetail{Trace: trace, Payloads: make([]TracePayload, 0)}
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
	if value := strings.TrimSpace(filter.TraceID); value != "" {
		add("t.trace_id=$%d", value)
	}
	if value := strings.TrimSpace(filter.RequestID); value != "" {
		add("t.request_id=$%d", value)
	}
	if value := strings.TrimSpace(filter.Route); value != "" {
		add("t.route=$%d", value)
	}
	if value := strings.TrimSpace(filter.RequestedModel); value != "" {
		add("t.requested_model ILIKE $%d", "%"+value+"%")
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
