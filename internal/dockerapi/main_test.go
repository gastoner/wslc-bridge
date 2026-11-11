package dockerapi

import (
	"io"
	"os"
	"testing"

	"wslc-bridge/internal/logging"
)

func TestMain(m *testing.M) {
	// Bridge diagnostics are not part of any assertion here; keep test output clean.
	logging.SetOutput(io.Discard)
	os.Exit(m.Run())
}
