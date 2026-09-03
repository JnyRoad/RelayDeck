// Package modeltrace 负责模型网关调用追踪的安全正文处理与路由范围判定。
package modeltrace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime"
	"net/url"
	"strings"
)

// DefaultPayloadLimitBytes 是单个追踪正文默认允许持久化的最大字节数。
const DefaultPayloadLimitBytes = 1 << 20

// CaptureStatus 描述追踪正文能否安全、完整地保存。
type CaptureStatus string

const (
	// CaptureStatusComplete 表示正文完整保存且不需要内容替换。
	CaptureStatusComplete CaptureStatus = "complete"
	// CaptureStatusTruncated 表示正文超过上限，只保存了安全前缀。
	CaptureStatusTruncated CaptureStatus = "truncated"
	// CaptureStatusRedacted 表示正文已保存，但其中敏感值已被替换。
	CaptureStatusRedacted CaptureStatus = "redacted"
	// CaptureStatusNotApplicable 表示正文属于不应保存的媒体或协议内容。
	CaptureStatusNotApplicable CaptureStatus = "not_applicable"
	// CaptureStatusFailed 表示正文采集或加密失败，且没有可靠正文可供查看。
	CaptureStatusFailed CaptureStatus = "failed"
)

// CapturedPayload 是可进入持久化层的安全正文及其完整性元数据。
type CapturedPayload struct {
	Body          []byte
	Status        CaptureStatus
	OriginalBytes int64
	StoredBytes   int64
	SHA256        string
}

// SanitizeForStorage removes credential and inline media values before a body
// reaches persistent storage. JSON streams retain their framing while each
// independently valid JSON record receives the same redaction treatment.
func SanitizeForStorage(contentType string, raw []byte) CapturedPayload {
	result := CapturedPayload{
		Body:          append([]byte(nil), raw...),
		Status:        CaptureStatusComplete,
		OriginalBytes: int64(len(raw)),
		StoredBytes:   int64(len(raw)),
		SHA256:        hashPayload(raw),
	}

	if len(raw) == 0 {
		return result
	}

	var sanitized []byte
	var redacted bool
	switch {
	case isJSONContentType(contentType):
		sanitized, redacted = sanitizeJSONDocument(raw)
	case isNDJSONContentType(contentType):
		sanitized, redacted = sanitizeJSONLines(raw, false)
	case isSSEContentType(contentType):
		sanitized, redacted = sanitizeJSONLines(raw, true)
	default:
		return result
	}
	if !redacted {
		return result
	}

	result.Body = sanitized
	result.StoredBytes = int64(len(sanitized))
	result.Status = CaptureStatusRedacted
	return result
}

// sanitizeJSONDocument redacts one complete JSON value without allowing an
// invalid document to become a new persistence representation.
func sanitizeJSONDocument(raw []byte) ([]byte, bool) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil || !redactJSONValue(&decoded, "") {
		return append([]byte(nil), raw...), false
	}
	sanitized, err := json.Marshal(decoded)
	if err != nil {
		return append([]byte(nil), raw...), false
	}
	return sanitized, true
}

// sanitizeJSONLines redacts complete JSON records while preserving NDJSON or
// SSE framing, line ordering, and non-JSON protocol control lines.
func sanitizeJSONLines(raw []byte, sse bool) ([]byte, bool) {
	lines := strings.SplitAfter(string(raw), "\n")
	changed := false
	for index, line := range lines {
		prefix, jsonText, suffix, ok := traceJSONLineParts(line, sse)
		if !ok {
			continue
		}
		sanitized, redacted := sanitizeJSONDocument([]byte(jsonText))
		if !redacted {
			continue
		}
		lines[index] = prefix + string(sanitized) + suffix
		changed = true
	}
	if !changed {
		return append([]byte(nil), raw...), false
	}
	return []byte(strings.Join(lines, "")), true
}

// traceJSONLineParts separates a line's JSON candidate from preserved protocol
// syntax. SSE only allows JSON after a data: field; NDJSON treats the body as
// the candidate and retains its original line ending.
func traceJSONLineParts(line string, sse bool) (prefix, jsonText, suffix string, ok bool) {
	suffix = ""
	if strings.HasSuffix(line, "\n") {
		line = strings.TrimSuffix(line, "\n")
		suffix = "\n"
	}
	if sse {
		leading := len(line) - len(strings.TrimLeft(line, " \t"))
		field := line[leading:]
		if !strings.HasPrefix(field, "data:") {
			return "", "", suffix, false
		}
		prefix = line[:leading+len("data:")]
		jsonText = strings.TrimSpace(field[len("data:"):])
		if jsonText == "" {
			return "", "", suffix, false
		}
		return prefix, jsonText, suffix, true
	}
	jsonText = strings.TrimSpace(line)
	if jsonText == "" {
		return "", "", suffix, false
	}
	return "", jsonText, suffix, true
}

