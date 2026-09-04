// Package middleware contains the non-intrusive HTTP boundary for model call tracing.
package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/JnyRoad/RelayDeck/internal/modeltrace"
	"github.com/JnyRoad/RelayDeck/internal/pkg/ctxkey"
	"github.com/JnyRoad/RelayDeck/internal/service"
	"github.com/gin-gonic/gin"
)

// NewModelCallTraceMiddleware observes traceable model gateway calls after
// authentication. Recorder failures are intentionally ignored to preserve the
// model handler's response and availability.
func NewModelCallTraceMiddleware(recorder modeltrace.Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		if recorder == nil || !modeltrace.IsTraceableGatewayRoute(c.Request.Method, traceRoute(c)) {
			c.Next()
			return
		}

		startedAt := time.Now()
		handle, err := recorder.Start(c.Request.Context(), modeltrace.StartInput{
			RequestID: ensureClientRequestID(c),
			Route:     traceRoute(c),
			Protocol:  "sync",
		})
		if err != nil || !handle.Enabled {
			c.Next()
			return
		}
		if observer := modeltrace.NewUpstreamAttemptObserver(recorder, handle); observer != nil {
			c.Request = c.Request.WithContext(modeltrace.WithUpstreamAttemptObserver(c.Request.Context(), observer))
		}

		requestStream := startTracePayloadStream(recorder, c.Request.Context(), handle, modeltrace.PayloadInput{
			Kind:        modeltrace.PayloadKindClientRequest,
			ContentType: c.GetHeader("Content-Type"),
		})
		responseStream := startTracePayloadStream(recorder, c.Request.Context(), handle, modeltrace.PayloadInput{
			Kind: modeltrace.PayloadKindClientResponse,
		})
		// Keep one bounded protocol-correlation prefix even when full bodies are
		// streamed to encrypted chunks. Session and response identifiers can follow
		// a large input field, so reducing this prefix would break conversation
		// reconstruction for exactly the calls whose payloads need chunking.
		captureLimit := modeltrace.DefaultPayloadLimitBytes
		requestCapture := newBoundedBodyCapture(c.Request.Body, captureLimit, requestStream)
		c.Request.Body = requestCapture
		responseCapture := newTraceResponseWriter(c.Writer, captureLimit, responseStream)
		c.Writer = responseCapture

		c.Next()

		traceCtx, cancelTrace := tracePersistenceContext(c.Request.Context())
		defer cancelTrace()
		requestPayload := requestCapture.Payload(modeltrace.PayloadKindClientRequest, c.GetHeader("Content-Type"))
		if requestStream == nil {
			_ = recorder.RecordPayload(traceCtx, handle, requestPayload)
		} else {
			_ = requestStream.Close()
		}
		responseKind := modeltrace.PayloadKindClientResponse
		if c.Writer.Status() >= http.StatusBadRequest {
			responseKind = modeltrace.PayloadKindErrorResponse
		}
		responsePayload := responseCapture.Payload(responseKind, c.Writer.Header().Get("Content-Type"))
		if responseStream == nil {
			_ = recorder.RecordPayload(traceCtx, handle, responsePayload)
		} else {
			setTracePayloadStreamMetadata(responseStream, responseKind, responsePayload.ContentType)
			_ = responseStream.Close()
		}

		firstByteMS := responseCapture.FirstByteMS(startedAt)
		finishInput := modeltrace.FinishInput{
			Outcome:       traceOutcome(c, responseCapture.Stream()),
			StatusCode:    c.Writer.Status(),
			Stream:        responseCapture.Stream(),
			DurationMS:    int(time.Since(startedAt).Milliseconds()),
			FirstByteMS:   firstByteMS,
			RequestBytes:  requestPayload.OriginalBytes,
			ResponseBytes: responsePayload.OriginalBytes,
		}
		links := modeltrace.ExtractConversationLinks(requestPayload.ContentType, requestPayload.Body, responsePayload.ContentType, responsePayload.Body)
		finishInput.SessionID = links.SessionID
		finishInput.PreviousResponseID = links.PreviousResponseID
		finishInput.ResponseID = links.ResponseID
		populateTraceFinishIdentity(c, &finishInput)
		_ = recorder.Finish(traceCtx, handle, finishInput)
	}
}

// tracePersistenceContext preserves completed handler values while decoupling
// best-effort tracing from a client disconnect and bounding database work.
func tracePersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}

// populateTraceFinishIdentity 读取鉴权和路由完成后形成的可信上下文，不读取客户端认证原文。
func populateTraceFinishIdentity(c *gin.Context, input *modeltrace.FinishInput) {
	if c == nil || input == nil {
		return
	}
	if apiKey, ok := GetAPIKeyFromContext(c); ok && apiKey != nil {
		populateTraceAPIKeyIdentity(input, apiKey)
	} else if apiKey, ok := GetOpsFallbackAPIKey(c); ok && apiKey != nil {
		populateTraceAPIKeyIdentity(input, apiKey)
	}
	if c.Request == nil {
		return
	}
	if accountID, ok := c.Request.Context().Value(ctxkey.AccountID).(int64); ok {
		input.AccountID = positiveTraceID(accountID)
		input.AccountSnapshot = traceIdentitySnapshot("account", "", accountID)
	}
	input.RequestedModel = traceContextString(c, ctxkey.RequestedPublicModel)
	if input.RequestedModel == "" {
		input.RequestedModel = traceContextString(c, ctxkey.Model)
	}
	input.UpstreamModel = traceContextString(c, ctxkey.ResolvedUpstreamModel)
}

