-- Store new model-trace body data in independently encrypted segments so one
-- gateway request never requires one unbounded payload ciphertext allocation.
-- Existing inline rows stay readable and are intentionally not rewritten.

ALTER TABLE model_call_payloads
    ADD COLUMN IF NOT EXISTS storage_mode VARCHAR(16) NOT NULL DEFAULT 'inline';

ALTER TABLE model_call_payloads
    DROP CONSTRAINT IF EXISTS chk_model_call_payloads_storage_mode;
ALTER TABLE model_call_payloads
    ADD CONSTRAINT chk_model_call_payloads_storage_mode
        CHECK (storage_mode IN ('inline', 'chunked')) NOT VALID;

CREATE TABLE IF NOT EXISTS model_call_payload_chunks (
    id                    BIGSERIAL PRIMARY KEY,
    model_call_payload_id BIGINT NOT NULL REFERENCES model_call_payloads(id) ON DELETE CASCADE,
    chunk_no              INTEGER NOT NULL,
    stored_bytes          BIGINT NOT NULL,
    ciphertext            TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_model_call_payload_chunks_payload_number
        UNIQUE (model_call_payload_id, chunk_no),
    CONSTRAINT chk_model_call_payload_chunks_size
        CHECK (chunk_no >= 0 AND stored_bytes >= 0 AND stored_bytes <= 262144)
);
