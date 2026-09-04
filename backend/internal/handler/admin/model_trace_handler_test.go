package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JnyRoad/RelayDeck/internal/modeltrace"
	"github.com/JnyRoad/RelayDeck/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// modelTraceQueryRepositoryStub 为管理端处理器提供不含数据库 I/O 的追踪查询结果。
type modelTraceQueryRepositoryStub struct {
	items        []modeltrace.TraceSummary
	total        int64
	detail       modeltrace.TraceDetail
	conversation modeltrace.TraceConversation
	payload      modeltrace.TracePayload
	filter       modeltrace.TraceFilter
	pageRequest  modeltrace.ConversationPageRequest
	chunkNo      int
	maxBytes     int
}

// ListTraces 记录处理器解析后的筛选条件并返回预置索引。
func (s *modelTraceQueryRepositoryStub) ListTraces(_ context.Context, filter modeltrace.TraceFilter, _, _ int) ([]modeltrace.TraceSummary, int64, error) {
	s.filter = filter
	return s.items, s.total, nil
}

// GetTrace 返回预置的单条调用详情。
func (s *modelTraceQueryRepositoryStub) GetTrace(context.Context, string) (modeltrace.TraceDetail, error) {
	return s.detail, nil
}

// GetPayload returns the test-selected payload without database I/O.
func (s *modelTraceQueryRepositoryStub) GetPayload(context.Context, string, modeltrace.PayloadKind, int) (modeltrace.TracePayload, error) {
	return s.payload, nil
}

// GetPayloadPage records the selected raw-body continuation while preserving
// the original test payload's encrypted content for query-service decryption.
func (s *modelTraceQueryRepositoryStub) GetPayloadPage(_ context.Context, _ string, _ modeltrace.PayloadKind, _ int, chunkNo, maxBytes int) (modeltrace.TracePayloadPage, error) {
	s.chunkNo = chunkNo
	s.maxBytes = maxBytes
	return modeltrace.TracePayloadPage{Payload: s.payload, Ciphertexts: []string{s.payload.Ciphertext}}, nil
}

// GetConversation returns the test-provided explicit replay index without I/O.
func (s *modelTraceQueryRepositoryStub) GetConversation(context.Context, string) (modeltrace.TraceConversation, error) {
	return s.conversation, nil
}

// GetConversationPage records the handler's parsed pagination request while
// returning the prebuilt metadata-only replay for HTTP contract tests.
func (s *modelTraceQueryRepositoryStub) GetConversationPage(_ context.Context, _ string, page modeltrace.ConversationPageRequest) (modeltrace.TraceConversation, error) {
	s.pageRequest = page
	return s.conversation, nil
}

// modelTraceDecryptorStub 仅返回测试期望的安全正文。
type modelTraceDecryptorStub struct{}

// Decrypt 返回已经脱敏的确定正文。
func (modelTraceDecryptorStub) Decrypt(string) (string, error) {
	return `{"prompt":"[REDACTED]"}`, nil
}

// modelTraceSettingsRepositoryStub 提供独立的系统设置内存存储。
type modelTraceSettingsRepositoryStub struct {
	value string
}

// modelTraceCleanupRepositoryStub records manual cleanup starts without
// deleting data so handler confirmation tests can observe side effects.
type modelTraceCleanupRepositoryStub struct {
	startedRuns int
}

// PreviewExpired returns an empty preview because handler tests do not inspect it.
func (s *modelTraceCleanupRepositoryStub) PreviewExpired(context.Context, time.Time) (modeltrace.CleanupPreview, error) {
	return modeltrace.CleanupPreview{}, nil
}

// DeleteExpired returns an empty result because valid confirmation tests only
// need to prove that the cleanup service was allowed to start.
func (s *modelTraceCleanupRepositoryStub) DeleteExpired(context.Context, time.Time, int) (modeltrace.CleanupResult, error) {
	return modeltrace.CleanupResult{}, nil
}

// StartCleanupRun records the audit-run creation requested by manual cleanup.
func (s *modelTraceCleanupRepositoryStub) StartCleanupRun(context.Context, modeltrace.CleanupMode, *int64, time.Time) (int64, error) {
	s.startedRuns++
	return int64(s.startedRuns), nil
}

