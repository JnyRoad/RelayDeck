package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewCodexAppServerService_UsesRemoteBridgeWhenConfigured(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "bridge.token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("bridge-test-token\n"), 0o600))
	t.Setenv("CODEX_APP_SERVER_REMOTE_URL", "ws://host.docker.internal:19881")
	t.Setenv("CODEX_APP_SERVER_REMOTE_TOKEN_FILE", tokenFile)

	service := NewCodexAppServerService(CodexAppServerServiceConfig{RootDir: t.TempDir()})

	require.Equal(t, "websocket", service.TransportKind())
}

func TestNewCodexAppServerService_RejectsIncompleteRemoteBridgeConfiguration(t *testing.T) {
	t.Setenv("CODEX_APP_SERVER_REMOTE_URL", "ws://host.docker.internal:19881")
	t.Setenv("CODEX_APP_SERVER_REMOTE_TOKEN_FILE", "")
	service := NewCodexAppServerService(CodexAppServerServiceConfig{RootDir: t.TempDir()})

	_, err := service.StartLogin(context.Background(), CodexAppServerLoginModeDeviceCode)

	require.EqualError(t, err, "本机 Codex app-server bridge 配置不完整")
}

func TestNewCodexAppServerService_IgnoresRemoteTokenFileWithoutRemoteURL(t *testing.T) {
	t.Setenv("CODEX_APP_SERVER_REMOTE_URL", "")
	t.Setenv("CODEX_APP_SERVER_REMOTE_TOKEN_FILE", "/run/relaydeck/codex-app-server.token")
	service := NewCodexAppServerService(CodexAppServerServiceConfig{RootDir: t.TempDir()})

	require.Equal(t, CodexAppServerTransportStdio, service.TransportKind())
	_, isLocalLauncher := service.launcher.(*ExecCodexAppServerLauncher)
	require.True(t, isLocalLauncher)
}

func TestNewCodexAppServerService_UsesRemoteTransportForInjectedRemoteLauncher(t *testing.T) {
	service := NewCodexAppServerService(CodexAppServerServiceConfig{
		RootDir:  t.TempDir(),
		Launcher: NewRemoteCodexAppServerLauncher("ws://host.docker.internal:19881", "/run/relaydeck/codex-app-server.token"),
	})

	require.Equal(t, CodexAppServerTransportWebSocket, service.TransportKind())
}

func TestCodexAppServerService_ReportsUnavailableRemoteBridgeWithoutToken(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "bridge.token")
	const bridgeToken = "bridge-token-must-never-appear-in-errors"
	require.NoError(t, os.WriteFile(tokenFile, []byte(bridgeToken+"\n"), 0o600))
	t.Setenv("CODEX_APP_SERVER_REMOTE_URL", "ws://127.0.0.1:1")
	t.Setenv("CODEX_APP_SERVER_REMOTE_TOKEN_FILE", tokenFile)
	service := NewCodexAppServerService(CodexAppServerServiceConfig{RootDir: t.TempDir()})

	_, err := service.StartLogin(context.Background(), CodexAppServerLoginModeDeviceCode)

	require.Error(t, err)
	require.Contains(t, err.Error(), "连接本机 Codex app-server 失败")
	require.NotContains(t, err.Error(), bridgeToken)
}

// This regression test catches a login session that drops the app-server's
// completion notification: the UI would otherwise keep polling forever after
// the user authorizes the device code.
func TestCodexAppServerService_DeviceCodeLoginTracksCompletion(t *testing.T) {
	transport := newFakeCodexAppServerTransport(t, map[string]json.RawMessage{
		"account/login/start": json.RawMessage(`{
			"type":"chatgptDeviceCode",
			"loginId":"official-login-1",
			"verificationUrl":"https://auth.openai.com/codex/device",
			"userCode":"ABCD-1234"
		}`),
	})
	launcher := &fakeCodexAppServerLauncher{transport: transport}
	service := NewCodexAppServerService(CodexAppServerServiceConfig{
		RootDir:  t.TempDir(),
		Launcher: launcher,
	})

	started, err := service.StartLogin(context.Background(), CodexAppServerLoginModeDeviceCode)
	require.NoError(t, err)
	require.Equal(t, CodexAppServerLoginStatusPending, started.Status)
	require.Equal(t, "official-login-1", started.LoginID)
	require.Equal(t, "https://auth.openai.com/codex/device", started.VerificationURL)
	require.Equal(t, "ABCD-1234", started.UserCode)
	require.Len(t, launcher.homeDirs(), 1)

	transport.notify(CodexAppServerNotification{
		Method: "account/login/completed",
		Params: json.RawMessage(`{
			"loginId":"official-login-1",
			"success":true
		}`),
	})

	require.Eventually(t, func() bool {
		status, statusErr := service.GetLogin(started.SessionID)
		return statusErr == nil && status.Status == CodexAppServerLoginStatusCompleted
	}, time.Second, 10*time.Millisecond)
}

