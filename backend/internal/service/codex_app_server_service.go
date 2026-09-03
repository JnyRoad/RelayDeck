package service

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// CodexAppServerLoginModeBrowser asks the official Codex app-server to own
	// the browser/loopback flow. It is useful only when the browser runs on the
	// same host as the app-server process.
	CodexAppServerLoginModeBrowser CodexAppServerLoginMode = "browser"
	// CodexAppServerLoginModeDeviceCode is suitable for a private server: the
	// user completes the login in their own browser without an inbound callback.
	CodexAppServerLoginModeDeviceCode CodexAppServerLoginMode = "device_code"

	CodexAppServerLoginStatusPending   CodexAppServerLoginStatus = "pending"
	CodexAppServerLoginStatusCompleted CodexAppServerLoginStatus = "completed"
	CodexAppServerLoginStatusFailed    CodexAppServerLoginStatus = "failed"
	CodexAppServerLoginStatusCancelled CodexAppServerLoginStatus = "cancelled"
)

// CodexAppServerLoginMode selects an authentication flow owned by the official
// Codex app-server. RelayDeck never constructs a provider OAuth URL or handles
// provider refresh tokens for this flow.
type CodexAppServerLoginMode string

// CodexAppServerLoginStatus is the user-visible state of a managed login.
type CodexAppServerLoginStatus string

// CodexAppServerLogin is safe to return through the admin API. It contains only
// user-action information and never access or refresh tokens.
type CodexAppServerLogin struct {
	SessionID        string                    `json:"session_id"`
	LoginID          string                    `json:"login_id"`
	Mode             CodexAppServerLoginMode   `json:"mode"`
	Status           CodexAppServerLoginStatus `json:"status"`
	AuthorizationURL string                    `json:"authorization_url,omitempty"`
	VerificationURL  string                    `json:"verification_url,omitempty"`
	UserCode         string                    `json:"user_code,omitempty"`
	Error            string                    `json:"error,omitempty"`
}

// CodexAppServerNotification is a server-initiated JSON-RPC message emitted by
// an official Codex app-server connection.
type CodexAppServerNotification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// CodexAppServerTransport is deliberately limited to the app-server JSON-RPC
// protocol. No raw OAuth tokens flow through this boundary.
type CodexAppServerTransport interface {
	Request(ctx context.Context, method string, params any) (json.RawMessage, error)
	Notify(ctx context.Context, method string, params any) error
	Notifications() <-chan CodexAppServerNotification
	Close() error
}

// CodexAppServerLauncher creates an initialized official Codex app-server
// transport in an isolated CODEX_HOME directory.
type CodexAppServerLauncher interface {
	Start(ctx context.Context, homeDir string) (CodexAppServerTransport, error)
}

// CodexAppServerServiceConfig contains only host-operator configuration. The
// process command is never supplied by an HTTP request.
type CodexAppServerServiceConfig struct {
	RootDir  string
	Launcher CodexAppServerLauncher
}

// CodexAppServerService starts and observes official app-server-managed login
// sessions. The credentials live only under the isolated CODEX_HOME profile.
type CodexAppServerService struct {
	rootDir  string
	launcher CodexAppServerLauncher

	mu       sync.RWMutex
	sessions map[string]*codexAppServerSession
}

type codexAppServerSession struct {
	mu        sync.RWMutex
	login     CodexAppServerLogin
	homeDir   string
	transport CodexAppServerTransport
}

// NewCodexAppServerService builds a service suitable for production and tests.
// Empty fields select the local, official `codex app-server` runtime.
func NewCodexAppServerService(cfg CodexAppServerServiceConfig) *CodexAppServerService {
	rootDir := strings.TrimSpace(cfg.RootDir)
	if rootDir == "" {
		base := strings.TrimSpace(os.Getenv("DATA_DIR"))
		if base == "" {
			base = "./data"
		}
		rootDir = filepath.Join(base, "codex-app-server-profiles")
	}
	launcher := cfg.Launcher
	if launcher == nil {
		launcher = NewExecCodexAppServerLauncher(strings.TrimSpace(os.Getenv("CODEX_APP_SERVER_BIN")))
	}
	return &CodexAppServerService{
		rootDir:  filepath.Clean(rootDir),
		launcher: launcher,
		sessions: make(map[string]*codexAppServerSession),
	}
}

