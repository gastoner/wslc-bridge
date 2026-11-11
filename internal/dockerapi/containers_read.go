package dockerapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"wslc-bridge/internal/guest"
	"wslc-bridge/internal/logging"
	"wslc-bridge/internal/native"
	"wslc-bridge/internal/overlay"
)

type guestContainerSummary struct {
	ID    string `json:"Id"`
	IDAlt string `json:"ID"`
	State string `json:"State"`
}

func (h *Handler) listContainers(w http.ResponseWriter, r *http.Request) {
	response, err := h.doGuest(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusBadGateway, "guest Docker API request failed: "+guest.Failure(h.guest, err))
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		copyResponseHeaders(w.Header(), response.Header)
		w.WriteHeader(response.StatusCode)
		copyGuestStream(w, r, response.Body)
		return
	}
	body, err := readBounded(response.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var summaries []guestContainerSummary
	if err := json.Unmarshal(body, &summaries); err != nil {
		writeError(w, http.StatusBadGateway, "guest Docker returned malformed container list JSON")
		return
	}
	items, nativeListErr := h.native.ListContainers(r.Context())
	if nativeListErr != nil {
		logging.Errorf("native WSLC container list failed while listing containers: %v", nativeListErr)
	}
	nativeContainers := make(map[string]native.ContainerListItem, len(items))
	for _, item := range items {
		nativeContainers[normalizeContainerID(item.ID)] = item
	}
	byID := make(map[string]overlay.ContainerPorts)
	for _, summary := range summaries {
		id := summary.ID
		if id == "" {
			id = summary.IDAlt
		}
		if id == "" {
			continue
		}
		running := strings.EqualFold(summary.State, "running")
		ports := make(map[string][]native.PortBinding)
		if nativeListErr == nil {
			if item, ok := nativeContainers[normalizeContainerID(id)]; ok {
				ports = item.Ports
			}
		}
		byID[normalizeContainerID(id)] = overlay.ContainerPorts{Ports: ports, Running: running}
	}
	transformed, err := overlay.Summary(body, byID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not overlay native WSLC ports: "+err.Error())
		return
	}
	copyResponseHeaders(w.Header(), response.Header)
	w.Header().Del("Content-Length")
	w.Header().Del("Content-Encoding")
	w.Header().Set("Content-Type", "application/json")
	if nativeListErr != nil {
		w.Header().Set(warningHeader, "WSLC container list failed; published Windows ports may be missing")
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, bytes.NewReader(transformed))
}

func (h *Handler) inspectContainer(w http.ResponseWriter, r *http.Request, path string) {
	id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/json")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "container ID is required")
		return
	}
	response, err := h.doGuest(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusBadGateway, "guest Docker API request failed: "+guest.Failure(h.guest, err))
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		copyResponseHeaders(w.Header(), response.Header)
		w.WriteHeader(response.StatusCode)
		copyGuestStream(w, r, response.Body)
		return
	}
	body, err := readBounded(response.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	ports := make(map[string][]native.PortBinding)
	var nativeListErr error
	if items, listErr := h.native.ListContainers(r.Context()); listErr == nil {
		for _, item := range items {
			if normalizeContainerID(item.ID) == normalizeContainerID(id) {
				ports = item.Ports
				break
			}
		}
	} else {
		nativeListErr = listErr
		logging.Errorf("native WSLC container list failed while inspecting %s: %v", id, listErr)
	}
	transformed, err := overlay.Inspect(body, ports)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not overlay native WSLC ports: "+err.Error())
		return
	}
	copyResponseHeaders(w.Header(), response.Header)
	w.Header().Del("Content-Length")
	w.Header().Del("Content-Encoding")
	w.Header().Set("Content-Type", "application/json")
	if nativeListErr != nil {
		w.Header().Set(warningHeader, "WSLC container list failed; published Windows ports may be missing")
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, bytes.NewReader(transformed))
}

func (h *Handler) guestContainerRunning(r *http.Request, id string) (bool, int, error) {
	inspectURL := "http://docker/containers/" + url.PathEscape(id) + "/json"
	inspectRequest, err := http.NewRequestWithContext(r.Context(), http.MethodGet, inspectURL, nil)
	if err != nil {
		return false, http.StatusInternalServerError, err
	}
	response, err := h.guest.Do(inspectRequest)
	if err != nil {
		return false, http.StatusBadGateway, fmt.Errorf("%s", guest.Failure(h.guest, err))
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, response.StatusCode, fmt.Errorf("guest Docker inspect returned %s", response.Status)
	}
	body, err := readBounded(response.Body)
	if err != nil {
		return false, http.StatusBadGateway, err
	}
	var result struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return false, http.StatusBadGateway, fmt.Errorf("decode guest container state: %w", err)
	}
	return result.State.Running, http.StatusOK, nil
}

func normalizeContainerID(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "/")
}
