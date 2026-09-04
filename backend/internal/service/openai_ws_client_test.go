package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type openAIWSClientMaxWindowResponseWriter struct {
	http.ResponseWriter
	extension string
}

// Unwrap 让 WebSocket 库可以取得底层 ResponseWriter 的连接劫持能力。
func (w *openAIWSClientMaxWindowResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// WriteHeader 在握手切换协议前模拟上游选择 15 位客户端压缩窗口。
func (w *openAIWSClientMaxWindowResponseWriter) WriteHeader(statusCode int) {
	if statusCode == http.StatusSwitchingProtocols {
		w.Header().Set("Sec-WebSocket-Extensions", w.extension)
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

// newOpenAIWSClientCompressionTestServer 创建会选定压缩扩展并发送压缩文本帧的本地上游。
func newOpenAIWSClientCompressionTestServer(t *testing.T) (*httptest.Server, <-chan string, <-chan error) {
	t.Helper()
	handshakeExtensions := make(chan string, 1)
	serverErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handshakeExtensions <- r.Header.Get("Sec-WebSocket-Extensions")
		conn, err := coderws.Accept(&openAIWSClientMaxWindowResponseWriter{
			ResponseWriter: w,
			extension:      "permessage-deflate; client_max_window_bits=15",
		}, r, &coderws.AcceptOptions{
			CompressionMode: coderws.CompressionContextTakeover,
		})
		if err != nil {
			serverErr <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		writeCtx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := conn.Write(writeCtx, coderws.MessageText, []byte("compressed WebSocket message")); err != nil {
			serverErr <- err
		}
	}))
	return server, handshakeExtensions, serverErr
}

// requireOpenAIWSClientCompressionHandshake 验证 Codex offer、协商后的压缩读取及服务端错误。
func requireOpenAIWSClientCompressionHandshake(
	t *testing.T,
	conn openAIWSClientConn,
	handshakeExtensions <-chan string,
	serverErr <-chan error,
) {
	t.Helper()
	payload, err := conn.ReadMessage(context.Background())
	require.NoError(t, err)
	require.Equal(t, []byte("compressed WebSocket message"), payload)

	select {
	case err := <-serverErr:
		require.NoError(t, err)
	case got := <-handshakeExtensions:
		require.Equal(t, "permessage-deflate; client_max_window_bits", got)
	case <-time.After(time.Second):
		t.Fatal("WebSocket server did not receive the handshake")
	}
}

// TestCoderOpenAIWSClientDialer_OffersCodexCompatibleDeflateExtension 防止底层库覆盖 Codex 的压缩协商头。
func TestCoderOpenAIWSClientDialer_OffersCodexCompatibleDeflateExtension(t *testing.T) {
	server, handshakeExtensions, serverErr := newOpenAIWSClientCompressionTestServer(t)
	defer server.Close()

	dialer := newDefaultOpenAIWSClientDialer()
	conn, _, _, err := dialer.Dial(
		context.Background(),
		"ws"+strings.TrimPrefix(server.URL, "http"),
		http.Header{"User-Agent": []string{"relaydeck-test"}},
		"",
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close()) }()
	requireOpenAIWSClientCompressionHandshake(t, conn, handshakeExtensions, serverErr)
}

// TestCoderOpenAIWSClientDialer_OffersCodexCompatibleDeflateExtensionThroughProxy 验证代理路径也使用同一 offer。
func TestCoderOpenAIWSClientDialer_OffersCodexCompatibleDeflateExtensionThroughProxy(t *testing.T) {
	upstream, handshakeExtensions, serverErr := newOpenAIWSClientCompressionTestServer(t)
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(upstreamURL))
	defer proxy.Close()

	dialer := newDefaultOpenAIWSClientDialer()
	conn, _, _, err := dialer.Dial(
		context.Background(),
		"ws"+strings.TrimPrefix(upstream.URL, "http"),
		http.Header{"User-Agent": []string{"relaydeck-test"}},
		proxy.URL,
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close()) }()
	requireOpenAIWSClientCompressionHandshake(t, conn, handshakeExtensions, serverErr)
}

