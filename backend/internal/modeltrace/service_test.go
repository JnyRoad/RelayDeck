package modeltrace

import (
	"context"
	"strings"
	"testing"
	"time"
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
	traces          []TraceRecord
	payloads        []PayloadRecord
	chunkedPayloads []PayloadRecord
	chunks          []tracePayloadChunkStub
	finishes        []TraceFinishRecord
}

// tracePayloadChunkStub records one encrypted payload segment without binding
// the service behavior test to a database implementation.
type tracePayloadChunkStub struct {
	payloadID   int64
	chunkNo     int
	storedBytes int64
	ciphertext  string
}

// rejectingPayloadPersistenceScheduler simulates an already-saturated
// best-effort persistence queue without introducing storage I/O into a unit test.
type rejectingPayloadPersistenceScheduler struct{}

// Enqueue rejects every task so the stream must immediately abandon body capture
// while preserving the caller's completed transport write.
func (rejectingPayloadPersistenceScheduler) Enqueue(func()) bool { return false }

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

// CreateChunkedPayload records the metadata row that owns a sequence of
// encrypted chunks and returns a deterministic test-only primary key.
func (s *traceRepositoryStub) CreateChunkedPayload(_ context.Context, record PayloadRecord) (int64, error) {
	s.chunkedPayloads = append(s.chunkedPayloads, record)
	return int64(len(s.chunkedPayloads)), nil
}

// AppendPayloadChunk records one encrypted segment in its caller-supplied
// order so the service test can verify a full payload is split predictably.
func (s *traceRepositoryStub) AppendPayloadChunk(_ context.Context, payloadID int64, chunkNo int, ciphertext string, storedBytes int64) error {
	s.chunks = append(s.chunks, tracePayloadChunkStub{payloadID: payloadID, chunkNo: chunkNo, ciphertext: ciphertext, storedBytes: storedBytes})
	return nil
}

// FinishChunkedPayload records the final aggregate metadata for the chunked
// body without requiring a real storage adapter.
func (s *traceRepositoryStub) FinishChunkedPayload(_ context.Context, payloadID int64, record PayloadRecord) error {
	if payloadID < 1 || int(payloadID) > len(s.chunkedPayloads) {
		return nil
	}
	s.chunkedPayloads[payloadID-1] = record
	return nil
}

// FinishTrace records terminal call metadata without database I/O.
func (s *traceRepositoryStub) FinishTrace(_ context.Context, record TraceFinishRecord) error {
	s.finishes = append(s.finishes, record)
	return nil
}

// traceEncryptorStub exposes the plaintext passed into encryption so tests can
// verify the selected body reaches only the encrypted persistence boundary.
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

// TestServiceEncryptsUnredactedPayload verifies the approved forensic policy:
// complete body fields reach only the encrypted chunk boundary without semantic
// replacement, while credential-bearing headers remain outside this input path.
func TestServiceEncryptsUnredactedPayload(t *testing.T) {
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
	if encryptor.inputs[0] != `{"api_key":"payload-canary","message":"safe"}` {
		t.Fatalf("encryptor plaintext = %s, want the complete unredacted payload", encryptor.inputs[0])
	}
	if len(repository.chunkedPayloads) != 1 || len(repository.chunks) != 1 || repository.chunks[0].ciphertext != "ciphertext" {
		t.Fatalf("stored chunked payloads=%#v chunks=%#v, want one encrypted chunk", repository.chunkedPayloads, repository.chunks)
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

// TestServiceStoresLegacyPrefixSizedText verifies that ordinary text past the
// former fixed prefix remains complete until its configured trace expiry.
func TestServiceStoresLegacyPrefixSizedText(t *testing.T) {
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
		Body:        []byte(`{"message":"` + strings.Repeat("x", DefaultPayloadLimitBytes) + `"}`),
	})

	if err != nil {
		t.Fatalf("record capture-limited payload: %v", err)
	}
	if len(encryptor.inputs) < 2 {
		t.Fatalf("complete legacy-prefix payload chunks=%d, want multiple chunks", len(encryptor.inputs))
	}
	if len(repository.chunkedPayloads) != 1 || len(repository.chunks) != len(encryptor.inputs) {
		t.Fatalf("stored chunked payloads=%d chunks=%d, want one payload and matching chunks", len(repository.chunkedPayloads), len(repository.chunks))
	}
	stored := repository.chunkedPayloads[0]
	if stored.CaptureStatus == CaptureStatusTruncated || stored.StoredBytes <= DefaultPayloadLimitBytes {
		t.Fatalf("stored complete payload = %#v, want chunked text beyond legacy prefix", stored)
	}
}