// populateTraceAPIKeyIdentity copies only durable display identifiers from a
// resolved API Key; the credential string itself is deliberately never read.
func populateTraceAPIKeyIdentity(input *modeltrace.FinishInput, apiKey *service.APIKey) {
	if input == nil || apiKey == nil {
		return
	}
	input.UserID = positiveTraceID(apiKey.UserID)
	input.APIKeyID = positiveTraceID(apiKey.ID)
	input.GroupID = apiKey.GroupID
	userName := ""
	if apiKey.User != nil {
		userName = apiKey.User.Email
		if userName == "" {
			userName = apiKey.User.Username
		}
	}
	input.UserSnapshot = traceIdentitySnapshot("user", userName, apiKey.UserID)
	input.APIKeySnapshot = traceIdentitySnapshot("api-key", apiKey.Name, apiKey.ID)
	groupName := ""
	if apiKey.Group != nil {
		groupName = apiKey.Group.Name
	}
	if apiKey.GroupID != nil {
		input.GroupSnapshot = traceIdentitySnapshot("group", groupName, *apiKey.GroupID)
	}
}

// traceIdentitySnapshot normalizes a bounded display value and falls back to
// a non-secret type-and-ID label when a cached relation lacks its name.
func traceIdentitySnapshot(kind, value string, identifier int64) string {
	value = strings.TrimSpace(value)
	if value != "" {
		characters := []rune(value)
		if len(characters) > 320 {
			return string(characters[:320])
		}
		return value
	}
	if identifier <= 0 {
		return ""
	}
	return fmt.Sprintf("%s#%d", kind, identifier)
}

