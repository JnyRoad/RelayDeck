package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelCallTraceMigrationsBuildExistingTraceIndexesOnline(t *testing.T) {
	schema, err := FS.ReadFile("232_model_call_trace_sessions_and_attempts.sql")
	require.NoError(t, err)
	schemaSQL := string(schema)
	require.Regexp(t, `(?s)ADD CONSTRAINT chk_model_call_trace_cleanup_runs_nonnegative\s+CHECK \(deleted_traces >= 0 AND deleted_attempts >= 0 AND deleted_payloads >= 0 AND deleted_bytes >= 0\) NOT VALID;`, schemaSQL)
	require.Regexp(t, `(?s)ADD CONSTRAINT chk_model_call_payloads_kind\s+CHECK \(kind IN \(.*?\)\) NOT VALID;`, schemaSQL)
	require.NotContains(t, schemaSQL, "CREATE INDEX IF NOT EXISTS idx_model_call_traces_")

	indexes, err := FS.ReadFile("233_add_model_call_trace_indexes_notx.sql")
	require.NoError(t, err)
	indexSQL := strings.Join(strings.Fields(string(indexes)), " ")
	for _, name := range []string{
		"idx_model_call_traces_session_created",
		"idx_model_call_traces_response_id",
		"idx_model_call_traces_previous_response_id",
		"idx_model_call_traces_user_snapshot_created",
		"idx_model_call_traces_api_key_snapshot_created",
	} {
		require.Contains(t, indexSQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS "+name)
	}

	validation, err := FS.ReadFile("234_validate_model_call_trace_constraints.sql")
	require.NoError(t, err)
	require.Contains(t, string(validation), "VALIDATE CONSTRAINT chk_model_call_trace_cleanup_runs_nonnegative")
	require.Contains(t, string(validation), "VALIDATE CONSTRAINT chk_model_call_payloads_kind")
}
