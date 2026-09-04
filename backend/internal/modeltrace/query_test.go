package modeltrace

import (
	"context"
	"errors"
	"fmt"
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

// traceConversationRepositoryStub adds an explicit replay result to the base
// query stub so conversation tests do not imply a database implementation.
type traceConversationRepositoryStub struct {
	traceQueryRepositoryStub
	conversation TraceConversation
}

// traceConversationPageRepositoryStub records page requests while returning an
// already bounded result, so query-service tests stay independent from SQL.
type traceConversationPageRepositoryStub struct {
	traceConversationRepositoryStub
	pageRequest ConversationPageRequest
}

// tracePayloadPageRepositoryStub returns only the chunks selected for one
// bounded body read and records the limit supplied by the query service.
type tracePayloadPageRepositoryStub struct {
	traceQueryRepositoryStub
	payloadPage TracePayloadPage
	chunkNo     int
	maxBytes    int
}

// GetConversation returns only the explicitly linked turns prepared by the test.
func (s traceConversationRepositoryStub) GetConversation(context.Context, string) (TraceConversation, error) {
	return s.conversation, s.err
}

// GetConversationPage records the requested direction and cursor for one test
// replay page without querying a database.
func (s *traceConversationPageRepositoryStub) GetConversationPage(_ context.Context, _ string, page ConversationPageRequest) (TraceConversation, error) {
	s.pageRequest = page
	return s.conversation, s.err
}

// GetPayloadPage records one continuation request without decrypting or
// expanding any sibling payload in the test repository.
func (s *tracePayloadPageRepositoryStub) GetPayloadPage(_ context.Context, _ string, _ PayloadKind, _ int, chunkNo, maxBytes int) (TracePayloadPage, error) {
	s.chunkNo = chunkNo
	s.maxBytes = maxBytes
	return s.payloadPage, s.err
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

// TestConversationHydrationSkipsDeletedTraceIDs keeps a page usable when a
// selected trace disappears after its bounded position query but before batch
// hydration completes.
func TestConversationHydrationSkipsDeletedTraceIDs(t *testing.T) {
	turns := orderedConversationTurns([]string{"trace-first", "trace-deleted", "trace-last"}, map[string]*TraceDetail{
		"trace-first": {Trace: TraceSummary{TraceID: "trace-first"}},
		"trace-last":  {Trace: TraceSummary{TraceID: "trace-last"}},
	})
	if len(turns) != 2 || turns[0].Trace.TraceID != "trace-first" || turns[1].Trace.TraceID != "trace-last" {
		t.Fatalf("hydrated turns=%#v, want surviving trace IDs in requested order", turns)
	}
}

// TestConversationHydrationRetainsLoadedCurrentTurn keeps the selected root
// visible when it was deleted after GetTrace but before page hydration.
func TestConversationHydrationRetainsLoadedCurrentTurn(t *testing.T) {
	current := TraceDetail{Trace: TraceSummary{
		TraceID:   "trace-current",
		CreatedAt: time.Date(2026, time.September, 3, 12, 1, 0, 0, time.UTC),
	}}
	positions := []conversationCursor{
		{CreatedAt: time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC), TraceID: "trace-first"},
		{CreatedAt: current.Trace.CreatedAt, TraceID: current.Trace.TraceID},
		{CreatedAt: time.Date(2026, time.September, 3, 12, 2, 0, 0, time.UTC), TraceID: "trace-last"},
	}
	turns := retainLoadedCurrentConversationTurn([]TraceDetail{
		{Trace: TraceSummary{TraceID: "trace-first", CreatedAt: positions[0].CreatedAt}},
		{Trace: TraceSummary{TraceID: "trace-last", CreatedAt: positions[2].CreatedAt}},
	}, positions, current)
	if len(turns) != 3 || turns[1].Trace.TraceID != current.Trace.TraceID {
		t.Fatalf("hydrated turns=%#v, want retained current turn in chronological position", turns)
	}
}

// TestConversationPageCursorsFallBackToPositions preserves a usable paging
// anchor when every selected trace is deleted before metadata hydration.
func TestConversationPageCursorsFallBackToPositions(t *testing.T) {
	positions := []conversationCursor{
		{CreatedAt: time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC), TraceID: "trace-first"},
		{CreatedAt: time.Date(2026, time.September, 3, 12, 1, 0, 0, time.UTC), TraceID: "trace-last"},
	}
	older, newer := conversationPageCursors(nil, positions, true, true)
	decodedOlder, olderErr := decodeConversationCursor(older)
	decodedNewer, newerErr := decodeConversationCursor(newer)
	if olderErr != nil || newerErr != nil || decodedOlder.TraceID != "trace-first" || decodedNewer.TraceID != "trace-last" {
		t.Fatalf("fallback cursors older=(%#v, %v) newer=(%#v, %v)", decodedOlder, olderErr, decodedNewer, newerErr)
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

// TestQueryServiceReturnsOnlyExplicitConversationTurns verifies that chat
// replay preserves the repository's exact lineage result and does not merge
// nearby records or decrypt any payload while building the replay index.
func TestQueryServiceReturnsOnlyExplicitConversationTurns(t *testing.T) {
	service := NewQueryService(traceConversationRepositoryStub{conversation: TraceConversation{
		CurrentTraceID: "trace-middle",
		Linked:         true,
		LinkSource:     "response_lineage",
		Turns: []TraceDetail{
			{Trace: TraceSummary{TraceID: "trace-first", ResponseID: "resp-first"}},
			{Trace: TraceSummary{TraceID: "trace-middle", PreviousResponseID: "resp-first", ResponseID: "resp-middle"}},
			{Trace: TraceSummary{TraceID: "trace-last", PreviousResponseID: "resp-middle"}},
		},
	}}, traceDecryptorStub{plaintext: "must-not-decrypt"})

	conversation, err := service.Conversation(context.Background(), "trace-middle")

	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if !conversation.Linked || conversation.CurrentTraceID != "trace-middle" || len(conversation.Turns) != 3 {
		t.Fatalf("conversation = %#v", conversation)
	}
	for _, turn := range conversation.Turns {
		for _, payload := range turn.Payloads {
			if payload.Content != "" || payload.Ciphertext != "" {
				t.Fatalf("conversation unexpectedly loaded payload content: %#v", payload)
			}
		}
	}
}

// TestQueryServiceLimitsLegacyConversationReplayTo50Turns verifies that even a
// repository without the page-read capability cannot make one administrator
// response contain an unbounded conversation history.
func TestQueryServiceLimitsLegacyConversationReplayTo50Turns(t *testing.T) {
	turns := make([]TraceDetail, 51)
	for index := range turns {
		turns[index] = TraceDetail{Trace: TraceSummary{TraceID: fmt.Sprintf("trace-%d", index)}}
	}
	service := NewQueryService(traceConversationRepositoryStub{conversation: TraceConversation{
		CurrentTraceID: "trace-50",
		Linked:         true,
		LinkSource:     "session_id",
		Turns:          turns,
	}}, traceDecryptorStub{})

	conversation, err := service.Conversation(context.Background(), "trace-50")

	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if len(conversation.Turns) != 50 {
		t.Fatalf("conversation turn count=%d, want 50", len(conversation.Turns))
	}
}

// TestQueryServiceRequestsBoundedConversationPages verifies that a follow-up
// replay request preserves its opaque cursor and never asks the repository for
// more than the supported fifty turns.
func TestQueryServiceRequestsBoundedConversationPages(t *testing.T) {
	repository := &traceConversationPageRepositoryStub{traceConversationRepositoryStub: traceConversationRepositoryStub{conversation: TraceConversation{
		CurrentTraceID: "trace-current",
		Linked:         true,
		LinkSource:     "session_id",
		Turns:          []TraceDetail{{Trace: TraceSummary{TraceID: "trace-next"}}},
	}}}
	service := NewQueryService(repository, traceDecryptorStub{})

	conversation, err := service.ConversationPage(context.Background(), "trace-current", ConversationPageRequest{
		Direction: "newer",
		Cursor:    "opaque-cursor",
		Limit:     200,
	})

	if err != nil {
		t.Fatalf("get conversation page: %v", err)
	}
	if conversation.CurrentTraceID != "trace-current" {
		t.Fatalf("conversation = %#v", conversation)
	}
	if repository.pageRequest.Direction != "newer" || repository.pageRequest.Cursor != "opaque-cursor" || repository.pageRequest.Limit != 50 {
		t.Fatalf("page request = %#v", repository.pageRequest)
	}
}

// TestConversationCursorRoundTripsStableOrdering verifies that paging cursors
// retain the chronological comparison fields while remaining opaque to the API
// consumer.
func TestConversationCursorRoundTripsStableOrdering(t *testing.T) {
	createdAt := time.Date(2026, time.September, 4, 1, 2, 3, 0, time.UTC)
	cursor, err := encodeConversationCursor(conversationCursor{CreatedAt: createdAt, TraceID: "trace-cursor"})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	if strings.Contains(cursor, "trace-cursor") {
		t.Fatalf("cursor leaks its internal fields: %q", cursor)
	}
	decoded, err := decodeConversationCursor(cursor)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if !decoded.CreatedAt.Equal(createdAt) || decoded.TraceID != "trace-cursor" {
		t.Fatalf("cursor = %#v", decoded)
	}
}

// TestQueryServiceDecryptsOnlyOneBoundedPayloadPage verifies that body replay
// decrypts the selected page's encrypted chunks and preserves its continuation
// cursor instead of loading the rest of a large payload.
func TestQueryServiceDecryptsOnlyOneBoundedPayloadPage(t *testing.T) {
	nextChunkNo := 8
	repository := &tracePayloadPageRepositoryStub{payloadPage: TracePayloadPage{
		Payload:     TracePayload{Kind: PayloadKindClientResponse, CaptureStatus: CaptureStatusComplete, StorageMode: "chunked"},
		Ciphertexts: []string{"first", "second"},
		NextChunkNo: &nextChunkNo,
	}}
	service := NewQueryService(repository, traceDecryptorSequenceStub{plaintexts: map[string]string{
		"first":  "part one ",
		"second": "part two",
	}})

	payload, err := service.PayloadPage(context.Background(), "trace-body", PayloadKindClientResponse, 0, 4)

	if err != nil {
		t.Fatalf("get payload page: %v", err)
	}
	if payload.Content != "part one part two" || payload.NextChunkNo == nil || *payload.NextChunkNo != nextChunkNo {
		t.Fatalf("payload = %#v", payload)
	}
	if repository.chunkNo != 4 || repository.maxBytes != maxPayloadPagePlaintextBytes {
		t.Fatalf("payload page request chunk=%d bytes=%d", repository.chunkNo, repository.maxBytes)
	}
}

// TestLoadConversationPagePositionsCentersAndContinues verifies that the first
// window includes the selected turn while directional windows retain the
// correct next-side availability without scanning a full conversation.
func TestLoadConversationPagePositionsCentersAndContinues(t *testing.T) {
	current := conversationCursor{CreatedAt: time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC), TraceID: "trace-current"}
	olderCandidates := makeConversationPositions(current.CreatedAt, "older", -30, 30)
	newerCandidates := makeConversationPositions(current.CreatedAt, "newer", 1, 30)
	olderCalls := 0
	newerCalls := 0
	listOlder := func(_ context.Context, _ conversationCursor, _ int) ([]conversationCursor, bool, error) {
		olderCalls++
		return olderCandidates, true, nil
	}
	listNewer := func(_ context.Context, _ conversationCursor, _ int) ([]conversationCursor, bool, error) {
		newerCalls++
		return newerCandidates, true, nil
	}

	positions, hasOlder, hasNewer, err := loadConversationPagePositions(context.Background(), current, ConversationPageRequest{Limit: 50}, listOlder, listNewer)

	if err != nil {
		t.Fatalf("load centered page: %v", err)
	}
	if olderCalls != 1 || newerCalls != 1 || len(positions) != 50 || positions[24] != current || !hasOlder || !hasNewer {
		t.Fatalf("centered positions=%#v older_calls=%d newer_calls=%d older=%t newer=%t", positions, olderCalls, newerCalls, hasOlder, hasNewer)
	}
	cursor, err := encodeConversationCursor(current)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	positions, hasOlder, hasNewer, err = loadConversationPagePositions(context.Background(), current, ConversationPageRequest{Direction: "older", Cursor: cursor, Limit: 50}, listOlder, listNewer)
	if err != nil {
		t.Fatalf("load older page: %v", err)
	}
	if len(positions) != len(olderCandidates) || !hasOlder || !hasNewer {
		t.Fatalf("older positions=%#v older=%t newer=%t", positions, hasOlder, hasNewer)
	}
}

// makeConversationPositions creates chronological position fixtures around one
// selected timestamp without tying paging behavior tests to database queries.
func makeConversationPositions(base time.Time, prefix string, firstOffset, count int) []conversationCursor {
	positions := make([]conversationCursor, 0, count)
	for offset := firstOffset; offset < firstOffset+count; offset++ {
		positions = append(positions, conversationCursor{
			CreatedAt: base.Add(time.Duration(offset) * time.Minute),
			TraceID:   fmt.Sprintf("trace-%s-%d", prefix, offset),
		})
	}
	return positions
}

// traceDecryptorSequenceStub maps a deterministic ciphertext test sequence to
// individual plaintext segments without coupling the assertion to encryption.
type traceDecryptorSequenceStub struct {
	plaintexts map[string]string
}

// Decrypt returns one plaintext segment or a deliberate unavailable error.
func (s traceDecryptorSequenceStub) Decrypt(ciphertext string) (string, error) {
	plaintext, ok := s.plaintexts[ciphertext]
	if !ok {
		return "", errors.New("unknown test ciphertext")
	}
	return plaintext, nil
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

// TestModelTraceWhereSearchesHistoricalAttributionSnapshots verifies that
// renamed or deleted users and API Keys remain searchable through their
// non-sensitive call-time display snapshots without joining payload rows.
func TestModelTraceWhereSearchesHistoricalAttributionSnapshots(t *testing.T) {
	where, arguments := modelTraceWhere(TraceFilter{
		User:          "dingrui@szyuto.com",
		APIKey:        "dingrui-key",
		SessionID:     "conversation-42",
		UpstreamModel: "gpt-5.6-terra",
	})

	for _, clause := range []string{
		"t.user_snapshot ILIKE $1",
		"t.api_key_snapshot ILIKE $2",
		"t.session_id=$3",
		"t.upstream_model ILIKE $4",
	} {
		if !strings.Contains(where, clause) {
			t.Fatalf("where clause = %q, missing %q", where, clause)
		}
	}
	if want := []any{"%dingrui@szyuto.com%", "%dingrui-key%", "conversation-42", "%gpt-5.6-terra%"}; !equalTraceFilterArguments(arguments, want) {
		t.Fatalf("filter arguments = %#v, want %#v", arguments, want)
	}
}

// equalTraceFilterArguments compares short literal query argument lists while
// keeping the assertion independent from the production SQL builder.
func equalTraceFilterArguments(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
