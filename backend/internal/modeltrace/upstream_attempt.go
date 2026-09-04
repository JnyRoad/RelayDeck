package modeltrace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"sync"
	"time"
)

// upstreamAttemptObserverContextKey is private so only this package can
// install an observer on a gateway request context.
type upstreamAttemptObserverContextKey struct{}

const (
	upstreamAttemptPersistenceWait      = 5 * time.Second
	upstreamAttemptPersistenceQueueSize = 256
)

// upstreamAttemptPersistenceScheduler accepts best-effort writes without
// making the HTTP dispatch wait for trace storage availability.
type upstreamAttemptPersistenceScheduler interface {
	Enqueue(func()) bool
}

// asyncUpstreamAttemptPersistenceScheduler runs bounded queued writes in one
// order-preserving worker. Queue saturation intentionally drops trace work.
type asyncUpstreamAttemptPersistenceScheduler struct {
	tasks chan func()
}

// newAsyncUpstreamAttemptPersistenceScheduler starts the one worker used for
// best-effort attempt persistence. It owns no request resources after a task
// completes and never feeds errors back into gateway request handling.
func newAsyncUpstreamAttemptPersistenceScheduler(queueSize int) *asyncUpstreamAttemptPersistenceScheduler {
	scheduler := &asyncUpstreamAttemptPersistenceScheduler{tasks: make(chan func(), queueSize)}
	go func() {
		for task := range scheduler.tasks {
			task()
		}
	}()
	return scheduler
}

// Enqueue accepts only immediately available capacity, preserving upstream
// latency when the tracing database is slow, unavailable, or overloaded.
func (s *asyncUpstreamAttemptPersistenceScheduler) Enqueue(task func()) bool {
	if s == nil || task == nil {
		return false
	}
	select {
	case s.tasks <- task:
		return true
	default:
		return false
	}
}

var defaultUpstreamAttemptPersistenceScheduler = newAsyncUpstreamAttemptPersistenceScheduler(upstreamAttemptPersistenceQueueSize)

// UpstreamAttemptObserver allocates positive, per-root attempt numbers at the
// shared HTTP boundary. Its persistence calls are best-effort by design.
type UpstreamAttemptObserver struct {
	recorder  UpstreamAttemptRecorder
	handle    TraceHandle
	scheduler upstreamAttemptPersistenceScheduler

	mu     sync.Mutex
	nextNo int
}

// UpstreamAttempt is one live transport dispatch. It owns request and response
// body observers without storing any HTTP header or credential information.
type UpstreamAttempt struct {
	observer *UpstreamAttemptObserver
	input    UpstreamAttemptInput
	started  time.Time

	requestOnce sync.Once
	finishOnce  sync.Once
	request     *upstreamAttemptBodyCapture
}

// NewUpstreamAttemptObserver returns nil unless the active root recorder also
// supports durable attempt metadata. Existing client-only adapters therefore
// keep their exact behavior.
func NewUpstreamAttemptObserver(recorder Recorder, handle TraceHandle) *UpstreamAttemptObserver {
	if recorder == nil || !handle.Enabled {
		return nil
	}
	attemptRecorder, ok := recorder.(UpstreamAttemptRecorder)
	if !ok || attemptRecorder == nil {
		return nil
	}
	return &UpstreamAttemptObserver{
		recorder:  attemptRecorder,
		handle:    handle,
		scheduler: defaultUpstreamAttemptPersistenceScheduler,
	}
}

// WithUpstreamAttemptObserver adds a root-bound observer to the request
// context so every downstream retry that keeps the context is independently
// recorded at the actual HTTP dispatch boundary.
func WithUpstreamAttemptObserver(ctx context.Context, observer *UpstreamAttemptObserver) context.Context {
	if observer == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, upstreamAttemptObserverContextKey{}, observer)
}

// UpstreamAttemptObserverFromContext returns the request-scoped observer, if
// tracing was enabled and its recorder supports attempt persistence.
func UpstreamAttemptObserverFromContext(ctx context.Context) *UpstreamAttemptObserver {
	if ctx == nil {
		return nil
	}
	observer, _ := ctx.Value(upstreamAttemptObserverContextKey{}).(*UpstreamAttemptObserver)
	return observer
}