// A failed device-code exchange must promptly release the isolated runtime
// profile. Keeping it would retain an unowned subprocess and credential data.
func TestCodexAppServerService_FailedLoginReleasesUnclaimedProfile(t *testing.T) {
	transport := newFakeCodexAppServerTransport(t, map[string]json.RawMessage{
		"account/login/start": json.RawMessage(`{
			"type":"chatgptDeviceCode",
			"loginId":"official-login-2",
			"verificationUrl":"https://auth.openai.com/codex/device",
			"userCode":"ABCD-5678"
		}`),
	})
	launcher := &fakeCodexAppServerLauncher{transport: transport}
	service := NewCodexAppServerService(CodexAppServerServiceConfig{
		RootDir:  t.TempDir(),
		Launcher: launcher,
	})

	started, err := service.StartLogin(context.Background(), CodexAppServerLoginModeDeviceCode)
	require.NoError(t, err)

	transport.notify(CodexAppServerNotification{
		Method: "account/login/completed",
		Params: json.RawMessage(`{
			"loginId":"official-login-2",
			"success":false,
			"error":"authorization denied"
		}`),
	})

	require.Eventually(t, func() bool {
		login, statusErr := service.GetLogin(started.SessionID)
		return statusErr == nil && login.Status == CodexAppServerLoginStatusFailed
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		return transport.closeCalls() == 1
	}, time.Second, 10*time.Millisecond)
	require.NoDirExists(t, launcher.homeDirs()[0])
}

// Login start is a child-process RPC and must not inherit an unbounded admin
// HTTP request context, otherwise a stuck runtime can hold the request forever.
func TestCodexAppServerService_BoundsLoginStartRequest(t *testing.T) {
	transport := newFakeCodexAppServerTransport(t, map[string]json.RawMessage{
		"account/login/start": json.RawMessage(`{
			"type":"chatgptDeviceCode",
			"loginId":"official-login-3",
			"verificationUrl":"https://auth.openai.com/codex/device",
			"userCode":"ABCD-9012"
		}`),
	})
	service := NewCodexAppServerService(CodexAppServerServiceConfig{
		RootDir:  t.TempDir(),
		Launcher: &fakeCodexAppServerLauncher{transport: transport},
	})

	_, err := service.StartLogin(context.Background(), CodexAppServerLoginModeDeviceCode)
	require.NoError(t, err)
	deadline, ok := transport.requestDeadline("account/login/start")
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(15*time.Second), deadline, time.Second)
}

// Cancellation uses the same bounded RPC window as login start, so a hung
// runtime cannot hold an administrative cancellation request indefinitely.
func TestCodexAppServerService_BoundsLoginCancelRequest(t *testing.T) {
	transport := newFakeCodexAppServerTransport(t, map[string]json.RawMessage{
		"account/login/start": json.RawMessage(`{
			"type":"chatgptDeviceCode",
			"loginId":"official-login-cancel",
			"verificationUrl":"https://auth.openai.com/codex/device",
			"userCode":"ABCD-7890"
		}`),
		"account/login/cancel": json.RawMessage(`{}`),
	})
	service := NewCodexAppServerService(CodexAppServerServiceConfig{
		RootDir:  t.TempDir(),
		Launcher: &fakeCodexAppServerLauncher{transport: transport},
	})

	started, err := service.StartLogin(context.Background(), CodexAppServerLoginModeDeviceCode)
	require.NoError(t, err)
	require.NoError(t, service.CancelLogin(context.Background(), started.SessionID))
	deadline, ok := transport.requestDeadline("account/login/cancel")
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(15*time.Second), deadline, time.Second)
}

