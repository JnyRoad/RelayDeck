package modeltrace

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TraceConfig is the effective runtime policy for model call tracing.
type TraceConfig struct {
	Enabled               bool `json:"enabled"`
	PayloadCaptureEnabled bool `json:"payload_capture_enabled"`
	AutoCleanupEnabled    bool `json:"auto_cleanup_enabled"`
	RetentionDays         int  `json:"retention_days"`
}

// ConfigStore loads the current trace policy without exposing settings storage
// details to the recorder or the HTTP middleware.
type ConfigStore interface {
	Load(ctx context.Context) (TraceConfig, error)
}

// Encryptor protects already-sanitized bodies before they reach persistent storage.
type Encryptor interface {
	Encrypt(plaintext string) (string, error)
}

// TraceRecord is the persistent header created before a model handler runs.
type TraceRecord struct {
	TraceID   string
	RequestID string
	Route     string
	Protocol  string
	Method    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// PayloadRecord is one prepared encrypted or metadata-only trace payload.
type PayloadRecord struct {
	TraceID       string
	Kind          PayloadKind
	AttemptNo     int
	CaptureStatus CaptureStatus
	ContentType   string
	OriginalBytes int64
	StoredBytes   int64
	SHA256        string
	RedactionVer  int16
	Ciphertext    string
	Model         string
	CreatedAt     time.Time
}

// TraceFinishRecord is the terminal state persisted after model handling ends.
type TraceFinishRecord struct {
	TraceID string
	FinishInput
}

// Repository is the storage boundary shared by the recorder, query service,
// and cleanup service. It stores no plaintext values supplied by callers.
type Repository interface {
	CreateTrace(ctx context.Context, record TraceRecord) error
	CreatePayload(ctx context.Context, record PayloadRecord) error
	FinishTrace(ctx context.Context, record TraceFinishRecord) error
}

// Service turns middleware observations into safe, encrypted persistence work.
type Service struct {
	configStore ConfigStore
	repository  Repository
	encryptor   Encryptor
	now         func() time.Time
}

// NewService builds the recorder from explicit dependencies. It has no global
// state, so tests and alternate runtimes can supply isolated settings and storage.
func NewService(configStore ConfigStore, repository Repository, encryptor Encryptor) *Service {
	return &Service{
		configStore: configStore,
		repository:  repository,
		encryptor:   encryptor,
		now:         time.Now,
	}
}

// Start creates a lightweight trace header when the effective policy enables
// tracing. A disabled policy returns a no-op handle and performs no storage I/O.
func (s *Service) Start(ctx context.Context, input StartInput) (TraceHandle, error) {
	if s == nil || s.configStore == nil {
		return TraceHandle{}, nil
	}
	config, err := s.configStore.Load(ctx)
	if err != nil {
		return TraceHandle{}, fmt.Errorf("load model trace config: %w", err)
	}
	if !config.Enabled {
		return TraceHandle{}, nil
	}
	if config.RetentionDays < 1 || config.RetentionDays > 90 {
		return TraceHandle{}, fmt.Errorf("invalid model trace retention days: %d", config.RetentionDays)
	}
	if s.repository == nil {
		return TraceHandle{}, fmt.Errorf("model trace repository is unavailable")
	}

	createdAt := s.now().UTC()
	handle := TraceHandle{
		TraceID:               uuid.NewString(),
		Enabled:               true,
		PayloadCaptureEnabled: config.PayloadCaptureEnabled,
	}
	record := TraceRecord{
		TraceID:   handle.TraceID,
		RequestID: input.RequestID,
		Route:     input.Route,
		Protocol:  input.Protocol,
		Method:    input.Method,
		ExpiresAt: createdAt.AddDate(0, 0, config.RetentionDays),
		CreatedAt: createdAt,
	}
	if err := s.repository.CreateTrace(ctx, record); err != nil {
		return TraceHandle{}, fmt.Errorf("create model trace: %w", err)
	}
	return handle, nil
}

// RecordPayload sanitizes and encrypts a complete textual payload. A truncated
// payload is stored as metadata only so an invalid partial JSON prefix cannot leak secrets.
func (s *Service) RecordPayload(ctx context.Context, handle TraceHandle, input PayloadInput) error {
	if s == nil || !handle.Enabled || !handle.PayloadCaptureEnabled {
		return nil
	}
	if s.repository == nil {
		return fmt.Errorf("model trace repository is unavailable")
	}

	record := PayloadRecord{
		TraceID:       handle.TraceID,
		Kind:          input.Kind,
		AttemptNo:     0,
		ContentType:   input.ContentType,
		OriginalBytes: input.OriginalBytes,
		SHA256:        input.SHA256,
		RedactionVer:  1,
		CreatedAt:     s.now().UTC(),
	}
	if input.Truncated {
		record.CaptureStatus = CaptureStatusTruncated
		return s.repository.CreatePayload(ctx, record)
	}
	if !isTextTracePayload(input.ContentType) {
		record.CaptureStatus = CaptureStatusNotApplicable
		return s.repository.CreatePayload(ctx, record)
	}

	captured := CaptureForStorage(input.ContentType, input.Body, DefaultPayloadLimitBytes)
	record.CaptureStatus = captured.Status
	record.StoredBytes = captured.StoredBytes
	if record.OriginalBytes == 0 {
		record.OriginalBytes = captured.OriginalBytes
	}
	if record.SHA256 == "" {
		record.SHA256 = captured.SHA256
	}
	if s.encryptor == nil {
		return fmt.Errorf("model trace encryptor is unavailable")
	}
	ciphertext, err := s.encryptor.Encrypt(string(captured.Body))
	if err != nil {
		return fmt.Errorf("encrypt model trace payload: %w", err)
	}
	record.Ciphertext = ciphertext
	record.Model = payloadModel(captured.Body)
	return s.repository.CreatePayload(ctx, record)
}

// Finish persists the final HTTP outcome independently from payload persistence
// so a body capture failure cannot erase a completed call diagnostic record.
func (s *Service) Finish(ctx context.Context, handle TraceHandle, input FinishInput) error {
	if s == nil || !handle.Enabled {
		return nil
	}
	if s.repository == nil {
		return fmt.Errorf("model trace repository is unavailable")
	}
	return s.repository.FinishTrace(ctx, TraceFinishRecord{TraceID: handle.TraceID, FinishInput: input})
}

// isTextTracePayload permits only textual protocols that can be safely viewed
// by an administrator. Other media types are retained as metadata only.
func isTextTracePayload(contentType string) bool {
	if strings.TrimSpace(contentType) == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return strings.HasPrefix(mediaType, "text/") || mediaType == "application/json" ||
		strings.HasSuffix(mediaType, "+json") || mediaType == "application/x-ndjson"
}

// payloadModel 从已脱敏的完整 JSON 中提取有限长度的模型摘要，供列表筛选而非正文检索使用。
func payloadModel(body []byte) string {
	var value struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &value); err != nil {
		return ""
	}
	model := strings.TrimSpace(value.Model)
	if len([]rune(model)) > 200 {
		return string([]rune(model)[:200])
	}
	return model
}
