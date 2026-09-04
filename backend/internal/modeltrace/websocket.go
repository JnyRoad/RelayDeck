package modeltrace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sync"
)

// WebSocketTurnTracer safely translates a multi-turn Responses WebSocket
// session into independent per-turn Recorder calls. It observes only frames
// already written to the client, so it never records an upstream-only reply.
type WebSocketTurnTracer struct {
	recorder     Recorder
	requestID    string
	route        string
	captureLimit int

	mu    sync.Mutex
	turns map[int]*webSocketTraceTurn
}

// webSocketTraceTurn keeps bounded in-memory state until a terminal frame is
// visible and the service has supplied the corresponding terminal metadata.
type webSocketTraceTurn struct {
	handle           TraceHandle
	requestBytes     int64
	responseBytes    int64
	responseHash     hashAccumulator
	responseFrames   []json.RawMessage
	responseStream   io.WriteCloser
	responseStarted  bool
	responseHasFrame bool
	responseTooLong  bool
	responseInvalid  bool
	terminalSeen     bool
	finish           *FinishInput
	finalized        bool
}

// hashAccumulator isolates the streaming digest dependency from turn state.
type hashAccumulator struct {
	hash hashWriter
}

// hashWriter is the narrow streaming hash contract needed by this tracer.
type hashWriter interface {
	Write([]byte) (int, error)
	Sum([]byte) []byte
}

// NewWebSocketTurnTracer creates an adapter for one upgraded client
// connection. The same request ID correlates every turn in that connection.
func NewWebSocketTurnTracer(recorder Recorder, requestID, route string) *WebSocketTurnTracer {
	return &WebSocketTurnTracer{
		recorder:     recorder,
		requestID:    requestID,
		route:        route,
		captureLimit: DefaultPayloadLimitBytes,
		turns:        make(map[int]*webSocketTraceTurn),
	}
}

// Begin starts a trace and records the accepted client response.create frame
// for one turn. Recorder failures stay best-effort so the WebSocket relay is
// never affected by tracing storage.
func (t *WebSocketTurnTracer) Begin(ctx context.Context, turn int, request []byte) {
	if t == nil || t.recorder == nil || turn < 1 {
		return
	}
	t.mu.Lock()
	_, exists := t.turns[turn]
	t.mu.Unlock()
	if exists {
		return
	}
	handle, err := t.recorder.Start(ctx, StartInput{
		RequestID: t.requestID,
		Route:     t.route,
		Protocol:  "websocket",
	})
	if err != nil || !handle.Enabled {
		return
	}
	requestStream := startWebSocketPayloadStream(t.recorder, ctx, handle, PayloadInput{
		Kind: PayloadKindClientRequest, ContentType: "application/json",
	})
	if requestStream == nil {
		_ = t.recorder.RecordPayload(ctx, handle, PayloadInput{
			Kind:          PayloadKindClientRequest,
			ContentType:   "application/json",
			Body:          append([]byte(nil), request...),
			OriginalBytes: int64(len(request)),
			SHA256:        hashPayload(request),
		})
	} else {
		_, _ = requestStream.Write(request)
		_ = requestStream.Close()
	}
	responseStream := startWebSocketPayloadStream(t.recorder, ctx, handle, PayloadInput{
		Kind: PayloadKindClientResponse, ContentType: "application/json",
	})

	t.mu.Lock()
	defer t.mu.Unlock()
	t.turns[turn] = &webSocketTraceTurn{
		handle:         handle,
		requestBytes:   int64(len(request)),
		responseHash:   hashAccumulator{hash: sha256.New()},
		responseStream: responseStream,
	}
}

// AppendClientFrame records a frame only after the gateway successfully wrote
// it to the WebSocket client. Binary and malformed text frames become safe
// metadata-only captures rather than plaintext persistence candidates.
func (t *WebSocketTurnTracer) AppendClientFrame(ctx context.Context, turn int, frame []byte) {
	if t == nil || turn < 1 {
		return
	}
	t.mu.Lock()
	state := t.turns[turn]
	if state == nil || state.finalized {
		t.mu.Unlock()
		return
	}
	state.responseBytes += int64(len(frame))
	_, _ = state.responseHash.hash.Write(frame)
	valid := json.Valid(frame)
	if !valid {
		state.responseInvalid = true
	} else if state.responseStream != nil && !state.responseInvalid {
		appendWebSocketStreamFrame(state, frame)
	} else if !state.responseTooLong && !state.responseInvalid {
		storedBytes := websocketFramesStoredBytes(state.responseFrames, frame)
		if t.captureLimit >= 0 && storedBytes > t.captureLimit {
			state.responseTooLong = true
			state.responseFrames = nil
		} else {
			state.responseFrames = append(state.responseFrames, append(json.RawMessage(nil), frame...))
		}
	}
	if isWebSocketTerminalFrame(frame) {
		state.terminalSeen = true
	}
	ready := t.readyToFinalizeLocked(turn, state, state.terminalSeen)
	t.mu.Unlock()
	t.persistFinalizedTurn(ctx, ready)
}

