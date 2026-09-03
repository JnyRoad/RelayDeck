package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const remoteCodexAppServerInitializeTimeout = 15 * time.Second

// RemoteCodexAppServerLauncher connects RelayDeck to an official Codex
// app-server that runs outside the RelayDeck container. The token is read only
// when a connection starts and is never retained by the resulting transport.
type RemoteCodexAppServerLauncher struct {
	endpoint  string
	tokenFile string
}

func NewRemoteCodexAppServerLauncher(endpoint, tokenFile string) *RemoteCodexAppServerLauncher {
	return &RemoteCodexAppServerLauncher{
		endpoint:  strings.TrimSpace(endpoint),
		tokenFile: strings.TrimSpace(tokenFile),
	}
}

func (l *RemoteCodexAppServerLauncher) Start(ctx context.Context, _ string) (CodexAppServerTransport, error) {
	if l == nil || l.endpoint == "" {
		return nil, errors.New("未配置本机 Codex app-server bridge 地址")
	}
	if l.tokenFile == "" {
		return nil, errors.New("未配置本机 Codex app-server bridge 令牌文件")
	}
	tokenBytes, err := os.ReadFile(l.tokenFile)
	if err != nil {
		return nil, fmt.Errorf("读取本机 Codex app-server bridge 令牌失败: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return nil, errors.New("本机 Codex app-server bridge 令牌为空")
	}

	initializeCtx, cancel := context.WithTimeout(ctx, remoteCodexAppServerInitializeTimeout)
	defer cancel()
	conn, _, err := websocket.Dial(initializeCtx, l.endpoint, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
	})
	if err != nil {
		return nil, fmt.Errorf("连接本机 Codex app-server 失败: %w", err)
	}

	transport := newRemoteCodexAppServerTransport(conn)
	go transport.readLoop()
	if _, err := transport.Request(initializeCtx, "initialize", map[string]any{
		"clientInfo": map[string]string{
			"name":    "relaydeck",
			"title":   "RelayDeck",
			"version": "1",
		},
	}); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("初始化本机 Codex app-server 失败: %w", err)
	}
	if err := transport.Notify(initializeCtx, "initialized", map[string]any{}); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("初始化本机 Codex app-server 失败: %w", err)
	}
	return transport, nil
}

type remoteCodexAppServerTransport struct {
	conn          *websocket.Conn
	notifications chan CodexAppServerNotification
	done          chan struct{}

	mu        sync.Mutex
	writeMu   sync.Mutex
	nextID    int64
	pending   map[int64]chan codexAppServerRPCMessage
	closed    bool
	closeOnce sync.Once
}

func newRemoteCodexAppServerTransport(conn *websocket.Conn) *remoteCodexAppServerTransport {
	return &remoteCodexAppServerTransport{
		conn:          conn,
		notifications: make(chan CodexAppServerNotification, 32),
		done:          make(chan struct{}),
		pending:       make(map[int64]chan codexAppServerRPCMessage),
	}
}

func (c *remoteCodexAppServerTransport) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, response, err := c.registerRequest()
	if err != nil {
		return nil, err
	}
	if err := c.write(ctx, codexAppServerRPCMessage{ID: &id, Method: method, Params: mustMarshalRaw(params)}); err != nil {
		c.unregisterRequest(id)
		return nil, err
	}
	select {
	case message := <-response:
		if message.Error != nil {
			return nil, fmt.Errorf("codex app-server %s 失败 (%d): %s", method, message.Error.Code, message.Error.Message)
		}
		return message.Result, nil
	case <-ctx.Done():
		c.unregisterRequest(id)
		return nil, ctx.Err()
	case <-c.done:
		c.unregisterRequest(id)
		return nil, errors.New("codex app-server 连接已关闭")
	}
}

func (c *remoteCodexAppServerTransport) Notify(ctx context.Context, method string, params any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.write(ctx, codexAppServerRPCMessage{Method: method, Params: mustMarshalRaw(params)})
}

func (c *remoteCodexAppServerTransport) Notifications() <-chan CodexAppServerNotification {
	return c.notifications
}

func (c *remoteCodexAppServerTransport) Done() <-chan struct{} {
	return c.done
}

func (c *remoteCodexAppServerTransport) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		if c.conn != nil {
			_ = c.conn.Close(websocket.StatusNormalClosure, "relaydeck app-server session closed")
			_ = c.conn.CloseNow()
		}
		close(c.done)
	})
	return nil
}

func (c *remoteCodexAppServerTransport) registerRequest() (int64, chan codexAppServerRPCMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, nil, errors.New("codex app-server 连接已关闭")
	}
	c.nextID++
	id := c.nextID
	response := make(chan codexAppServerRPCMessage, 1)
	c.pending[id] = response
	return id, response, nil
}

func (c *remoteCodexAppServerTransport) unregisterRequest(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
}

func (c *remoteCodexAppServerTransport) write(ctx context.Context, message codexAppServerRPCMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return errors.New("codex app-server 连接已关闭")
	}
	if c.conn == nil {
		return errors.New("codex app-server 连接未初始化")
	}
	return wsjson.Write(ctx, c.conn, message)
}

func (c *remoteCodexAppServerTransport) readLoop() {
	for {
		var message codexAppServerRPCMessage
		if err := wsjson.Read(context.Background(), c.conn, &message); err != nil {
			_ = c.Close()
			return
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
		if message.Method == "" {
			continue
		}
		select {
		case c.notifications <- CodexAppServerNotification{Method: message.Method, Params: message.Params}:
		case <-c.done:
			return
		}
	}
}
