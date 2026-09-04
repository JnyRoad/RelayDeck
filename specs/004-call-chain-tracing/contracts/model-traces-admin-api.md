# Admin Model Trace API Contract

All endpoints are under `/api/v1/admin/model-traces`, require authenticated administrator authorization, and are covered by the existing audit middleware. Summary endpoints never return body text or ciphertext.

## `GET /`

Supports existing pagination plus: `user_id`, `api_key_id`, `user`, `api_key`, `trace_id`, `request_id`, `session_id`, `route`, `requested_model`, `upstream_model`, `account_id`, `protocol`, `outcome`, `capture_status`, `start_time`, `end_time`, and retention state. The response item includes historical display snapshots and explicit session/link metadata but no body fields.

## `GET /:traceID`

Returns one root summary, ordered attempt metadata, and payload metadata only. It neither reads nor decrypts ciphertext.

## `GET /:traceID/conversation`

Returns the selected call plus only calls connected by the same non-empty explicit session ID or exact response lineage. If neither exists, it returns the single call and `linked: false`. No user/Key/time inference is permitted. Body text remains absent.

## `GET /:traceID/payloads/:kind?attempt_no=N`

Decrypts and returns exactly one selected payload. Valid kinds include client, upstream request/response and upstream error kinds. The response contains content, MIME type, original/stored byte counts, capture status and SHA-256; never ciphertext.

## `POST /:traceID/access-events`

Body: `{ "action": "copy", "kind": "client_request", "attempt_no": 0 }`.

Validates that the referenced payload metadata exists, records an audit action with identifiers only, and returns no protected content. It does not modify the trace.

## Existing configuration and cleanup endpoints

`GET/PUT /config`, `GET /cleanup-preview`, `POST /cleanup` remain. `retention_days` accepts only an integer from 1 through 365. Cleanup remains explicit-confirmation only and cascades only trace, attempt and payload data.
