// Package modeltrace 的测试覆盖调用正文在进入持久化前的安全处理边界。
package modeltrace

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// TestSanitizeForStorageRedactsCredentialsAndMedia verifies that a persistence
// regression cannot keep credential values or Base64 media in readable payloads.
func TestSanitizeForStorageRedactsCredentialsAndMedia(t *testing.T) {
	raw := []byte(`{"authorization":"Bearer trace-canary","nested":{"api_key":"key-canary"},"image":"data:image/png;base64,base64-canary"}`)

	got := SanitizeForStorage("application/json", raw)

	stored := string(got.Body)
	for _, secret := range []string{"trace-canary", "key-canary", "base64-canary"} {
		if strings.Contains(stored, secret) {
			t.Fatalf("sanitized payload leaked %q: %s", secret, stored)
		}
	}
	if !strings.Contains(stored, `"[REDACTED]"`) {
		t.Fatalf("sanitized payload = %s, want redaction marker", stored)
	}
	if got.Status != CaptureStatusRedacted {
		t.Fatalf("capture status = %q, want %q", got.Status, CaptureStatusRedacted)
	}
}

// TestCaptureForStorageMarksOversizedPayload verifies that a body exceeding its
// allowed stored size remains diagnosable without being reported as complete.
func TestCaptureForStorageMarksOversizedPayload(t *testing.T) {
	raw := []byte(strings.Repeat("x", 17))

	got := CaptureForStorage("text/plain", raw, 8)

	wantHash := sha256.Sum256(raw)
	if got.Status != CaptureStatusTruncated {
		t.Fatalf("capture status = %q, want %q", got.Status, CaptureStatusTruncated)
	}
	if got.OriginalBytes != int64(len(raw)) {
		t.Fatalf("original bytes = %d, want %d", got.OriginalBytes, len(raw))
	}
	if got.StoredBytes != 8 || len(got.Body) != 8 {
		t.Fatalf("stored bytes = %d and body length = %d, want 8", got.StoredBytes, len(got.Body))
	}
	if got.SHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("payload hash = %q, want %q", got.SHA256, hex.EncodeToString(wantHash[:]))
	}
}

// TestIsTraceableGatewayRouteLimitsCaptureToModelCalls verifies that adjacent
// gateway management endpoints cannot begin storing arbitrary admin-style data.
func TestIsTraceableGatewayRouteLimitsCaptureToModelCalls(t *testing.T) {
	if !IsTraceableGatewayRoute("POST", "/v1/chat/completions") {
		t.Fatal("chat completions route should be traceable")
	}
	if !IsTraceableGatewayRoute("POST", "/v1/responses") {
		t.Fatal("responses route should be traceable")
	}
	if IsTraceableGatewayRoute("GET", "/v1/models") {
		t.Fatal("models discovery route must not be traceable")
	}
	if IsTraceableGatewayRoute("GET", "/v1/images/tasks/task-1") {
		t.Fatal("asynchronous image status route must not be traceable")
	}
}

// TestIsTraceableGatewayRouteCoversRegisteredAliases 验证公开别名和通配子路径不会绕过模型调用追踪。
func TestIsTraceableGatewayRouteCoversRegisteredAliases(t *testing.T) {
	traceablePaths := []string{
		"/v1/responses/*subpath",
		"/responses/*subpath",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/*subpath",
		"/backend-api/codex/realtime/calls",
		"/antigravity/v1/messages",
	}
	for _, path := range traceablePaths {
		if !IsTraceableGatewayRoute("POST", path) {
			t.Fatalf("POST %s should be traceable", path)
		}
	}

	nonTraceablePaths := []string{
		"/v1/messages/count_tokens",
		"/v1/images/batches",
		"/api/v1/admin/usage",
	}
	for _, path := range nonTraceablePaths {
		if IsTraceableGatewayRoute("POST", path) {
			t.Fatalf("POST %s should not be traceable", path)
		}
	}
}
