// Package middleware 的测试覆盖模型调用追踪对网关协议的非侵入性。
package middleware

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JnyRoad/RelayDeck/internal/modeltrace"
	"github.com/JnyRoad/RelayDeck/internal/pkg/ctxkey"
	"github.com/JnyRoad/RelayDeck/internal/service"
	"github.com/gin-gonic/gin"
)

// modelTraceRecorderStub captures the middleware boundary without replacing the
// request handlers that determine the client-visible response.
type modelTraceRecorderStub struct {
	started       []modeltrace.StartInput
	payloads      []modeltrace.PayloadInput
	finished      []modeltrace.FinishInput
	payloadCtxErr []error
	finishCtxErr  []error
	payloadErr    error
	tracingActive bool
}

// Start creates a deterministic handle for middleware tests and records the
// supplied call metadata without performing storage I/O.
func (s *modelTraceRecorderStub) Start(_ context.Context, input modeltrace.StartInput) (modeltrace.TraceHandle, error) {
	s.started = append(s.started, input)
	return modeltrace.TraceHandle{TraceID: "trace-test", Enabled: s.tracingActive}, nil
}

// RecordPayload keeps the raw middleware boundary input for assertions and can
// simulate a best-effort persistence failure without changing handler output.
func (s *modelTraceRecorderStub) RecordPayload(ctx context.Context, _ modeltrace.TraceHandle, input modeltrace.PayloadInput) error {
	s.payloads = append(s.payloads, input)
	s.payloadCtxErr = append(s.payloadCtxErr, ctx.Err())
	return s.payloadErr
}

// Finish records the computed terminal state for assertions without persistence.
func (s *modelTraceRecorderStub) Finish(ctx context.Context, _ modeltrace.TraceHandle, input modeltrace.FinishInput) error {
	s.finished = append(s.finished, input)
	s.finishCtxErr = append(s.finishCtxErr, ctx.Err())
	return nil
}

// partialTraceResponseWriter simulates a downstream writer that accepts only
// a prefix, so the trace wrapper can prove it records delivered bytes only.
type partialTraceResponseWriter struct {
	gin.ResponseWriter
	n int
}

// Write returns the configured successful byte count without claiming the
// unsent suffix was delivered to a client.
func (w *partialTraceResponseWriter) Write([]byte) (int, error) {
	return w.n, nil
}

// TestModelCallTraceMiddlewarePreservesRequestAndRecordsRoundTrip verifies that
// the feature observes a model call without consuming its request body.
func TestModelCallTraceMiddlewarePreservesRequestAndRecordsRoundTrip(t *testing.T) {
	recorder := &modelTraceRecorderStub{tracingActive: true}
	router := newModelTraceTestRouter(recorder, func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Fatalf("handler read request body: %v", err)
		}
		c.Data(http.StatusCreated, "application/json", body)
	})

	rec := doModelTraceRequest(router, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-test","message":"hello"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if rec.Body.String() != `{"model":"gpt-test","message":"hello"}` {
		t.Fatalf("response body = %s", rec.Body.String())
	}
	if len(recorder.started) != 1 || recorder.started[0].Route != "/v1/chat/completions" {
		t.Fatalf("starts = %#v, want one chat-completions trace", recorder.started)
	}
	if len(recorder.payloads) != 2 {
		t.Fatalf("payload count = %d, want request and response", len(recorder.payloads))
	}
	if string(recorder.payloads[0].Body) != `{"model":"gpt-test","message":"hello"}` {
		t.Fatalf("captured request = %s", recorder.payloads[0].Body)
	}
	if string(recorder.payloads[1].Body) != `{"model":"gpt-test","message":"hello"}` {
		t.Fatalf("captured response = %s", recorder.payloads[1].Body)
	}
	if len(recorder.finished) != 1 || recorder.finished[0].Outcome != modeltrace.OutcomeSucceeded {
		t.Fatalf("finishes = %#v, want succeeded", recorder.finished)
	}
}

// TestModelCallTraceMiddlewareCapturesStreamingWrites verifies that streamed
// client-visible bytes are recorded in write order without delaying the stream.
func TestModelCallTraceMiddlewareCapturesStreamingWrites(t *testing.T) {
	recorder := &modelTraceRecorderStub{tracingActive: true}
	router := newModelTraceTestRouter(recorder, func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: first\n\n")
		c.Writer.Flush()
		_, _ = c.Writer.WriteString("data: second\n\n")
	})

	rec := doModelTraceRequest(router, http.MethodPost, "/v1/responses", `{"model":"gpt-test"}`)

	if rec.Body.String() != "data: first\n\ndata: second\n\n" {
		t.Fatalf("stream response = %q", rec.Body.String())
	}
	if len(recorder.payloads) != 2 || string(recorder.payloads[1].Body) != rec.Body.String() {
		t.Fatalf("response payloads = %#v, want stream bytes", recorder.payloads)
	}
	if len(recorder.finished) != 1 || !recorder.finished[0].Stream {
		t.Fatalf("finishes = %#v, want stream terminal state", recorder.finished)
	}
}