// Complete supplies one turn's terminal metadata. A trace finalizes once the
// matching terminal frame has also been successfully sent to the client.
func (t *WebSocketTurnTracer) Complete(ctx context.Context, turn int, input FinishInput) {
	if t == nil || turn < 1 {
		return
	}
	t.mu.Lock()
	state := t.turns[turn]
	if state == nil || state.finalized {
		t.mu.Unlock()
		return
	}
	copyInput := input
	state.finish = &copyInput
	ready := t.readyToFinalizeLocked(turn, state, state.terminalSeen)
	t.mu.Unlock()
	t.persistFinalizedTurn(ctx, ready)
}

// Close finalizes all remaining begun turns when a connection closes before a
// terminal client frame is available, preserving safe request diagnostics and
// explicit response-capture status instead of leaving orphaned active traces.
func (t *WebSocketTurnTracer) Close(ctx context.Context) {
	if t == nil {
		return
	}
	t.mu.Lock()
	ready := make([]websocketFinalizedTurn, 0, len(t.turns))
	for turn, state := range t.turns {
		if state.finish == nil {
			state.finish = &FinishInput{Outcome: OutcomePartial, Stream: true}
		}
		if finalized := t.readyToFinalizeLocked(turn, state, true); finalized != nil {
			ready = append(ready, *finalized)
		}
	}
	t.mu.Unlock()
	for index := range ready {
		t.persistFinalizedTurn(ctx, &ready[index])
	}
}

// websocketFinalizedTurn is an immutable persistence snapshot prepared while
// holding the tracer lock and written after the lock is released.
type websocketFinalizedTurn struct {
	handle          TraceHandle
	payload         PayloadInput
	finish          FinishInput
	recorder        Recorder
	responseStream  io.WriteCloser
	responseInvalid bool
	responseStarted bool
}

// readyToFinalizeLocked returns a snapshot when the turn has result metadata
// and removes its mutable buffers before persistence. The caller holds t.mu.
func (t *WebSocketTurnTracer) readyToFinalizeLocked(turn int, state *webSocketTraceTurn, allow bool) *websocketFinalizedTurn {
	if t == nil || state == nil || state.finalized || state.finish == nil || !allow {
		return nil
	}
	state.finalized = true
	finish := *state.finish
	finish.RequestBytes = state.requestBytes
	finish.ResponseBytes = state.responseBytes
	payload := PayloadInput{}
	if state.responseStream == nil {
		payload = websocketResponsePayload(state, t.captureLimit)
	}
	finalized := &websocketFinalizedTurn{
		handle:          state.handle,
		payload:         payload,
		finish:          finish,
		recorder:        t.recorder,
		responseStream:  state.responseStream,
		responseInvalid: state.responseInvalid,
		responseStarted: state.responseStarted,
	}
	delete(t.turns, turn)
	return finalized
}

// persistFinalizedTurn performs best-effort storage outside the tracer lock.
func (t *WebSocketTurnTracer) persistFinalizedTurn(ctx context.Context, ready *websocketFinalizedTurn) {
	if ready == nil || ready.recorder == nil || !ready.handle.Enabled {
		return
	}
	if ready.responseStream == nil {
		_ = ready.recorder.RecordPayload(ctx, ready.handle, ready.payload)
	} else {
		finishWebSocketPayloadStream(ready.responseStream, ready.responseInvalid, ready.responseStarted)
	}
	_ = ready.recorder.Finish(ctx, ready.handle, ready.finish)
}

// startWebSocketPayloadStream opens an optional per-turn body sink without
// changing the response relay when a recorder cannot provide one.
func startWebSocketPayloadStream(recorder Recorder, ctx context.Context, handle TraceHandle, input PayloadInput) io.WriteCloser {
	streamingRecorder, ok := recorder.(PayloadStreamRecorder)
	if !ok || streamingRecorder == nil {
		return nil
	}
	return streamingRecorder.StartPayloadStream(ctx, handle, input)
}

