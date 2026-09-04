// Package modeltrace streams long trace bodies through bounded encrypted chunks
// while preserving the gateway's best-effort tracing failure boundary.
package modeltrace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"sync"
	"time"
)

const (
	payloadModelPrefixBytes          = 64 * 1024
	payloadChunkPersistenceQueueSize = 64
	payloadChunkPersistenceWait      = 5 * time.Second
)

var defaultPayloadChunkPersistenceScheduler = newAsyncUpstreamAttemptPersistenceScheduler(payloadChunkPersistenceQueueSize)

// chunkedPayloadStream accepts observed body bytes, retaining only one fixed
// plaintext chunk and a small model-summary prefix while queued work persists
// encrypted chunks in order. Its methods always preserve caller I/O semantics.
type chunkedPayloadStream struct {
	service    *Service
	repository ChunkedPayloadRepository
	traceID    string
	payloadID  int64
	input      PayloadInput
	context    context.Context
	scheduler  upstreamAttemptPersistenceScheduler

	mu            sync.Mutex
	buffer        []byte
	modelPrefix   []byte
	digest        hash.Hash
	total         int64
	stored        int64
	nextChunkNo   int
	failed        bool
	closed        bool
	captureStatus CaptureStatus
}

// StartPayloadStream opens a fail-closed chunked payload sink when the active
// recorder has all dependencies required for durable text capture. It returns
// nil for disabled, non-text, or unavailable storage so callers can take a
// bounded metadata-only fallback without affecting the gateway request.
func (s *Service) StartPayloadStream(ctx context.Context, handle TraceHandle, input PayloadInput) io.WriteCloser {
	if s == nil || !handle.Enabled || !handle.PayloadCaptureEnabled || s.repository == nil || s.encryptor == nil || !isTextTracePayload(input.ContentType) {
		return nil
	}
	repository, ok := s.repository.(ChunkedPayloadRepository)
	if !ok || repository == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	persistCtx, cancel := payloadChunkPersistenceContext(ctx)
	defer cancel()
	payloadID, err := repository.CreateChunkedPayload(persistCtx, PayloadRecord{
		TraceID:       handle.TraceID,
		Kind:          input.Kind,
		AttemptNo:     input.AttemptNo,
		CaptureStatus: CaptureStatusFailed,
		ContentType:   input.ContentType,
		RedactionVer:  1,
		StorageMode:   "chunked",
		CreatedAt:     s.now().UTC(),
	})
	if err != nil {
		return nil
	}
	return &chunkedPayloadStream{
		service:       s,
		repository:    repository,
		traceID:       handle.TraceID,
		payloadID:     payloadID,
		input:         input,
		context:       context.WithoutCancel(ctx),
		scheduler:     defaultPayloadChunkPersistenceScheduler,
		buffer:        make([]byte, 0, payloadChunkPlaintextBytes),
		digest:        sha256.New(),
		captureStatus: CaptureStatusComplete,
	}
}