func TestCoderOpenAIWSClientDialer_ProxyHTTPClientReuse(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	c1, err := impl.proxyHTTPClient("http://127.0.0.1:8080")
	require.NoError(t, err)
	c2, err := impl.proxyHTTPClient("http://127.0.0.1:8080")
	require.NoError(t, err)
	require.Same(t, c1, c2, "同一代理地址应复用同一个 HTTP 客户端")

	c3, err := impl.proxyHTTPClient("http://127.0.0.1:8081")
	require.NoError(t, err)
	require.NotSame(t, c1, c3, "不同代理地址应分离客户端")
}

func TestCoderOpenAIWSClientDialer_ProxyHTTPClientInvalidURL(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	_, err := impl.proxyHTTPClient("://bad")
	require.Error(t, err)
}

func TestCoderOpenAIWSClientDialer_TransportMetricsSnapshot(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	_, err := impl.proxyHTTPClient("http://127.0.0.1:18080")
	require.NoError(t, err)
	_, err = impl.proxyHTTPClient("http://127.0.0.1:18080")
	require.NoError(t, err)
	_, err = impl.proxyHTTPClient("http://127.0.0.1:18081")
	require.NoError(t, err)

	snapshot := impl.SnapshotTransportMetrics()
	require.Equal(t, int64(1), snapshot.ProxyClientCacheHits)
	require.Equal(t, int64(2), snapshot.ProxyClientCacheMisses)
	require.InDelta(t, 1.0/3.0, snapshot.TransportReuseRatio, 0.0001)
}

func TestCoderOpenAIWSClientDialer_ProxyClientCacheCapacity(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	total := openAIWSProxyClientCacheMaxEntries + 32
	for i := 0; i < total; i++ {
		_, err := impl.proxyHTTPClient(fmt.Sprintf("http://127.0.0.1:%d", 20000+i))
		require.NoError(t, err)
	}

	impl.proxyMu.Lock()
	cacheSize := len(impl.proxyClients)
	impl.proxyMu.Unlock()

	require.LessOrEqual(t, cacheSize, openAIWSProxyClientCacheMaxEntries, "代理客户端缓存应受容量上限约束")
}

func TestCoderOpenAIWSClientDialer_ProxyClientCacheIdleTTL(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	oldProxy := "http://127.0.0.1:28080"
	_, err := impl.proxyHTTPClient(oldProxy)
	require.NoError(t, err)

	impl.proxyMu.Lock()
	oldEntry := impl.proxyClients[oldProxy]
	require.NotNil(t, oldEntry)
	oldEntry.lastUsedUnixNano = time.Now().Add(-openAIWSProxyClientCacheIdleTTL - time.Minute).UnixNano()
	impl.proxyMu.Unlock()

	// 触发一次新的代理获取，驱动 TTL 清理。
	_, err = impl.proxyHTTPClient("http://127.0.0.1:28081")
	require.NoError(t, err)

	impl.proxyMu.Lock()
	_, exists := impl.proxyClients[oldProxy]
	impl.proxyMu.Unlock()

	require.False(t, exists, "超过空闲 TTL 的代理客户端应被回收")
}

func TestCoderOpenAIWSClientDialer_ProxyTransportTLSHandshakeTimeout(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	client, err := impl.proxyHTTPClient("http://127.0.0.1:38080")
	require.NoError(t, err)
	require.NotNil(t, client)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport)
	require.Equal(t, 10*time.Second, transport.TLSHandshakeTimeout)
}

func TestCoderOpenAIWSClientConn_DoesNotSupportIdlePingWithoutReader(t *testing.T) {
	require.False(t, (&coderOpenAIWSClientConn{}).SupportsIdlePingWithoutReader())
}
