package logging

import (
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	mu     sync.Mutex
	output io.Writer = os.Stderr
)

// SetOutput redirects bridge diagnostics. The readiness event owns stdout, so
// diagnostics are written to stderr by default; tests use this to capture them.
func SetOutput(writer io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	if writer == nil {
		writer = io.Discard
	}
	output = writer
}

// Errorf writes one diagnostic line to the bridge stderr stream.
func Errorf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	_, _ = fmt.Fprintf(output, "wslc-bridge: "+format+"\n", args...)
}
