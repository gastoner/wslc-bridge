package guest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"wslc-bridge/internal/logging"
)

func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv("WSLC_BRIDGE_STDIO_HELPER") != "1" {
		return
	}
	if os.Getenv("WSLC_BRIDGE_STDIO_FAIL") == "1" {
		_, _ = fmt.Fprint(os.Stderr, "wslc.exe could not start the guest session\n")
		os.Exit(3)
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if line == "\r\n" {
			break
		}
	}
	_, _ = fmt.Fprint(os.Stdout, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nOK")
	_, _ = fmt.Fprint(os.Stderr, "helper diagnostic must not enter HTTP output\n")
	_, _ = io.Copy(io.Discard, reader)
}

func TestStdioDialerCarriesRawBytesAndSeparatesStderr(t *testing.T) {
	dialer := &StdioDialer{
		WSLCPath: os.Args[0],
		Command:  []string{"-test.run=TestStdioHelperProcess"},
		Env:      []string{"WSLC_BRIDGE_STDIO_HELPER=1"},
	}
	connection, err := dialer.DialContext(context.Background(), "tcp", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	request := "GET /_ping HTTP/1.1\r\nHost: docker\r\nConnection: close\r\n\r\n"
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatal(readErr)
		}
		if line == "\r\n" {
			break
		}
	}
	body := make([]byte, 2)
	if _, err := io.ReadFull(reader, body); err != nil {
		t.Fatal(err)
	}
	if status != "HTTP/1.1 200 OK\r\n" || string(body) != "OK" {
		t.Fatalf("unexpected response status %q body %q", status, body)
	}
	if strings.Contains(status+string(body), "helper diagnostic") {
		t.Fatal("stderr leaked into the Docker HTTP byte stream")
	}
}

// A guest process that exits before serving a request must leave behind the exit
// status and its stderr, otherwise the HTTP layer can only report a bare EOF.
func TestStdioDialerReportsTheGuestExitStatusAndStderr(t *testing.T) {
	logging.SetOutput(io.Discard)
	t.Cleanup(func() { logging.SetOutput(os.Stderr) })
	dialer := &StdioDialer{
		WSLCPath: os.Args[0],
		Command:  []string{"-test.run=TestStdioHelperProcess"},
		Env:      []string{"WSLC_BRIDGE_STDIO_HELPER=1", "WSLC_BRIDGE_STDIO_FAIL=1"},
	}
	connection, err := dialer.DialContext(context.Background(), "tcp", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 16)
	for {
		if _, err := connection.Read(buffer); err != nil {
			// The pipe carries a bare EOF: the cause only survives in the diagnostic.
			if !errors.Is(err, io.EOF) {
				t.Fatalf("guest transport read error = %v, want EOF", err)
			}
			break
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	var diagnostic string
	for time.Now().Before(deadline) {
		if diagnostic = dialer.Diagnostic(); diagnostic != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diagnostic, "exited with code 3") {
		t.Fatalf("diagnostic %q does not report the guest exit code", diagnostic)
	}
	if !strings.Contains(diagnostic, "could not start the guest session") {
		t.Fatalf("diagnostic %q does not retain the guest stderr", diagnostic)
	}
}