// Write observes bytes already accepted by the wrapped transport and queues
// fixed-size encrypted segments. Queue or encryption failures mark the payload
// unreadable but still return the original byte count to the gateway caller.
func (s *chunkedPayloadStream) Write(body []byte) (int, error) {
	written := len(body)
	if s == nil || len(body) == 0 {
		return written, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.failed {
		return written, nil
	}
	_, _ = s.digest.Write(body)
	s.total += int64(len(body))
	s.captureModelPrefixLocked(body)
	for len(body) > 0 {
		remaining := payloadChunkPlaintextBytes - len(s.buffer)
		if remaining > len(body) {
			remaining = len(body)
		}
		s.buffer = append(s.buffer, body[:remaining]...)
		body = body[remaining:]
		if len(s.buffer) == payloadChunkPlaintextBytes {
			s.flushChunkLocked()
			if s.failed {
				// Once the best-effort queue rejects any chunk, retaining or
				// iterating over later bytes can only consume gateway memory/CPU.
				// The parent is already fail-closed, so preserve transport success
				// and abandon the rest of this observed write immediately.
				return written, nil
			}
		}
	}
	return written, nil
}

// Close queues the terminal metadata update after the final partial chunk. The
// parent row remains failed if any prior operation or this final enqueue fails.
func (s *chunkedPayloadStream) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.flushChunkLocked()
	if s.failed || s.scheduler == nil {
		return nil
	}
	record := PayloadRecord{
		TraceID:       s.traceID,
		Kind:          s.input.Kind,
		AttemptNo:     s.input.AttemptNo,
		CaptureStatus: s.captureStatus,
		ContentType:   s.input.ContentType,
		OriginalBytes: s.total,
		StoredBytes:   s.stored,
		SHA256:        hex.EncodeToString(s.digest.Sum(nil)),
		RedactionVer:  1,
		StorageMode:   "chunked",
		Model:         payloadModel(s.modelPrefix),
		CreatedAt:     s.service.now().UTC(),
	}
	if !s.scheduler.Enqueue(func() {
		s.mu.Lock()
		failed := s.failed
		s.mu.Unlock()
		if failed {
			return
		}
		persistCtx, cancel := payloadChunkPersistenceContext(s.context)
		defer cancel()
		if err := s.repository.FinishChunkedPayload(persistCtx, s.payloadID, record); err != nil {
			s.markFailed()
		}
	}) {
		s.failed = true
	}
	return nil
}

// SetPayloadMetadata applies the payload kind and MIME type determined after a
// response has been delivered. It is intentionally ignored after Close because
// queued chunks must retain one stable parent metadata snapshot.
func (s *chunkedPayloadStream) SetPayloadMetadata(kind PayloadKind, contentType string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if isStoredPayloadKind(kind) {
		s.input.Kind = kind
	}
	s.input.ContentType = contentType
}

// SetPayloadCaptureStatus applies a terminal readability status after a stream
// observes a malformed or non-text protocol event that makes its bytes unsafe
// to expose, while preserving the chunks for normal retention cleanup.
func (s *chunkedPayloadStream) SetPayloadCaptureStatus(status CaptureStatus) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.captureStatus = status
}

// captureModelPrefixLocked retains only enough leading JSON for the optional
// list summary; missing a late model field affects only that summary, never the
// stored body sequence.
func (s *chunkedPayloadStream) captureModelPrefixLocked(body []byte) {
	remaining := payloadModelPrefixBytes - len(s.modelPrefix)
	if remaining <= 0 {
		return
	}
	if remaining > len(body) {
		remaining = len(body)
	}
	s.modelPrefix = append(s.modelPrefix, body[:remaining]...)
}

// flushChunkLocked encrypts and enqueues one complete or final partial chunk
// while the caller holds the stream mutex, keeping the mutable buffer private.
func (s *chunkedPayloadStream) flushChunkLocked() {
	if s.failed || len(s.buffer) == 0 {
		return
	}
	plaintext := string(s.buffer)
	s.buffer = s.buffer[:0]
	ciphertext, err := s.service.encryptor.Encrypt(plaintext)
	if err != nil {
		s.failed = true
		return
	}
	chunkNo := s.nextChunkNo
	storedBytes := int64(len(plaintext))
	s.nextChunkNo++
	s.stored += storedBytes
	if s.scheduler == nil || !s.scheduler.Enqueue(func() {
		persistCtx, cancel := payloadChunkPersistenceContext(s.context)
		defer cancel()
		if err := s.repository.AppendPayloadChunk(persistCtx, s.payloadID, chunkNo, ciphertext, storedBytes); err != nil {
			s.markFailed()
		}
	}) {
		s.failed = true
	}
}

// markFailed changes only the local terminal state; the database header was
// created fail-closed and therefore needs no unsafe best-effort repair write.
func (s *chunkedPayloadStream) markFailed() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.failed = true
	s.mu.Unlock()
}

// payloadChunkPersistenceContext detaches trace storage from client cancellation
// and bounds each database operation without changing the transport result.
func payloadChunkPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), payloadChunkPersistenceWait)
}
