package modeltrace

import (
	"context"
	"io"
	"time"
)

// PayloadKind identifies a persisted view of one model gateway call.
type PayloadKind string

const (
	// PayloadKindClientRequest is the client request received by the gateway.
	PayloadKindClientRequest PayloadKind = "client_request"
	// PayloadKindClientResponse is the response bytes written back to the client.
	PayloadKindClientResponse PayloadKind = "client_response"
	// PayloadKindErrorResponse is a client-visible non-success response body.
	PayloadKindErrorResponse PayloadKind = "error_response"
	// PayloadKindUpstreamRequest is one dispatched upstream request body.
	PayloadKindUpstreamRequest PayloadKind = "upstream_request"
	// PayloadKindUpstreamResponse is one upstream response body.
	PayloadKindUpstreamResponse PayloadKind = "upstream_response"
	// PayloadKindUpstreamError is one upstream transport or protocol error body.
	PayloadKindUpstreamError PayloadKind = "upstream_error"
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
	AttemptNo     int
	ContentType   string
	Body          []byte
	OriginalBytes int64
	SHA256        string
	Truncated     bool
}

// FinishInput is the final call state calculated after the handler returns.
type FinishInput struct {
	Outcome            Outcome
	StatusCode         int
	Stream             bool
	DurationMS         int
	FirstByteMS        *int
	RequestBytes       int64
	ResponseBytes      int64
	UserID             *int64
	APIKeyID           *int64
	GroupID            *int64
	AccountID          *int64
	RequestedModel     string
	UpstreamModel      string
	UserSnapshot       string
	APIKeySnapshot     string
	GroupSnapshot      string
	AccountSnapshot    string
	SessionID          string
	PreviousResponseID string
	ResponseID         string
}

// UpstreamAttemptInput identifies one actual shared-transport dispatch. It
// contains only route and attribution summaries; headers and credentials never
// enter this contract.
type UpstreamAttemptInput struct {
	AttemptNo       int
	AccountID       *int64
	AccountSnapshot string
	UpstreamRoute   string
	UpstreamModel   string
	StartedAt       time.Time
}

// UpstreamAttemptFinishInput records an attempt terminal result without
// duplicating the potentially sensitive upstream error text.
type UpstreamAttemptFinishInput struct {
	AttemptNo  int
	Outcome    Outcome
	StatusCode int
	ErrorStage string
	ErrorCode  string
	DurationMS int
}

// Recorder is the only dependency the gateway middleware needs. Implementers
// must treat RecordPayload failures as best-effort and never alter the caller's
// response semantics.
type Recorder interface {
	Start(ctx context.Context, input StartInput) (TraceHandle, error)
	RecordPayload(ctx context.Context, handle TraceHandle, input PayloadInput) error
	Finish(ctx context.Context, handle TraceHandle, input FinishInput) error
}

// UpstreamAttemptRecorder is an optional Recorder capability used only by the
// shared HTTP transport. Keeping it optional preserves existing recorder
// adapters that only observe client-visible gateway traffic.
type UpstreamAttemptRecorder interface {
	Recorder
	StartUpstreamAttempt(ctx context.Context, handle TraceHandle, input UpstreamAttemptInput) error
	FinishUpstreamAttempt(ctx context.Context, handle TraceHandle, input UpstreamAttemptFinishInput) error
}

// PayloadStreamRecorder is an optional streaming extension for recorders that
// can persist a long text body without requiring middleware to aggregate it.
// A nil writer tells callers to use their bounded metadata-only fallback.
type PayloadStreamRecorder interface {
	Recorder
	StartPayloadStream(ctx context.Context, handle TraceHandle, input PayloadInput) io.WriteCloser
}
