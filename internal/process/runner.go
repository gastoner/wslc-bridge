package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const MaxOutputBytes = 16 << 20

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	// Truncated reports that stdout or stderr exceeded MaxOutputBytes and the
	// captured content is incomplete.
	Truncated bool
}

type CommandError struct {
	Name     string
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *CommandError) Unwrap() error { return e.Err }

func (e *CommandError) Error() string {
	message := fmt.Sprintf("%s exited with code %d", e.Name, e.ExitCode)
	if detail := strings.TrimSpace(e.Stderr); detail != "" {
		message += ": " + detail
	}
	if e.Err != nil && e.ExitCode < 0 {
		return fmt.Sprintf("run %s: %v", e.Name, e.Err)
	}
	return message
}

type Runner struct {
	WslcPath  string
	WslPath   string
	WaitDelay time.Duration
}

func NewRunner(wslcPath, wslPath string) *Runner {
	return &Runner{WslcPath: wslcPath, WslPath: wslPath, WaitDelay: 3 * time.Second}
}

func (r *Runner) RunWSLC(ctx context.Context, args ...string) (Result, error) {
	return r.run(ctx, r.WslcPath, args...)
}

func (r *Runner) RunWSL(ctx context.Context, args ...string) (Result, error) {
	return r.run(ctx, r.WslPath, args...)
}

func (r *Runner) RunWSLCStreaming(ctx context.Context, stdout, stderr io.Writer, args ...string) (int, error) {
	if r.WslcPath == "" {
		return -1, &CommandError{Name: "wslc.exe", ExitCode: -1, Err: errors.New("not found on PATH")}
	}
	cmd := exec.CommandContext(ctx, r.WslcPath, args...)
	cmd.WaitDelay = r.WaitDelay
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	code := -1
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		code = exitError.ExitCode()
	}
	if ctx.Err() != nil {
		return code, ctx.Err()
	}
	return code, &CommandError{Name: r.WslcPath, Args: append([]string(nil), args...), ExitCode: code, Err: err}
}

func (r *Runner) run(ctx context.Context, name string, args ...string) (Result, error) {
	if name == "" {
		return Result{ExitCode: -1}, &CommandError{Name: "executable", ExitCode: -1, Err: errors.New("not found on PATH")}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = r.WaitDelay
	var stdout, stderr limitedBuffer
	stdout.limit = MaxOutputBytes
	stderr.limit = MaxOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: 0, Truncated: stdout.truncated || stderr.truncated}
	if err == nil {
		return result, nil
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	result.ExitCode = -1
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
	}
	return result, &CommandError{Name: name, Args: append([]string(nil), args...), ExitCode: result.ExitCode, Stderr: string(result.Stderr), Err: err}
}

// StderrOf returns the captured stderr of a failed command. Error classification
// must look at the tool output only, never at the rendered error message.
func StderrOf(err error) string {
	var commandError *CommandError
	if errors.As(err, &commandError) {
		return commandError.Stderr
	}
	return ""
}

type limitedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
