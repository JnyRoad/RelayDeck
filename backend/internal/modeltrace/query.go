package modeltrace

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TraceFilter 定义管理端列表可用的轻量索引筛选条件，不包含任何正文搜索字段。
type TraceFilter struct {
	TraceID        string
	UserID         *int64
	APIKeyID       *int64
	GroupID        *int64
	AccountID      *int64
	RequestID      string
	Route          string
	RequestedModel string
	Protocol       string
	Outcome        Outcome
	CaptureStatus  CaptureStatus
	StartAt        *time.Time
	EndAt          *time.Time
}

// TraceSummary 是列表和详情共用的模型调用索引字段，不含正文或密文。
type TraceSummary struct {
	TraceID               string        `json:"trace_id"`
	RequestID             string        `json:"request_id"`
	UserID                *int64        `json:"user_id,omitempty"`
	APIKeyID              *int64        `json:"api_key_id,omitempty"`
	GroupID               *int64        `json:"group_id,omitempty"`
	AccountID             *int64        `json:"account_id,omitempty"`
	Route                 string        `json:"route"`
	Protocol              string        `json:"protocol"`
	RequestedModel        string        `json:"requested_model"`
	UpstreamModel         string        `json:"upstream_model"`
	ResponseModel         string        `json:"response_model"`
	Outcome               Outcome       `json:"outcome"`
	StatusCode            *int          `json:"status_code,omitempty"`
	Stream                bool          `json:"stream"`
	DurationMS            *int          `json:"duration_ms,omitempty"`
	FirstByteMS           *int          `json:"first_byte_ms,omitempty"`
	RequestCaptureStatus  CaptureStatus `json:"request_capture_status"`
	ResponseCaptureStatus CaptureStatus `json:"response_capture_status"`
	RequestBytes          int64         `json:"request_bytes"`
	ResponseBytes         int64         `json:"response_bytes"`
	ExpiresAt             time.Time     `json:"expires_at"`
	CreatedAt             time.Time     `json:"created_at"`
	CompletedAt           *time.Time    `json:"completed_at,omitempty"`
}

// TracePayload 是按需解密后可在详情页呈现的单种正文与安全元数据。
type TracePayload struct {
	Kind          PayloadKind   `json:"kind"`
	AttemptNo     int           `json:"attempt_no"`
	CaptureStatus CaptureStatus `json:"capture_status"`
	ContentType   string        `json:"content_type"`
	OriginalBytes int64         `json:"original_bytes"`
	StoredBytes   int64         `json:"stored_bytes"`
	SHA256        string        `json:"sha256"`
	CreatedAt     time.Time     `json:"created_at"`
	Content       string        `json:"content,omitempty"`
	ContentStatus string        `json:"content_status"`
	Ciphertext    string        `json:"-"`
}

// TraceDetail 组合一条调用索引及其按需展示的客户端可见正文。
type TraceDetail struct {
	Trace    TraceSummary   `json:"trace"`
	Payloads []TracePayload `json:"payloads"`
}

// TraceQueryRepository 是查询索引和密文的只读存储边界。
type TraceQueryRepository interface {
	ListTraces(ctx context.Context, filter TraceFilter, page, pageSize int) ([]TraceSummary, int64, error)
	GetTrace(ctx context.Context, traceID string) (TraceDetail, error)
	GetPayload(ctx context.Context, traceID string, kind PayloadKind, attemptNo int) (TracePayload, error)
}

// Decryptor 只在管理员查看单条详情时按需解开已脱敏的正文。
type Decryptor interface {
	Decrypt(ciphertext string) (string, error)
}

// QueryService 为管理端提供索引查询与谨慎的按需正文解密。
type QueryService struct {
	repository TraceQueryRepository
	decryptor  Decryptor
}

// NewQueryService 使用显式只读仓库和解密器构建查询服务。
func NewQueryService(repository TraceQueryRepository, decryptor Decryptor) *QueryService {
	return &QueryService{repository: repository, decryptor: decryptor}
}

// List 返回模型调用的分页轻量索引，列表路径从不读取或解密正文。
func (s *QueryService) List(ctx context.Context, filter TraceFilter, page, pageSize int) ([]TraceSummary, int64, error) {
	if s == nil || s.repository == nil {
		return nil, 0, fmt.Errorf("model trace query repository is unavailable")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return s.repository.ListTraces(ctx, filter, page, pageSize)
}

// Detail 返回一条调用头和可读取的正文种类；它从不读取或解密正文密文。
func (s *QueryService) Detail(ctx context.Context, traceID string) (TraceDetail, error) {
	if s == nil || s.repository == nil {
		return TraceDetail{}, fmt.Errorf("model trace query repository is unavailable")
	}
	if strings.TrimSpace(traceID) == "" {
		return TraceDetail{}, fmt.Errorf("model trace id is required")
	}
	detail, err := s.repository.GetTrace(ctx, strings.TrimSpace(traceID))
	if err != nil {
		return TraceDetail{}, err
	}
	for index := range detail.Payloads {
		detail.Payloads[index].ContentStatus = payloadContentStatus(detail.Payloads[index])
		detail.Payloads[index].Ciphertext = ""
	}
	return detail, nil
}

// Payload decrypts exactly one administrator-selected payload after the
// detail header has already identified its kind and attempt number.
func (s *QueryService) Payload(ctx context.Context, traceID string, kind PayloadKind, attemptNo int) (TracePayload, error) {
	if s == nil || s.repository == nil {
		return TracePayload{}, fmt.Errorf("model trace query repository is unavailable")
	}
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return TracePayload{}, fmt.Errorf("model trace id is required")
	}
	if !isReadablePayloadKind(kind) {
		return TracePayload{}, fmt.Errorf("model trace payload kind is invalid")
	}
	if attemptNo < 0 {
		return TracePayload{}, fmt.Errorf("model trace payload attempt number is invalid")
	}
	payload, err := s.repository.GetPayload(ctx, traceID, kind, attemptNo)
	if err != nil {
		return TracePayload{}, err
	}
	payload.ContentStatus = payloadContentStatus(payload)
	if payload.Ciphertext == "" {
		return payload, nil
	}
	if s.decryptor == nil {
		payload.Ciphertext = ""
		payload.ContentStatus = "unavailable"
		return payload, nil
	}
	content, decryptErr := s.decryptor.Decrypt(payload.Ciphertext)
	payload.Ciphertext = ""
	if decryptErr != nil {
		payload.ContentStatus = "unavailable"
		return payload, nil
	}
	payload.Content = content
	payload.ContentStatus = "available"
	return payload, nil
}

// payloadContentStatus derives the UI-safe availability hint without loading
// ciphertext. Only complete or redacted records may have readable content.
func payloadContentStatus(payload TracePayload) string {
	switch payload.CaptureStatus {
	case CaptureStatusComplete, CaptureStatusRedacted:
		return "available"
	default:
		return "not_captured"
	}
}

// isReadablePayloadKind limits plaintext access to the client-visible body
// types promised by the administrator API contract.
func isReadablePayloadKind(kind PayloadKind) bool {
	switch kind {
	case PayloadKindClientRequest, PayloadKindClientResponse, PayloadKindErrorResponse:
		return true
	default:
		return false
	}
}
