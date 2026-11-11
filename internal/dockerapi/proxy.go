package dockerapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"wslc-bridge/internal/guest"
	"wslc-bridge/internal/logging"
)

const maxJSONResponseBytes = 32 << 20

func (h *Handler) proxy(w http.ResponseWriter, r *http.Request) {
	response, err := h.doGuest(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusBadGateway, "guest Docker API request failed: "+guest.Failure(h.guest, err))
		return
	}
	defer response.Body.Close()
	copyResponseHeaders(w.Header(), response.Header)
	if r.Method == http.MethodHead {
		w.WriteHeader(response.StatusCode)
		return
	}
	w.WriteHeader(response.StatusCode)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	copyGuestStream(w, r, response.Body)
}

// copyGuestStream relays a guest response body to the client. Headers are
// already written here, so a truncated stream cannot be repaired: abort the
// connection instead of letting net/http terminate a chunked response as if it
// had completed normally.
func copyGuestStream(w http.ResponseWriter, request *http.Request, body io.Reader) {
	if _, err := io.Copy(flushWriter{writer: w}, body); err != nil {
		logging.Errorf("%s %s: guest response stream interrupted: %v", request.Method, request.URL.Path, err)
		panic(http.ErrAbortHandler)
	}
}

func (h *Handler) doGuest(ctx context.Context, request *http.Request) (*http.Response, error) {
	upstream, err := http.NewRequestWithContext(ctx, request.Method, "http://docker"+request.URL.RequestURI(), request.Body)
	if err != nil {
		return nil, err
	}
	upstream.Header = request.Header.Clone()
	upstream.Host = "docker"
	upstream.ContentLength = request.ContentLength
	upstream.TransferEncoding = append([]string(nil), request.TransferEncoding...)
	return h.guest.Do(upstream)
}

func copyResponseHeaders(destination, source http.Header) {
	for key, values := range source {
		lower := strings.ToLower(key)
		if lower == "connection" || lower == "keep-alive" || lower == "proxy-authenticate" || lower == "proxy-authorization" || lower == "te" || lower == "trailer" || lower == "transfer-encoding" || lower == "upgrade" {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func readBounded(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxJSONResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxJSONResponseBytes {
		return nil, fmt.Errorf("Docker JSON response exceeds %d bytes", maxJSONResponseBytes)
	}
	return data, nil
}

type flushWriter struct {
	writer http.ResponseWriter
}

func (w flushWriter) Write(value []byte) (int, error) {
	n, err := w.writer.Write(value)
	if flusher, ok := w.writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}