// TestServiceStoresCompleteTextPastLegacyPrefix verifies that normal textual
// prompts are retained whole instead of silently becoming a 1 MiB prefix. The
// retention policy, rather than a prefix cap, bounds their database lifetime.
func TestServiceStoresCompleteTextPastLegacyPrefix(t *testing.T) {
	repository := &traceRepositoryStub{}
	encryptor := &traceEncryptorStub{}
	service := NewService(traceConfigStoreStub{config: TraceConfig{Enabled: true, PayloadCaptureEnabled: true, RetentionDays: 7}}, repository, encryptor)
	handle, err := service.Start(context.Background(), StartInput{Route: "/v1/responses"})
	if err != nil {
		t.Fatalf("start trace: %v", err)
	}
	body := []byte(`{"input":"` + strings.Repeat("x", DefaultPayloadLimitBytes+1) + `"}`)
	if err := service.RecordPayload(context.Background(), handle, PayloadInput{
		Kind:        PayloadKindClientRequest,
		ContentType: "application/json",
		Body:        body,
	}); err != nil {
		t.Fatalf("record complete payload: %v", err)
	}
	if len(encryptor.inputs) < 2 {
		t.Fatalf("encryptor inputs=%d, want a multi-chunk payload", len(encryptor.inputs))
	}
	for index, plaintext := range encryptor.inputs {
		if len(plaintext) > payloadChunkPlaintextBytes {
			t.Fatalf("chunk %d plaintext bytes=%d, want <=%d", index, len(plaintext), payloadChunkPlaintextBytes)
		}
	}
	if len(repository.chunkedPayloads) != 1 || repository.chunkedPayloads[0].CaptureStatus == CaptureStatusTruncated {
		t.Fatalf("stored chunked payload=%#v, want complete encrypted text", repository.chunkedPayloads)
	}
}

// TestServiceStoresLargeTextAsFixedEncryptedChunks proves a payload that grows
// beyond one chunk remains complete without being stored as one unbounded
// ciphertext value. Removing the chunked storage path must make this fail.
func TestServiceStoresLargeTextAsFixedEncryptedChunks(t *testing.T) {
	const chunkBytes = 256 * 1024
	repository := &traceRepositoryStub{}
	encryptor := &traceEncryptorStub{}
	service := NewService(traceConfigStoreStub{config: TraceConfig{Enabled: true, PayloadCaptureEnabled: true, RetentionDays: 7}}, repository, encryptor)
	handle, err := service.Start(context.Background(), StartInput{Route: "/v1/responses"})
	if err != nil {
		t.Fatalf("start trace: %v", err)
	}
	body := []byte(`{"input":"` + strings.Repeat("x", 2*chunkBytes+1) + `"}`)

	if err := service.RecordPayload(context.Background(), handle, PayloadInput{
		Kind:        PayloadKindClientRequest,
		ContentType: "application/json",
		Body:        body,
	}); err != nil {
		t.Fatalf("record payload: %v", err)
	}

	if len(repository.payloads) != 0 {
		t.Fatalf("inline payloads=%d, want 0 for chunked storage", len(repository.payloads))
	}
	if len(repository.chunkedPayloads) != 1 {
		t.Fatalf("chunked payload metadata=%d, want 1", len(repository.chunkedPayloads))
	}
	if len(repository.chunks) != 3 {
		t.Fatalf("encrypted chunks=%d, want 3", len(repository.chunks))
	}
	if len(encryptor.inputs) != 3 {
		t.Fatalf("encryptor inputs=%d, want 3", len(encryptor.inputs))
	}
	for index, plaintext := range encryptor.inputs {
		if len(plaintext) > chunkBytes {
			t.Fatalf("chunk %d plaintext bytes=%d, want <=%d", index, len(plaintext), chunkBytes)
		}
	}
	if got := strings.Join(encryptor.inputs, ""); got != string(body) {
		t.Fatalf("joined encrypted plaintext differs from original body")
	}
}

// TestChunkedPayloadStreamReportsOriginalWriteLength verifies that a tracing
// sink cannot change the byte count seen by its wrapped gateway transport.
// Returning a drained-buffer length would make the capture wrapper misreport a
// successful client or upstream write.
func TestChunkedPayloadStreamReportsOriginalWriteLength(t *testing.T) {
	repository := &traceRepositoryStub{}
	service := NewService(traceConfigStoreStub{config: TraceConfig{Enabled: true, PayloadCaptureEnabled: true, RetentionDays: 7}}, repository, &traceEncryptorStub{})
	handle, err := service.Start(context.Background(), StartInput{Route: "/v1/responses"})
	if err != nil {
		t.Fatalf("start trace: %v", err)
	}
	body := []byte(`{"input":"streamed"}`)
	stream := service.StartPayloadStream(context.Background(), handle, PayloadInput{Kind: PayloadKindClientRequest, ContentType: "application/json"})
	if stream == nil {
		t.Fatal("start payload stream returned nil")
	}

	written, writeErr := stream.Write(body)

	if writeErr != nil || written != len(body) {
		t.Fatalf("stream write=(%d, %v), want (%d, nil)", written, writeErr, len(body))
	}
}