// Abandoned sessions must expire even when the browser never returns to call
// the cancel endpoint, otherwise each retry retains a process and profile.
func TestCodexAppServerService_ExpiresAbandonedLogin(t *testing.T) {
	transport := newFakeCodexAppServerTransport(t, map[string]json.RawMessage{
		"account/login/start": json.RawMessage(`{
			"type":"chatgptDeviceCode",
			"loginId":"official-login-4",
			"verificationUrl":"https://auth.openai.com/codex/device",
			"userCode":"ABCD-3456"
		}`),
	})
	launcher := &fakeCodexAppServerLauncher{transport: transport}
	service := NewCodexAppServerService(CodexAppServerServiceConfig{
		RootDir:  t.TempDir(),
		Launcher: launcher,
	})

	started, err := service.StartLogin(context.Background(), CodexAppServerLoginModeDeviceCode)
	require.NoError(t, err)
	session, err := service.session(started.SessionID)
	require.NoError(t, err)
	session.mu.Lock()
	session.expiryTimer.Stop()
	session.expiresAt = time.Now().Add(10 * time.Millisecond)
	session.mu.Unlock()
	service.scheduleExpiry(session)

	require.Eventually(t, func() bool {
		_, statusErr := service.GetLogin(started.SessionID)
		return statusErr != nil
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, 1, transport.closeCalls())
	require.NoDirExists(t, launcher.homeDirs()[0])
}

// Official app-server profiles authenticate a user-owned runtime instead of
// RelayDeck's HTTP relay. They must remain unavailable to the legacy scheduler
// even if an administrator has configured a default OpenAI account group.
func TestBuildAccountForCreate_HonorsExplicitUnschedulableState(t *testing.T) {
	notSchedulable := false
	account, err := buildAccountForCreate(&CreateAccountInput{
		Name:        "official runtime profile",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"auth_provider": "codex_app_server"},
		Schedulable: &notSchedulable,
	}, map[string]any{})

	require.NoError(t, err)
	require.False(t, account.Schedulable)
}

func TestAccount_IsCodexAppServerManaged(t *testing.T) {
	managed := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"auth_provider": "codex_app_server"},
	}
	require.True(t, managed.IsCodexAppServerManaged())
	require.False(t, managed.UsesOpenAICodexProtocol())
	require.False(t, (&Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"auth_provider": "oauth"},
	}).IsCodexAppServerManaged())
}

type fakeCodexAppServerLauncher struct {
	transport CodexAppServerTransport

	mu    sync.Mutex
	homes []string
}

func (l *fakeCodexAppServerLauncher) Start(_ context.Context, homeDir string) (CodexAppServerTransport, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.homes = append(l.homes, homeDir)
	return l.transport, nil
}

func (l *fakeCodexAppServerLauncher) homeDirs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.homes...)
}

type fakeCodexAppServerTransport struct {
	t             *testing.T
	responses     map[string]json.RawMessage
	notifications chan CodexAppServerNotification
	done          chan struct{}

	mu               sync.Mutex
	requestDeadlines map[string]time.Time
	closed           bool
	closeCount       int
}

func newFakeCodexAppServerTransport(t *testing.T, responses map[string]json.RawMessage) *fakeCodexAppServerTransport {
	t.Helper()
	return &fakeCodexAppServerTransport{
		t:                t,
		responses:        responses,
		notifications:    make(chan CodexAppServerNotification, 4),
		done:             make(chan struct{}),
		requestDeadlines: make(map[string]time.Time),
	}
}

func (p *fakeCodexAppServerTransport) Request(ctx context.Context, method string, _ any) (json.RawMessage, error) {
	if deadline, ok := ctx.Deadline(); ok {
		p.mu.Lock()
		p.requestDeadlines[method] = deadline
		p.mu.Unlock()
	}
	response, ok := p.responses[method]
	if !ok {
		p.t.Fatalf("unexpected app-server request: %s", method)
	}
	return response, nil
}

func (p *fakeCodexAppServerTransport) Notify(_ context.Context, method string, _ any) error {
	if method != "initialized" {
		p.t.Fatalf("unexpected app-server notification: %s", method)
	}
	return nil
}

func (p *fakeCodexAppServerTransport) Notifications() <-chan CodexAppServerNotification {
	return p.notifications
}

func (p *fakeCodexAppServerTransport) Done() <-chan struct{} {
	return p.done
}

func (p *fakeCodexAppServerTransport) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCount++
	if !p.closed {
		p.closed = true
		close(p.done)
	}
	return nil
}

func (p *fakeCodexAppServerTransport) notify(notification CodexAppServerNotification) {
	p.notifications <- notification
}

func (p *fakeCodexAppServerTransport) closeCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeCount
}

func (p *fakeCodexAppServerTransport) requestDeadline(method string) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	deadline, ok := p.requestDeadlines[method]
	return deadline, ok
}
