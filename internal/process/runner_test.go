package process

import (
	"errors"
	"testing"
)

func TestLimitedBufferReportsTruncation(t *testing.T) {
	buffer := &limitedBuffer{limit: 4}
	written, err := buffer.Write([]byte("abcdef"))
	if err != nil || written != 6 {
		t.Fatalf("Write() = (%d, %v), want (6, nil) so the command never sees a short write", written, err)
	}
	if buffer.String() != "abcd" {
		t.Fatalf("captured output = %q, want %q", buffer.String(), "abcd")
	}
	if !buffer.truncated {
		t.Fatal("truncation was not reported")
	}
}

func TestStderrOfIgnoresTheRenderedErrorMessage(t *testing.T) {
	err := &CommandError{Name: "wslc.exe", ExitCode: 1, Stderr: "container is already running"}
	if got := StderrOf(err); got != "container is already running" {
		t.Fatalf("StderrOf() = %q", got)
	}
	if got := StderrOf(errors.New("already running")); got != "" {
		t.Fatalf("StderrOf() on a plain error = %q, want an empty string", got)
	}
}
