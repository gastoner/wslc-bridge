package dockerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

type networkCreateRequest struct {
	Name           string            `json:"Name"`
	CheckDuplicate bool              `json:"CheckDuplicate"`
	Driver         string            `json:"Driver"`
	Internal       bool              `json:"Internal"`
	Attachable     bool              `json:"Attachable"`
	Ingress        bool              `json:"Ingress"`
	EnableIPv6     bool              `json:"EnableIPv6"`
	Options        map[string]string `json:"Options"`
	Labels         map[string]string `json:"Labels"`
	IPAM           struct {
		Driver  string            `json:"Driver"`
		Config  []json.RawMessage `json:"Config"`
		Options map[string]string `json:"Options"`
	} `json:"IPAM"`
}

func (h *Handler) createNetwork(w http.ResponseWriter, r *http.Request) {
	var request networkCreateRequest
	if err := decodeRequest(r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}
	if strings.TrimSpace(request.Name) == "" || !validNamePattern.MatchString(request.Name) {
		writeError(w, http.StatusBadRequest, "a valid network Name is required")
		return
	}
	if request.Internal || request.Attachable || request.Ingress || request.EnableIPv6 || request.IPAM.Driver != "" || len(request.IPAM.Config) > 0 || len(request.IPAM.Options) > 0 {
		writeError(w, http.StatusNotImplemented, "requested network IPAM or mode options are not supported by WSLC 2.9.3")
		return
	}
	arguments := []string{"network", "create"}
	if request.Driver != "" {
		arguments = append(arguments, "--driver", request.Driver)
	}
	keys := make([]string, 0, len(request.Options))
	for key := range request.Options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		arguments = append(arguments, "--opt", key+"="+request.Options[key])
	}
	keys = keys[:0]
	for key := range request.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		arguments = append(arguments, "--label", key+"="+request.Labels[key])
	}
	arguments = append(arguments, request.Name)
	commandCtx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	id, err := h.native.CreateNetwork(commandCtx, arguments, request.Name)
	if err != nil {
		writeError(w, commandStatus(err), "wslc network create failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"Id": id, "Warning": ""})
}

func (h *Handler) removeNetwork(w http.ResponseWriter, r *http.Request, path string) {
	name := strings.TrimPrefix(path, "/networks/")
	arguments := []string{"network", "remove"}
	if r.URL.Query().Get("force") == "1" || strings.EqualFold(r.URL.Query().Get("force"), "true") {
		arguments = append(arguments, "--force")
	}
	arguments = append(arguments, name)
	_, err := h.native.RunWithTimeout(r.Context(), 2*time.Minute, arguments...)
	if err != nil {
		writeError(w, commandStatus(err), "wslc network remove failed: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
