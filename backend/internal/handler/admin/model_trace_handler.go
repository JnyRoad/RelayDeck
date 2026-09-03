package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JnyRoad/RelayDeck/internal/modeltrace"
	"github.com/JnyRoad/RelayDeck/internal/pkg/response"
	"github.com/JnyRoad/RelayDeck/internal/server/middleware"

	"github.com/gin-gonic/gin"
)

// ModelTraceHandler 提供模型调用追踪的配置、索引、按需正文和清理管理接口。
type ModelTraceHandler struct {
	queryService   *modeltrace.QueryService
	settings       *modeltrace.SettingsConfigStore
	cleanupService *modeltrace.CleanupService
}

// NewModelTraceHandler 以独立的查询、设置和清理依赖构建管理端处理器。
func NewModelTraceHandler(queryService *modeltrace.QueryService, settings *modeltrace.SettingsConfigStore, cleanupService *modeltrace.CleanupService) *ModelTraceHandler {
	return &ModelTraceHandler{
		queryService:   queryService,
		settings:       settings,
		cleanupService: cleanupService,
	}
}

// List 返回模型调用轻量索引；正文必须通过单条详情接口按需读取。
func (h *ModelTraceHandler) List(c *gin.Context) {
	if h == nil || h.queryService == nil {
		response.InternalError(c, "Model trace query is unavailable")
		return
	}
	filter, ok := parseModelTraceFilter(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.queryService.List(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

// Detail 返回一条调用及其已脱敏正文；路由层会在启用时加 Step-up 门控。
func (h *ModelTraceHandler) Detail(c *gin.Context) {
	if h == nil || h.queryService == nil {
		response.InternalError(c, "Model trace query is unavailable")
		return
	}
	detail, err := h.queryService.Detail(c.Request.Context(), c.Param("traceID"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, detail)
}

// GetConfig 返回当前有效追踪与保留期设置，不包含任何密钥或正文。
func (h *ModelTraceHandler) GetConfig(c *gin.Context) {
	if h == nil || h.settings == nil {
		response.InternalError(c, "Model trace settings are unavailable")
		return
	}
	config, err := h.settings.Load(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}

// UpdateConfig 保存模型追踪策略；路由层会在启用时加 Step-up 门控。
func (h *ModelTraceHandler) UpdateConfig(c *gin.Context) {
	if h == nil || h.settings == nil {
		response.InternalError(c, "Model trace settings are unavailable")
		return
	}
	var config modeltrace.TraceConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		response.BadRequest(c, "Invalid model trace config")
		return
	}
	if err := h.settings.Save(c.Request.Context(), config); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}

// PreviewCleanup 返回当前过期追踪的影响范围，供管理员在删除前确认。
func (h *ModelTraceHandler) PreviewCleanup(c *gin.Context) {
	if h == nil || h.cleanupService == nil {
		response.InternalError(c, "Model trace cleanup is unavailable")
		return
	}
	preview, err := h.cleanupService.Preview(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, preview)
}

// RunCleanup 执行管理员已确认的即时清理，并绑定当前认证操作者。
func (h *ModelTraceHandler) RunCleanup(c *gin.Context) {
	if h == nil || h.cleanupService == nil {
		response.InternalError(c, "Model trace cleanup is unavailable")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	result, err := h.cleanupService.RunManual(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// parseModelTraceFilter 将允许的 URL 参数转换为强类型索引筛选，拒绝模糊日期或非法关联键。
func parseModelTraceFilter(c *gin.Context) (modeltrace.TraceFilter, bool) {
	filter := modeltrace.TraceFilter{
		RequestID:      strings.TrimSpace(c.Query("request_id")),
		Route:          strings.TrimSpace(c.Query("route")),
		RequestedModel: strings.TrimSpace(c.Query("requested_model")),
		Outcome:        modeltrace.Outcome(strings.TrimSpace(c.Query("outcome"))),
	}
	for _, target := range []struct {
		query string
		set   func(*int64)
	}{
		{query: "user_id", set: func(value *int64) { filter.UserID = value }},
		{query: "api_key_id", set: func(value *int64) { filter.APIKeyID = value }},
		{query: "group_id", set: func(value *int64) { filter.GroupID = value }},
		{query: "account_id", set: func(value *int64) { filter.AccountID = value }},
	} {
		value := strings.TrimSpace(c.Query(target.query))
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			response.BadRequest(c, "Invalid "+target.query)
			return modeltrace.TraceFilter{}, false
		}
		target.set(&parsed)
	}
	for _, target := range []struct {
		query string
		set   func(*time.Time)
	}{
		{query: "start_time", set: func(value *time.Time) { filter.StartAt = value }},
		{query: "end_time", set: func(value *time.Time) { filter.EndAt = value }},
	} {
		value := strings.TrimSpace(c.Query(target.query))
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			response.BadRequest(c, "Invalid "+target.query+", expect RFC3339")
			return modeltrace.TraceFilter{}, false
		}
		target.set(&parsed)
	}
	return filter, true
}
