//go:build !js

package websocket

import (
	"net"

	"github.com/coder/websocket/internal/util"
)

func (c *Conn) RecordBytesWritten() *int {
	var bytesWritten int
	c.bw.Reset(util.WriterFunc(func(p []byte) (int, error) {
		bytesWritten += len(p)
		return c.rwc.Write(p)
	}))
	return &bytesWritten
}

func (c *Conn) RecordBytesRead() *int {
	var bytesRead int
	c.br.Reset(util.ReaderFunc(func(p []byte) (int, error) {
		n, err := c.rwc.Read(p)
		bytesRead += n
		return n, err
	}))
	return &bytesRead
}

// ClientMaxWindowBits returns the selected client compression window for tests.
func (c *Conn) ClientMaxWindowBits() int {
	if c == nil || c.copts == nil {
		return 0
	}
	return c.copts.clientMaxWindowBits
}

var ErrClosed = net.ErrClosed

var (
	ExportedDial         = dial
	SecWebSocketAccept   = secWebSocketAccept
	SecWebSocketKey      = secWebSocketKey
	VerifyServerResponse = verifyServerResponse
)

var CompressionModeOpts = CompressionMode.opts
