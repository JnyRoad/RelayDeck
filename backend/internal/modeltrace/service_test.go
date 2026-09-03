package modeltrace

import (
	"context"
	"strings"
	"testing"
)

// traceConfigStoreStub supplies one deterministic effective configuration to
// service tests without reading the settings repository.
type traceConfigStoreStub struct {
	config TraceConfig
	err    error
}

// Load returns the configured effective trace settings for the current test.
func (s traceConfigStoreStub) Load(context.Context) (TraceConfig, error) {
	return s.config, s.err
}

// traceRepositoryStub captures persistence requests so tests assert observable
// stored values instead of internal implementation details.
type traceRepositoryStub struct {
	traces   []TraceRecord
	payloads []PayloadRecord
	finishes []TraceFinishRecord
}

// CreateTrace records a header creation request without database I/O.
func (s *traceRepositoryStub) CreateTrace(_ context.Context, record TraceRecord) error {
	s.traces = append(s.traces, record)
	return nil
}

// CreatePayload records a prepared encrypted payload without database I/O.
func (s *traceRepositoryStub) CreatePayload(_ context.Context, record PayloadRecord) error {
	s.payloads = append(s.payloads, record)
	return nil
}

// FinishTrace records terminal call metadata without database I/O.
func (s *traceRepositoryStub) FinishTrace(_ context.Context, record TraceFinishRecord) error {
	s.finishes = append(s.finishes, record)
	return nil
}

// traceEncryptorStub exposes the plaintext passed into encryption so tests can
// prove sanitization runs before any encryptor or persistence boundary.
type traceEncryptorStub struct {
	inputs []string
}

// Encrypt records the safe plaintext and returns a non-reversible test token.
func (s *traceEncryptorStub) Encrypt(plaintext string) (string, error) {
	s.inputs = append(s.inputs, plaintext)
	return "ciphertext", nil
}

// TestServiceDisabledDoesNotCreateTrace verifies that the default-off switch
// prevents all trace writes while callers continue receiving a disabled handle.
func TestServiceDisabledDoesNotCreateTrace(t *testing.T) {
	repository := &traceRepositoryStub{}
	service := NewService(traceConfigStoreStub{config: TraceConfig{}}, repository, &traceEncryptorStub{})

	handle, err := service.Start(context.Background(), StartInput{Route: "/v1/chat/completions"})

	if err != nil {
		t.Fatalf("start trace: %v", err)
	}
	if handle.Enabled {
		t.Fatal("disabled trace configuration returned an enabled handle")
	}
	if len(repository.traces) != 0 {
		t.Fatalf("disabled trace created %d headers, want 0", len(repository.traces))
	}
}

// TestServiceSanitizesBeforeEncryptingPayload verifies that sensitive body data
// cannot reach either the encryptor input or the persisted ciphertext field.
func TestServiceSanitizesBeforeEncryptingPayload(t *testing.T) {
	repository := &traceRepositoryStub{}
	encryptor := &traceEncryptorStub{}
	service := NewService(traceConfigStoreStub{config: TraceConfig{Enabled: true, PayloadCaptureEnabled: true, RetentionDays: 7}}, repository, encryptor)
	handle, err := service.Start(context.Background(), StartInput{Route: "/v1/chat/completions"})
	if err != nil {
		t.Fatalf("start trace: %v", err)
	}

	err = service.RecordPayload(context.Background(), handle, PayloadInput{
		Kind:        PayloadKindClientRequest,
		ContentType: "application/json",
		Body:        []byte(`{"api_key":"payload-canary","message":"safe"}`),
	})

	if err != nil {
		t.Fatalf("record payload: %v", err)
	}
	if len(encryptor.inputs) != 1 {
		t.Fatalf("encryptor inputs = %d, want 1", len(encryptor.inputs))
	}
	if strings.Contains(encryptor.inputs[0], "payload-canary") {
		t.Fatalf("encryptor received raw credential: %s", encryptor.inputs[0])
	}
	if !strings.Contains(encryptor.inputs[0], "[REDACTED]") {
		t.Fatalf("encryptor plaintext = %s, want redaction marker", encryptor.inputs[0])
	}
	if len(repository.payloads) != 1 || repository.payloads[0].Ciphertext != "ciphertext" {
		t.Fatalf("stored payloads = %#v, want one encrypted payload", repository.payloads)
	}
}

// TestServiceNeverStoresTruncatedRawPrefix verifies that a partial JSON prefix
// cannot bypass structural redaction just because the complete document is absent.
func TestServiceNeverStoresTruncatedRawPrefix(t *testing.T) {
	repository := &traceRepositoryStub{}
	encryptor := &traceEncryptorStub{}
	service := NewService(traceConfigStoreStub{config: TraceConfig{Enabled: true, PayloadCaptureEnabled: true, RetentionDays: 7}}, repository, encryptor)
	handle, err := service.Start(context.Background(), StartInput{Route: "/v1/chat/completions"})
	if err != nil {
		t.Fatalf("start trace: %v", err)
	}

	err = service.RecordPayload(context.Background(), handle, PayloadInput{
		Kind:          PayloadKindClientRequest,
		ContentType:   "application/json",
		Body:          []byte(`{"api_key":"truncated-canary"`),
		OriginalBytes: 2 * DefaultPayloadLimitBytes,
		SHA256:        strings.Repeat("a", 64),
		Truncated:     true,
	})

	if err != nil {
		t.Fatalf("record truncated payload: %v", err)
	}
	if len(encryptor.inputs) != 0 {
		t.Fatalf("truncated payload reached encryptor: %#v", encryptor.inputs)
	}
	if len(repository.payloads) != 1 {
		t.Fatalf("stored payload count = %d, want 1", len(repository.payloads))
	}
	stored := repository.payloads[0]
	if stored.CaptureStatus != CaptureStatusTruncated || stored.Ciphertext != "" {
		t.Fatalf("stored truncated payload = %#v, want metadata only", stored)
	}
	if stored.SHA256 != strings.Repeat("a", 64) || stored.OriginalBytes != 2*DefaultPayloadLimitBytes {
		t.Fatalf("stored truncated metadata = %#v", stored)
	}
}

// TestServiceDerivesModelSummary 验证列表页所需模型摘要从完整 JSON 中提取，但不额外保存原始正文。
func TestServiceDerivesModelSummary(t *testing.T) {
	repository := &traceRepositoryStub{}
	service := NewService(traceConfigStoreStub{config: TraceConfig{Enabled: true, PayloadCaptureEnabled: true, RetentionDays: 7}}, repository, &traceEncryptorStub{})
	handle, err := service.Start(context.Background(), StartInput{Route: "/v1/chat/completions"})
	if err != nil {
		t.Fatalf("start trace: %v", err)
	}

	err = service.RecordPayload(context.Background(), handle, PayloadInput{
		Kind:        PayloadKindClientRequest,
		ContentType: "application/json",
		Body:        []byte(`{"model":"gpt-trace-test","messages":[]}`),
	})

	if err != nil {
		t.Fatalf("record payload: %v", err)
	}
	if len(repository.payloads) != 1 || repository.payloads[0].Model != "gpt-trace-test" {
		t.Fatalf("stored payload summaries = %#v, want requested model", repository.payloads)
	}
}
