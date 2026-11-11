package guest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"wslc-bridge/internal/logging"
)

const (
	stderrDrainLimit = 64 << 10
	// pipeDrainTimeout bounds how long a finished guest process may keep its
	// pipes open, for example when a grandchild inherited them.
	pipeDrainTimeout = 2 * time.Second
	// guestShutdownGrace is how long Close waits for a guest process to exit.
	guestShutdownGrace = 5 * time.Second
	// guestKillGrace is how long Close waits after killing the guest process.
	guestKillGrace = 2 * time.Second
)

type StdioDialer struct {
	WSLCPath string
	Command  []string
	Env      []string

	mu          sync.Mutex
	lastFailure string
}

func (d *StdioDialer) DialContext(ctx context.Context, _ string, _ string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.WSLCPath == "" {
		return nil, &net.DNSError{Err: "wslc.exe not found", Name: "guest-docker"}
	}
	processCtx, cancel := context.WithCancel(context.Background())
	arguments := d.Command
	if arguments == nil {
		arguments = []string{
			"system", "session", "run", "docker",
			"-H", "unix:///var/run/docker.sock", "system", "dial-stdio",
		}
	}
	cmd := exec.CommandContext(processCtx, d.WSLCPath, arguments...)
	if len(d.Env) > 0 {
		cmd.Env = append(os.Environ(), d.Env...)
	}
	cmd.WaitDelay = 3 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		cancel()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		cancel()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		_ = cmd.Wait()
		return nil, err
	}
	clientEnd, processEnd := net.Pipe()
	connection := &stdioConn{
		Conn: clientEnd, processEnd: processEnd,
		cmd: cmd, cancel: cancel, stdin: stdin, stdout: stdout, stderr: stderr, dialer: d,
		waitDone: make(chan struct{}), outputDone: make(chan struct{}), stderrDone: make(chan struct{}),
		closeDone: make(chan struct{}),
	}
	go connection.writeStdin()
	go connection.readStdout()
	go connection.drainStderr()
	go connection.wait()
	return connection, nil
}

// Diagnostic reports why the most recent guest transport ended abnormally, if it
// did. A named-pipe EOF cannot carry a cause, so this is the only place the
// guest exit status and its stderr tail survive for the HTTP layer.
func (d *StdioDialer) Diagnostic() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastFailure
}

func (d *StdioDialer) recordFailure(message string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastFailure = message
}

type stdioConn struct {
	net.Conn
	processEnd net.Conn
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderr     io.ReadCloser
	dialer     *StdioDialer
	waitDone   chan struct{}
	outputDone chan struct{}
	stderrDone chan struct{}
	closeOnce  sync.Once
	closeDone  chan struct{}
	closeError error

	mu         sync.Mutex
	stderrTail []byte
	exitErr    error
	exitCode   int
	closing    atomic.Bool
}

func (c *stdioConn) writeStdin() {
	_, _ = io.Copy(c.stdin, c.processEnd)
	_ = c.stdin.Close()
}

func (c *stdioConn) readStdout() {
	_, _ = io.Copy(c.processEnd, c.stdout)
	_ = c.processEnd.Close()
	close(c.outputDone)
}

// drainStderr keeps the tail of the guest stderr so a failed transport can be
// explained. It never writes guest diagnostics into the Docker byte stream.
func (c *stdioConn) drainStderr() {
	defer close(c.stderrDone)
	buffer := make([]byte, 4096)
	for {
		read, err := c.stderr.Read(buffer)
		if read > 0 {
			c.retainStderr(buffer[:read])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) && !c.closing.Load() {
				logging.Errorf("guest docker transport stderr read failed: %v", err)
			}
			return
		}
	}
}

func (c *stdioConn) retainStderr(chunk []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stderrTail = append(c.stderrTail, chunk...)
	if overflow := len(c.stderrTail) - stderrDrainLimit; overflow > 0 {
		c.stderrTail = append(c.stderrTail[:0], c.stderrTail[overflow:]...)
	}
}

func (c *stdioConn) wait() {
	defer close(c.waitDone)
	// Wait returns once the guest process exits; cmd.WaitDelay bounds how long it
	// then waits for the pipes, which only close when no child holds them.
	err := c.cmd.Wait()
	if c.cancel != nil {
		c.cancel()
	}
	// Give the drain goroutines a moment to record the tail of the stderr stream.
	if !drainWithin(pipeDrainTimeout, c.outputDone, c.stderrDone) {
		logging.Errorf("guest docker transport pipes stayed open for %s", pipeDrainTimeout)
	}
	c.mu.Lock()
	c.exitErr = err
	if c.cmd.ProcessState != nil {
		c.exitCode = c.cmd.ProcessState.ExitCode()
	}
	abnormal := err != nil || c.exitCode != 0
	diagnostic := ""
	if abnormal && !c.closing.Load() {
		diagnostic = c.diagnosticLocked()
	}
	c.mu.Unlock()
	if diagnostic == "" {
		return
	}
	logging.Errorf("%s", diagnostic)
	if c.dialer != nil {
		c.dialer.recordFailure(diagnostic)
	}
}

func (c *stdioConn) diagnosticLocked() string {
	var message string
	switch {
	case c.exitErr != nil && errors.Is(c.exitErr, exec.ErrWaitDelay):
		message = fmt.Sprintf("guest docker transport exited with code %d and left its pipes open", c.exitCode)
	case c.exitErr != nil:
		message = fmt.Sprintf("guest docker transport exited with code %d: %v", c.exitCode, c.exitErr)
	default:
		message = fmt.Sprintf("guest docker transport exited with code %d", c.exitCode)
	}
	if tail := strings.TrimSpace(string(c.stderrTail)); tail != "" {
		message += ": " + tail
	}
	return message
}

// drainWithin reports whether all channels closed before the timeout elapsed.
func drainWithin(timeout time.Duration, channels ...chan struct{}) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for _, channel := range channels {
		select {
		case <-channel:
		case <-timer.C:
			return false
		}
	}
	return true
}

func (c *stdioConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeError = c.teardown()
		close(c.closeDone)
	})
	<-c.closeDone
	return c.closeError
}

func (c *stdioConn) teardown() error {
	c.closing.Store(true)
	if c.cancel != nil {
		c.cancel()
	}
	closeErr := c.Conn.Close()
	_ = c.processEnd.Close()
	_ = c.stdin.Close()
	_ = c.stdout.Close()
	_ = c.stderr.Close()
	select {
	case <-c.waitDone:
	case <-time.After(guestShutdownGrace):
		logging.Errorf("guest docker transport did not stop within %s; killing it", guestShutdownGrace)
		if c.cmd.Process != nil {
			if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				logging.Errorf("guest docker transport kill failed: %v", err)
			}
		}
		select {
		case <-c.waitDone:
		case <-time.After(guestKillGrace):
			logging.Errorf("guest docker transport did not exit after being killed")
		}
	}
	return closeErr
}
