package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/require"
)

func TestRemoteCodexAppServerLauncher_StartUsesBridgeToken(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "bridge.token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("bridge-test-token\n"), 0o600))

	serverErr := make(chan error, 1)
	initialized := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer bridge-test-token" {
			serverErr <- &unexpectedBridgeAuthorizationError{got: got}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		var initialize codexAppServerRPCMessage
		if err := wsjson.Read(ctx, conn, &initialize); err != nil {
			serverErr <- err
			return
		}
		if initialize.ID == nil || initialize.Method != "initialize" {
			serverErr <- &unexpectedCodexAppServerMessageError{method: initialize.Method}
			return
		}
		if err := wsjson.Write(ctx, conn, codexAppServerRPCMessage{
			ID:     initialize.ID,
			Result: json.RawMessage(`{}`),
		}); err != nil {
			serverErr <- err
			return
		}

		var notification codexAppServerRPCMessage
		if err := wsjson.Read(ctx, conn, &notification); err != nil {
			serverErr <- err
			return
		}
		if notification.ID != nil || notification.Method != "initialized" {
			serverErr <- &unexpectedCodexAppServerMessageError{method: notification.Method}
			return
		}
		initialized <- struct{}{}
	}))
	defer server.Close()

	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	transport, err := NewRemoteCodexAppServerLauncher(endpoint, tokenFile).Start(context.Background(), t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, transport)
	defer func() { require.NoError(t, transport.Close()) }()

	select {
	case <-initialized:
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initialized notification")
	}
}

func TestRemoteCodexAppServerTransport_RoutesLoginCompletionNotification(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "bridge.token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("bridge-test-token\n"), 0o600))

	serverErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		var initialize codexAppServerRPCMessage
		if err := wsjson.Read(ctx, conn, &initialize); err != nil {
			serverErr <- err
			return
		}
		if err := wsjson.Write(ctx, conn, codexAppServerRPCMessage{ID: initialize.ID, Result: json.RawMessage(`{}`)}); err != nil {
			serverErr <- err
			return
		}
		var initializedNotification codexAppServerRPCMessage
		if err := wsjson.Read(ctx, conn, &initializedNotification); err != nil {
			serverErr <- err
			return
		}
		var loginStart codexAppServerRPCMessage
		if err := wsjson.Read(ctx, conn, &loginStart); err != nil {
			serverErr <- err
			return
		}
		if loginStart.ID == nil || loginStart.Method != "account/login/start" {
			serverErr <- &unexpectedCodexAppServerMessageError{method: loginStart.Method}
			return
		}
		if err := wsjson.Write(ctx, conn, codexAppServerRPCMessage{
			ID:     loginStart.ID,
			Result: json.RawMessage(`{"loginId":"official-login-1"}`),
		}); err != nil {
			serverErr <- err
			return
		}
		if err := wsjson.Write(ctx, conn, codexAppServerRPCMessage{
			Method: "account/login/completed",
			Params: json.RawMessage(`{"loginId":"official-login-1","success":true}`),
		}); err != nil {
			serverErr <- err
		}
	}))
	defer server.Close()

	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	transport, err := NewRemoteCodexAppServerLauncher(endpoint, tokenFile).Start(context.Background(), t.TempDir())
	require.NoError(t, err)
	defer func() { require.NoError(t, transport.Close()) }()

	_, err = transport.Request(context.Background(), "account/login/start", map[string]string{"type": "chatgptDeviceCode"})
	require.NoError(t, err)

	select {
	case notification := <-transport.Notifications():
		require.Equal(t, "account/login/completed", notification.Method)
		require.JSONEq(t, `{"loginId":"official-login-1","success":true}`, string(notification.Params))
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for login completion notification")
	}
}

type unexpectedBridgeAuthorizationError struct {
	got string
}

func (e *unexpectedBridgeAuthorizationError) Error() string {
	return "unexpected bridge authorization header"
}

type unexpectedCodexAppServerMessageError struct {
	method string
}

func (e *unexpectedCodexAppServerMessageError) Error() string {
	return "unexpected Codex app-server message"
}
