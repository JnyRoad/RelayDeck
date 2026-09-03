package modeltrace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

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
