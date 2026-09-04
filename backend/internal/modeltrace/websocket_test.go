package modeltrace

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// webSocketPayloadStreamStub captures one optional stream in test memory so
// WebSocket tracing can be tested without a database-backed encryptor.
type webSocketPayloadStreamStub struct {
	body   []byte
	closed bool
}

// Write appends one test frame while preserving the caller-visible byte count.
func (s *webSocketPayloadStreamStub) Write(body []byte) (int, error) {
	s.body = append(s.body, body...)
	return len(body), nil
}

// Close marks the stream finalized after the terminal client event.
func (s *webSocketPayloadStreamStub) Close() error {
	s.closed = true
	return nil
}

// webSocketStreamingRecorderStub exposes the optional stream capability while
// retaining a legacy call list that must stay empty on the streaming path.
type webSocketStreamingRecorderStub struct {
	starts         []StartInput
	legacyPayloads []PayloadInput
	finishes       []FinishInput
	streams        []*webSocketPayloadStreamStub
}

// Start records one deterministic enabled WebSocket trace handle.
func (s *webSocketStreamingRecorderStub) Start(_ context.Context, input StartInput) (TraceHandle, error) {
	s.starts = append(s.starts, input)
	return TraceHandle{TraceID: "trace-ws-stream", Enabled: true, PayloadCaptureEnabled: true}, nil
}

// RecordPayload records only the legacy fallback path, which this regression
// test expects a stream-capable recorder not to receive.
func (s *webSocketStreamingRecorderStub) RecordPayload(_ context.Context, _ TraceHandle, input PayloadInput) error {
	s.legacyPayloads = append(s.legacyPayloads, input)
	return nil
}

// Finish records one terminal trace state for the stream-capable test recorder.
func (s *webSocketStreamingRecorderStub) Finish(_ context.Context, _ TraceHandle, input FinishInput) error {
	s.finishes = append(s.finishes, input)
	return nil
}

// StartPayloadStream returns a test stream for each request or client-visible
// response body that production code persists as bounded encrypted chunks.
func (s *webSocketStreamingRecorderStub) StartPayloadStream(context.Context, TraceHandle, PayloadInput) io.WriteCloser {
	stream := &webSocketPayloadStreamStub{}
	s.streams = append(s.streams, stream)
	return stream
}

// TestWebSocketTurnTracerPersistsBoundedClientVisibleTurn verifies that a
// multi-frame WebSocket turn records the client request and client-visible
// response only after its terminal event, without retaining an unbounded body.
func TestWebSocketTurnTracerPersistsBoundedClientVisibleTurn(t *testing.T) {
	recorder := &webSocketRecorderStub{}
	tracer := NewWebSocketTurnTracer(recorder, "connection-request", "/v1/responses")

	tracer.Begin(context.Background(), 1, []byte(`{"type":"response.create","model":"gpt-test","input":"hello"}`))
	tracer.Begin(context.Background(), 1, []byte(`{"type":"response.create","model":"must-not-duplicate"}`))
	tracer.AppendClientFrame(context.Background(), 1, []byte(`{"type":"response.output_text.delta","delta":"hello"}`))
	tracer.Complete(context.Background(), 1, FinishInput{Outcome: OutcomeSucceeded, Stream: true})
	require.Len(t, recorder.finishes, 0, "completion must wait until the terminal client frame is written")

	tracer.AppendClientFrame(context.Background(), 1, []byte(`{"type":"response.completed","response":{"id":"resp_1"}}`))
	require.Len(t, recorder.starts, 1)
	require.Len(t, recorder.payloads, 2)
	require.Len(t, recorder.finishes, 1)
	require.Equal(t, PayloadKindClientRequest, recorder.payloads[0].Kind)
	require.Equal(t, PayloadKindClientResponse, recorder.payloads[1].Kind)
	require.JSONEq(t, `{"frames":[{"type":"response.output_text.delta","delta":"hello"},{"type":"response.completed","response":{"id":"resp_1"}}]}`, string(recorder.payloads[1].Body))
	require.EqualValues(t, len(`{"type":"response.output_text.delta","delta":"hello"}`)+len(`{"type":"response.completed","response":{"id":"resp_1"}}`), recorder.finishes[0].ResponseBytes)
}

