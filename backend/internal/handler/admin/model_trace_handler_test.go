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
	items   []modeltrace.TraceSummary
	total   int64
	detail  modeltrace.TraceDetail
	payload modeltrace.TracePayload
	filter  modeltrace.TraceFilter
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
