//go:build !js

package websocket_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/util"
	"github.com/coder/websocket/internal/xsync"
)

func TestBadDials(t *testing.T) {
	t.Parallel()

	t.Run("badReq", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name   string
			url    string
			opts   *websocket.DialOptions
			rand   util.ReaderFunc
			nilCtx bool
		}{
			{
				name: "badURL",
				url:  "://noscheme",
			},
			{
				name: "badURLScheme",
				url:  "ftp://nhooyr.io",
			},
			{
				name: "badTLS",
				url:  "wss://totallyfake.nhooyr.io",
			},
			{
				name: "badReader",
				rand: func(p []byte) (int, error) {
					return 0, io.EOF
				},
			},
			{
				name:   "nilContext",
				url:    "http://localhost",
				nilCtx: true,
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				var ctx context.Context
				var cancel func()
				if !tc.nilCtx {
					ctx, cancel = context.WithTimeout(context.Background(), time.Second*5)
					defer cancel()
				}

				if tc.rand == nil {
					tc.rand = rand.Reader.Read
				}

				_, _, err := websocket.ExportedDial(ctx, tc.url, tc.opts, tc.rand)
				assert.Error(t, err)
			})
		}
	})

	t.Run("badResponse", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		_, _, err := websocket.Dial(ctx, "ws://example.com", &websocket.DialOptions{
			HTTPClient: mockHTTPClient(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					Body: io.NopCloser(strings.NewReader("hi")),
				}, nil
			}),
		})
		assert.Contains(t, err, "failed to WebSocket dial: expected handshake response status code 101 but got 0")
	})

	t.Run("badBody", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		rt := func(r *http.Request) (*http.Response, error) {
			h := http.Header{}
			h.Set("Connection", "Upgrade")
			h.Set("Upgrade", "websocket")
			h.Set("Sec-WebSocket-Accept", websocket.SecWebSocketAccept(r.Header.Get("Sec-WebSocket-Key")))

			return &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				Header:     h,
				Body:       io.NopCloser(strings.NewReader("hi")),
			}, nil
		}

		_, _, err := websocket.Dial(ctx, "ws://example.com", &websocket.DialOptions{
			HTTPClient: mockHTTPClient(rt),
		})
		assert.Contains(t, err, "response body is not a io.ReadWriteCloser")
	})
}

func Test_verifyHostOverride(t *testing.T) {
	testCases := []struct {
		name string
		host string
		exp  string
	}{
		{
			name: "noOverride",
			host: "",
			exp:  "example.com",
		},
		{
			name: "hostOverride",
			host: "example.net",
			exp:  "example.net",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()

			rt := func(r *http.Request) (*http.Response, error) {
				assert.Equal(t, "Host", tc.exp, r.Host)

				h := http.Header{}
				h.Set("Connection", "Upgrade")
				h.Set("Upgrade", "websocket")
				h.Set("Sec-WebSocket-Accept", websocket.SecWebSocketAccept(r.Header.Get("Sec-WebSocket-Key")))

				return &http.Response{
					StatusCode: http.StatusSwitchingProtocols,
					Header:     h,
					Body:       mockBody{bytes.NewBufferString("hi")},
				}, nil
			}

			c, _, err := websocket.Dial(ctx, "ws://example.com", &websocket.DialOptions{
				HTTPClient: mockHTTPClient(rt),
				Host:       tc.host,
			})
			assert.Success(t, err)
			c.CloseNow()
		})
	}
}

type mockBody struct {
	*bytes.Buffer
}

func (mb mockBody) Close() error {
	return nil
}

