package modeltrace

import "context"

// PayloadKind identifies a persisted view of one model gateway call.
type PayloadKind string

const (
	// PayloadKindClientRequest is the client request received by the gateway.
	PayloadKindClientRequest PayloadKind = "client_request"
	// PayloadKindClientResponse is the response bytes written back to the client.
	PayloadKindClientResponse PayloadKind = "client_response"
	// PayloadKindErrorResponse is a client-visible non-success response body.
	PayloadKindErrorResponse PayloadKind = "error_response"
)

// Outcome is the terminal result of a traceable model gateway call.
type Outcome string

const (
	// OutcomeSucceeded means the gateway produced a successful client response.
	OutcomeSucceeded Outcome = "succeeded"
	// OutcomeFailed means the gateway returned an unsuccessful response.
	OutcomeFailed Outcome = "failed"
	// OutcomeBlocked means the gateway denied the request before model dispatch.
	OutcomeBlocked Outcome = "blocked"
	// OutcomeClientCancelled means the client connection ended before completion.
	OutcomeClientCancelled Outcome = "client_cancelled"
	// OutcomePartial means a stream emitted bytes before it was interrupted.
	OutcomePartial Outcome = "partial"
)

// TraceHandle identifies a started trace. Disabled handles let callers avoid
// storage work while preserving the original model gateway execution path.
type TraceHandle struct {
	TraceID               string
	Enabled               bool
	PayloadCaptureEnabled bool
}

// StartInput is the non-sensitive metadata available before a model handler
// runs. Later payload and finish inputs provide body and terminal information.
type StartInput struct {
	RequestID string
	Route     string
	Protocol  string
}

// PayloadInput carries a bounded copy of one body plus original-stream
// metadata. A truncated body must never be persisted without safe processing.
type PayloadInput struct {
	Kind          PayloadKind
	ContentType   string
	Body          []byte
	OriginalBytes int64
	SHA256        string
	Truncated     bool
}

// FinishInput is the final call state calculated after the handler returns.
type FinishInput struct {
	Outcome        Outcome
	StatusCode     int
	Stream         bool
	DurationMS     int
	FirstByteMS    *int
	RequestBytes   int64
	ResponseBytes  int64
	UserID         *int64
	APIKeyID       *int64
	GroupID        *int64
	AccountID      *int64
	RequestedModel string
	UpstreamModel  string
}

// Recorder is the only dependency the gateway middleware needs. Implementers
// must treat RecordPayload failures as best-effort and never alter the caller's
// response semantics.
type Recorder interface {
	Start(ctx context.Context, input StartInput) (TraceHandle, error)
	RecordPayload(ctx context.Context, handle TraceHandle, input PayloadInput) error
	Finish(ctx context.Context, handle TraceHandle, input FinishInput) error
}
