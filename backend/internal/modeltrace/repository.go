package modeltrace

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PostgresRepository persists model trace headers and encrypted payloads using
// parameterized SQL so list queries can remain independent from payload rows.
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository constructs the concrete storage adapter for the
// dedicated trace tables. A nil database is reported by each operation.
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// CreateTrace inserts the lightweight call header before a model handler runs.
func (r *PostgresRepository) CreateTrace(ctx context.Context, record TraceRecord) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("model trace database is unavailable")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO model_call_traces (
			trace_id, request_id, route, protocol, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, record.TraceID, record.RequestID, record.Route, record.Protocol, record.ExpiresAt, record.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert model trace: %w", err)
	}
	return nil
}

// CreatePayload stores one prepared payload and updates only the corresponding
// request or response capture status on its owning lightweight trace header.
func (r *PostgresRepository) CreatePayload(ctx context.Context, record PayloadRecord) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("model trace database is unavailable")
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO model_call_payloads (
			model_call_trace_id, kind, attempt_no, capture_status, mime_type, original_bytes,
			stored_bytes, sha256, redaction_version, ciphertext, created_at
		)
		SELECT id, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		FROM model_call_traces
		WHERE trace_id=$1
	`, record.TraceID, string(record.Kind), record.AttemptNo, string(record.CaptureStatus), record.ContentType,
		record.OriginalBytes, record.StoredBytes, record.SHA256, record.RedactionVer, record.Ciphertext, record.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert model trace payload: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read model trace payload insert result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("model trace %q not found", record.TraceID)
	}
	if column := captureStatusColumn(record.Kind); column != "" {
		updates := []string{column + "=$2"}
		arguments := []any{record.TraceID, string(record.CaptureStatus)}
		if modelColumn := payloadModelColumn(record.Kind); modelColumn != "" && record.Model != "" {
			updates = append(updates, modelColumn+"=$3")
			arguments = append(arguments, record.Model)
		}
		query := `UPDATE model_call_traces SET ` + strings.Join(updates, ", ") + ` WHERE trace_id=$1`
		if _, err := r.db.ExecContext(ctx, query, arguments...); err != nil {
			return fmt.Errorf("update model trace capture status: %w", err)
		}
	}
	return nil
}

// payloadModelColumn maps client payloads to their list-visible model summary column.
func payloadModelColumn(kind PayloadKind) string {
	switch kind {
	case PayloadKindClientRequest:
		return "requested_model"
	case PayloadKindClientResponse:
		return "response_model"
	default:
		return ""
	}
}

// FinishTrace updates only terminal and timing metadata after a model handler
// returns; it never reads or rewrites any encrypted payload content.
func (r *PostgresRepository) FinishTrace(ctx context.Context, record TraceFinishRecord) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("model trace database is unavailable")
	}
	var firstByteMS any
	if record.FirstByteMS != nil {
		firstByteMS = *record.FirstByteMS
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE model_call_traces
		SET outcome=$2,
			status_code=NULLIF($3, 0),
			stream=$4,
			duration_ms=$5,
			first_byte_ms=$6,
			request_bytes=$7,
			response_bytes=$8,
			completed_at=$9,
			user_id=COALESCE($10, user_id),
			api_key_id=COALESCE($11, api_key_id),
			group_id=COALESCE($12, group_id),
			account_id=COALESCE($13, account_id),
			requested_model=CASE WHEN $14 <> '' THEN $14 ELSE requested_model END,
			upstream_model=CASE WHEN $15 <> '' THEN $15 ELSE upstream_model END
		WHERE trace_id=$1
	`, record.TraceID, string(record.Outcome), record.StatusCode, record.Stream, record.DurationMS,
		firstByteMS, record.RequestBytes, record.ResponseBytes, time.Now().UTC(), record.UserID,
		record.APIKeyID, record.GroupID, record.AccountID, record.RequestedModel, record.UpstreamModel)
	if err != nil {
		return fmt.Errorf("finish model trace: %w", err)
	}
	return nil
}

// captureStatusColumn maps each client-visible payload kind to the one header
// status column that list queries expose without joining encrypted payload rows.
func captureStatusColumn(kind PayloadKind) string {
	switch kind {
	case PayloadKindClientRequest:
		return "request_capture_status"
	case PayloadKindClientResponse, PayloadKindErrorResponse:
		return "response_capture_status"
	default:
		return ""
	}
}
