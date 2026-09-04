package modeltrace

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestPostgresRepositoryPersistsHeadersAndPayloadMetadata verifies that the
// database adapter keeps summary fields separate from encrypted payload content.
func TestPostgresRepositoryPersistsHeadersAndPayloadMetadata(t *testing.T) {
	db := openModelTraceIntegrationDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `TRUNCATE TABLE model_call_trace_cleanup_runs, model_call_payloads, model_call_traces RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
	repository := NewPostgresRepository(db)
	createdAt := time.Now().UTC().Truncate(time.Microsecond)

	err = repository.CreateTrace(ctx, TraceRecord{
		TraceID:   "trace-repository-canary",
		RequestID: "request-repository-canary",
		Route:     "/v1/chat/completions",
		Protocol:  "sync",
		ExpiresAt: createdAt.AddDate(0, 0, 7),
		CreatedAt: createdAt,
	})
	require.NoError(t, err)
	err = repository.CreatePayload(ctx, PayloadRecord{
		TraceID:       "trace-repository-canary",
		Kind:          PayloadKindClientRequest,
		CaptureStatus: CaptureStatusRedacted,
		ContentType:   "application/json",
		OriginalBytes: 42,
		StoredBytes:   24,
		SHA256:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RedactionVer:  1,
		Ciphertext:    "ciphertext-canary",
		CreatedAt:     createdAt,
	})
	require.NoError(t, err)
	err = repository.FinishTrace(ctx, TraceFinishRecord{
		TraceID: "trace-repository-canary",
		FinishInput: FinishInput{
			Outcome:       OutcomeSucceeded,
			StatusCode:    200,
			DurationMS:    12,
			RequestBytes:  42,
			ResponseBytes: 24,
		},
	})
	require.NoError(t, err)

	var requestStatus, outcome, ciphertext string
	var statusCode, requestBytes, responseBytes int
	err = db.QueryRowContext(ctx, `
		SELECT t.request_capture_status, t.outcome, t.status_code, t.request_bytes, t.response_bytes, p.ciphertext
		FROM model_call_traces t
		JOIN model_call_payloads p ON p.model_call_trace_id=t.id
		WHERE t.trace_id=$1
	`, "trace-repository-canary").Scan(&requestStatus, &outcome, &statusCode, &requestBytes, &responseBytes, &ciphertext)
	require.NoError(t, err)
	require.Equal(t, string(CaptureStatusRedacted), requestStatus)
	require.Equal(t, string(OutcomeSucceeded), outcome)
	require.Equal(t, 200, statusCode)
	require.Equal(t, 42, requestBytes)
	require.Equal(t, 24, responseBytes)
	require.Equal(t, "ciphertext-canary", ciphertext)
}