// TestDialCodexClientMaxWindowBits 验证 opt-in 的裸 offer、9–15 选择和对 8 的拒绝。
func TestDialCodexClientMaxWindowBits(t *testing.T) {
	testCases := []struct {
		name              string
		selectedWindow    int
		responseExtension string
		wantWindow        int
		wantErrContains   string
	}{
		{name: "omittedUsesDefault15", wantWindow: 15},
		{name: "window9", selectedWindow: 9, wantWindow: 9},
		{name: "window10", selectedWindow: 10, wantWindow: 10},
		{name: "window11", selectedWindow: 11, wantWindow: 11},
		{name: "window12", selectedWindow: 12, wantWindow: 12},
		{name: "window13", selectedWindow: 13, wantWindow: 13},
		{name: "window14", selectedWindow: 14, wantWindow: 14},
		{name: "window15", selectedWindow: 15, wantWindow: 15},
		{name: "window8Rejected", selectedWindow: 8, wantErrContains: "client_max_window_bits=8"},
		{name: "window16Rejected", selectedWindow: 16, wantErrContains: "client_max_window_bits=16"},
		{name: "malformedWindowRejected", responseExtension: "permessage-deflate; client_max_window_bits=bogus", wantErrContains: "client_max_window_bits=bogus"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := mockHTTPClient(func(r *http.Request) (*http.Response, error) {
				assert.Equal(t, "Sec-WebSocket-Extensions", "permessage-deflate; client_max_window_bits", r.Header.Get("Sec-WebSocket-Extensions"))
				headers := http.Header{}
				headers.Set("Connection", "Upgrade")
				headers.Set("Upgrade", "websocket")
				headers.Set("Sec-WebSocket-Accept", websocket.SecWebSocketAccept(r.Header.Get("Sec-WebSocket-Key")))
				if tc.responseExtension != "" {
					headers.Set("Sec-WebSocket-Extensions", tc.responseExtension)
				} else if tc.selectedWindow != 0 {
					headers.Set("Sec-WebSocket-Extensions", "permessage-deflate; client_max_window_bits="+strconv.Itoa(tc.selectedWindow))
				} else {
					headers.Set("Sec-WebSocket-Extensions", "permessage-deflate")
				}
				return &http.Response{
					StatusCode: http.StatusSwitchingProtocols,
					Header:     headers,
					Body:       mockBody{bytes.NewBuffer(nil)},
				}, nil
			})

			opts := &websocket.DialOptions{
				HTTPClient:                     client,
				CompressionMode:                websocket.CompressionContextTakeover,
				CompressionClientMaxWindowBits: true,
			}
			conn, _, err := websocket.Dial(context.Background(), "ws://example.com", opts)
			if tc.wantErrContains != "" {
				assert.Contains(t, err, tc.wantErrContains)
				return
			}
			assert.Success(t, err)
			assert.Equal(t, "selected client window", tc.wantWindow, conn.ClientMaxWindowBits())
			conn.CloseNow()
		})
	}
}

// TestDialDefaultDeflateOfferUnchanged 验证未开启 Codex 选项的调用方维持原始 offer。
func TestDialDefaultDeflateOfferUnchanged(t *testing.T) {
	client := mockHTTPClient(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "Sec-WebSocket-Extensions", "permessage-deflate", r.Header.Get("Sec-WebSocket-Extensions"))
		headers := http.Header{}
		headers.Set("Connection", "Upgrade")
		headers.Set("Upgrade", "websocket")
		headers.Set("Sec-WebSocket-Accept", websocket.SecWebSocketAccept(r.Header.Get("Sec-WebSocket-Key")))
		headers.Set("Sec-WebSocket-Extensions", "permessage-deflate")
		return &http.Response{
			StatusCode: http.StatusSwitchingProtocols,
			Header:     headers,
			Body:       mockBody{bytes.NewBuffer(nil)},
		}, nil
	})

	conn, _, err := websocket.Dial(context.Background(), "ws://example.com", &websocket.DialOptions{
		HTTPClient:      client,
		CompressionMode: websocket.CompressionContextTakeover,
	})
	assert.Success(t, err)
	assert.Equal(t, "selected client window", 0, conn.ClientMaxWindowBits())
	conn.CloseNow()
}

