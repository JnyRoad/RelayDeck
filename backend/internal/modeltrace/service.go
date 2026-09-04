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

// Encryptor protects selected trace bodies before they reach persistent storage.
type Encryptor interface {
	Encrypt(plaintext string) (string, error)
}

// TraceRecord is the persistent header created before a model handler runs.
type TraceRecord struct {
	TraceID   string
	RequestID string
	Route     string
	Protocol  string
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
	StorageMode   string
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

// TraceAttemptRecord is the persisted metadata for one actual upstream
// dispatch. The payload rows use the same positive attempt number.
type TraceAttemptRecord struct {
	TraceID         string
	AttemptNo       int
	AccountID       *int64
	AccountSnapshot string
	UpstreamRoute   string
	UpstreamModel   string
	StartedAt       time.Time
}

// TraceAttemptFinishRecord is the terminal metadata update for an existing
// upstream attempt row.
type TraceAttemptFinishRecord struct {
	TraceID string
	UpstreamAttemptFinishInput
	CompletedAt time.Time
}

// AttemptRepository is an additive optional extension for storage adapters
// that support the per-dispatch table introduced by migration 232.
type AttemptRepository interface {
	CreateAttempt(ctx context.Context, record TraceAttemptRecord) error
	FinishAttempt(ctx context.Context, record TraceAttemptFinishRecord) error
}

const payloadChunkPlaintextBytes = 256 * 1024

// ChunkedPayloadRepository persists one payload header and independently
// encrypted fixed-size body chunks. A new header starts failed and is promoted
// only after every chunk and aggregate value has been committed.
type ChunkedPayloadRepository interface {
	CreateChunkedPayload(ctx context.Context, record PayloadRecord) (int64, error)
	AppendPayloadChunk(ctx context.Context, payloadID int64, chunkNo int, ciphertext string, storedBytes int64) error
	FinishChunkedPayload(ctx context.Context, payloadID int64, record PayloadRecord) error
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
	if err := ValidateTraceConfig(config); err != nil {
		return TraceHandle{}, err
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
		ExpiresAt: createdAt.AddDate(0, 0, config.RetentionDays),
		CreatedAt: createdAt,
	}
	if err := s.repository.CreateTrace(ctx, record); err != nil {
		return TraceHandle{}, fmt.Errorf("create model trace: %w", err)
	}
	return handle, nil
}

// RecordPayload encrypts a complete textual payload without body redaction, as
// explicitly approved for forensic replay. A truncated observation remains
// metadata-only because it cannot truthfully represent the full exchange.
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
		AttemptNo:     input.AttemptNo,
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

	record.CaptureStatus = CaptureStatusComplete
	if record.OriginalBytes == 0 {
		record.OriginalBytes = int64(len(input.Body))
	}
	if record.SHA256 == "" {
		record.SHA256 = hashPayload(input.Body)
	}
	record.StoredBytes = int64(len(input.Body))
	record.Model = payloadModel(input.Body)
	if s.encryptor == nil {
		return fmt.Errorf("model trace encryptor is unavailable")
	}
	if repository, ok := s.repository.(ChunkedPayloadRepository); ok && repository != nil {
		return s.recordChunkedPayload(ctx, repository, record, input.Body)
	}
	ciphertext, err := s.encryptor.Encrypt(string(input.Body))
	if err != nil {
		return fmt.Errorf("encrypt model trace payload: %w", err)
	}
	record.Ciphertext = ciphertext
	return s.repository.CreatePayload(ctx, record)
}

// recordChunkedPayload writes a complete selected body in bounded encrypted
// segments. The parent remains failed if creation, encryption, or any append
// fails, so readers cannot mistake a partial sequence for a complete body.
func (s *Service) recordChunkedPayload(ctx context.Context, repository ChunkedPayloadRepository, record PayloadRecord, body []byte) error {
	pending := record
	pending.CaptureStatus = CaptureStatusFailed
	pending.StorageMode = "chunked"
	pending.Ciphertext = ""
	payloadID, err := repository.CreateChunkedPayload(ctx, pending)
	if err != nil {
		return fmt.Errorf("create chunked model trace payload: %w", err)
	}
	for chunkNo, offset := 0, 0; offset < len(body); chunkNo, offset = chunkNo+1, offset+payloadChunkPlaintextBytes {
		end := offset + payloadChunkPlaintextBytes
		if end > len(body) {
			end = len(body)
		}
		ciphertext, encryptErr := s.encryptor.Encrypt(string(body[offset:end]))
		if encryptErr != nil {
			return fmt.Errorf("encrypt model trace payload chunk: %w", encryptErr)
		}
		if appendErr := repository.AppendPayloadChunk(ctx, payloadID, chunkNo, ciphertext, int64(end-offset)); appendErr != nil {
			return fmt.Errorf("append model trace payload chunk: %w", appendErr)
		}
	}
	record.StorageMode = "chunked"
	record.Ciphertext = ""
	if err := repository.FinishChunkedPayload(ctx, payloadID, record); err != nil {
		return fmt.Errorf("finish chunked model trace payload: %w", err)
	}
	return nil
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

// StartUpstreamAttempt persists one actual shared-transport dispatch when the
// concrete repository supports attempt rows. Callers intentionally ignore an
// error so tracing can never block a model response.
func (s *Service) StartUpstreamAttempt(ctx context.Context, handle TraceHandle, input UpstreamAttemptInput) error {
	if s == nil || !handle.Enabled {
		return nil
	}
	if input.AttemptNo < 1 {
		return fmt.Errorf("model trace upstream attempt number is invalid")
	}
	repository, ok := s.repository.(AttemptRepository)
	if !ok || repository == nil {
		return fmt.Errorf("model trace attempt repository is unavailable")
	}
	startedAt := input.StartedAt
	if startedAt.IsZero() {
		startedAt = s.now().UTC()
	}
	return repository.CreateAttempt(ctx, TraceAttemptRecord{
		TraceID:         handle.TraceID,
		AttemptNo:       input.AttemptNo,
		AccountID:       input.AccountID,
		AccountSnapshot: input.AccountSnapshot,
		UpstreamRoute:   input.UpstreamRoute,
		UpstreamModel:   input.UpstreamModel,
		StartedAt:       startedAt,
	})
}

// FinishUpstreamAttempt stores terminal attempt metadata without reading or
// modifying the attempt's encrypted request or response payload rows.
func (s *Service) FinishUpstreamAttempt(ctx context.Context, handle TraceHandle, input UpstreamAttemptFinishInput) error {
	if s == nil || !handle.Enabled {
		return nil
	}
	if input.AttemptNo < 1 {
		return fmt.Errorf("model trace upstream attempt number is invalid")
	}
	repository, ok := s.repository.(AttemptRepository)
	if !ok || repository == nil {
		return fmt.Errorf("model trace attempt repository is unavailable")
	}
	return repository.FinishAttempt(ctx, TraceAttemptFinishRecord{
		TraceID:                    handle.TraceID,
		UpstreamAttemptFinishInput: input,
		CompletedAt:                s.now().UTC(),
	})
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

// payloadModel 从完整 JSON 中提取有限长度的模型摘要，供列表筛选而非正文检索使用。
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
