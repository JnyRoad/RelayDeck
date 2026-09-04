package modeltrace

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// traceQueryRepositoryStub 返回确定的索引和正文记录，隔离管理端服务的安全转换逻辑。
type traceQueryRepositoryStub struct {
	items   []TraceSummary
	total   int64
	detail  TraceDetail
	payload TracePayload
	err     error
}

// ListTraces 返回测试预置的分页索引结果。
func (s traceQueryRepositoryStub) ListTraces(context.Context, TraceFilter, int, int) ([]TraceSummary, int64, error) {
	return s.items, s.total, s.err
}

// GetTrace 返回测试预置的单调用详情。
func (s traceQueryRepositoryStub) GetTrace(context.Context, string) (TraceDetail, error) {
	return s.detail, s.err
}

// GetPayload returns one ciphertext-bearing payload only when a caller opens
// that exact payload, mirroring the production repository boundary.
func (s traceQueryRepositoryStub) GetPayload(context.Context, string, PayloadKind, int) (TracePayload, error) {
	return s.payload, s.err
}

// traceDecryptorStub 模拟安全解密器，但不依赖实际持久化密文格式。
type traceDecryptorStub struct {
	plaintext string
	err       error
}

// Decrypt 返回预置明文或错误。
func (s traceDecryptorStub) Decrypt(string) (string, error) {
	return s.plaintext, s.err
}

// TestQueryServiceListsHeadersOnly 验证列表接口只返回轻量索引，不触发正文解密。
func TestQueryServiceListsHeadersOnly(t *testing.T) {
	now := time.Date(2026, time.September, 3, 10, 0, 0, 0, time.UTC)
	service := NewQueryService(traceQueryRepositoryStub{
		items: []TraceSummary{{TraceID: "trace-list", Route: "/v1/chat/completions", CreatedAt: now}},
		total: 1,
	}, traceDecryptorStub{plaintext: "must-not-decrypt"})

	items, total, err := service.List(context.Background(), TraceFilter{}, 1, 20)

	if err != nil {
		t.Fatalf("list traces: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].TraceID != "trace-list" {
		t.Fatalf("list result items=%#v total=%d", items, total)
	}
}

// TestQueryServiceHidesDecryptionFailure 验证单个正文无法解密时不泄露底层错误或密文。
func TestQueryServiceHidesDecryptionFailure(t *testing.T) {
	const canary = "ciphertext-canary-must-not-leak"
	service := NewQueryService(traceQueryRepositoryStub{payload: TracePayload{
		Kind:          PayloadKindClientRequest,
		CaptureStatus: CaptureStatusRedacted,
		Ciphertext:    canary,
	}}, traceDecryptorStub{err: errors.New("decrypt failed for " + canary)})

	payload, err := service.Payload(context.Background(), "trace-detail", PayloadKindClientRequest, 0)

	if err != nil {
		t.Fatalf("get payload: %v", err)
	}
	if payload.Content != "" || payload.ContentStatus != "unavailable" || payload.Ciphertext != "" {
		t.Fatalf("unsafe decrypted payload = %#v", payload)
	}
}

// TestQueryServiceReturnsSafeDecryptedContent 验证已通过入库前脱敏的正文可在详情中按需呈现。
func TestQueryServiceReturnsSafeDecryptedContent(t *testing.T) {
	service := NewQueryService(traceQueryRepositoryStub{payload: TracePayload{
		Kind:          PayloadKindClientResponse,
		CaptureStatus: CaptureStatusRedacted,
		Ciphertext:    "encrypted",
	}}, traceDecryptorStub{plaintext: `{"message":"[REDACTED]"}`})

	payload, err := service.Payload(context.Background(), "trace-content", PayloadKindClientResponse, 0)

	if err != nil {
		t.Fatalf("get payload: %v", err)
	}
	if payload.Content != `{"message":"[REDACTED]"}` || payload.ContentStatus != "available" || payload.Ciphertext != "" {
		t.Fatalf("safe detail payload = %#v", payload)
	}
}

// TestQueryServiceReadsOnlySelectedPayload verifies that opening the detail
// header cannot decrypt every stored request and response payload at once.
func TestQueryServiceReadsOnlySelectedPayload(t *testing.T) {
	service := NewQueryService(traceQueryRepositoryStub{
		detail: TraceDetail{
			Trace:    TraceSummary{TraceID: "trace-header"},
			Payloads: []TracePayload{{Kind: PayloadKindClientRequest, CaptureStatus: CaptureStatusRedacted}, {Kind: PayloadKindClientResponse, CaptureStatus: CaptureStatusRedacted}},
		},
		payload: TracePayload{Kind: PayloadKindClientResponse, AttemptNo: 0, CaptureStatus: CaptureStatusRedacted, Ciphertext: "selected-ciphertext"},
	}, traceDecryptorStub{plaintext: `{"message":"[REDACTED]"}`})

	detail, err := service.Detail(context.Background(), "trace-header")
	if err != nil {
		t.Fatalf("get trace detail: %v", err)
	}
	if detail.Payloads[0].Content != "" || detail.Payloads[1].Content != "" {
		t.Fatalf("detail unexpectedly contained body content: %#v", detail.Payloads)
	}
	payload, err := service.Payload(context.Background(), "trace-header", PayloadKindClientResponse, 0)
	if err != nil {
		t.Fatalf("get selected payload: %v", err)
	}
	if payload.Content != `{"message":"[REDACTED]"}` || payload.Ciphertext != "" || payload.ContentStatus != "available" {
		t.Fatalf("selected payload = %#v", payload)
	}
}

// TestModelTraceWhereSupportsDocumentedFilters verifies that every documented
// index-only filter becomes a parameterized predicate without opening payloads.
func TestModelTraceWhereSupportsDocumentedFilters(t *testing.T) {
	where, arguments := modelTraceWhere(TraceFilter{
		TraceID:       "trace-filter",
		Protocol:      "websocket",
		CaptureStatus: CaptureStatusTruncated,
	})

	for _, clause := range []string{"t.trace_id=$1", "t.protocol=$2", "(t.request_capture_status=$3 OR t.response_capture_status=$3)"} {
		if !strings.Contains(where, clause) {
			t.Fatalf("where clause = %q, missing %q", where, clause)
		}
	}
	if len(arguments) != 3 || arguments[0] != "trace-filter" || arguments[1] != "websocket" || arguments[2] != "truncated" {
		t.Fatalf("filter arguments = %#v", arguments)
	}
}
