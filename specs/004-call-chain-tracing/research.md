# Research: 完整模型调用链追踪

## Decision: Extend the existing trace schema with an additive migration

- **Rationale**: `183_model_call_traces.sql` is already a deployment migration and the current repository has migrations through 231. A new `232_*` migration can add snapshots, explicit session links and attempts without changing deployed history or usage records.
- **Alternatives considered**:
  - Rewriting migration 183: rejected because existing databases would not receive the changes.
  - Storing all retry data in one JSON blob: rejected because it cannot be indexed, ordered or independently expose request and response metadata.

## Decision: Capture upstream attempts at the shared HTTP transport boundary

- **Rationale**: `repository.HTTPUpstream.Do` and `DoWithTLS` are the actual HTTP dispatch points. A trace handle in the request context observes every retry/fallback that passes through them without scattering capture code through each provider handler.
- **Alternatives considered**:
  - Capture only final handler metadata: rejected because it misses failed retries and account/model switches.
  - Globally instrument every HTTP client: rejected because it would collect unrelated outbound traffic and make scope unsafe.

## Decision: Use only explicit session IDs and response lineage

- **Rationale**: Request fields such as `conversation_id`/`session_id` and Responses API `previous_response_id` are observable protocol links. The query can use the response ID returned by the API to connect a later turn.
- **Alternatives considered**:
  - Group by user, API Key, IP, model or time window: rejected by FR-005 because those signals can join unrelated conversations.
  - Infer links from prompt similarity: rejected because it is neither stable nor auditable.

## Decision: Keep list, details and bodies as separate read paths

- **Rationale**: Existing list queries already avoid payload joins. Preserve that guarantee; details only fetch metadata, and a body is decrypted only after the administrator opens it. Conversation opening may request the client request/final response bodies for linked turns, but never before the detail is opened.
- **Alternatives considered**:
  - Include body previews in the list: rejected by FR-008 and SC-006.
  - Bulk-decrypt every body in a trace: rejected because raw retry payloads can be large and are not needed for chat replay.

## Decision: Retain full encrypted body text without redaction

- **Rationale**: The requested forensic value requires the exact text that was received/delivered, and the administrator explicitly chose no body redaction. Authentication headers, cookies and route credentials are never captured as bodies; encryption, administrator authorization and expiration bound the remaining exposure.
- **Alternatives considered**:
  - Keep the current fixed prefix-only limit for text: rejected because a partial prompt/response prevents the requested full replay.
  - Persist raw HTTP headers: rejected because headers carry API keys, tokens and cookies.

## Decision: Audit server-observable reads and copies without content

- **Rationale**: The route audit middleware can record sensitive GET endpoints. A dedicated POST copy-event endpoint records the otherwise browser-local clipboard action, with trace ID, payload kind and attempt number only.
- **Alternatives considered**:
  - Audit the clipboard text: rejected because it duplicates protected content into the audit log.
  - Treat browser copy as unauditable: rejected by FR-013.
