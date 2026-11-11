package dockerapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"wslc-bridge/internal/process"
)

const (
	maxRequestBodyBytes = 2 << 20
	// warningHeader reports a degraded response whose data could not be
	// completed, for example when the native WSLC port listing failed.
	warningHeader = "X-Wslc-Bridge-Warning"
)

var (
	errUnsupportedAPIVersion = errors.New("Docker API versions older than 1.24 are not supported")
	errRequestEntityTooLarge = errors.New("request body exceeds 2097152 bytes")
)

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// writeDecodeError maps request decoding failures onto Docker-compatible status
// codes: an oversized body is the client's fault and must not read as 400.
func writeDecodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errRequestEntityTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func commandStatus(err error) int {
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	message := strings.ToLower(process.StderrOf(err))
	if message != "" {
		if strings.Contains(message, "not found") || strings.Contains(message, "no such") {
			return http.StatusNotFound
		}
		if strings.Contains(message, "already") || strings.Contains(message, "conflict") || strings.Contains(message, "running") {
			return http.StatusConflict
		}
		return http.StatusBadGateway
	}
	return http.StatusBadGateway
}