// CaptureForStorage sanitizes a payload, records the original hash, and bounds
// the persisted body so model traffic cannot consume unbounded trace storage.
func CaptureForStorage(contentType string, raw []byte, limit int) CapturedPayload {
	result := SanitizeForStorage(contentType, raw)
	if limit < 0 || len(result.Body) <= limit {
		return result
	}

	result.Body = append([]byte(nil), result.Body[:limit]...)
	result.StoredBytes = int64(len(result.Body))
	result.Status = CaptureStatusTruncated
	return result
}

// IsTraceableGatewayRoute reports whether a gateway route creates or performs
// a model call whose request and client-visible response belong in a trace.
func IsTraceableGatewayRoute(method, requestPath string) bool {
	if method != "POST" {
		return false
	}

	path := normalizeGatewayPath(requestPath)
	if path == "/responses" || strings.HasPrefix(path, "/responses/") {
		return true
	}
	switch path {
	case "/messages", "/chat/completions", "/embeddings", "/live",
		"/alpha/search", "/images/generations", "/images/edits", "/videos",
		"/videos/generations", "/videos/edits", "/videos/extensions", "/tts", "/stt",
		"/web_search", "/x_search":
		return true
	default:
		return false
	}
}

// redactJSONValue walks JSON values and replaces secret-bearing fields or
// inline Base64 media. It returns true when at least one value was changed.
func redactJSONValue(value *any, key string) bool {
	switch current := (*value).(type) {
	case map[string]any:
		changed := false
		for childKey, childValue := range current {
			if isSensitiveField(childKey) || isInlineMedia(childValue) {
				current[childKey] = "[REDACTED]"
				changed = true
				continue
			}
			if redactJSONValue(&childValue, childKey) {
				current[childKey] = childValue
				changed = true
			}
		}
		return changed
	case []any:
		changed := false
		for index, childValue := range current {
			if redactJSONValue(&childValue, key) {
				current[index] = childValue
				changed = true
			}
		}
		return changed
	case string:
		if isSensitiveField(key) || isInlineMedia(current) {
			*value = "[REDACTED]"
			return true
		}
	}
	return false
}

// isSensitiveField identifies field names whose values are never allowed in
// a trace body, regardless of their nesting level or source protocol.
func isSensitiveField(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"), " ", "_"))
	switch normalized {
	case "authorization", "api_key", "apikey", "access_token", "refresh_token", "token", "secret", "password", "cookie", "set_cookie":
		return true
	default:
		return false
	}
}

// isInlineMedia detects data URLs that encode binary content inline in a JSON
// value. The URL parser avoids treating ordinary prompt text as media.
func isInlineMedia(value any) bool {
	text, ok := value.(string)
	if !ok || !strings.HasPrefix(strings.ToLower(text), "data:") {
		return false
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(parsed.Opaque), ";base64,")
}

// isJSONContentType accepts JSON media types while ignoring charset and other
// parameters supplied by clients or upstream protocol adapters.
func isJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

// isNDJSONContentType identifies line-delimited JSON protocol bodies whose
// individual records can be safely redacted without discarding stream framing.
func isNDJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/x-ndjson" || mediaType == "application/ndjson" || mediaType == "application/jsonl"
}

// isSSEContentType identifies textual event streams that may embed structured
// model frames in data fields rather than in one top-level JSON document.
func isSSEContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

// hashPayload returns the lowercase SHA-256 digest used to correlate a body
// without storing the original unredacted bytes in any additional field.
func hashPayload(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// normalizeGatewayPath strips the public version prefix so route decisions
// match both mounted and unmounted Gin paths without widening the allowlist.
func normalizeGatewayPath(requestPath string) string {
	path := strings.TrimSpace(requestPath)
	if path == "/backend-api/codex/realtime/calls" {
		return "/live"
	}
	for _, prefix := range []string{"/backend-api/codex", "/antigravity/v1"} {
		if path == prefix {
			return "/"
		}
		if strings.HasPrefix(path, prefix+"/") {
			return strings.TrimPrefix(path, prefix)
		}
	}
	for _, prefix := range []string{"/v1", "/openai/v1"} {
		if path == prefix {
			return "/"
		}
		if strings.HasPrefix(path, prefix+"/") {
			return strings.TrimPrefix(path, prefix)
		}
	}
	return path
}
