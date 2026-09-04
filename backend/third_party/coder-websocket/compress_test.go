//go:build !js

package websocket

import (
	"bytes"
	"compress/flate"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/test/xrand"
)

func Test_slidingWindow(t *testing.T) {
	t.Parallel()

	const testCount = 99
	const maxWindow = 99999
	for range testCount {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			input := xrand.String(maxWindow)
			windowLength := xrand.Int(maxWindow)
			var sw slidingWindow
			sw.init(windowLength)
			sw.write([]byte(input))

			assert.Equal(t, "window length", windowLength, cap(sw.buf))
			if !strings.HasSuffix(input, string(sw.buf)) {
				t.Fatalf("r.buf is not a suffix of input: %q and %q", input, sw.buf)
			}
		})
	}
}

// TestMsgWriterUsesCodexClientWindow 验证协商的客户端窗口会选择对应的可变窗口编码器。
func TestMsgWriterUsesCodexClientWindow(t *testing.T) {
	for _, windowBits := range []int{9, 10, 11, 12, 13, 14, 15} {
		t.Run("window="+strconv.Itoa(windowBits), func(t *testing.T) {
			copts := CompressionContextTakeover.opts()
			copts.clientMaxWindowBits = windowBits

			mw := newMsgWriter(&Conn{client: true, copts: copts})
			assert.Success(t, mw.ensureFlate())
			typeOfWriter := reflect.TypeOf(mw.flateWriter)
			assert.Equal(t, "writer package", "github.com/klauspost/compress/flate", typeOfWriter.Elem().PkgPath())
			mw.putFlateWriter()
		})
	}
}

func BenchmarkFlateWriter(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w, _ := flate.NewWriter(io.Discard, flate.BestSpeed)
		// We have to write a byte to get the writer to allocate to its full extent.
		w.Write([]byte{'a'})
		w.Flush()
	}
}

func BenchmarkFlateReader(b *testing.B) {
	b.ReportAllocs()

	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestSpeed)
	w.Write([]byte{'a'})
	w.Flush()

	for i := 0; i < b.N; i++ {
		r := flate.NewReader(bytes.NewReader(buf.Bytes()))
		io.ReadAll(r)
	}
}
