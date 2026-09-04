-- Model gateway call traces are separate from billing usage logs so large,
-- encrypted payloads have an independent query and retention lifecycle.

CREATE TABLE IF NOT EXISTS model_call_traces (
    id                      BIGSERIAL PRIMARY KEY,
    trace_id                VARCHAR(64) NOT NULL,
    request_id              VARCHAR(128) NOT NULL DEFAULT '',
    parent_trace_id         VARCHAR(64),
    turn_number             INTEGER,
    user_id                 BIGINT REFERENCES users(id) ON DELETE SET NULL,
    api_key_id              BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    group_id                BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    account_id              BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    route                   VARCHAR(160) NOT NULL DEFAULT '',
    protocol                VARCHAR(24) NOT NULL DEFAULT 'sync',
    requested_model         VARCHAR(200) NOT NULL DEFAULT '',
    upstream_model          VARCHAR(200) NOT NULL DEFAULT '',
    response_model          VARCHAR(200) NOT NULL DEFAULT '',
    outcome                 VARCHAR(32) NOT NULL DEFAULT 'failed',
    status_code             INTEGER,
    upstream_status_code    INTEGER,
    error_stage             VARCHAR(64) NOT NULL DEFAULT '',
    error_code              VARCHAR(64) NOT NULL DEFAULT '',
    stream                  BOOLEAN NOT NULL DEFAULT FALSE,
    duration_ms             INTEGER,
    first_byte_ms           INTEGER,
    input_tokens            INTEGER,
    output_tokens           INTEGER,
    request_capture_status  VARCHAR(24) NOT NULL DEFAULT 'not_applicable',
    response_capture_status VARCHAR(24) NOT NULL DEFAULT 'not_applicable',
    request_bytes           BIGINT NOT NULL DEFAULT 0,
    response_bytes          BIGINT NOT NULL DEFAULT 0,
    expires_at              TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '7 days'),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at            TIMESTAMPTZ,
    CONSTRAINT uq_model_call_traces_trace_id UNIQUE (trace_id),
    CONSTRAINT chk_model_call_traces_protocol
        CHECK (protocol IN ('sync', 'sse', 'websocket')),
    CONSTRAINT chk_model_call_traces_outcome
        CHECK (outcome IN ('succeeded', 'failed', 'blocked', 'client_cancelled', 'partial')),
    CONSTRAINT chk_model_call_traces_capture_status
        CHECK (
            request_capture_status IN ('complete', 'truncated', 'redacted', 'not_applicable', 'failed') AND
            response_capture_status IN ('complete', 'truncated', 'redacted', 'not_applicable', 'failed')
        ),
    CONSTRAINT chk_model_call_traces_nonnegative
        CHECK (
            (turn_number IS NULL OR turn_number >= 0) AND
            (duration_ms IS NULL OR duration_ms >= 0) AND
            (first_byte_ms IS NULL OR first_byte_ms >= 0) AND
            (input_tokens IS NULL OR input_tokens >= 0) AND
            (output_tokens IS NULL OR output_tokens >= 0) AND
            request_bytes >= 0 AND response_bytes >= 0
        )
);

CREATE TABLE IF NOT EXISTS model_call_payloads (
    id                  BIGSERIAL PRIMARY KEY,
    model_call_trace_id BIGINT NOT NULL REFERENCES model_call_traces(id) ON DELETE CASCADE,
    kind                VARCHAR(32) NOT NULL,
    attempt_no          INTEGER NOT NULL DEFAULT 0,
    capture_status      VARCHAR(24) NOT NULL,
    mime_type           VARCHAR(128) NOT NULL DEFAULT '',
    content_encoding    VARCHAR(64) NOT NULL DEFAULT '',
    original_bytes      BIGINT NOT NULL DEFAULT 0,
    stored_bytes        BIGINT NOT NULL DEFAULT 0,
    sha256              CHAR(64) NOT NULL DEFAULT '',
    redaction_version   SMALLINT NOT NULL DEFAULT 1,
    ciphertext          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_model_call_payloads_trace_kind_attempt UNIQUE (model_call_trace_id, kind, attempt_no),
    CONSTRAINT chk_model_call_payloads_kind
        CHECK (kind IN ('client_request', 'client_response', 'upstream_attempt', 'error_response')),
    CONSTRAINT chk_model_call_payloads_capture_status
        CHECK (capture_status IN ('complete', 'truncated', 'redacted', 'not_applicable', 'failed')),
    CONSTRAINT chk_model_call_payloads_nonnegative
        CHECK (attempt_no >= 0 AND original_bytes >= 0 AND stored_bytes >= 0 AND redaction_version >= 1)
);

CREATE TABLE IF NOT EXISTS model_call_trace_cleanup_runs (
    id                BIGSERIAL PRIMARY KEY,
    mode              VARCHAR(16) NOT NULL,
    requested_by      BIGINT REFERENCES users(id) ON DELETE SET NULL,
    cutoff_at         TIMESTAMPTZ NOT NULL,
    status            VARCHAR(16) NOT NULL DEFAULT 'running',
    deleted_traces    BIGINT NOT NULL DEFAULT 0,
    deleted_payloads  BIGINT NOT NULL DEFAULT 0,
    deleted_bytes     BIGINT NOT NULL DEFAULT 0,
    error_code        VARCHAR(64) NOT NULL DEFAULT '',
    started_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at       TIMESTAMPTZ,
    CONSTRAINT chk_model_call_trace_cleanup_runs_mode
        CHECK (mode IN ('automatic', 'manual')),
    CONSTRAINT chk_model_call_trace_cleanup_runs_status
        CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT chk_model_call_trace_cleanup_runs_nonnegative
        CHECK (deleted_traces >= 0 AND deleted_payloads >= 0 AND deleted_bytes >= 0)
);

CREATE INDEX IF NOT EXISTS idx_model_call_traces_created
    ON model_call_traces(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_model_call_traces_request
    ON model_call_traces(request_id);
CREATE INDEX IF NOT EXISTS idx_model_call_traces_user_created
    ON model_call_traces(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_call_traces_api_key_created
    ON model_call_traces(api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_call_traces_outcome_created
    ON model_call_traces(outcome, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_call_traces_model_created
    ON model_call_traces(requested_model, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_call_traces_expires
    ON model_call_traces(expires_at, id);
CREATE INDEX IF NOT EXISTS idx_model_call_payloads_trace
    ON model_call_payloads(model_call_trace_id, kind, attempt_no);
CREATE INDEX IF NOT EXISTS idx_model_call_trace_cleanup_runs_started
    ON model_call_trace_cleanup_runs(started_at DESC, id DESC);
