package websocket_test

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

func goroutineStacks() []byte {
	buf := make([]byte, 512)
	for {
		m := runtime.Stack(buf, true)
		if m < len(buf) {
			return buf[:m]
		}
		buf = make([]byte, len(buf)*2)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	expectedGoroutines := 1
	if runtime.GOOS == "js" {
		expectedGoroutines = 2
	}
	if runtime.NumGoroutine() != expectedGoroutines {
		fmt.Fprintf(os.Stderr, "goroutine leak detected, expected %d but got %d goroutines\n", expectedGoroutines, runtime.NumGoroutine())
		fmt.Fprintf(os.Stderr, "%s\n", goroutineStacks())
		os.Exit(1)
	}
	os.Exit(code)
}