// TestChunkedPayloadStreamDropsFurtherBytesAfterQueueFailure verifies that a
// saturated trace queue cannot retain another chunk or spin inside a gateway
// write after the payload has already become fail-closed.
func TestChunkedPayloadStreamDropsFurtherBytesAfterQueueFailure(t *testing.T) {
	repository := &traceRepositoryStub{}
	service := NewService(traceConfigStoreStub{config: TraceConfig{Enabled: true, PayloadCaptureEnabled: true, RetentionDays: 7}}, repository, &traceEncryptorStub{})
	handle, err := service.Start(context.Background(), StartInput{Route: "/v1/responses"})
	if err != nil {
		t.Fatalf("start trace: %v", err)
	}
	stream, ok := service.StartPayloadStream(context.Background(), handle, PayloadInput{Kind: PayloadKindClientRequest, ContentType: "application/json"}).(*chunkedPayloadStream)
	if !ok {
		t.Fatal("start payload stream returned a non-chunked stream")
	}
	stream.scheduler = rejectingPayloadPersistenceScheduler{}
	body := []byte(strings.Repeat("x", 2*payloadChunkPlaintextBytes+1))
	result := make(chan struct {
		written int
		err     error
	}, 1)
	go func() {
		written, writeErr := stream.Write(body)
		result <- struct {
			written int
			err     error
		}{written, writeErr}
	}()

	select {
	case outcome := <-result:
		if outcome.err != nil || outcome.written != len(body) {
			t.Fatalf("stream write=(%d, %v), want (%d, nil)", outcome.written, outcome.err, len(body))
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("stream write blocked after its persistence queue rejected a chunk")
	}
}

// TestChunkedPayloadStreamFinalizesDeliveredMetadata verifies that a response
// stream can apply the content type and error/result kind known only after the
// handler writes it. Without this, raw-chain replay would mislabel failures.
func TestChunkedPayloadStreamFinalizesDeliveredMetadata(t *testing.T) {
	repository := &traceRepositoryStub{}
	service := NewService(traceConfigStoreStub{config: TraceConfig{Enabled: true, PayloadCaptureEnabled: true, RetentionDays: 7}}, repository, &traceEncryptorStub{})
	handle, err := service.Start(context.Background(), StartInput{Route: "/v1/responses"})
	if err != nil {
		t.Fatalf("start trace: %v", err)
	}
	stream := service.StartPayloadStream(context.Background(), handle, PayloadInput{Kind: PayloadKindClientResponse})
	metadata, ok := stream.(interface {
		SetPayloadMetadata(PayloadKind, string)
	})
	if !ok {
		t.Fatal("chunked stream cannot receive delivered response metadata")
	}
	metadata.SetPayloadMetadata(PayloadKindErrorResponse, "application/json")
	if _, err := stream.Write([]byte(`{"error":"upstream failed"}`)); err != nil {
		t.Fatalf("stream write: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("stream close: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for len(repository.chunkedPayloads) != 1 || repository.chunkedPayloads[0].CaptureStatus != CaptureStatusComplete {
		if time.Now().After(deadline) {
			t.Fatalf("finalized chunked payloads=%#v", repository.chunkedPayloads)
		}
		time.Sleep(time.Millisecond)
	}
	stored := repository.chunkedPayloads[0]
	if stored.Kind != PayloadKindErrorResponse || stored.ContentType != "application/json" {
		t.Fatalf("finalized metadata=%#v, want error JSON", stored)
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
	if len(repository.chunkedPayloads) != 1 || repository.chunkedPayloads[0].Model != "gpt-trace-test" {
		t.Fatalf("stored payload summaries = %#v, want requested model", repository.chunkedPayloads)
	}
}

// TestServiceStoresPayloadAtItsActualUpstreamAttempt verifies that retry
// payloads retain their transport occurrence instead of being overwritten as
// attempt zero on the root trace.
func TestServiceStoresPayloadAtItsActualUpstreamAttempt(t *testing.T) {
	repository := &traceRepositoryStub{}
	service := NewService(traceConfigStoreStub{config: TraceConfig{Enabled: true, PayloadCaptureEnabled: true, RetentionDays: 7}}, repository, &traceEncryptorStub{})
	handle, err := service.Start(context.Background(), StartInput{Route: "/v1/responses"})
	if err != nil {
		t.Fatalf("start trace: %v", err)
	}

	err = service.RecordPayload(context.Background(), handle, PayloadInput{
		Kind:        PayloadKind("upstream_request"),
		AttemptNo:   2,
		ContentType: "application/json",
		Body:        []byte(`{"model":"fallback-model","input":"retry"}`),
	})

	if err != nil {
		t.Fatalf("record attempt payload: %v", err)
	}
	if len(repository.chunkedPayloads) != 1 || repository.chunkedPayloads[0].AttemptNo != 2 {
		t.Fatalf("stored attempt payloads = %#v, want attempt 2", repository.chunkedPayloads)
	}
}
