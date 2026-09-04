package service

import (
	"context"
	"encoding/json"
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

const openAIWSClientCompressedPayload = "relaydeck-codex-deflate-payload-"

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

// newOpenAIWSClientCompressionTestServer 创建会选定压缩扩展、发送压缩文本帧并读取客户端消息的本地上游。
func newOpenAIWSClientCompressionTestServer(
	t *testing.T,
	clientWindowBits int,
) (*httptest.Server, <-chan string, <-chan []byte, <-chan error) {
	return newOpenAIWSClientCompressionTestServerWithExtension(
		t,
		fmt.Sprintf("permessage-deflate; client_max_window_bits=%d", clientWindowBits),
	)
}

// newOpenAIWSClientCompressionTestServerWithExtension 创建使用指定压缩响应扩展的本地上游。
func newOpenAIWSClientCompressionTestServerWithExtension(
	t *testing.T,
	extension string,
) (*httptest.Server, <-chan string, <-chan []byte, <-chan error) {
	t.Helper()
	handshakeExtensions := make(chan string, 1)
	clientMessages := make(chan []byte, 1)
	serverErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handshakeExtensions <- r.Header.Get("Sec-WebSocket-Extensions")
		conn, err := coderws.Accept(&openAIWSClientMaxWindowResponseWriter{
			ResponseWriter: w,
			extension:      extension,
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
			return
		}

		readCtx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		_, payload, err := conn.Read(readCtx)
		if err != nil {
			serverErr <- err
			return
		}
		clientMessages <- payload
	}))
	return server, handshakeExtensions, clientMessages, serverErr
}

// requireOpenAIWSClientCompressionHandshake 验证 Codex offer、双向压缩消息及服务端错误。
func requireOpenAIWSClientCompressionHandshake(
	t *testing.T,
	conn openAIWSClientConn,
	handshakeExtensions <-chan string,
	clientMessages <-chan []byte,
	serverErr <-chan error,
) {
	t.Helper()
	payload, err := conn.ReadMessage(context.Background())
	require.NoError(t, err)
	require.Equal(t, []byte("compressed WebSocket message"), payload)
	require.NoError(t, conn.WriteJSON(context.Background(), map[string]string{
		"kind":    "relaydeck-test",
		"payload": strings.Repeat(openAIWSClientCompressedPayload, 32),
	}))

	select {
	case got := <-handshakeExtensions:
		require.Equal(t, "permessage-deflate; client_max_window_bits", got)
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("WebSocket server did not receive the handshake")
	}

	select {
	case payload := <-clientMessages:
		var clientMessage map[string]string
		require.NoError(t, json.Unmarshal(payload, &clientMessage))
		require.Equal(t, "relaydeck-test", clientMessage["kind"])
		require.Equal(t, strings.Repeat(openAIWSClientCompressedPayload, 32), clientMessage["payload"])
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("WebSocket server did not receive the client message")
	}
}

// TestCoderOpenAIWSClientDialer_OffersCodexCompatibleDeflateExtension 防止底层库覆盖 Codex 的压缩协商头。
func TestCoderOpenAIWSClientDialer_OffersCodexCompatibleDeflateExtension(t *testing.T) {
	server, handshakeExtensions, clientMessages, serverErr := newOpenAIWSClientCompressionTestServer(t, 15)
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
	requireOpenAIWSClientCompressionHandshake(t, conn, handshakeExtensions, clientMessages, serverErr)
}

// TestCoderOpenAIWSClientDialer_OffersCodexCompatibleDeflateExtensionThroughProxy 验证代理路径也使用同一 offer。
func TestCoderOpenAIWSClientDialer_OffersCodexCompatibleDeflateExtensionThroughProxy(t *testing.T) {
	upstream, handshakeExtensions, clientMessages, serverErr := newOpenAIWSClientCompressionTestServer(t, 15)
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
	requireOpenAIWSClientCompressionHandshake(t, conn, handshakeExtensions, clientMessages, serverErr)
}

