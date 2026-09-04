package modeltrace

import "testing"

// TestExtractConversationLinksUsesOnlyExplicitProtocolFields verifies that
// only declared conversation identifiers and Responses API lineage can join
// turns; matching request metadata is never treated as a session.
func TestExtractConversationLinksUsesOnlyExplicitProtocolFields(t *testing.T) {
	links := ExtractConversationLinks(
		"application/json",
		[]byte(`{"conversation_id":"conversation-42","previous_response_id":"resp-previous"}`),
		"application/json",
		[]byte(`{"object":"response","id":"resp-current"}`),
	)

	if links.SessionID != "conversation-42" || links.PreviousResponseID != "resp-previous" || links.ResponseID != "resp-current" {
		t.Fatalf("conversation links = %#v", links)
	}
}

// TestExtractConversationLinksRejectsIdentityAndTimingLookalikes verifies that
// values which merely resemble correlation metadata cannot create a replay
// session without an explicit protocol relationship.
func TestExtractConversationLinksRejectsIdentityAndTimingLookalikes(t *testing.T) {
	links := ExtractConversationLinks(
		"application/json",
		[]byte(`{"user_id":99,"api_key_id":4,"request_id":"same-request","metadata":{"trace_id":"nearby"}}`),
		"application/json",
		[]byte(`{"id":"chatcmpl-not-a-responses-object"}`),
	)

	if links != (ConversationLinks{}) {
		t.Fatalf("lookalike metadata created conversation links: %#v", links)
	}
}

// TestExtractConversationLinksReadsResponsesCompletedSSE verifies that a
// completed Responses event can extend a turn whose final delivery is SSE.
func TestExtractConversationLinksReadsResponsesCompletedSSE(t *testing.T) {
	links := ExtractConversationLinks(
		"application/json",
		[]byte(`{"previous_response_id":"resp-parent"}`),
		"text/event-stream",
		[]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-child\"}}\n\n"),
	)

	if links.PreviousResponseID != "resp-parent" || links.ResponseID != "resp-child" || links.SessionID != "" {
		t.Fatalf("SSE conversation links = %#v", links)
	}
}
