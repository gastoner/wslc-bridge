package dockerapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type interruptedBody struct {
	sent bool
}

func (b *interruptedBody) Read(target []byte) (int, error) {
	if b.sent {
		return 0, errors.New("guest transport closed mid-stream")
	}
	b.sent = true
	return copy(target, []byte("partial")), nil
}

func (b *interruptedBody) Close() error { return nil }

// A guest stream that dies mid-response must not be delivered as a complete
// chunked response; the client has to observe a truncated stream.
func TestProxyAbortsWhenTheGuestStreamIsInterrupted(t *testing.T) {
	guest := &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       &interruptedBody{},
		}, nil
	})}
	server := httptest.NewServer(NewHandler(nil, guest))
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/events")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	if _, err := io.ReadAll(response.Body); err == nil {
		t.Fatal("truncated guest stream was reported to the client as a complete response")
	}
}
