//go:build integration

package repository

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/JnyRoad/RelayDeck/migrations"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Reconstruct the pre-integration schema with the unchanged RelayDeck SQL files,
// including its 232-235 migrations, then apply the lower-numbered upstream files.
func TestUpstreamIntegration_UpgradePreservesRelayDeckData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pg, err := tcpostgres.Run(ctx, selectDockerImage(ctx, postgresImageTag),
		tcpostgres.WithDatabase("relaydeck_upgrade_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		require.NoError(t, pg.Terminate(cleanupCtx))
	})
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	added := map[string]bool{
		"232_add_usage_log_upstream_request_id.sql":            true,
		"232_channel_cache_write_1h_pricing.sql":               true,
		"232_group_force_openai_fast.sql":                      true,
		"232_group_reasoning_effort_over_limit.sql":            true,
		"233_add_usage_log_upstream_request_id_index_notx.sql": true,
		"233_group_free_openai_fast.sql":                       true,
		"234_group_codex_models_manifest_config.sql":           true,
	}
	files, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	baseline := fstest.MapFS{}
	for _, name := range files {
		if added[name] {
			continue
		}
		content, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		baseline[name] = &fstest.MapFile{Data: content}
	}
	require.Len(t, baseline, len(files)-len(added))
	require.NoError(t, applyMigrationsFS(ctx, db, baseline))

	var userID, groupID, keyID, accountID, traceID, usageID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users (email, password_hash)
VALUES ('upgrade@example.com', 'test-only-hash') RETURNING id`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups (name, platform)
VALUES ('upgrade-openai', 'openai') RETURNING id`).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO api_keys (user_id, group_id, key, name, idempotency_record_id)
VALUES ($1, $2, 'sk-upgrade-fixture', 'upgrade-key', 42) RETURNING id`, userID, groupID).Scan(&keyID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts (name, platform, type)
VALUES ('upgrade-account', 'openai', 'apikey') RETURNING id`).Scan(&accountID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO usage_logs (user_id, api_key_id, account_id, model)
VALUES ($1, $2, $3, 'gpt-5') RETURNING id`, userID, keyID, accountID).Scan(&usageID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO model_call_traces (trace_id, user_id, api_key_id, session_id, response_id)
VALUES ('upgrade-trace', $1, $2, 'upgrade-session', 'resp-upgrade') RETURNING id`, userID, keyID).Scan(&traceID))
	_, err = db.ExecContext(ctx, `INSERT INTO model_call_trace_attempts (model_call_trace_id, attempt_no, account_id)
VALUES ($1, 1, $2)`, traceID, accountID)
	require.NoError(t, err)

	require.NoError(t, ApplyMigrations(ctx, db))
	require.NoError(t, ApplyMigrations(ctx, db), "upgrading twice must remain idempotent")
	var applied int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&applied))
	require.Equal(t, len(files), applied, "same-numbered local and upstream files must all execute")

	var forceFast, freeFast bool
	var overLimit, manifest string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT force_openai_fast, free_openai_fast,
max_reasoning_effort_over_limit, codex_models_manifest_config::text FROM groups WHERE id = $1`, groupID).
		Scan(&forceFast, &freeFast, &overLimit, &manifest))
	require.False(t, forceFast)
	require.False(t, freeFast)
	require.Equal(t, "downgrade", overLimit)
	require.JSONEq(t, "{}", manifest)
	var claimID int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT idempotency_record_id FROM api_keys WHERE id = $1", keyID).Scan(&claimID))
	require.Equal(t, int64(42), claimID)
	var sessionID, responseID string
	var attempts int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT session_id, response_id,
(SELECT COUNT(*) FROM model_call_trace_attempts WHERE model_call_trace_id = $1)
FROM model_call_traces WHERE id = $1`, traceID).Scan(&sessionID, &responseID, &attempts))
	require.Equal(t, "upgrade-session", sessionID)
	require.Equal(t, "resp-upgrade", responseID)
	require.Equal(t, 1, attempts)
	var upstreamID sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, "SELECT upstream_request_id FROM usage_logs WHERE id = $1", usageID).Scan(&upstreamID))
	require.False(t, upstreamID.Valid, "historical usage rows must not acquire fabricated upstream IDs")
	for _, name := range []string{usageLogsUpstreamRequestIDIndex, "api_keys_idempotency_record_id_key", "idx_model_call_traces_session_created"} {
		var usable bool
		require.NoError(t, db.QueryRowContext(ctx, `SELECT indisvalid AND indisready FROM pg_index WHERE indexrelid = to_regclass($1)`, name).Scan(&usable))
		require.True(t, usable, "index %s", name)
	}
}