// TestModelCallTraceMiddlewareDoesNotBreakResponsesOnRecorderFailure verifies
// that a tracing storage fault never changes the model gateway's HTTP result.
func TestModelCallTraceMiddlewareDoesNotBreakResponsesOnRecorderFailure(t *testing.T) {
	recorder := &modelTraceRecorderStub{tracingActive: true, payloadErr: errors.New("trace storage unavailable")}
	router := newModelTraceTestRouter(recorder, func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", []byte(`{"ok":true}`))
	})

	rec := doModelTraceRequest(router, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-test"}`)

	if rec.Code != http.StatusOK || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("gateway response changed after recorder failure: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestModelCallTraceMiddlewareSkipsNonModelRoutes verifies that gateway-adjacent
// discovery endpoints cannot create calls or capture their responses.
func TestModelCallTraceMiddlewareSkipsNonModelRoutes(t *testing.T) {
	recorder := &modelTraceRecorderStub{tracingActive: true}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(NewModelCallTraceMiddleware(recorder))
	router.GET("/v1/models", func(c *gin.Context) { c.String(http.StatusOK, "models") })

	rec := doModelTraceRequest(router, http.MethodGet, "/v1/models", "")

	if rec.Code != http.StatusOK || rec.Body.String() != "models" {
		t.Fatalf("unexpected models response: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(recorder.started) != 0 || len(recorder.payloads) != 0 || len(recorder.finished) != 0 {
		t.Fatalf("non-model route was traced: starts=%d payloads=%d finishes=%d", len(recorder.started), len(recorder.payloads), len(recorder.finished))
	}
}

// TestModelCallTraceMiddlewareUsesGatewayCorrelationID 验证追踪记录复用网关统一生成的客户端请求标识。
func TestModelCallTraceMiddlewareUsesGatewayCorrelationID(t *testing.T) {
	recorder := &modelTraceRecorderStub{tracingActive: true}
	router := newModelTraceTestRouter(recorder, func(c *gin.Context) {
		requestID, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string)
		c.String(http.StatusOK, requestID)
	})

	rec := doModelTraceRequest(router, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-test"}`)

	if len(recorder.started) != 1 || recorder.started[0].RequestID == "" {
		t.Fatalf("starts = %#v, want generated client request id", recorder.started)
	}
	if rec.Body.String() != recorder.started[0].RequestID {
		t.Fatalf("handler request id = %q, trace request id = %q", rec.Body.String(), recorder.started[0].RequestID)
	}
	if rec.Header().Get(clientRequestIDHeader) != recorder.started[0].RequestID {
		t.Fatalf("response request id = %q, trace request id = %q", rec.Header().Get(clientRequestIDHeader), recorder.started[0].RequestID)
	}
}

// TestModelCallTraceMiddlewareCapturesResolvedIdentity 验证鉴权和调度完成后，终态追踪会保留调用归属与模型摘要。
func TestModelCallTraceMiddlewareCapturesResolvedIdentity(t *testing.T) {
	groupID := int64(33)
	recorder := &modelTraceRecorderStub{tracingActive: true}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(NewModelCallTraceMiddleware(recorder))
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{ID: 22, UserID: 11, GroupID: &groupID})
		ctx := context.WithValue(c.Request.Context(), ctxkey.AccountID, int64(44))
		ctx = context.WithValue(ctx, ctxkey.RequestedPublicModel, "public-model")
		ctx = context.WithValue(ctx, ctxkey.ResolvedUpstreamModel, "upstream-model")
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.POST("/v1/chat/completions", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	rec := doModelTraceRequest(router, http.MethodPost, "/v1/chat/completions", `{"model":"public-model"}`)

	if rec.Code != http.StatusOK || len(recorder.finished) != 1 {
		t.Fatalf("response=%d finishes=%#v", rec.Code, recorder.finished)
	}
	finished := recorder.finished[0]
	if finished.UserID == nil || *finished.UserID != 11 || finished.APIKeyID == nil || *finished.APIKeyID != 22 || finished.GroupID == nil || *finished.GroupID != 33 || finished.AccountID == nil || *finished.AccountID != 44 {
		t.Fatalf("finish identity = %#v", finished)
	}
	if finished.RequestedModel != "public-model" || finished.UpstreamModel != "upstream-model" {
		t.Fatalf("finish models = %#v", finished)
	}
}

// TestModelCallTraceMiddlewareCapturesHistoricalIdentitySnapshots verifies
// that trace completion carries display-safe identity values instead of API Key
// material, so later rename or deletion cannot erase the call attribution.
func TestModelCallTraceMiddlewareCapturesHistoricalIdentitySnapshots(t *testing.T) {
	groupID := int64(33)
	recorder := &modelTraceRecorderStub{tracingActive: true}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(NewModelCallTraceMiddleware(recorder))
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{
			ID: 22, UserID: 11, Name: "dingrui-key", GroupID: &groupID,
			User:  &service.User{ID: 11, Email: "dingrui@szyuto.com"},
			Group: &service.Group{ID: 33, Name: "development"},
		})
		ctx := context.WithValue(c.Request.Context(), ctxkey.AccountID, int64(44))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.POST("/v1/chat/completions", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	rec := doModelTraceRequest(router, http.MethodPost, "/v1/chat/completions", `{"model":"public-model"}`)

	if rec.Code != http.StatusOK || len(recorder.finished) != 1 {
		t.Fatalf("response=%d finishes=%#v", rec.Code, recorder.finished)
	}
	finished := recorder.finished[0]
	if finished.UserSnapshot != "dingrui@szyuto.com" || finished.APIKeySnapshot != "dingrui-key" || finished.GroupSnapshot != "development" || finished.AccountSnapshot != "account#44" {
		t.Fatalf("finish snapshots = %#v", finished)
	}
}

// TestModelCallTraceMiddlewareCapturesExplicitConversationLinks verifies that
// the final trace stores only session and lineage values parsed from the
// client-visible request and delivered Responses API result.
func TestModelCallTraceMiddlewareCapturesExplicitConversationLinks(t *testing.T) {
	recorder := &modelTraceRecorderStub{tracingActive: true}
	router := newModelTraceTestRouter(recorder, func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			t.Fatalf("handler read request body: %v", err)
		}
		c.Data(http.StatusOK, "application/json", []byte(`{"object":"response","id":"resp-current"}`))
	})

	rec := doModelTraceRequest(router, http.MethodPost, "/v1/responses", `{"conversation_id":"conversation-42","previous_response_id":"resp-previous"}`)

	if rec.Code != http.StatusOK || len(recorder.finished) != 1 {
		t.Fatalf("response=%d finishes=%#v", rec.Code, recorder.finished)
	}
	finished := recorder.finished[0]
	if finished.SessionID != "conversation-42" || finished.PreviousResponseID != "resp-previous" || finished.ResponseID != "resp-current" {
		t.Fatalf("finish conversation links = %#v", finished)
	}
}

// TestModelCallTraceMiddlewarePersistsAfterClientCancellation verifies that a
// cancellation changes the trace outcome but does not cancel final trace I/O.
func TestModelCallTraceMiddlewarePersistsAfterClientCancellation(t *testing.T) {
	recorder := &modelTraceRecorderStub{tracingActive: true}
	router := newModelTraceTestRouter(recorder, func(c *gin.Context) {
		requestCtx, cancel := context.WithCancel(c.Request.Context())
		c.Request = c.Request.WithContext(requestCtx)
		cancel()
		c.String(http.StatusOK, "client-visible")
	})

	_ = doModelTraceRequest(router, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-test"}`)

	if len(recorder.payloadCtxErr) != 2 || recorder.payloadCtxErr[0] != nil || recorder.payloadCtxErr[1] != nil || len(recorder.finishCtxErr) != 1 || recorder.finishCtxErr[0] != nil {
		t.Fatalf("trace persistence inherited cancelled request context: payload=%#v finish=%#v", recorder.payloadCtxErr, recorder.finishCtxErr)
	}
	if len(recorder.finished) != 1 || recorder.finished[0].Outcome != modeltrace.OutcomeClientCancelled {
		t.Fatalf("finish outcome=%#v, want client cancelled", recorder.finished)
	}
}

// TestTraceResponseWriterRecordsOnlyDeliveredBytes verifies that a partial
// downstream write cannot make unsent response suffixes appear in a trace.
func TestTraceResponseWriterRecordsOnlyDeliveredBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	writer := newTraceResponseWriter(&partialTraceResponseWriter{ResponseWriter: ctx.Writer, n: 3}, 32)

	written, err := writer.Write([]byte("abcdef"))

	if err != nil || written != 3 {
		t.Fatalf("write result = (%d, %v), want (3, nil)", written, err)
	}
	payload := writer.Payload(modeltrace.PayloadKindClientResponse, "text/plain")
	if string(payload.Body) != "abc" || payload.OriginalBytes != 3 {
		t.Fatalf("captured response payload = %#v, want delivered prefix only", payload)
	}
}

// newModelTraceTestRouter builds one traceable route whose handler controls the
// exact bytes used to assert that middleware preserves gateway behavior.
func newModelTraceTestRouter(recorder modeltrace.Recorder, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(NewModelCallTraceMiddleware(recorder))
	router.POST("/v1/chat/completions", handler)
	router.POST("/v1/responses", handler)
	return router
}

// doModelTraceRequest executes a single in-memory HTTP request without opening
// sockets or relying on external model providers.
func doModelTraceRequest(router http.Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
