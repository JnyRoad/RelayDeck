-- Online indexes for established model_call_traces rows.  The _notx suffix is
-- required by the migration runner because CONCURRENTLY cannot run in a SQL
-- transaction.  Each statement remains independently idempotent.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_model_call_traces_session_created
    ON model_call_traces(session_id, created_at ASC, id ASC)
    WHERE session_id <> '';
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_model_call_traces_response_id
    ON model_call_traces(response_id)
    WHERE response_id <> '';
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_model_call_traces_previous_response_id
    ON model_call_traces(previous_response_id)
    WHERE previous_response_id <> '';
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_model_call_traces_user_snapshot_created
    ON model_call_traces(user_snapshot, created_at DESC, id DESC)
    WHERE user_snapshot <> '';
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_model_call_traces_api_key_snapshot_created
    ON model_call_traces(api_key_snapshot, created_at DESC, id DESC)
    WHERE api_key_snapshot <> '';
