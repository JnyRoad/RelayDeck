package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JnyRoad/RelayDeck/internal/modeltrace"
	infraerrors "github.com/JnyRoad/RelayDeck/internal/pkg/errors"
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

// modelTraceCleanupRequest requires an explicit destructive-action confirmation
// from the administrator interface before expired traces can be deleted.
type modelTraceCleanupRequest struct {
	Confirm bool `json:"confirm"`
}

// modelTraceAccessEventRequest identifies one metadata-selected payload for
// an administrator action without accepting any prompt or response content.
type modelTraceAccessEventRequest struct {
	Action    string                 `json:"action"`
	Kind      modeltrace.PayloadKind `json:"kind"`
	AttemptNo int                    `json:"attempt_no"`
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

// Detail 返回一条调用头和正文元数据；实际正文必须通过单个正文接口读取。
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

// Conversation returns one bounded protocol-confirmed replay window and its
// payload metadata. Administrators must still select each individual body page.
func (h *ModelTraceHandler) Conversation(c *gin.Context) {
	if h == nil || h.queryService == nil {
		response.InternalError(c, "Model trace query is unavailable")
		return
	}
	page, ok := parseModelTraceConversationPage(c)
	if !ok {
		return
	}
	conversation, err := h.queryService.ConversationPage(c.Request.Context(), c.Param("traceID"), page)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, conversation)
}

// parseModelTraceConversationPage validates only transport-safe replay paging
// inputs; service validation applies the final fixed fifty-turn maximum.
func parseModelTraceConversationPage(c *gin.Context) (modeltrace.ConversationPageRequest, bool) {
	page := modeltrace.ConversationPageRequest{
		Direction: strings.TrimSpace(c.Query("direction")),
		Cursor:    strings.TrimSpace(c.Query("cursor")),
	}
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			response.BadRequest(c, "Invalid limit")
			return modeltrace.ConversationPageRequest{}, false
		}
		page.Limit = limit
	}
	return page, true
}

// Payload 返回管理员明确选中的一种已加密正文，避免详情页一次读取全部密文。
func (h *ModelTraceHandler) Payload(c *gin.Context) {
	if h == nil || h.queryService == nil {
		response.InternalError(c, "Model trace query is unavailable")
		return
	}
	attemptNo := 0
	if raw := strings.TrimSpace(c.Query("attempt_no")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			response.BadRequest(c, "Invalid attempt_no")
			return
		}
		attemptNo = parsed
	}
	chunkNo := 0
	if raw := strings.TrimSpace(c.Query("chunk_no")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			response.BadRequest(c, "Invalid chunk_no")
			return
		}
		chunkNo = parsed
	}
	payload, err := h.queryService.PayloadPage(c.Request.Context(), c.Param("traceID"), modeltrace.PayloadKind(c.Param("kind")), attemptNo, chunkNo)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, payload)
}

// RecordAccessEvent validates a copy event against payload metadata only; the
// audit middleware records identifiers while excluding the request body.
func (h *ModelTraceHandler) RecordAccessEvent(c *gin.Context) {
	if h == nil || h.queryService == nil {
		response.InternalError(c, "Model trace query is unavailable")
		return
	}
	var request modelTraceAccessEventRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid model trace access event")
		return
	}
	if strings.TrimSpace(request.Action) != "copy" || request.AttemptNo < 0 {
		response.BadRequest(c, "Invalid model trace access event")
		return
	}
	middleware.SetAuditAction(c, "admin.model_traces.payload.copy")
	traceID := strings.TrimSpace(c.Param("traceID"))
	exists, err := h.queryService.HasPayloadMetadata(c.Request.Context(), traceID, request.Kind, request.AttemptNo)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !exists {
		response.BadRequest(c, "Model trace payload does not exist")
		return
	}
	middleware.SetAuditExtra(c, map[string]any{
		"trace_id":     traceID,
		"payload_kind": string(request.Kind),
		"attempt_no":   request.AttemptNo,
	})
	response.Success(c, gin.H{})
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
	middleware.SetAuditAction(c, "admin.model_traces.cleanup")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var request modelTraceCleanupRequest
	if err := c.ShouldBindJSON(&request); err != nil || !request.Confirm {
		response.ErrorFrom(c, infraerrors.BadRequest("model_call_trace_cleanup_confirmation_invalid", "Explicit cleanup confirmation is required"))
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
		TraceID:        strings.TrimSpace(c.Query("trace_id")),
		User:           strings.TrimSpace(c.Query("user")),
		APIKey:         strings.TrimSpace(c.Query("api_key")),
		RequestID:      strings.TrimSpace(c.Query("request_id")),
		SessionID:      strings.TrimSpace(c.Query("session_id")),
		Route:          strings.TrimSpace(c.Query("route")),
		RequestedModel: strings.TrimSpace(c.Query("requested_model")),
		UpstreamModel:  strings.TrimSpace(c.Query("upstream_model")),
		Protocol:       strings.TrimSpace(c.Query("protocol")),
		Outcome:        modeltrace.Outcome(strings.TrimSpace(c.Query("outcome"))),
		CaptureStatus:  modeltrace.CaptureStatus(strings.TrimSpace(c.Query("capture_status"))),
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