// positiveTraceID 转换有效关联键为指针，避免将零值写入外键字段。
func positiveTraceID(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

// traceContextString 从服务端建立的上下文读取有限长度的模型摘要。
func traceContextString(c *gin.Context, key ctxkey.Key) string {
	if c == nil || c.Request == nil {
		return ""
	}
	value, _ := c.Request.Context().Value(key).(string)
	value = strings.TrimSpace(value)
	if len([]rune(value)) > 200 {
		return string([]rune(value)[:200])
	}
	return value
}

// boundedBodyCapture forwards all reads to the original body while retaining a
// bounded prefix and whole-stream digest for later best-effort trace storage.
type boundedBodyCapture struct {
	io.ReadCloser
	limit     int
	body      []byte
	total     int64
	digest    hash.Hash
	truncated bool
	stream    io.WriteCloser
}

// newBoundedBodyCapture wraps a request body without eagerly reading or
// replacing it, so handlers retain their original validation and body limits.
func newBoundedBodyCapture(body io.ReadCloser, limit int, streams ...io.WriteCloser) *boundedBodyCapture {
	if body == nil {
		body = io.NopCloser(strings.NewReader(""))
	}
	var stream io.WriteCloser
	if len(streams) > 0 {
		stream = streams[0]
	}
	return &boundedBodyCapture{
		ReadCloser: body,
		limit:      limit,
		digest:     sha256.New(),
		stream:     stream,
	}
}

// Read copies only the bytes actually consumed by the handler and never
// changes the original read count or error returned to the gateway handler.
func (c *boundedBodyCapture) Read(buffer []byte) (int, error) {
	read, err := c.ReadCloser.Read(buffer)
	if read <= 0 {
		return read, err
	}
	_, _ = c.digest.Write(buffer[:read])
	c.total += int64(read)
	if c.stream != nil {
		_, _ = c.stream.Write(buffer[:read])
	}
	if c.limit >= 0 {
		remaining := c.limit - len(c.body)
		if remaining <= 0 {
			c.truncated = true
			return read, err
		}
		if read > remaining {
			c.body = append(c.body, buffer[:remaining]...)
			c.truncated = true
			return read, err
		}
	}
	c.body = append(c.body, buffer[:read]...)
	return read, err
}

// Payload returns an immutable snapshot of the body capture for the recorder.
func (c *boundedBodyCapture) Payload(kind modeltrace.PayloadKind, contentType string) modeltrace.PayloadInput {
	return modeltrace.PayloadInput{
		Kind:          kind,
		ContentType:   contentType,
		Body:          append([]byte(nil), c.body...),
		OriginalBytes: c.total,
		SHA256:        hex.EncodeToString(c.digest.Sum(nil)),
		Truncated:     c.truncated,
	}
}

// traceResponseWriter delegates every Gin response-writer capability while
// observing bytes that have already been selected for delivery to the client.
type traceResponseWriter struct {
	gin.ResponseWriter
	capture    *boundedBodyCapture
	firstWrite time.Time
}

// newTraceResponseWriter creates a writer wrapper that preserves Gin's flush,
// hijack, and header behavior through embedded ResponseWriter delegation.
func newTraceResponseWriter(writer gin.ResponseWriter, limit int, streams ...io.WriteCloser) *traceResponseWriter {
	var stream io.WriteCloser
	if len(streams) > 0 {
		stream = streams[0]
	}
	return &traceResponseWriter{
		ResponseWriter: writer,
		capture:        newBoundedBodyCapture(io.NopCloser(strings.NewReader("")), limit, stream),
	}
}

// Write records only the bytes Gin reports as successfully delivered.
func (w *traceResponseWriter) Write(body []byte) (int, error) {
	written, err := w.ResponseWriter.Write(body)
	w.record(deliveredTraceBytes(body, written))
	return written, err
}

// WriteString records only the delivered prefix of a streamed string chunk.
func (w *traceResponseWriter) WriteString(value string) (int, error) {
	written, err := w.ResponseWriter.WriteString(value)
	w.record(deliveredTraceBytes([]byte(value), written))
	return written, err
}

// deliveredTraceBytes clamps an untrusted writer count to the supplied buffer
// before the trace recorder observes it.
func deliveredTraceBytes(body []byte, written int) []byte {
	if written <= 0 {
		return nil
	}
	if written >= len(body) {
		return body
	}
	return body[:written]
}

// record appends a response chunk to the bounded capture without performing I/O.
func (w *traceResponseWriter) record(body []byte) {
	if len(body) == 0 {
		return
	}
	if w.firstWrite.IsZero() {
		w.firstWrite = time.Now()
	}
	_, _ = w.capture.digest.Write(body)
	w.capture.total += int64(len(body))
	if w.capture.stream != nil {
		_, _ = w.capture.stream.Write(body)
	}
	if w.capture.limit >= 0 {
		remaining := w.capture.limit - len(w.capture.body)
		if remaining <= 0 {
			w.capture.truncated = true
			return
		}
		if len(body) > remaining {
			w.capture.body = append(w.capture.body, body[:remaining]...)
			w.capture.truncated = true
			return
		}
	}
	w.capture.body = append(w.capture.body, body...)
}

// startTracePayloadStream asks only capable recorders for a bounded body sink;
// legacy recorders retain the metadata capture path without changing requests.
func startTracePayloadStream(recorder modeltrace.Recorder, ctx context.Context, handle modeltrace.TraceHandle, input modeltrace.PayloadInput) io.WriteCloser {
	streamingRecorder, ok := recorder.(modeltrace.PayloadStreamRecorder)
	if !ok || streamingRecorder == nil {
		return nil
	}
	return streamingRecorder.StartPayloadStream(ctx, handle, input)
}

// setTracePayloadStreamMetadata passes post-handler response metadata only to
// sinks that support it, preserving compatibility with generic io.WriteCloser
// implementations used by recorder adapters and tests.
func setTracePayloadStreamMetadata(stream io.WriteCloser, kind modeltrace.PayloadKind, contentType string) {
	if stream == nil {
		return
	}
	setter, ok := stream.(interface {
		SetPayloadMetadata(modeltrace.PayloadKind, string)
	})
	if ok {
		setter.SetPayloadMetadata(kind, contentType)
	}
}

// Payload returns the response capture with the recorder's requested kind.
func (w *traceResponseWriter) Payload(kind modeltrace.PayloadKind, contentType string) modeltrace.PayloadInput {
	return w.capture.Payload(kind, contentType)
}

// FirstByteMS reports nil until at least one response byte is written.
func (w *traceResponseWriter) FirstByteMS(startedAt time.Time) *int {
	if w.firstWrite.IsZero() {
		return nil
	}
	value := int(w.firstWrite.Sub(startedAt).Milliseconds())
	return &value
}

// Stream reports whether the client-visible response uses the SSE media type.
func (w *traceResponseWriter) Stream() bool {
	return strings.HasPrefix(strings.ToLower(w.Header().Get("Content-Type")), "text/event-stream")
}

// traceRoute obtains the matched Gin path when available and otherwise keeps
// the raw request path for route classification during tests and edge cases.
func traceRoute(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return c.Request.URL.Path
}

// traceOutcome turns the observable HTTP result into a stable trace terminal
// state while preserving the handler's original status and error protocol.
func traceOutcome(c *gin.Context, stream bool) modeltrace.Outcome {
	if c.Request.Context().Err() != nil {
		if stream {
			return modeltrace.OutcomePartial
		}
		return modeltrace.OutcomeClientCancelled
	}
	if c.Writer.Status() == http.StatusForbidden {
		return modeltrace.OutcomeBlocked
	}
	if c.Writer.Status() >= http.StatusBadRequest {
		return modeltrace.OutcomeFailed
	}
	return modeltrace.OutcomeSucceeded
}