// TestWebSocketTurnTracerDropsOversizedOrInvalidResponses verifies that
// streamed frames never create a persistent plaintext prefix when their
// aggregate size exceeds the capture limit or cannot form safe JSON.
func TestWebSocketTurnTracerDropsOversizedOrInvalidResponses(t *testing.T) {
	recorder := &webSocketRecorderStub{}
	tracer := NewWebSocketTurnTracer(recorder, "connection-request", "/v1/responses")
	tracer.captureLimit = 8

	tracer.Begin(context.Background(), 1, []byte(`{"type":"response.create"}`))
	tracer.AppendClientFrame(context.Background(), 1, []byte(`{"type":"response.output_text.delta","delta":"too-large"}`))
	tracer.AppendClientFrame(context.Background(), 1, []byte(`{"type":"response.completed"}`))
	tracer.Complete(context.Background(), 1, FinishInput{Outcome: OutcomeSucceeded, Stream: true})

	require.Len(t, recorder.payloads, 2)
	response := recorder.payloads[1]
	require.Equal(t, PayloadKindClientResponse, response.Kind)
	require.True(t, response.Truncated)
	require.Empty(t, response.Body)
	require.Greater(t, response.OriginalBytes, int64(8))
	require.Len(t, recorder.finishes, 1)
}

// TestWebSocketTurnTracerReleasesFinalizedTurn verifies that completed turns
// do not accumulate their request and response buffers for a long-lived socket.
func TestWebSocketTurnTracerReleasesFinalizedTurn(t *testing.T) {
	recorder := &webSocketRecorderStub{}
	tracer := NewWebSocketTurnTracer(recorder, "connection-request", "/v1/responses")

	tracer.Begin(context.Background(), 1, []byte(`{"type":"response.create"}`))
	tracer.AppendClientFrame(context.Background(), 1, []byte(`{"type":"response.completed"}`))
	tracer.Complete(context.Background(), 1, FinishInput{Outcome: OutcomeSucceeded, Stream: true})

	tracer.mu.Lock()
	defer tracer.mu.Unlock()
	if len(tracer.turns) != 0 {
		t.Fatalf("retained finalized turns = %#v, want none", tracer.turns)
	}
}

// TestWebSocketTurnTracerStreamsClientFrames verifies that valid frames are
// serialized into the same raw-chain envelope while sent, without retaining a
// complete `responseFrames` slice until the turn terminates.
func TestWebSocketTurnTracerStreamsClientFrames(t *testing.T) {
	recorder := &webSocketStreamingRecorderStub{}
	tracer := NewWebSocketTurnTracer(recorder, "connection-request", "/v1/responses")
	request := []byte(`{"type":"response.create","model":"gpt-test"}`)
	first := []byte(`{"type":"response.output_text.delta","delta":"hello"}`)
	terminal := []byte(`{"type":"response.completed","response":{"id":"resp_1"}}`)

	tracer.Begin(context.Background(), 1, request)
	tracer.AppendClientFrame(context.Background(), 1, first)
	tracer.Complete(context.Background(), 1, FinishInput{Outcome: OutcomeSucceeded, Stream: true})
	tracer.AppendClientFrame(context.Background(), 1, terminal)

	if len(recorder.streams) != 2 {
		t.Fatalf("stream count=%d, want request and response", len(recorder.streams))
	}
	if !recorder.streams[0].closed || string(recorder.streams[0].body) != string(request) {
		t.Fatalf("request stream=%#v, want closed original request", recorder.streams[0])
	}
	if !recorder.streams[1].closed || string(recorder.streams[1].body) != `{"frames":[{"type":"response.output_text.delta","delta":"hello"},{"type":"response.completed","response":{"id":"resp_1"}}]}` {
		t.Fatalf("response stream=%#v, want complete frame envelope", recorder.streams[1])
	}
	if len(recorder.legacyPayloads) != 0 {
		t.Fatalf("legacy payloads=%#v, want none", recorder.legacyPayloads)
	}
}

// webSocketRecorderStub captures recorder inputs without encrypting data so
// WebSocket aggregation behavior can be unit-tested in isolation.
type webSocketRecorderStub struct {
	starts   []StartInput
	payloads []PayloadInput
	finishes []FinishInput
}

// Start appends a deterministic enabled trace handle for the current test turn.
func (s *webSocketRecorderStub) Start(_ context.Context, input StartInput) (TraceHandle, error) {
	s.starts = append(s.starts, input)
	return TraceHandle{TraceID: "trace", Enabled: true, PayloadCaptureEnabled: true}, nil
}

// RecordPayload records the bounded payload supplied by the WebSocket tracer.
func (s *webSocketRecorderStub) RecordPayload(_ context.Context, _ TraceHandle, input PayloadInput) error {
	s.payloads = append(s.payloads, input)
	return nil
}

// Finish records the terminal state supplied by the WebSocket tracer.
func (s *webSocketRecorderStub) Finish(_ context.Context, _ TraceHandle, input FinishInput) error {
	s.finishes = append(s.finishes, input)
	return nil
}