// StartLogin delegates the authentication choice to the official app-server.
// The process receives an isolated CODEX_HOME and therefore owns all tokens.
func (s *CodexAppServerService) StartLogin(ctx context.Context, mode CodexAppServerLoginMode) (*CodexAppServerLogin, error) {
	if s == nil || s.launcher == nil {
		return nil, errors.New("Codex app-server 登录服务未配置")
	}
	if mode != CodexAppServerLoginModeBrowser && mode != CodexAppServerLoginModeDeviceCode {
		return nil, fmt.Errorf("不支持的 Codex app-server 登录方式: %q", mode)
	}

	sessionID, err := randomSessionID()
	if err != nil {
		return nil, err
	}
	homeDir := filepath.Join(s.rootDir, sessionID)
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 Codex app-server 凭据目录失败: %w", err)
	}
	if err := os.Chmod(homeDir, 0o700); err != nil {
		_ = os.RemoveAll(homeDir)
		return nil, fmt.Errorf("保护 Codex app-server 凭据目录失败: %w", err)
	}

	transport, err := s.launcher.Start(ctx, homeDir)
	if err != nil {
		_ = os.RemoveAll(homeDir)
		return nil, err
	}

	loginType := "chatgpt"
	if mode == CodexAppServerLoginModeDeviceCode {
		loginType = "chatgptDeviceCode"
	}
	raw, err := transport.Request(ctx, "account/login/start", map[string]string{"type": loginType})
	if err != nil {
		_ = transport.Close()
		_ = os.RemoveAll(homeDir)
		return nil, fmt.Errorf("启动 Codex app-server 登录失败: %w", err)
	}

	var response struct {
		LoginID          string `json:"loginId"`
		AuthorizationURL string `json:"authUrl"`
		VerificationURL  string `json:"verificationUrl"`
		UserCode         string `json:"userCode"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		_ = transport.Close()
		_ = os.RemoveAll(homeDir)
		return nil, fmt.Errorf("解析 Codex app-server 登录响应失败: %w", err)
	}
	if strings.TrimSpace(response.LoginID) == "" {
		_ = transport.Close()
		_ = os.RemoveAll(homeDir)
		return nil, errors.New("Codex app-server 未返回登录会话标识")
	}
	if mode == CodexAppServerLoginModeBrowser && strings.TrimSpace(response.AuthorizationURL) == "" {
		_ = transport.Close()
		_ = os.RemoveAll(homeDir)
		return nil, errors.New("Codex app-server 未返回浏览器授权地址")
	}
	if mode == CodexAppServerLoginModeDeviceCode && (strings.TrimSpace(response.VerificationURL) == "" || strings.TrimSpace(response.UserCode) == "") {
		_ = transport.Close()
		_ = os.RemoveAll(homeDir)
		return nil, errors.New("Codex app-server 未返回设备授权信息")
	}

	session := &codexAppServerSession{
		login: CodexAppServerLogin{
			SessionID:        sessionID,
			LoginID:          response.LoginID,
			Mode:             mode,
			Status:           CodexAppServerLoginStatusPending,
			AuthorizationURL: response.AuthorizationURL,
			VerificationURL:  response.VerificationURL,
			UserCode:         response.UserCode,
		},
		homeDir:   homeDir,
		transport: transport,
	}
	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()
	go s.watchLogin(session)

	login := session.snapshot()
	return &login, nil
}

// GetLogin returns the current state without reading credentials from disk.
func (s *CodexAppServerService) GetLogin(sessionID string) (*CodexAppServerLogin, error) {
	session, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	login := session.snapshot()
	return &login, nil
}

// CompleteLogin confirms that the official process reported success and returns
// the stable profile ID used by the account record. It does not expose the
// profile path to API callers.
func (s *CodexAppServerService) CompleteLogin(sessionID string) (string, error) {
	session, err := s.session(sessionID)
	if err != nil {
		return "", err
	}
	login := session.snapshot()
	if login.Status != CodexAppServerLoginStatusCompleted {
		return "", fmt.Errorf("Codex app-server 登录尚未完成: %s", login.Status)
	}
	return login.SessionID, nil
}

// FinalizeLogin releases the transient JSON-RPC connection after a successful
// account record has been created. The official app-server credential profile
// remains on disk for future official app-server runs.
func (s *CodexAppServerService) FinalizeLogin(sessionID string) error {
	session, err := s.session(sessionID)
	if err != nil {
		return err
	}
	if session.snapshot().Status != CodexAppServerLoginStatusCompleted {
		return errors.New("不能完成尚未认证的 Codex app-server 登录")
	}
	if err := session.transport.Close(); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	return nil
}

// CancelLogin cancels a pending official login and removes its incomplete
// isolated profile. Completed profiles must be finalized instead.
func (s *CodexAppServerService) CancelLogin(ctx context.Context, sessionID string) error {
	session, err := s.session(sessionID)
	if err != nil {
		return err
	}
	login := session.snapshot()
	if login.Status == CodexAppServerLoginStatusCompleted {
		return errors.New("已完成的 Codex app-server 登录不能取消")
	}
	_, requestErr := session.transport.Request(ctx, "account/login/cancel", map[string]string{"loginId": login.LoginID})
	closeErr := session.transport.Close()
	session.setStatus(CodexAppServerLoginStatusCancelled, "")
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	removeErr := os.RemoveAll(session.homeDir)
	return errors.Join(requestErr, closeErr, removeErr)
}

func (s *CodexAppServerService) session(sessionID string) (*codexAppServerSession, error) {
	s.mu.RLock()
	session := s.sessions[sessionID]
	s.mu.RUnlock()
	if session == nil {
		return nil, errors.New("Codex app-server 登录会话不存在或已结束")
	}
	return session, nil
}

func (s *CodexAppServerService) watchLogin(session *codexAppServerSession) {
	for notification := range session.transport.Notifications() {
		if notification.Method != "account/login/completed" {
			continue
		}
		var completed struct {
			LoginID string `json:"loginId"`
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(notification.Params, &completed); err != nil {
			continue
		}
		if completed.LoginID != session.snapshot().LoginID {
			continue
		}
		if completed.Success {
			session.setStatus(CodexAppServerLoginStatusCompleted, "")
		} else {
			session.setStatus(CodexAppServerLoginStatusFailed, completed.Error)
		}
		return
	}
	if session.snapshot().Status == CodexAppServerLoginStatusPending {
		session.setStatus(CodexAppServerLoginStatusFailed, "Codex app-server 连接已关闭")
	}
}

func (s *codexAppServerSession) snapshot() CodexAppServerLogin {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.login
}

func (s *codexAppServerSession) setStatus(status CodexAppServerLoginStatus, errMessage string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.login.Status = status
	s.login.Error = errMessage
}

func randomSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成 Codex app-server 登录会话标识失败: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// ExecCodexAppServerLauncher starts the OpenAI-maintained `codex app-server`
// binary over its supported stdio JSONL transport.
type ExecCodexAppServerLauncher struct {
	binary string
}

func NewExecCodexAppServerLauncher(binary string) *ExecCodexAppServerLauncher {
	if strings.TrimSpace(binary) == "" {
		binary = "codex"
	}
	return &ExecCodexAppServerLauncher{binary: binary}
}

func (l *ExecCodexAppServerLauncher) Start(ctx context.Context, homeDir string) (CodexAppServerTransport, error) {
	if l == nil || strings.TrimSpace(l.binary) == "" {
		return nil, errors.New("未配置 Codex app-server 可执行文件")
	}
	transport, err := startStdioCodexAppServer(l.binary, homeDir)
	if err != nil {
		return nil, err
	}
	initializeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err = transport.Request(initializeCtx, "initialize", map[string]any{
		"clientInfo": map[string]string{
			"name":    "relaydeck",
			"title":   "RelayDeck",
			"version": "1",
		},
	})
	if err == nil {
		err = transport.Notify(initializeCtx, "initialized", map[string]any{})
	}
	if err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("初始化 Codex app-server 失败: %w", err)
	}
	return transport, nil
}

type stdioCodexAppServerTransport struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan codexAppServerRPCMessage
	closed  bool

	notifications chan CodexAppServerNotification
	done          chan struct{}
	shutdownOnce  sync.Once
	notifyOnce    sync.Once
}

type codexAppServerRPCMessage struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func startStdioCodexAppServer(binary, homeDir string) (*stdioCodexAppServerTransport, error) {
	command := exec.Command(binary, "app-server", "--listen", "stdio://")
	command.Env = sanitizedCodexAppServerEnv(homeDir)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("准备 Codex app-server stdin 失败: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("准备 Codex app-server stdout 失败: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("启动官方 Codex app-server 失败: %w", err)
	}
	transport := &stdioCodexAppServerTransport{
		cmd:           command,
		stdin:         stdin,
		pending:       make(map[int64]chan codexAppServerRPCMessage),
		notifications: make(chan CodexAppServerNotification, 32),
		done:          make(chan struct{}),
	}
	go transport.readLoop(stdout)
	go func() {
		_ = command.Wait()
		transport.closeNotifications()
	}()
	return transport, nil
}

func sanitizedCodexAppServerEnv(homeDir string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch key {
		case "CODEX_HOME", "CODEX_API_KEY", "OPENAI_API_KEY":
			continue
		default:
			env = append(env, value)
		}
	}
	return append(env, "CODEX_HOME="+homeDir)
}

func (c *stdioCodexAppServerTransport) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, response, err := c.registerRequest()
	if err != nil {
		return nil, err
	}
	if err := c.write(codexAppServerRPCMessage{ID: &id, Method: method, Params: mustMarshalRaw(params)}); err != nil {
		c.unregisterRequest(id)
		return nil, err
	}
	select {
	case message := <-response:
		if message.Error != nil {
			return nil, fmt.Errorf("Codex app-server %s 失败 (%d): %s", method, message.Error.Code, message.Error.Message)
		}
		return message.Result, nil
	case <-ctx.Done():
		c.unregisterRequest(id)
		return nil, ctx.Err()
	case <-c.done:
		c.unregisterRequest(id)
		return nil, errors.New("Codex app-server 连接已关闭")
	}
}

func (c *stdioCodexAppServerTransport) Notify(ctx context.Context, method string, params any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.write(codexAppServerRPCMessage{Method: method, Params: mustMarshalRaw(params)})
}

func (c *stdioCodexAppServerTransport) Notifications() <-chan CodexAppServerNotification {
	return c.notifications
}

func (c *stdioCodexAppServerTransport) Close() error {
	c.shutdownOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		_ = c.stdin.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		c.closeNotifications()
	})
	return nil
}

func (c *stdioCodexAppServerTransport) registerRequest() (int64, chan codexAppServerRPCMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, nil, errors.New("Codex app-server 连接已关闭")
	}
	c.nextID++
	id := c.nextID
	response := make(chan codexAppServerRPCMessage, 1)
	c.pending[id] = response
	return id, response, nil
}

func (c *stdioCodexAppServerTransport) unregisterRequest(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
}

func (c *stdioCodexAppServerTransport) write(message codexAppServerRPCMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("Codex app-server 连接已关闭")
	}
	_, err = c.stdin.Write(append(payload, '\n'))
	return err
}

func (c *stdioCodexAppServerTransport) readLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var message codexAppServerRPCMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		if message.ID != nil {
			c.mu.Lock()
			response := c.pending[*message.ID]
			delete(c.pending, *message.ID)
			c.mu.Unlock()
			if response != nil {
				response <- message
			}
			continue
		}
		if message.Method != "" {
			select {
			case c.notifications <- CodexAppServerNotification{Method: message.Method, Params: message.Params}:
			case <-c.done:
				return
			}
		}
	}
	c.closeNotifications()
}

func (c *stdioCodexAppServerTransport) closeNotifications() {
	c.notifyOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		close(c.done)
		close(c.notifications)
	})
}

func mustMarshalRaw(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return payload
}
