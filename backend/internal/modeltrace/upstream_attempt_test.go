package modeltrace

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// immediateUpstreamAttemptScheduler executes queued tracing work synchronously
// so an observer unit test can inspect its completed storage boundary.
type immediateUpstreamAttemptScheduler struct{}

// Enqueue runs one test operation immediately and reports accepted capacity.
func (immediateUpstreamAttemptScheduler) Enqueue(task func()) bool {
	if task != nil {
		task()
	}
	return true
}

// upstreamPayloadStreamStub records one optional streaming payload sink without
// using a database, allowing the transport test to detect legacy buffering.
type upstreamPayloadStreamStub struct {
	body   []byte
	closed bool
}

// Write records test bytes and preserves the caller-visible write count.
func (s *upstreamPayloadStreamStub) Write(body []byte) (int, error) {
	s.body = append(s.body, body...)
	return len(body), nil
}

// Close marks the test stream finalized after transport consumption ends.
func (s *upstreamPayloadStreamStub) Close() error {
	s.closed = true
	return nil
}

// upstreamStreamingAttemptRecorder combines the optional attempt and payload
// stream capabilities exercised by the shared HTTP transport observer.
type upstreamStreamingAttemptRecorder struct {
	streams        []*upstreamPayloadStreamStub
	legacyPayloads []PayloadInput
}

// Start returns one enabled root trace for the isolated observer test.
func (*upstreamStreamingAttemptRecorder) Start(context.Context, StartInput) (TraceHandle, error) {
	return TraceHandle{TraceID: "trace-upstream-stream", Enabled: true, PayloadCaptureEnabled: true}, nil
}

// RecordPayload records only fallback writes, which the streaming regression
// test expects to remain empty for a fully observed textual request.
func (r *upstreamStreamingAttemptRecorder) RecordPayload(_ context.Context, _ TraceHandle, input PayloadInput) error {
	r.legacyPayloads = append(r.legacyPayloads, input)
	return nil
}

// Finish is unused by this request-only observer test.
func (*upstreamStreamingAttemptRecorder) Finish(context.Context, TraceHandle, FinishInput) error {
	return nil
}

// StartUpstreamAttempt accepts one test transport occurrence.
func (*upstreamStreamingAttemptRecorder) StartUpstreamAttempt(context.Context, TraceHandle, UpstreamAttemptInput) error {
	return nil
}

// FinishUpstreamAttempt accepts the terminal result outside this request-only test.
func (*upstreamStreamingAttemptRecorder) FinishUpstreamAttempt(context.Context, TraceHandle, UpstreamAttemptFinishInput) error {
	return nil
}

// StartPayloadStream returns one test sink used to prove request bytes are sent
// through the streaming path before RecordRequest executes.
func (r *upstreamStreamingAttemptRecorder) StartPayloadStream(context.Context, TraceHandle, PayloadInput) io.WriteCloser {
	stream := &upstreamPayloadStreamStub{}
	r.streams = append(r.streams, stream)
	return stream
}

// blockingUpstreamAttemptRecorder simulates an unavailable trace store while
// keeping the test independent from a live PostgreSQL instance.
type blockingUpstreamAttemptRecorder struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*blockingUpstreamAttemptRecorder) Start(context.Context, StartInput) (TraceHandle, error) {
	return TraceHandle{TraceID: "trace-slow-store", Enabled: true, PayloadCaptureEnabled: true}, nil
}

func (*blockingUpstreamAttemptRecorder) RecordPayload(context.Context, TraceHandle, PayloadInput) error {
	return nil
}

func (*blockingUpstreamAttemptRecorder) Finish(context.Context, TraceHandle, FinishInput) error {
	return nil
}

func (r *blockingUpstreamAttemptRecorder) StartUpstreamAttempt(context.Context, TraceHandle, UpstreamAttemptInput) error {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return nil
}

func (*blockingUpstreamAttemptRecorder) FinishUpstreamAttempt(context.Context, TraceHandle, UpstreamAttemptFinishInput) error {
	return nil
}

func TestUpstreamAttemptObserverBeginDoesNotWaitForPersistence(t *testing.T) {
	recorder := &blockingUpstreamAttemptRecorder{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	t.Cleanup(func() { close(recorder.release) })
	observer := NewUpstreamAttemptObserver(recorder, TraceHandle{
		TraceID: "trace-slow-store", Enabled: true, PayloadCaptureEnabled: true,
	})

	attempts := make(chan *UpstreamAttempt, 1)
	go func() {
		attempts <- observer.Begin(context.Background(), UpstreamAttemptInput{UpstreamRoute: "https://upstream.example/v1/responses"})
	}()

	select {
	case attempt := <-attempts:
		require.NotNil(t, attempt)
	case <-time.After(time.Second):
		t.Fatal("Begin waited for persistence")
	}

	select {
	case <-recorder.started:
	case <-time.After(time.Second):
		t.Fatal("queued persistence never started")
	}
}

// TestUpstreamAttemptStreamsRequestBody verifies that a shared HTTP dispatch
// writes observed request bytes to the optional stream while net/http consumes
// them, instead of retaining one final aggregate before RecordRequest.
func TestUpstreamAttemptStreamsRequestBody(t *testing.T) {
	recorder := &upstreamStreamingAttemptRecorder{}
	observer := NewUpstreamAttemptObserver(recorder, TraceHandle{TraceID: "trace-upstream-stream", Enabled: true, PayloadCaptureEnabled: true})
	observer.scheduler = immediateUpstreamAttemptScheduler{}
	attempt := observer.Begin(context.Background(), UpstreamAttemptInput{UpstreamRoute: "https://upstream.example/v1/responses"})
	if attempt == nil {
		t.Fatal("begin upstream attempt returned nil")
	}
	body := `{"model":"gpt-test","input":"` + strings.Repeat("x", 2*256*1024+1) + `"}`
	wrapped := attempt.WrapRequestBody(io.NopCloser(strings.NewReader(body)))
	if _, err := io.ReadAll(wrapped); err != nil {
		t.Fatalf("read wrapped request: %v", err)
	}
	attempt.RecordRequest(context.Background(), "application/json")

	if len(recorder.streams) != 1 || !recorder.streams[0].closed || string(recorder.streams[0].body) != body {
		t.Fatalf("streamed requests=%#v, want one closed complete stream", recorder.streams)
	}
	if len(recorder.legacyPayloads) != 0 {
		t.Fatalf("legacy request payloads=%d, want 0", len(recorder.legacyPayloads))
	}
}
