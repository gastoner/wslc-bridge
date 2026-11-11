package app

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"wslc-bridge/internal/guest"
)

type DoctorResult struct {
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	WSLCVersion     string `json:"wslcVersion,omitempty"`
	WSLVersion      string `json:"wslVersion,omitempty"`
	GuestVersion    string `json:"guestVersion,omitempty"`
	GuestAPIVersion string `json:"guestApiVersion,omitempty"`
	Error           string `json:"error,omitempty"`
	ExitCode        int    `json:"-"`
}

func Doctor(ctx context.Context) DoctorResult {
	result := DoctorResult{OS: runtime.GOOS, Arch: runtime.GOARCH}
	info, err := DetectRuntime(ctx)
	if err != nil {
		result.Error = err.Error()
		result.ExitCode = 4
		if exitError, ok := err.(*ExitError); ok {
			result.ExitCode = exitError.Code
		}
		return result
	}
	result.WSLCVersion = info.WSLCVersion
	result.WSLVersion = info.WSLVersion
	client := guest.NewClient(info.WSLCPath)
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "http://docker/version", nil)
	if err != nil {
		result.Error = err.Error()
		result.ExitCode = 6
		return result
	}
	response, err := client.Do(request)
	if err != nil {
		result.Error = "guest Docker API unavailable: " + guest.Failure(client, err)
		result.ExitCode = 6
		return result
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.Error = "guest Docker API returned " + response.Status
		result.ExitCode = 6
		return result
	}
	var version struct {
		Version    string `json:"Version"`
		APIVersion string `json:"ApiVersion"`
	}
	if err := json.NewDecoder(response.Body).Decode(&version); err != nil {
		result.Error = guestVersionError(client, err)
		result.ExitCode = 6
		return result
	}
	result.GuestVersion = version.Version
	result.GuestAPIVersion = version.APIVersion
	return result
}