// Begin allocates one actual dispatch occurrence before its HTTP request is
// sent. Metadata persistence is queued and never delays the transport flow.
func (o *UpstreamAttemptObserver) Begin(ctx context.Context, input UpstreamAttemptInput) *UpstreamAttempt {
	if o == nil || o.recorder == nil || !o.handle.Enabled {
		return nil
	}
	now := time.Now().UTC()
	o.mu.Lock()
	o.nextNo++
	input.AttemptNo = o.nextNo
	o.mu.Unlock()
	input.StartedAt = now
	if !o.enqueue(ctx, func(persistCtx context.Context) error {
		return o.recorder.StartUpstreamAttempt(persistCtx, o.handle, input)
	}) {
		return nil
	}
	return &UpstreamAttempt{observer: o, input: input, started: now}
}

// WrapRequestBody returns a transparent read wrapper that observes exactly the
// bytes consumed by net/http. A nil body remains nil to preserve request wire
// semantics while an empty metadata record is still produced at dispatch end.
func (a *UpstreamAttempt) WrapRequestBody(body io.ReadCloser) io.ReadCloser {
	if a == nil {
		return body
	}
	if body == nil {
		a.request = newUpstreamAttemptBodyCapture(nil, a.startPayloadStream(context.Background(), PayloadInput{
			Kind: PayloadKindUpstreamRequest, AttemptNo: a.input.AttemptNo,
		}))
		return nil
	}
	capture := newUpstreamAttemptBodyCapture(body, a.startPayloadStream(context.Background(), PayloadInput{
		Kind: PayloadKindUpstreamRequest, AttemptNo: a.input.AttemptNo,
	}))
	a.request = capture
	return capture
}

// RecordRequest persists the completed outgoing request observation once the
// transport has consumed it. Repeated calls are intentionally harmless.
func (a *UpstreamAttempt) RecordRequest(ctx context.Context, contentType string) {
	if a == nil {
		return
	}
	a.requestOnce.Do(func() {
		capture := a.request
		if capture == nil {
			capture = newUpstreamAttemptBodyCapture(nil)
		}
		if capture.stream != nil {
			setAttemptPayloadStreamMetadata(capture.stream, PayloadKindUpstreamRequest, contentType)
			_ = capture.stream.Close()
			return
		}
		_ = a.observer.enqueue(ctx, func(persistCtx context.Context) error {
			return a.observer.recorder.RecordPayload(persistCtx, a.observer.handle,
				capture.Payload(PayloadKindUpstreamRequest, a.input.AttemptNo, contentType))
		})
	})
}

// WrapResponseBody returns a transparent body wrapper. It finalizes metadata
// only when callers close the response exactly as the existing transport does.
func (a *UpstreamAttempt) WrapResponseBody(ctx context.Context, body io.ReadCloser, contentType string, statusCode int) io.ReadCloser {
	if a == nil {
		return body
	}
	if body == nil {
		a.finishResponse(ctx, newUpstreamAttemptBodyCapture(nil, a.startPayloadStream(ctx, PayloadInput{
			Kind: PayloadKindUpstreamResponse, AttemptNo: a.input.AttemptNo, ContentType: contentType,
		})), contentType, statusCode, "", "")
		return nil
	}
	return &upstreamAttemptResponseBody{
		ReadCloser: body,
		attempt:    a,
		capture: newUpstreamAttemptBodyCapture(body, a.startPayloadStream(ctx, PayloadInput{
			Kind: PayloadKindUpstreamResponse, AttemptNo: a.input.AttemptNo, ContentType: contentType,
		})),
		context:     ctx,
		contentType: contentType,
		statusCode:  statusCode,
	}
}

// RecordTransportError stores a stable, content-free error representation for
// DNS/TCP/TLS failures, whose raw Go error text may contain credential URLs.
func (a *UpstreamAttempt) RecordTransportError(ctx context.Context) {
	if a == nil {
		return
	}
	a.RecordRequest(ctx, "")
	a.finishOnce.Do(func() {
		body := []byte(`{"error_code":"upstream_transport_error"}`)
		_ = a.observer.enqueue(ctx, func(persistCtx context.Context) error {
			return a.observer.recorder.RecordPayload(persistCtx, a.observer.handle, PayloadInput{
				Kind:          PayloadKindUpstreamError,
				AttemptNo:     a.input.AttemptNo,
				ContentType:   "application/json",
				Body:          body,
				OriginalBytes: int64(len(body)),
				SHA256:        hashPayload(body),
			})
		})
		_ = a.observer.enqueue(ctx, func(persistCtx context.Context) error {
			return a.observer.recorder.FinishUpstreamAttempt(persistCtx, a.observer.handle, UpstreamAttemptFinishInput{
				AttemptNo:  a.input.AttemptNo,
				Outcome:    OutcomeFailed,
				ErrorStage: "transport",
				ErrorCode:  "upstream_transport_error",
				DurationMS: attemptDurationMS(a.started),
			})
		})
	})
}

