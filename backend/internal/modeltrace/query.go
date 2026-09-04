package modeltrace

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	maxConversationTurnPageSize     = 50
	maxPayloadPagePlaintextBytes    = 1024 * 1024
	defaultConversationTurnPageSize = maxConversationTurnPageSize
)

// TraceFilter 定义管理端列表可用的轻量索引筛选条件，不包含任何正文搜索字段。
type TraceFilter struct {
	TraceID        string
	UserID         *int64
	APIKeyID       *int64
	GroupID        *int64
	AccountID      *int64
	User           string
	APIKey         string
	RequestID      string
	SessionID      string
	Route          string
	RequestedModel string
	UpstreamModel  string
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
	UserSnapshot          string        `json:"user_snapshot"`
	APIKeySnapshot        string        `json:"api_key_snapshot"`
	GroupSnapshot         string        `json:"group_snapshot"`
	AccountSnapshot       string        `json:"account_snapshot"`
	SessionID             string        `json:"session_id"`
	PreviousResponseID    string        `json:"previous_response_id"`
	ResponseID            string        `json:"response_id"`
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
	StorageMode   string        `json:"storage_mode"`
	NextChunkNo   *int          `json:"next_chunk_no,omitempty"`
	Ciphertext    string        `json:"-"`
}

// TraceAttempt is one ordered upstream dispatch metadata record. It contains
// neither headers nor body text; the matching selected payload remains an
// independent, on-demand read.
type TraceAttempt struct {
	AttemptNo       int        `json:"attempt_no"`
	AccountID       *int64     `json:"account_id,omitempty"`
	AccountSnapshot string     `json:"account_snapshot"`
	UpstreamRoute   string     `json:"upstream_route"`
	UpstreamModel   string     `json:"upstream_model"`
	Outcome         Outcome    `json:"outcome"`
	StatusCode      *int       `json:"status_code,omitempty"`
	ErrorStage      string     `json:"error_stage"`
	ErrorCode       string     `json:"error_code"`
	DurationMS      *int       `json:"duration_ms,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

// TraceDetail combines one call index, ordered upstream attempt metadata, and
// body metadata. It never contains ciphertext or content until a body is read.
type TraceDetail struct {
	Trace    TraceSummary   `json:"trace"`
	Attempts []TraceAttempt `json:"attempts"`
	Payloads []TracePayload `json:"payloads"`
}

// TraceConversation contains only calls joined by one explicit client session
// identifier or exact Responses API parent-child lineage.
type TraceConversation struct {
	CurrentTraceID string        `json:"current_trace_id"`
	Linked         bool          `json:"linked"`
	LinkSource     string        `json:"link_source"`
	Turns          []TraceDetail `json:"turns"`
	OlderCursor    string        `json:"older_cursor,omitempty"`
	NewerCursor    string        `json:"newer_cursor,omitempty"`
}

// ConversationPageRequest selects a chronological conversation window. The
// initial request has no direction or cursor; follow-up requests must use the
// opaque cursor returned for the requested side.
type ConversationPageRequest struct {
	Direction string
	Cursor    string
	Limit     int
}

// TraceQueryRepository 是查询索引和密文的只读存储边界。
type TraceQueryRepository interface {
	ListTraces(ctx context.Context, filter TraceFilter, page, pageSize int) ([]TraceSummary, int64, error)
	GetTrace(ctx context.Context, traceID string) (TraceDetail, error)
	GetPayload(ctx context.Context, traceID string, kind PayloadKind, attemptNo int) (TracePayload, error)
}

// TraceConversationRepository is an optional read boundary for repositories
// that can resolve protocol-confirmed replay turns without loading bodies.
type TraceConversationRepository interface {
	GetConversation(ctx context.Context, traceID string) (TraceConversation, error)
}

// TraceConversationPageRepository is an optional capability for repositories
// that can page a replay without materializing every linked trace in memory.
type TraceConversationPageRepository interface {
	GetConversationPage(ctx context.Context, traceID string, page ConversationPageRequest) (TraceConversation, error)
}

// TracePayloadPageRepository is an optional capability for repositories that
// can return bounded encrypted body chunks instead of one full ciphertext.
type TracePayloadPageRepository interface {
	GetPayloadPage(ctx context.Context, traceID string, kind PayloadKind, attemptNo, chunkNo, maxPlaintextBytes int) (TracePayloadPage, error)
}

// TracePayloadPage keeps one payload header separate from the ciphertext
// segments required for the requested bounded plaintext page.
type TracePayloadPage struct {
	Payload     TracePayload
	Ciphertexts []string
	NextChunkNo *int
}

// Decryptor 只在管理员查看单条详情时按需解开已加密的正文。
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

// Conversation returns the initial centered replay page and leaves all payload
// ciphertext unread until the administrator selects an individual body.
func (s *QueryService) Conversation(ctx context.Context, traceID string) (TraceConversation, error) {
	return s.ConversationPage(ctx, traceID, ConversationPageRequest{})
}

// ConversationPage returns one bounded protocol-confirmed replay window. It
// delegates cursor semantics to capable repositories and never lets a legacy
// repository turn one response into an unbounded history read.
func (s *QueryService) ConversationPage(ctx context.Context, traceID string, page ConversationPageRequest) (TraceConversation, error) {
	if s == nil || s.repository == nil {
		return TraceConversation{}, fmt.Errorf("model trace query repository is unavailable")
	}
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return TraceConversation{}, fmt.Errorf("model trace id is required")
	}
	normalizedPage, err := normalizeConversationPageRequest(page)
	if err != nil {
		return TraceConversation{}, err
	}
	conversation, err := s.readConversationPage(ctx, traceID, normalizedPage)
	if err != nil {
		return TraceConversation{}, err
	}
	for turnIndex := range conversation.Turns {
		for payloadIndex := range conversation.Turns[turnIndex].Payloads {
			payload := &conversation.Turns[turnIndex].Payloads[payloadIndex]
			payload.ContentStatus = payloadContentStatus(*payload)
			payload.Ciphertext = ""
		}
	}
	if len(conversation.Turns) > normalizedPage.Limit {
		conversation.Turns = conversation.Turns[:normalizedPage.Limit]
	}
	return conversation, nil
}

// readConversationPage chooses the paged repository capability when present.
// Legacy repositories remain usable only for the initial page because they
// cannot honor a cursor without repeating or omitting replay turns.
func (s *QueryService) readConversationPage(ctx context.Context, traceID string, page ConversationPageRequest) (TraceConversation, error) {
	if repository, ok := s.repository.(TraceConversationPageRepository); ok {
		return repository.GetConversationPage(ctx, traceID, page)
	}
	if page.Direction != "" || page.Cursor != "" {
		return TraceConversation{}, fmt.Errorf("model trace conversation paging is unavailable")
	}
	repository, ok := s.repository.(TraceConversationRepository)
	if !ok {
		return TraceConversation{}, fmt.Errorf("model trace conversation query is unavailable")
	}
	return repository.GetConversation(ctx, traceID)
}

// normalizeConversationPageRequest validates public cursor inputs and applies
// the fixed maximum that protects both the database batch and browser replay.
func normalizeConversationPageRequest(page ConversationPageRequest) (ConversationPageRequest, error) {
	page.Direction = strings.TrimSpace(page.Direction)
	page.Cursor = strings.TrimSpace(page.Cursor)
	if page.Direction != "" && page.Direction != "older" && page.Direction != "newer" {
		return ConversationPageRequest{}, fmt.Errorf("model trace conversation direction is invalid")
	}
	if page.Direction == "" && page.Cursor != "" {
		return ConversationPageRequest{}, fmt.Errorf("model trace conversation cursor direction is required")
	}
	if page.Direction != "" && page.Cursor == "" {
		return ConversationPageRequest{}, fmt.Errorf("model trace conversation cursor is required")
	}
	if page.Limit < 1 {
		page.Limit = defaultConversationTurnPageSize
	}
	if page.Limit > maxConversationTurnPageSize {
		page.Limit = maxConversationTurnPageSize
	}
	return page, nil
}

// Payload returns the first bounded page of one administrator-selected body.
// Callers with a continuation cursor must use PayloadPage instead.
func (s *QueryService) Payload(ctx context.Context, traceID string, kind PayloadKind, attemptNo int) (TracePayload, error) {
	return s.PayloadPage(ctx, traceID, kind, attemptNo, 0)
}

// PayloadPage decrypts exactly one selected bounded body page after the detail
// header identified its kind and attempt number; it never reads sibling bodies.
func (s *QueryService) PayloadPage(ctx context.Context, traceID string, kind PayloadKind, attemptNo, chunkNo int) (TracePayload, error) {
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
	if attemptNo < 0 || chunkNo < 0 {
		return TracePayload{}, fmt.Errorf("model trace payload attempt number is invalid")
	}
	if repository, ok := s.repository.(TracePayloadPageRepository); ok {
		page, err := repository.GetPayloadPage(ctx, traceID, kind, attemptNo, chunkNo, maxPayloadPagePlaintextBytes)
		if err != nil {
			return TracePayload{}, err
		}
		return s.decryptPayloadPage(page)
	}
	if chunkNo != 0 {
		return TracePayload{}, fmt.Errorf("model trace payload paging is unavailable")
	}
	payload, err := s.repository.GetPayload(ctx, traceID, kind, attemptNo)
	if err != nil {
		return TracePayload{}, err
	}
	return s.decryptPayloadPage(TracePayloadPage{Payload: payload, Ciphertexts: []string{payload.Ciphertext}})
}

// decryptPayloadPage turns only the repository-selected ciphertext segments
// into one bounded plaintext response and suppresses all decryptor failures.
func (s *QueryService) decryptPayloadPage(page TracePayloadPage) (TracePayload, error) {
	payload := page.Payload
	payload.ContentStatus = payloadContentStatus(payload)
	payload.NextChunkNo = page.NextChunkNo
	if len(page.Ciphertexts) == 0 || payload.ContentStatus != "available" {
		payload.Ciphertext = ""
		return payload, nil
	}
	if s.decryptor == nil {
		payload.Ciphertext = ""
		payload.ContentStatus = "unavailable"
		return payload, nil
	}
	var content strings.Builder
	for _, ciphertext := range page.Ciphertexts {
		if ciphertext == "" {
			continue
		}
		plaintext, decryptErr := s.decryptor.Decrypt(ciphertext)
		if decryptErr != nil || content.Len()+len(plaintext) > maxPayloadPagePlaintextBytes {
			payload.Ciphertext = ""
			payload.ContentStatus = "unavailable"
			return payload, nil
		}
		_, _ = content.WriteString(plaintext)
	}
	payload.Ciphertext = ""
	payload.Content = content.String()
	payload.ContentStatus = "available"
	return payload, nil
}

// HasPayloadMetadata confirms a selected payload exists without requesting,
// decrypting, or returning its ciphertext or body content.
func (s *QueryService) HasPayloadMetadata(ctx context.Context, traceID string, kind PayloadKind, attemptNo int) (bool, error) {
	if s == nil || s.repository == nil {
		return false, fmt.Errorf("model trace query repository is unavailable")
	}
	traceID = strings.TrimSpace(traceID)
	if traceID == "" || !isStoredPayloadKind(kind) || attemptNo < 0 {
		return false, nil
	}
	detail, err := s.Detail(ctx, traceID)
	if err != nil {
		return false, err
	}
	for _, payload := range detail.Payloads {
		if payload.Kind == kind && payload.AttemptNo == attemptNo {
			return true, nil
		}
	}
	return false, nil
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
	return isStoredPayloadKind(kind)
}

// isStoredPayloadKind accepts every representation that may be attached to a
// trace, including upstream kinds that become readable in the raw-chain view.
func isStoredPayloadKind(kind PayloadKind) bool {
	switch kind {
	case PayloadKindClientRequest, PayloadKindClientResponse, PayloadKindErrorResponse,
		PayloadKindUpstreamRequest, PayloadKindUpstreamResponse, PayloadKindUpstreamError:
		return true
	default:
		return false
	}
}
