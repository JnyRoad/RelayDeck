package modeltrace

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const maxConversationLinkLength = 255

// ConversationLinks holds only protocol-declared identifiers that can safely
// connect trace turns. Empty fields mean the trace must remain unlinked.
type ConversationLinks struct {
	SessionID          string
	PreviousResponseID string
	ResponseID         string
}

// ExtractConversationLinks reads explicit client conversation fields and
// Responses API result identifiers from bounded textual captures. It returns
// no heuristic identity, timing, user, or API-key based links.
func ExtractConversationLinks(requestContentType string, requestBody []byte, responseContentType string, responseBody []byte) ConversationLinks {
	links := ConversationLinks{}
	if isJSONContentType(requestContentType) {
		links.SessionID, links.PreviousResponseID = extractRequestConversationLinks(requestBody)
	}
	if isJSONContentType(responseContentType) {
		links.ResponseID = extractResponseID(responseBody)
	} else if isSSEContentType(responseContentType) {
		links.ResponseID = extractResponseIDFromSSE(responseBody)
	}
	return links
}

// extractRequestConversationLinks accepts only documented, explicit request
// fields and returns normalized identifiers suitable for exact equality queries.
func extractRequestConversationLinks(body []byte) (string, string) {
	var request struct {
		ConversationID     string `json:"conversation_id"`
		SessionID          string `json:"session_id"`
		PreviousResponseID string `json:"previous_response_id"`
		Metadata           struct {
			ConversationID string `json:"conversation_id"`
			SessionID      string `json:"session_id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return "", ""
	}
	sessionID := normalizeConversationLink(request.ConversationID)
	if sessionID == "" {
		sessionID = normalizeConversationLink(request.SessionID)
	}
	if sessionID == "" {
		sessionID = normalizeConversationLink(request.Metadata.ConversationID)
	}
	if sessionID == "" {
		sessionID = normalizeConversationLink(request.Metadata.SessionID)
	}
	return sessionID, normalizeConversationLink(request.PreviousResponseID)
}

// extractResponseID returns a Responses API root identifier only when the
// JSON object explicitly identifies itself as a response.
func extractResponseID(body []byte) string {
	var response struct {
		Object string `json:"object"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Object != "response" {
		return ""
	}
	return normalizeConversationLink(response.ID)
}

// extractResponseIDFromSSE walks delivered SSE data lines in order and returns
// the completed response identifier, if one was actually delivered to the client.
func extractResponseIDFromSSE(body []byte) string {
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event struct {
			Type     string `json:"type"`
			Response struct {
				ID string `json:"id"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			continue
		}
		if event.Type == "response.completed" || event.Type == "response.done" {
			if responseID := normalizeConversationLink(event.Response.ID); responseID != "" {
				return responseID
			}
		}
	}
	return ""
}

// normalizeConversationLink enforces the database width and excludes control
// characters so untrusted protocol input cannot alter list rendering or queries.
func normalizeConversationLink(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > maxConversationLinkLength {
		return ""
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return ""
		}
	}
	return value
}
