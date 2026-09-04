-- Preserve a Key's idempotency claim, so retries can recover after a crash.
-- No foreign key: idempotency records expire while API Keys remain valid.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS idempotency_record_id BIGINT;

CREATE UNIQUE INDEX IF NOT EXISTS api_keys_idempotency_record_id_key
ON api_keys (idempotency_record_id);