// finishResponse persists the captured upstream result and terminal attempt
// metadata after the caller has read or closed its response body.
func (a *UpstreamAttempt) finishResponse(ctx context.Context, capture *upstreamAttemptBodyCapture, contentType string, statusCode int, errorStage, errorCode string) {
	if a == nil {
		return
	}
	a.finishOnce.Do(func() {
		a.RecordRequest(ctx, "")
		kind := PayloadKindUpstreamResponse
		outcome := OutcomeSucceeded
		if statusCode >= 400 {
			kind = PayloadKindUpstreamError
			outcome = OutcomeFailed
			errorStage = "http_response"
			errorCode = "upstream_http_error"
		}
		if capture == nil {
			capture = newUpstreamAttemptBodyCapture(nil)
		}
		if capture.readFailed {
			outcome = OutcomePartial
			errorStage = "response_read"
			errorCode = "upstream_response_read_error"
		}
		if capture.stream != nil {
			setAttemptPayloadStreamMetadata(capture.stream, kind, contentType)
			_ = capture.stream.Close()
		} else {
			_ = a.observer.enqueue(ctx, func(persistCtx context.Context) error {
				return a.observer.recorder.RecordPayload(persistCtx, a.observer.handle,
					capture.Payload(kind, a.input.AttemptNo, contentType))
			})
		}
		_ = a.observer.enqueue(ctx, func(persistCtx context.Context) error {
			return a.observer.recorder.FinishUpstreamAttempt(persistCtx, a.observer.handle, UpstreamAttemptFinishInput{
				AttemptNo:  a.input.AttemptNo,
				Outcome:    outcome,
				StatusCode: statusCode,
				ErrorStage: errorStage,
				ErrorCode:  errorCode,
				DurationMS: attemptDurationMS(a.started),
			})
		})
	})
}

// enqueue schedules one detached persistence operation only when the bounded
// worker has capacity. Returning false intentionally disables this occurrence
// rather than making an upstream request wait for tracing storage.
func (o *UpstreamAttemptObserver) enqueue(ctx context.Context, operation func(context.Context) error) bool {
	if o == nil || o.scheduler == nil || operation == nil {
		return false
	}
	return o.scheduler.Enqueue(func() {
		_ = withUpstreamAttemptPersistence(ctx, operation)
	})
}

// upstreamAttemptResponseBody delegates reads unchanged and finalizes the
// attempt once Close is called by the existing service path.
type upstreamAttemptResponseBody struct {
	io.ReadCloser
	attempt     *UpstreamAttempt
	capture     *upstreamAttemptBodyCapture
	context     context.Context
	contentType string
	statusCode  int
	once        sync.Once
}

// Read forwards the upstream bytes unchanged while observing only delivered
// response bytes and a non-EOF read failure state.
func (b *upstreamAttemptResponseBody) Read(buffer []byte) (int, error) {
	read, err := b.ReadCloser.Read(buffer)
	if b.capture != nil {
		b.capture.Record(buffer[:maxInt(read, 0)], err)
	}
	return read, err
}

// Close preserves the original close result and performs best-effort trace
// persistence exactly once after the downstream consumer is finished.
func (b *upstreamAttemptResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() {
		if b.attempt != nil {
			b.attempt.finishResponse(b.context, b.capture, b.contentType, b.statusCode, "", "")
		}
	})
	return err
}

// upstreamAttemptBodyCapture observes a body stream without changing its
// return values. It retains only a bounded fallback prefix while an optional
// stream receives each consumed byte for fixed-memory chunk persistence.
type upstreamAttemptBodyCapture struct {
	io.ReadCloser
	body       []byte
	total      int64
	digest     hashWriter
	readFailed bool
	limit      int
	truncated  bool
	stream     io.WriteCloser
}

