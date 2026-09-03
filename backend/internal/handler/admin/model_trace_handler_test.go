package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JnyRoad/RelayDeck/internal/modeltrace"
	"github.com/gin-gonic/gin"
)

// modelTraceQueryRepositoryStub 为管理端处理器提供不含数据库 I/O 的追踪查询结果。
type modelTraceQueryRepositoryStub struct {
	items  []modeltrace.TraceSummary
	total  int64
	detail modeltrace.TraceDetail
	filter modeltrace.TraceFilter
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

	request := httptest.NewRequest(http.MethodGet, "/admin/model-traces?request_id=creq-list", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "trace-list") {
		t.Fatalf("list response status=%d body=%s", response.Code, response.Body.String())
	}
	if repository.filter.RequestID != "creq-list" {
		t.Fatalf("parsed filter=%#v", repository.filter)
	}
}

// TestModelTraceHandlerDetailReturnsSafeDecryptedPayload 验证详情可以展示安全正文但永不回传数据库密文。
func TestModelTraceHandlerDetailReturnsSafeDecryptedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &modelTraceQueryRepositoryStub{detail: modeltrace.TraceDetail{
		Trace: modeltrace.TraceSummary{TraceID: "trace-detail"},
		Payloads: []modeltrace.TracePayload{{
			Kind:          modeltrace.PayloadKindClientRequest,
			CaptureStatus: modeltrace.CaptureStatusRedacted,
			Ciphertext:    "ciphertext-canary",
		}},
	}}
	handler := newModelTraceHandlerForTest(repository)
	router := gin.New()
	router.GET("/admin/model-traces/:traceID", handler.Detail)

	request := httptest.NewRequest(http.MethodGet, "/admin/model-traces/trace-detail", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "[REDACTED]") || strings.Contains(response.Body.String(), "ciphertext-canary") {
		t.Fatalf("detail response status=%d body=%s", response.Code, response.Body.String())
	}
}
