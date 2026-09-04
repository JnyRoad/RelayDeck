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
	return r.updatePayloadCaptureStatus(ctx, record)
}

// CreateChunkedPayload creates a fail-closed metadata row before any encrypted
// chunk is appended. The returned database ID remains internal to the storage
// adapter and is never exposed to gateway callers.
func (r *PostgresRepository) CreateChunkedPayload(ctx context.Context, record PayloadRecord) (int64, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("model trace database is unavailable")
	}
	var payloadID int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO model_call_payloads (
			model_call_trace_id, kind, attempt_no, capture_status, mime_type, original_bytes,
			stored_bytes, sha256, redaction_version, storage_mode, ciphertext, created_at
		)
		SELECT id, $2, $3, $4, $5, $6, $7, $8, $9, 'chunked', '', $10
		FROM model_call_traces
		WHERE trace_id=$1
		RETURNING id
	`, record.TraceID, string(record.Kind), record.AttemptNo, string(record.CaptureStatus), record.ContentType,
		record.OriginalBytes, record.StoredBytes, record.SHA256, record.RedactionVer, record.CreatedAt).Scan(&payloadID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("model trace %q not found", record.TraceID)
		}
		return 0, fmt.Errorf("insert chunked model trace payload: %w", err)
	}
	if err := r.updatePayloadCaptureStatus(ctx, record); err != nil {
		return 0, err
	}
	return payloadID, nil
}

// AppendPayloadChunk persists one already-encrypted plaintext segment in its
// immutable ordinal sequence. Callers must not retry a different value at the
// same chunk number because the table enforces unique ordering.
func (r *PostgresRepository) AppendPayloadChunk(ctx context.Context, payloadID int64, chunkNo int, ciphertext string, storedBytes int64) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("model trace database is unavailable")
	}
	if payloadID < 1 || chunkNo < 0 || storedBytes < 0 {
		return fmt.Errorf("model trace payload chunk is invalid")
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO model_call_payload_chunks (model_call_payload_id, chunk_no, stored_bytes, ciphertext)
		VALUES ($1, $2, $3, $4)
	`, payloadID, chunkNo, storedBytes, ciphertext); err != nil {
		return fmt.Errorf("insert model trace payload chunk: %w", err)
	}
	return nil
}

// FinishChunkedPayload marks aggregate metadata readable after its chunks exist,
// then updates the root summary status without reading any body.
func (r *PostgresRepository) FinishChunkedPayload(ctx context.Context, payloadID int64, record PayloadRecord) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("model trace database is unavailable")
	}
	if payloadID < 1 {
		return fmt.Errorf("model trace payload ID is invalid")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE model_call_payloads
		SET kind=$2,
			capture_status=$3,
			mime_type=$4,
			original_bytes=$5,
			stored_bytes=$6,
			sha256=$7,
			redaction_version=$8,
			storage_mode='chunked'
		WHERE id=$1
	`, payloadID, string(record.Kind), string(record.CaptureStatus), record.ContentType,
		record.OriginalBytes, record.StoredBytes, record.SHA256, record.RedactionVer)
	if err != nil {
		return fmt.Errorf("finish chunked model trace payload: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read chunked model trace payload update result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("model trace payload %d not found", payloadID)
	}
	return r.updatePayloadCaptureStatus(ctx, record)
}

// updatePayloadCaptureStatus keeps the list-visible root capture fields in
// sync with one persisted payload without ever loading its ciphertext.
func (r *PostgresRepository) updatePayloadCaptureStatus(ctx context.Context, record PayloadRecord) error {
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
			upstream_model=CASE WHEN $15 <> '' THEN $15 ELSE upstream_model END,
			user_snapshot=CASE WHEN $16 <> '' THEN $16 ELSE user_snapshot END,
			api_key_snapshot=CASE WHEN $17 <> '' THEN $17 ELSE api_key_snapshot END,
			group_snapshot=CASE WHEN $18 <> '' THEN $18 ELSE group_snapshot END,
			account_snapshot=CASE WHEN $19 <> '' THEN $19 ELSE account_snapshot END,
			session_id=CASE WHEN $20 <> '' THEN $20 ELSE session_id END,
			previous_response_id=CASE WHEN $21 <> '' THEN $21 ELSE previous_response_id END,
			response_id=CASE WHEN $22 <> '' THEN $22 ELSE response_id END
		WHERE trace_id=$1
	`, record.TraceID, string(record.Outcome), record.StatusCode, record.Stream, record.DurationMS,
		firstByteMS, record.RequestBytes, record.ResponseBytes, time.Now().UTC(), record.UserID,
		record.APIKeyID, record.GroupID, record.AccountID, record.RequestedModel, record.UpstreamModel,
		record.UserSnapshot, record.APIKeySnapshot, record.GroupSnapshot, record.AccountSnapshot,
		record.SessionID, record.PreviousResponseID, record.ResponseID)
	if err != nil {
		return fmt.Errorf("finish model trace: %w", err)
	}
	return nil
}

// CreateAttempt inserts one occurrence at the shared upstream transport
// boundary. It resolves the root ID in SQL so callers never handle database
// primary keys or encrypted payload values.
func (r *PostgresRepository) CreateAttempt(ctx context.Context, record TraceAttemptRecord) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("model trace database is unavailable")
	}
	if record.AttemptNo < 1 {
		return fmt.Errorf("model trace upstream attempt number is invalid")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO model_call_trace_attempts (
			model_call_trace_id, attempt_no, account_id, account_snapshot,
			upstream_route, upstream_model, started_at
		)
		SELECT id, $2, $3, $4, $5, $6, $7
		FROM model_call_traces
		WHERE trace_id=$1
	`, record.TraceID, record.AttemptNo, record.AccountID, record.AccountSnapshot,
		record.UpstreamRoute, record.UpstreamModel, record.StartedAt)
	if err != nil {
		return fmt.Errorf("insert model trace upstream attempt: %w", err)
	}
	return nil
}

// FinishAttempt records a shared transport result after its response body has
// been consumed or closed. It never touches payload ciphertext.
func (r *PostgresRepository) FinishAttempt(ctx context.Context, record TraceAttemptFinishRecord) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("model trace database is unavailable")
	}
	if record.AttemptNo < 1 {
		return fmt.Errorf("model trace upstream attempt number is invalid")
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE model_call_trace_attempts attempt
		SET outcome=$3,
			status_code=NULLIF($4, 0),
			error_stage=$5,
			error_code=$6,
			duration_ms=$7,
			completed_at=$8
		FROM model_call_traces trace
		WHERE trace.id=attempt.model_call_trace_id
			AND trace.trace_id=$1
			AND attempt.attempt_no=$2
	`, record.TraceID, record.AttemptNo, string(record.Outcome), record.StatusCode,
		record.ErrorStage, record.ErrorCode, record.DurationMS, record.CompletedAt)
	if err != nil {
		return fmt.Errorf("finish model trace upstream attempt: %w", err)
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
