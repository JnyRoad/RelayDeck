-- Extend model-call tracing without modifying deployed migration 183. Historical
-- roots stay readable while new roots gain immutable attribution snapshots,
-- explicit conversation links and per-dispatch upstream attempt metadata.

ALTER TABLE model_call_traces
    ADD COLUMN IF NOT EXISTS user_snapshot VARCHAR(320) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS api_key_snapshot VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS group_snapshot VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS account_snapshot VARCHAR(320) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS session_id VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS previous_response_id VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS response_id VARCHAR(255) NOT NULL DEFAULT '';

ALTER TABLE model_call_trace_cleanup_runs
    ADD COLUMN IF NOT EXISTS deleted_attempts BIGINT NOT NULL DEFAULT 0;
ALTER TABLE model_call_trace_cleanup_runs
    DROP CONSTRAINT IF EXISTS chk_model_call_trace_cleanup_runs_nonnegative;
ALTER TABLE model_call_trace_cleanup_runs
    ADD CONSTRAINT chk_model_call_trace_cleanup_runs_nonnegative
        CHECK (deleted_traces >= 0 AND deleted_attempts >= 0 AND deleted_payloads >= 0 AND deleted_bytes >= 0);

CREATE TABLE IF NOT EXISTS model_call_trace_attempts (
    id                  BIGSERIAL PRIMARY KEY,
    model_call_trace_id BIGINT NOT NULL REFERENCES model_call_traces(id) ON DELETE CASCADE,
    attempt_no          INTEGER NOT NULL,
    account_id          BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    account_snapshot    VARCHAR(320) NOT NULL DEFAULT '',
    upstream_route      VARCHAR(512) NOT NULL DEFAULT '',
    upstream_model      VARCHAR(200) NOT NULL DEFAULT '',
    outcome             VARCHAR(32) NOT NULL DEFAULT 'failed',
    status_code         INTEGER,
    error_stage         VARCHAR(64) NOT NULL DEFAULT '',
    error_code          VARCHAR(64) NOT NULL DEFAULT '',
    duration_ms         INTEGER,
    started_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ,
    CONSTRAINT uq_model_call_trace_attempts_trace_number UNIQUE (model_call_trace_id, attempt_no),
    CONSTRAINT chk_model_call_trace_attempts_outcome
        CHECK (outcome IN ('succeeded', 'failed', 'partial', 'client_cancelled')),
    CONSTRAINT chk_model_call_trace_attempts_nonnegative
        CHECK (attempt_no > 0 AND (duration_ms IS NULL OR duration_ms >= 0))
);

-- Keep the legacy aggregate upstream_attempt kind readable, while allowing
-- new traces to persist the request, response and error of each attempt
-- independently under the existing root-and-attempt uniqueness key.
ALTER TABLE model_call_payloads
    DROP CONSTRAINT IF EXISTS chk_model_call_payloads_kind;
ALTER TABLE model_call_payloads
    ADD CONSTRAINT chk_model_call_payloads_kind
        CHECK (kind IN (
            'client_request',
            'client_response',
            'error_response',
            'upstream_attempt',
            'upstream_request',
            'upstream_response',
            'upstream_error'
        ));

CREATE INDEX IF NOT EXISTS idx_model_call_traces_session_created
    ON model_call_traces(session_id, created_at ASC, id ASC)
    WHERE session_id <> '';
CREATE INDEX IF NOT EXISTS idx_model_call_traces_response_id
    ON model_call_traces(response_id)
    WHERE response_id <> '';
CREATE INDEX IF NOT EXISTS idx_model_call_traces_previous_response_id
    ON model_call_traces(previous_response_id)
    WHERE previous_response_id <> '';
CREATE INDEX IF NOT EXISTS idx_model_call_traces_user_snapshot_created
    ON model_call_traces(user_snapshot, created_at DESC, id DESC)
    WHERE user_snapshot <> '';
CREATE INDEX IF NOT EXISTS idx_model_call_traces_api_key_snapshot_created
    ON model_call_traces(api_key_snapshot, created_at DESC, id DESC)
    WHERE api_key_snapshot <> '';
CREATE INDEX IF NOT EXISTS idx_model_call_trace_attempts_trace_number
    ON model_call_trace_attempts(model_call_trace_id, attempt_no ASC);
CREATE INDEX IF NOT EXISTS idx_model_call_trace_attempts_account_created
    ON model_call_trace_attempts(account_id, started_at DESC, id DESC);