// newUpstreamAttemptBodyCapture creates a transparent capture that forwards
// reads and closes to the original stream without eagerly consuming it.
func newUpstreamAttemptBodyCapture(body io.ReadCloser, streams ...io.WriteCloser) *upstreamAttemptBodyCapture {
	if body == nil {
		body = io.NopCloser(strings.NewReader(""))
	}
	var stream io.WriteCloser
	if len(streams) > 0 {
		stream = streams[0]
	}
	return &upstreamAttemptBodyCapture{
		ReadCloser: body,
		digest:     sha256.New(),
		limit:      DefaultPayloadLimitBytes,
		stream:     stream,
	}
}

// Read delegates to the original body before recording exactly the bytes it
// already reported to its caller.
func (c *upstreamAttemptBodyCapture) Read(buffer []byte) (int, error) {
	read, err := c.ReadCloser.Read(buffer)
	c.Record(buffer[:maxInt(read, 0)], err)
	return read, err
}

// Close delegates to the original request body so net/http ownership remains
// identical to the request it received.
func (c *upstreamAttemptBodyCapture) Close() error { return c.ReadCloser.Close() }

// Record appends bytes already returned by the underlying reader and notes only
// non-EOF read faults for a terminal partial result.
func (c *upstreamAttemptBodyCapture) Record(body []byte, readErr error) {
	if c == nil {
		return
	}
	if len(body) > 0 {
		_, _ = c.digest.Write(body)
		c.total += int64(len(body))
		if c.stream != nil {
			_, _ = c.stream.Write(body)
		}
		remaining := c.limit - len(c.body)
		if remaining <= 0 {
			c.truncated = true
		} else if len(body) > remaining {
			c.body = append(c.body, body[:remaining]...)
			c.truncated = true
		} else {
			c.body = append(c.body, body...)
		}
	}
	if readErr != nil && readErr != io.EOF {
		c.readFailed = true
	}
}

// Payload returns an immutable observation for the encrypted persistence
// pipeline without exposing the mutable capture buffer.
func (c *upstreamAttemptBodyCapture) Payload(kind PayloadKind, attemptNo int, contentType string) PayloadInput {
	if c == nil {
		return PayloadInput{Kind: kind, AttemptNo: attemptNo, ContentType: contentType}
	}
	return PayloadInput{
		Kind:          kind,
		AttemptNo:     attemptNo,
		ContentType:   contentType,
		Body:          append([]byte(nil), c.body...),
		OriginalBytes: c.total,
		SHA256:        hex.EncodeToString(c.digest.Sum(nil)),
		Truncated:     c.truncated,
	}
}

// startPayloadStream opens an optional root-bound stream for one upstream
// body. A legacy recorder or storage setup simply receives the bounded fallback.
func (a *UpstreamAttempt) startPayloadStream(ctx context.Context, input PayloadInput) io.WriteCloser {
	if a == nil || a.observer == nil || a.observer.recorder == nil {
		return nil
	}
	streamingRecorder, ok := a.observer.recorder.(PayloadStreamRecorder)
	if !ok || streamingRecorder == nil {
		return nil
	}
	return streamingRecorder.StartPayloadStream(ctx, a.observer.handle, input)
}

// setAttemptPayloadStreamMetadata applies response status/type information to
// capable stream sinks after the shared transport knows the terminal outcome.
func setAttemptPayloadStreamMetadata(stream io.WriteCloser, kind PayloadKind, contentType string) {
	if stream == nil {
		return
	}
	setter, ok := stream.(interface {
		SetPayloadMetadata(PayloadKind, string)
	})
	if ok {
		setter.SetPayloadMetadata(kind, contentType)
	}
}

// withUpstreamAttemptPersistence detaches the audit write from a client
// cancellation and gives each best-effort storage operation a bounded deadline.
func withUpstreamAttemptPersistence(ctx context.Context, operation func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	persistenceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), upstreamAttemptPersistenceWait)
	defer cancel()
	return operation(persistenceCtx)
}

// attemptDurationMS normalizes a non-negative elapsed duration for storage.
func attemptDurationMS(startedAt time.Time) int {
	if startedAt.IsZero() {
		return 0
	}
	duration := time.Since(startedAt).Milliseconds()
	if duration < 0 {
		return 0
	}
	return int(duration)
}

// maxInt prevents an invalid read count from slicing a response buffer.
func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