// FinishCleanupRun completes the fake audit lifecycle without external I/O.
func (*modelTraceCleanupRepositoryStub) FinishCleanupRun(context.Context, int64, modeltrace.CleanupResult, error) error {
	return nil
}

// GetValue 返回当前设置值；空值代表首次部署默认策略。
func (s *modelTraceSettingsRepositoryStub) GetValue(context.Context, string) (string, error) {
	return s.value, nil
}

// Set 保存处理器更新的设置值。
func (s *modelTraceSettingsRepositoryStub) Set(_ context.Context, _ string, value string) error {
	s.value = value
	return nil
}

// newModelTraceHandlerForTest 组装没有外部依赖的模型追踪管理端处理器。
func newModelTraceHandlerForTest(repository *modelTraceQueryRepositoryStub) *ModelTraceHandler {
	settings := modeltrace.NewSettingsConfigStore(&modelTraceSettingsRepositoryStub{})
	query := modeltrace.NewQueryService(repository, modelTraceDecryptorStub{})
	return NewModelTraceHandler(query, settings, nil)
}

// TestModelTraceHandlerListReturnsIndexes 验证列表返回轻量索引并正确解析关联请求标识筛选。
func TestModelTraceHandlerListReturnsIndexes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &modelTraceQueryRepositoryStub{items: []modeltrace.TraceSummary{{TraceID: "trace-list", CreatedAt: time.Now().UTC()}}, total: 1}
	handler := newModelTraceHandlerForTest(repository)
	router := gin.New()
	router.GET("/admin/model-traces", handler.List)

	request := httptest.NewRequest(http.MethodGet, "/admin/model-traces?request_id=creq-list&trace_id=trace-list&protocol=websocket&capture_status=truncated", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "trace-list") {
		t.Fatalf("list response status=%d body=%s", response.Code, response.Body.String())
	}
	if repository.filter.RequestID != "creq-list" || repository.filter.TraceID != "trace-list" || repository.filter.Protocol != "websocket" || repository.filter.CaptureStatus != modeltrace.CaptureStatusTruncated {
		t.Fatalf("parsed filter=%#v", repository.filter)
	}
}

// TestModelTraceHandlerListParsesHistoricalAttributionFilters verifies that
// administrators can search call-time identity snapshots and explicit session
// fields without asking the list endpoint to load a trace body.
func TestModelTraceHandlerListParsesHistoricalAttributionFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &modelTraceQueryRepositoryStub{}
	handler := newModelTraceHandlerForTest(repository)
	router := gin.New()
	router.GET("/admin/model-traces", handler.List)

	request := httptest.NewRequest(http.MethodGet, "/admin/model-traces?user=dingrui%40szyuto.com&api_key=dingrui-key&session_id=conversation-42&upstream_model=gpt-5.6-terra", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("list response status=%d body=%s", response.Code, response.Body.String())
	}
	filter := repository.filter
	if filter.User != "dingrui@szyuto.com" || filter.APIKey != "dingrui-key" || filter.SessionID != "conversation-42" || filter.UpstreamModel != "gpt-5.6-terra" {
		t.Fatalf("parsed historical attribution filter=%#v", filter)
	}
}

