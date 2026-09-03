package modeltrace

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const modelTracePostgresTestEnv = "MODEL_TRACE_TEST_POSTGRES_DSN"

// openModelTraceIntegrationDB opens an explicitly configured isolated PostgreSQL
// database, applies the immutable migration twice, and never falls back to a
// developer or production database when the test DSN is absent.
func openModelTraceIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "183_model_call_traces.sql"))
	require.NoError(t, err)

	dsn := strings.TrimSpace(os.Getenv(modelTracePostgresTestEnv))
	if dsn == "" {
		t.Skip(modelTracePostgresTestEnv + " is not set")
	}

	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(ctx))
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (id BIGSERIAL PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS api_keys (id BIGSERIAL PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS groups (id BIGSERIAL PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS accounts (id BIGSERIAL PRIMARY KEY);
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// TestModelTraceMigrationCascadesPayloads verifies that deleting a retained
// trace removes all encrypted payload rows without touching unrelated tables.
func TestModelTraceMigrationCascadesPayloads(t *testing.T) {
	db := openModelTraceIntegrationDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `TRUNCATE TABLE model_call_trace_cleanup_runs, model_call_payloads, model_call_traces RESTART IDENTITY CASCADE`)
	require.NoError(t, err)

	var traceID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO model_call_traces (trace_id, route, protocol, outcome, expires_at)
		VALUES ('trace-migration-canary', '/v1/chat/completions', 'sync', 'succeeded', NOW() + INTERVAL '7 days')
		RETURNING id
	`).Scan(&traceID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO model_call_payloads (model_call_trace_id, kind, attempt_no, capture_status, ciphertext)
		VALUES ($1, 'client_request', 0, 'redacted', 'encrypted-canary')
	`, traceID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `DELETE FROM model_call_traces WHERE id=$1`, traceID)
	require.NoError(t, err)
	var payloadCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_call_payloads WHERE model_call_trace_id=$1`, traceID).Scan(&payloadCount))
	require.Zero(t, payloadCount)
}

// TestPostgresRepositoryListsAndReadsDetails 验证索引列表不会丢失调用摘要，详情只按 trace ID 读取关联密文。
func TestPostgresRepositoryListsAndReadsDetails(t *testing.T) {
	db := openModelTraceIntegrationDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `TRUNCATE TABLE model_call_trace_cleanup_runs, model_call_payloads, model_call_traces RESTART IDENTITY CASCADE`)
	require.NoError(t, err)

	repository := NewPostgresRepository(db)
	createdAt := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
	require.NoError(t, repository.CreateTrace(ctx, TraceRecord{
		TraceID: "trace-query-canary", RequestID: "creq-query", Route: "/v1/chat/completions", Protocol: "sync",
		ExpiresAt: createdAt.AddDate(0, 0, 7), CreatedAt: createdAt,
	}))
	require.NoError(t, repository.CreatePayload(ctx, PayloadRecord{
		TraceID: "trace-query-canary", Kind: PayloadKindClientRequest, AttemptNo: 0, CaptureStatus: CaptureStatusRedacted,
		ContentType: "application/json", OriginalBytes: 20, StoredBytes: 18, SHA256: strings.Repeat("a", 64),
		RedactionVer: 1, Ciphertext: "encrypted-query-canary", Model: "gpt-query", CreatedAt: createdAt,
	}))
	require.NoError(t, repository.FinishTrace(ctx, TraceFinishRecord{TraceID: "trace-query-canary", FinishInput: FinishInput{
		Outcome: OutcomeSucceeded, StatusCode: 200, DurationMS: 12, RequestBytes: 20, ResponseBytes: 10,
	}}))

	items, total, err := repository.ListTraces(ctx, TraceFilter{RequestID: "creq-query"}, 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, "trace-query-canary", items[0].TraceID)
	require.Equal(t, "gpt-query", items[0].RequestedModel)

	detail, err := repository.GetTrace(ctx, "trace-query-canary")
	require.NoError(t, err)
	require.Equal(t, "trace-query-canary", detail.Trace.TraceID)
	require.Len(t, detail.Payloads, 1)
	require.Equal(t, "encrypted-query-canary", detail.Payloads[0].Ciphertext)
}

// TestPostgresRepositoryPreviewsAndDeletesExpired 验证清理只命中 expires_at 已过期的调用，并汇总级联正文统计。
func TestPostgresRepositoryPreviewsAndDeletesExpired(t *testing.T) {
	db := openModelTraceIntegrationDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `TRUNCATE TABLE model_call_trace_cleanup_runs, model_call_payloads, model_call_traces RESTART IDENTITY CASCADE`)
	require.NoError(t, err)

	repository := NewPostgresRepository(db)
	now := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
	for _, record := range []TraceRecord{
		{TraceID: "trace-expired", Route: "/v1/messages", Protocol: "sync", CreatedAt: now.AddDate(0, 0, -8), ExpiresAt: now.Add(-time.Hour)},
		{TraceID: "trace-active", Route: "/v1/messages", Protocol: "sync", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
	} {
		require.NoError(t, repository.CreateTrace(ctx, record))
	}
	require.NoError(t, repository.CreatePayload(ctx, PayloadRecord{
		TraceID: "trace-expired", Kind: PayloadKindClientRequest, AttemptNo: 0, CaptureStatus: CaptureStatusRedacted,
		StoredBytes: 128, SHA256: strings.Repeat("b", 64), RedactionVer: 1, Ciphertext: "encrypted-expired", CreatedAt: now,
	}))

	preview, err := repository.PreviewExpired(ctx, now)
	require.NoError(t, err)
	require.Equal(t, int64(1), preview.ExpiredTraces)
	require.Equal(t, int64(1), preview.ExpiredPayloads)
	require.Equal(t, int64(128), preview.StoredBytes)

	deleted, err := repository.DeleteExpired(ctx, now, 50)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted.DeletedTraces)
	require.Equal(t, int64(1), deleted.DeletedPayloads)
	require.Equal(t, int64(128), deleted.DeletedBytes)
	items, total, err := repository.ListTraces(ctx, TraceFilter{}, 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, "trace-active", items[0].TraceID)
}
