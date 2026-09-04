# Data Model: 完整模型调用链追踪

## `model_call_traces` (existing root record, extended)

| Field | Purpose | Rules |
|---|---|---|
| `trace_id` | One gateway invocation identifier | Unique and immutable. |
| `request_id` | Client/gateway correlation identifier | Searchable, not a session heuristic. |
| `user_id`, `api_key_id`, `group_id`, `account_id` | Current relational attribution | Nullable FKs; retained for filter and joins. |
| `user_snapshot`, `api_key_snapshot`, `group_snapshot`, `account_snapshot` | Historical non-sensitive display values | Written at call time; remain after rename, disable or deletion; never contain a plaintext credential. |
| `session_id` | Explicit stable client session/conversation ID | Empty when absent; indexed; never synthesized from identity or time. |
| `previous_response_id`, `response_id` | Protocol-confirmed turn lineage | Used only for exact parent/child linkage. |
| route/model/outcome/timing/capture fields | Existing call summary | No body or ciphertext in summary responses. |

## `model_call_trace_attempts` (new)

One row per actual upstream dispatch. `model_call_trace_id` references its root and `attempt_no` starts at 1 and increases in occurrence order within that trace.

| Field | Purpose |
|---|---|
| `attempt_no` | Stable display and payload association order. |
| `account_id`, `account_snapshot` | The selected upstream account for this attempt. |
| `upstream_model`, `upstream_route` | Resolved model and target route. |
| `started_at`, `completed_at`, `duration_ms` | Attempt timing. |
| `status_code`, `outcome`, `error_stage`, `error_code` | Actual completion result without error body duplication. |

## `model_call_payloads` (existing, extended kinds)

Each row has capture status, MIME type, original/stored byte sizes, content hash, encrypted text and occurrence time. The approved capture policy retains body fields without redaction; headers, cookies and route credentials remain outside this model. Migration expands allowed `kind` values:

- `client_request`
- `client_response`
- `error_response`
- `upstream_request`
- `upstream_response`
- `upstream_error`

The existing `(model_call_trace_id, kind, attempt_no)` uniqueness applies. Client payloads use attempt `0`; upstream payloads use the matching attempt row’s positive sequence number.

New rows set `storage_mode` to `chunked` and retain only aggregate metadata in
this table. Existing `inline` ciphertext rows remain readable for backward
compatibility; no deployed row is rewritten by the schema migration.

## `model_call_payload_chunks` (new)

One row contains one independently encrypted plaintext segment of a `chunked`
payload. Rows are appended in `chunk_no` order and are never returned in list or
detail responses.

| Field | Purpose | Rules |
|---|---|---|
| `model_call_payload_id` | Owning payload metadata row | Cascades when the payload or root trace is deleted. |
| `chunk_no` | Stable zero-based sequence number | Unique per payload; used as the raw-body continuation cursor. |
| `ciphertext` | Independently encrypted plaintext chunk | At most 256 KiB plaintext per new chunk. |
| `stored_bytes` | Plaintext bytes represented by the chunk | Aggregates to the parent payload’s `stored_bytes`. |

## Relationships and deletion

```text
model_call_traces (1) ──< model_call_trace_attempts
         │
         └──────────────< model_call_payloads (1) ──< model_call_payload_chunks
```

Deleting an expired root trace cascades to both attempts and payloads. It never deletes `usage_logs`, users, API keys, groups or accounts. Audit logs retain only action metadata and do not reference payload content.

## State rules

- A root trace is created before the model handler; it is finalized after client response handling.
- An upstream attempt starts immediately before dispatch. A transport error finalizes it as failed; a response finalizes after its body is consumed or closed, preserving partial delivery if applicable.
- `session_id`, `previous_response_id` and `response_id` are blank when protocol extraction is absent or invalid. A blank value means “unlinked”, not “best effort”.
- `complete`, `redacted`, `truncated`, `not_applicable` and `failed` retain their current meaning. Only complete/redacted textual data can be viewed.
- A chunked payload starts as `failed` and changes to its terminal readable state only after all queued chunks and aggregate metadata commit. This fail-closed state prevents queue loss from being displayed as a complete body.
