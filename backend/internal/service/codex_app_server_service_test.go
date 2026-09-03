package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
}

func newFakeCodexAppServerTransport(t *testing.T, responses map[string]json.RawMessage) *fakeCodexAppServerTransport {
	t.Helper()
	return &fakeCodexAppServerTransport{
		t:             t,
		responses:     responses,
		notifications: make(chan CodexAppServerNotification, 4),
	}
}

func (p *fakeCodexAppServerTransport) Request(_ context.Context, method string, _ any) (json.RawMessage, error) {
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

func (p *fakeCodexAppServerTransport) Close() error {
	return nil
}

func (p *fakeCodexAppServerTransport) notify(notification CodexAppServerNotification) {
	p.notifications <- notification
}