func Test_verifyServerHandshake(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		response func(w http.ResponseWriter)
		success  bool
	}{
		{
			name: "badStatus",
			response: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusOK)
			},
			success: false,
		},
		{
			name: "badConnection",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "???")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "badUpgrade",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "???")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "badSecWebSocketAccept",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "websocket")
				w.Header().Set("Sec-WebSocket-Accept", "xd")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "badSecWebSocketProtocol",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "websocket")
				w.Header().Set("Sec-WebSocket-Protocol", "xd")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "unsupportedExtension",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "websocket")
				w.Header().Set("Sec-WebSocket-Extensions", "meow")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "unsupportedDeflateParam",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "websocket")
				w.Header().Set("Sec-WebSocket-Extensions", "permessage-deflate; meow")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "success",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "websocket")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			tc.response(w)
			resp := w.Result()

			r := httptest.NewRequest("GET", "/", nil)
			key, err := websocket.SecWebSocketKey(rand.Reader)
			assert.Success(t, err)
			r.Header.Set("Sec-WebSocket-Key", key)

			if resp.Header.Get("Sec-WebSocket-Accept") == "" {
				resp.Header.Set("Sec-WebSocket-Accept", websocket.SecWebSocketAccept(key))
			}

			opts := &websocket.DialOptions{
				Subprotocols: strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ","),
			}
			_, err = websocket.VerifyServerResponse(opts, websocket.CompressionModeOpts(opts.CompressionMode), key, resp)
			if tc.success {
				assert.Success(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func mockHTTPClient(fn roundTripperFunc) *http.Client {
	return &http.Client{
		Transport: fn,
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestDialRedirect(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	_, _, err := websocket.Dial(ctx, "ws://example.com", &websocket.DialOptions{
		HTTPClient: mockHTTPClient(func(r *http.Request) (*http.Response, error) {
			resp := &http.Response{
				Header: http.Header{},
			}
			if r.URL.Scheme != "https" {
				resp.Header.Set("Location", "wss://example.com")
				resp.StatusCode = http.StatusFound
				return resp, nil
			}
			resp.Header.Set("Connection", "Upgrade")
			resp.Header.Set("Upgrade", "meow")
			resp.StatusCode = http.StatusSwitchingProtocols
			return resp, nil
		}),
	})
	assert.Contains(t, err, "failed to WebSocket dial: WebSocket protocol violation: Upgrade header \"meow\" does not contain websocket")
}

type forwardProxy struct {
	hc *http.Client
}

func newForwardProxy() *forwardProxy {
	return &forwardProxy{
		hc: &http.Client{},
	}
}

func (fc *forwardProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second*10)
	defer cancel()

	r = r.WithContext(ctx)
	r.RequestURI = ""
	resp, err := fc.hc.Do(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer resp.Body.Close()

	maps.Copy(w.Header(), resp.Header)
	w.Header().Set("PROXIED", "true")
	w.WriteHeader(resp.StatusCode)

	if resprw, ok := resp.Body.(io.ReadWriter); ok {
		c, brw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		brw.Flush()

		errc1 := xsync.Go(func() error {
			_, err := io.Copy(c, resprw)
			return err
		})
		errc2 := xsync.Go(func() error {
			_, err := io.Copy(resprw, c)
			return err
		})
		select {
		case <-errc1:
		case <-errc2:
		case <-r.Context().Done():
		}
	} else {
		io.Copy(w, resp.Body)
	}
}

func TestDialViaProxy(t *testing.T) {
	t.Parallel()

	ps := httptest.NewServer(newForwardProxy())
	defer ps.Close()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := echoServer(w, r, nil)
		assert.Success(t, err)
	}))
	defer s.Close()

	psu, err := url.Parse(ps.URL)
	assert.Success(t, err)
	proxyTransport := http.DefaultTransport.(*http.Transport).Clone()
	proxyTransport.Proxy = http.ProxyURL(psu)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, s.URL, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: proxyTransport,
		},
	})
	assert.Success(t, err)
	assert.Equal(t, "", "true", resp.Header.Get("PROXIED"))

	assertEcho(t, ctx, c)
	assertClose(t, c)
}