// appendWebSocketStreamFrame writes the raw-chain envelope incrementally after
// each valid client-visible frame, avoiding a growing frame slice in memory.
func appendWebSocketStreamFrame(state *webSocketTraceTurn, frame []byte) {
	if state == nil || state.responseStream == nil || state.responseInvalid {
		return
	}
	if !state.responseStarted {
		_, _ = state.responseStream.Write([]byte(`{"frames":[`))
		state.responseStarted = true
	}
	if state.responseHasFrame {
		_, _ = state.responseStream.Write([]byte(","))
	}
	_, _ = state.responseStream.Write(frame)
	state.responseHasFrame = true
}

// finishWebSocketPayloadStream closes a valid JSON envelope or marks malformed
// frame sequences unreadable before finalizing the independently stored chunks.
func finishWebSocketPayloadStream(stream io.WriteCloser, invalid, started bool) {
	if stream == nil {
		return
	}
	if invalid {
		setWebSocketPayloadCaptureStatus(stream, CaptureStatusNotApplicable)
		setWebSocketPayloadMetadata(stream, PayloadKindClientResponse, "application/octet-stream")
		_ = stream.Close()
		return
	}
	if !started {
		_, _ = stream.Write([]byte(`{"frames":[]}`))
	} else {
		_, _ = stream.Write([]byte(`]}`))
	}
	setWebSocketPayloadMetadata(stream, PayloadKindClientResponse, "application/json")
	_ = stream.Close()
}

// setWebSocketPayloadMetadata applies terminal raw-chain labels only when the
// stream exposes the optional metadata mutator used by the chunked recorder.
func setWebSocketPayloadMetadata(stream io.WriteCloser, kind PayloadKind, contentType string) {
	setter, ok := stream.(interface {
		SetPayloadMetadata(PayloadKind, string)
	})
	if ok {
		setter.SetPayloadMetadata(kind, contentType)
	}
}

// setWebSocketPayloadCaptureStatus marks a stream non-readable after malformed
// frame content without discarding the metadata required for trace analysis.
func setWebSocketPayloadCaptureStatus(stream io.WriteCloser, status CaptureStatus) {
	setter, ok := stream.(interface {
		SetPayloadCaptureStatus(CaptureStatus)
	})
	if ok {
		setter.SetPayloadCaptureStatus(status)
	}
}

// websocketResponsePayload converts safely bounded JSON frames into one JSON
// document. Oversized or malformed streams preserve only size and hash data.
func websocketResponsePayload(state *webSocketTraceTurn, limit int) PayloadInput {
	payload := PayloadInput{
		Kind:          PayloadKindClientResponse,
		ContentType:   "application/json",
		OriginalBytes: state.responseBytes,
		SHA256:        hex.EncodeToString(state.responseHash.hash.Sum(nil)),
	}
	if state.responseTooLong {
		payload.Truncated = true
		return payload
	}
	if state.responseInvalid || len(state.responseFrames) == 0 {
		payload.ContentType = "application/octet-stream"
		return payload
	}
	body, err := json.Marshal(struct {
		Frames []json.RawMessage `json:"frames"`
	}{Frames: state.responseFrames})
	if err != nil || (limit >= 0 && len(body) > limit) {
		payload.Truncated = true
		return payload
	}
	payload.Body = body
	return payload
}

// websocketFramesStoredBytes estimates the final JSON envelope size before a
// new valid frame is retained, preventing long-lived sessions from retaining
// more than the configured capture bound in memory.
func websocketFramesStoredBytes(existing []json.RawMessage, next []byte) int {
	bytes := len(`{"frames":[]}`) + len(next)
	if len(existing) > 0 {
		bytes++
	}
	for _, frame := range existing {
		bytes += len(frame)
	}
	if len(existing) > 1 {
		bytes += len(existing) - 1
	}
	return bytes
}

// isWebSocketTerminalFrame identifies client-visible events that conclusively
// end a Responses turn and therefore make its accumulated reply safe to close.
func isWebSocketTerminalFrame(frame []byte) bool {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(frame, &envelope); err != nil {
		return false
	}
	switch envelope.Type {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "error":
		return true
	default:
		return false
	}
}
