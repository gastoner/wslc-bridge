package dockerapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func requestWithBody(body io.Reader, contentLength int64) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/exec/exec-id/start", body)
	request.ContentLength = contentLength
	return request
}

func TestDecodeRequestRejectsAnEmptyBody(t *testing.T) {
	var destination map[string]any
	err := decodeRequest(requestWithBody(strings.NewReader(""), 0), &destination)
	if err == nil || err.Error() != "request body is required" {
		t.Fatalf("decodeRequest() error = %v, want a required-body error", err)
	}
}

func TestDecodeOptionalRequestAcceptsAnEmptyChunkedBody(t *testing.T) {
	var destination execStartRequest
	// ContentLength is -1 for chunked requests, which is why the exec start
	// route must not use it to decide whether a body is present.
	if err := decodeOptionalRequest(requestWithBody(strings.NewReader(""), -1), &destination); err != nil {
		t.Fatalf("decodeOptionalRequest() error = %v, want nil for an empty body", err)
	}
}

func TestDecodeRequestReportsOversizedBodiesAsTooLarge(t *testing.T) {
	oversized := strings.NewReader(`{"Name":"` + strings.Repeat("a", maxRequestBodyBytes) + `"}`)
	var destination map[string]any
	err := decodeRequest(requestWithBody(oversized, oversized.Size()), &destination)
	if !errors.Is(err, errRequestEntityTooLarge) {
		t.Fatalf("decodeRequest() error = %v, want the too-large sentinel", err)
	}
	response := httptest.NewRecorder()
	writeDecodeError(response, err)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("writeDecodeError() status = %d, want 413", response.Code)
	}
}

func TestDecodeRequestRejectsTrailingJSONValues(t *testing.T) {
	body := strings.NewReader(`{"Name":"bridge"}{"Name":"other"}`)
	var destination map[string]any
	err := decodeRequest(requestWithBody(body, body.Size()), &destination)
	if err == nil || !strings.Contains(err.Error(), "single JSON value") {
		t.Fatalf("decodeRequest() error = %v, want a single-value error", err)
	}
}
