package modeltrace

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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

	startedAt := time.Now()
	attempt := observer.Begin(context.Background(), UpstreamAttemptInput{UpstreamRoute: "https://upstream.example/v1/responses"})
	require.NotNil(t, attempt)
	require.Less(t, time.Since(startedAt), 100*time.Millisecond)

	select {
	case <-recorder.started:
	case <-time.After(time.Second):
		t.Fatal("queued persistence never started")
	}
}