// TestCoderOpenAIWSClientDialer_UsesCodexNegotiatedClientWindows 覆盖官方 Codex 支持的全部客户端窗口值与代理路径。
func TestCoderOpenAIWSClientDialer_UsesCodexNegotiatedClientWindows(t *testing.T) {
	for _, throughProxy := range []bool{false, true} {
		for clientWindowBits := 9; clientWindowBits <= 15; clientWindowBits++ {
			t.Run(fmt.Sprintf("proxy=%t/window=%d", throughProxy, clientWindowBits), func(t *testing.T) {
				upstream, handshakeExtensions, clientMessages, serverErr := newOpenAIWSClientCompressionTestServer(t, clientWindowBits)
				defer upstream.Close()

				proxyURL := ""
				if throughProxy {
					upstreamURL, err := url.Parse(upstream.URL)
					require.NoError(t, err)
					proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(upstreamURL))
					defer proxy.Close()
					proxyURL = proxy.URL
				}

				dialer := newDefaultOpenAIWSClientDialer()
				conn, _, _, err := dialer.Dial(
					context.Background(),
					"ws"+strings.TrimPrefix(upstream.URL, "http"),
					http.Header{"User-Agent": []string{"relaydeck-test"}},
					proxyURL,
				)
				require.NoError(t, err)
				defer func() { require.NoError(t, conn.Close()) }()
				requireOpenAIWSClientCompressionHandshake(t, conn, handshakeExtensions, clientMessages, serverErr)
			})
		}
	}
}

// TestCoderOpenAIWSClientDialer_RejectsUnsupportedCodexClientWindow 拒绝官方 Codex 不支持的 8 位客户端窗口。
func TestCoderOpenAIWSClientDialer_RejectsUnsupportedCodexClientWindow(t *testing.T) {
	for _, responseExtension := range []string{
		"permessage-deflate; client_max_window_bits=8",
		"permessage-deflate; client_max_window_bits=invalid",
	} {
		t.Run(responseExtension, func(t *testing.T) {
			server, _, _, _ := newOpenAIWSClientCompressionTestServerWithExtension(t, responseExtension)
			defer server.Close()

			dialer := newDefaultOpenAIWSClientDialer()
			conn, _, _, err := dialer.Dial(
				context.Background(),
				"ws"+strings.TrimPrefix(server.URL, "http"),
				http.Header{"User-Agent": []string{"relaydeck-test"}},
				"",
			)
			require.Nil(t, conn)
			require.Error(t, err)
			require.ErrorContains(t, err, responseExtension[len("permessage-deflate; "):])
		})
	}
}

// TestCoderOpenAIWSClientDialer_UsesCodexDefaultWindowWhenOmitted 验证省略选择时沿用 Codex 默认的 15 位窗口。
func TestCoderOpenAIWSClientDialer_UsesCodexDefaultWindowWhenOmitted(t *testing.T) {
	server, handshakeExtensions, clientMessages, serverErr := newOpenAIWSClientCompressionTestServerWithExtension(t, "permessage-deflate")
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
	requireOpenAIWSClientCompressionHandshake(t, conn, handshakeExtensions, clientMessages, serverErr)
}

// TestCoderWebSocketDial_DefaultDeflateOfferIsUnchanged 验证未开启 OAuth 专用选项的调用方仍使用原始 offer。
func TestCoderWebSocketDial_DefaultDeflateOfferIsUnchanged(t *testing.T) {
	handshakeExtensions := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handshakeExtensions <- r.Header.Get("Sec-WebSocket-Extensions")
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{
			CompressionMode: coderws.CompressionContextTakeover,
		})
		if err == nil {
			_ = conn.CloseNow()
		}
	}))
	defer server.Close()

	conn, _, err := coderws.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), &coderws.DialOptions{
		CompressionMode: coderws.CompressionContextTakeover,
	})
	require.NoError(t, err)
	require.NoError(t, conn.CloseNow())
	require.Equal(t, "permessage-deflate", <-handshakeExtensions)
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
