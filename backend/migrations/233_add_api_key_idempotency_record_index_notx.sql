-- Build the unique index online so API-key writes remain available.
CREATE UNIQUE INDEX
CONCURRENTLY IF NOT EXISTS api_keys_idempotency_record_id_key
ON api_keys (idempotency_record_id);