// TestModelTraceHandlerConversationReturnsMetadataOnly verifies that the
// conversation endpoint serves only explicit turn indexes; individual bodies
// remain behind the selected-payload endpoint.
func TestModelTraceHandlerConversationReturnsMetadataOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &modelTraceQueryRepositoryStub{conversation: modeltrace.TraceConversation{
		CurrentTraceID: "trace-middle",
		Linked:         true,
		LinkSource:     "session_id",
		Turns: []modeltrace.TraceDetail{{
			Trace:    modeltrace.TraceSummary{TraceID: "trace-middle", SessionID: "conversation-42"},
			Payloads: []modeltrace.TracePayload{{Kind: modeltrace.PayloadKindClientRequest, CaptureStatus: modeltrace.CaptureStatusRedacted, Ciphertext: "must-not-leak"}},
		}},
	}}
	handler := newModelTraceHandlerForTest(repository)
	router := gin.New()
	router.GET("/admin/model-traces/:traceID/conversation", handler.Conversation)

	request := httptest.NewRequest(http.MethodGet, "/admin/model-traces/trace-middle/conversation", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "trace-middle") || strings.Contains(response.Body.String(), "must-not-leak") {
		t.Fatalf("conversation response status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestModelTraceHandlerConversationParsesReplayCursor verifies that the
// administrator API forwards a bounded direction and opaque cursor rather than
// asking a repository to infer or load a complete conversation.
func TestModelTraceHandlerConversationParsesReplayCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &modelTraceQueryRepositoryStub{conversation: modeltrace.TraceConversation{CurrentTraceID: "trace-current", Turns: []modeltrace.TraceDetail{}}}
	handler := newModelTraceHandlerForTest(repository)
	router := gin.New()
	router.GET("/admin/model-traces/:traceID/conversation", handler.Conversation)

	request := httptest.NewRequest(http.MethodGet, "/admin/model-traces/trace-current/conversation?direction=older&cursor=opaque-cursor&limit=500", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("conversation response status=%d body=%s", response.Code, response.Body.String())
	}
	if repository.pageRequest.Direction != "older" || repository.pageRequest.Cursor != "opaque-cursor" || repository.pageRequest.Limit != 50 {
		t.Fatalf("page request=%#v", repository.pageRequest)
	}
}

// TestModelTraceHandlerReadsOnlySelectedPayload verifies that detail returns
// metadata while the selected payload endpoint alone can decrypt safe content.
func TestModelTraceHandlerReadsOnlySelectedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &modelTraceQueryRepositoryStub{detail: modeltrace.TraceDetail{
		Trace: modeltrace.TraceSummary{TraceID: "trace-detail"},
		Payloads: []modeltrace.TracePayload{{
			Kind:          modeltrace.PayloadKindClientRequest,
			CaptureStatus: modeltrace.CaptureStatusRedacted,
		}},
	}, payload: modeltrace.TracePayload{Kind: modeltrace.PayloadKindClientRequest, CaptureStatus: modeltrace.CaptureStatusRedacted, Ciphertext: "ciphertext-canary"}}
	handler := newModelTraceHandlerForTest(repository)
	router := gin.New()
	router.GET("/admin/model-traces/:traceID", handler.Detail)
	router.GET("/admin/model-traces/:traceID/payloads/:kind", handler.Payload)

	request := httptest.NewRequest(http.MethodGet, "/admin/model-traces/trace-detail", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "[REDACTED]") || strings.Contains(response.Body.String(), "ciphertext-canary") {
		t.Fatalf("detail response status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/admin/model-traces/trace-detail/payloads/client_request", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "[REDACTED]") || strings.Contains(response.Body.String(), "ciphertext-canary") {
		t.Fatalf("payload response status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestModelTraceHandlerPayloadParsesChunkCursor verifies that a raw payload
// continuation reaches the bounded page service rather than re-reading page 0.
func TestModelTraceHandlerPayloadParsesChunkCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &modelTraceQueryRepositoryStub{payload: modeltrace.TracePayload{
		Kind: modeltrace.PayloadKindClientResponse, CaptureStatus: modeltrace.CaptureStatusRedacted, Ciphertext: "encrypted",
	}}
	handler := newModelTraceHandlerForTest(repository)
	router := gin.New()
	router.GET("/admin/model-traces/:traceID/payloads/:kind", handler.Payload)

	request := httptest.NewRequest(http.MethodGet, "/admin/model-traces/trace-body/payloads/client_response?chunk_no=4", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("payload response status=%d body=%s", response.Code, response.Body.String())
	}
	if repository.chunkNo != 4 || repository.maxBytes != 1024*1024 {
		t.Fatalf("payload request chunk=%d max=%d", repository.chunkNo, repository.maxBytes)
	}
}

// TestModelTraceHandlerReadsSelectedUpstreamPayload verifies that a raw-chain
// reader may decrypt only its chosen upstream error for the correct retry.
func TestModelTraceHandlerReadsSelectedUpstreamPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &modelTraceQueryRepositoryStub{payload: modeltrace.TracePayload{
		Kind:          modeltrace.PayloadKindUpstreamError,
		AttemptNo:     2,
		CaptureStatus: modeltrace.CaptureStatusRedacted,
		Ciphertext:    "upstream-error-ciphertext",
	}}
	handler := newModelTraceHandlerForTest(repository)
	router := gin.New()
	router.GET("/admin/model-traces/:traceID/payloads/:kind", handler.Payload)

	request := httptest.NewRequest(http.MethodGet, "/admin/model-traces/trace-retry/payloads/upstream_error?attempt_no=2", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "[REDACTED]") || strings.Contains(response.Body.String(), "upstream-error-ciphertext") {
		t.Fatalf("upstream payload response status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestModelTraceHandlerRecordsCopyOnlyForExistingPayload verifies that the
// browser can create a content-free copy audit event only after it selects an
// existing payload metadata record. The endpoint never returns body content.
func TestModelTraceHandlerRecordsCopyOnlyForExistingPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &modelTraceQueryRepositoryStub{detail: modeltrace.TraceDetail{
		Trace: modeltrace.TraceSummary{TraceID: "trace-copy"},
		Payloads: []modeltrace.TracePayload{{
			Kind:      modeltrace.PayloadKindClientRequest,
			AttemptNo: 0,
		}},
	}}
	handler := newModelTraceHandlerForTest(repository)
	router := gin.New()
	router.POST("/admin/model-traces/:traceID/access-events", handler.RecordAccessEvent)

	request := httptest.NewRequest(http.MethodPost, "/admin/model-traces/trace-copy/access-events", strings.NewReader(`{"action":"copy","kind":"client_request","attempt_no":0}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "ciphertext") || strings.Contains(response.Body.String(), "prompt") {
		t.Fatalf("copy event response status=%d body=%s", response.Code, response.Body.String())
	}

	missing := httptest.NewRequest(http.MethodPost, "/admin/model-traces/trace-copy/access-events", strings.NewReader(`{"action":"copy","kind":"client_response","attempt_no":0}`))
	missing.Header.Set("Content-Type", "application/json")
	missingResponse := httptest.NewRecorder()
	router.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing payload copy status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}
}

// TestModelTraceHandlerRequiresCleanupConfirmation verifies that a manual
// cleanup cannot reach deletion until an authenticated administrator supplies
// an explicit confirmation body.
func TestModelTraceHandlerRequiresCleanupConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanupRepository := &modelTraceCleanupRepositoryStub{}
	handler := newModelTraceHandlerForTest(&modelTraceQueryRepositoryStub{})
	handler.cleanupService = modeltrace.NewCleanupService(nil, cleanupRepository)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		c.Next()
	})
	router.POST("/admin/model-traces/cleanup", handler.RunCleanup)

	for _, testCase := range []struct {
		name          string
		body          string
		wantStatus    int
		wantRunStarts int
	}{
		{name: "missing confirmation", body: `{}`, wantStatus: http.StatusBadRequest, wantRunStarts: 0},
		{name: "negative confirmation", body: `{"confirm":false}`, wantStatus: http.StatusBadRequest, wantRunStarts: 0},
		{name: "explicit confirmation", body: `{"confirm":true}`, wantStatus: http.StatusOK, wantRunStarts: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/admin/model-traces/cleanup", strings.NewReader(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != testCase.wantStatus {
				t.Fatalf("cleanup status=%d body=%s, want %d", response.Code, response.Body.String(), testCase.wantStatus)
			}
			if cleanupRepository.startedRuns != testCase.wantRunStarts {
				t.Fatalf("cleanup starts=%d, want %d", cleanupRepository.startedRuns, testCase.wantRunStarts)
			}
			if testCase.wantStatus == http.StatusBadRequest && !strings.Contains(response.Body.String(), "model_call_trace_cleanup_confirmation_invalid") {
				t.Fatalf("invalid confirmation response=%s", response.Body.String())
			}
		})
	}
}
