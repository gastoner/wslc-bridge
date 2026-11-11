package dockerapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"wslc-bridge/internal/guest"
	"wslc-bridge/internal/process"
)

type dockerPortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type dockerCreateRequest struct {
	Hostname     string                     `json:"Hostname"`
	Domainname   string                     `json:"Domainname"`
	User         string                     `json:"User"`
	Image        string                     `json:"Image"`
	ExposedPorts map[string]json.RawMessage `json:"ExposedPorts"`
	Healthcheck  json.RawMessage            `json:"Healthcheck"`
	Cmd          []string                   `json:"Cmd"`
	Entrypoint   []string                   `json:"Entrypoint"`
	Env          []string                   `json:"Env"`
	Labels       map[string]string          `json:"Labels"`
	WorkingDir   string                     `json:"WorkingDir"`
	StopSignal   string                     `json:"StopSignal"`
	StopTimeout  *int                       `json:"StopTimeout"`
	ArgsEscaped  bool                       `json:"ArgsEscaped"`
	OpenStdin    bool                       `json:"OpenStdin"`
	AttachStdin  bool                       `json:"AttachStdin"`
	AttachStdout bool                       `json:"AttachStdout"`
	AttachStderr bool                       `json:"AttachStderr"`
	Tty          bool                       `json:"Tty"`
	StdinOnce    bool                       `json:"StdinOnce"`
	// Podman Desktop uses this extension option to request a separate start call after create.
	Start            bool                       `json:"start"`
	Volumes          map[string]json.RawMessage `json:"Volumes"`
	MacAddress       string                     `json:"MacAddress"`
	NetworkDisabled  bool                       `json:"NetworkDisabled"`
	OnBuild          []string                   `json:"OnBuild"`
	Shell            []string                   `json:"Shell"`
	HostConfig       dockerHostConfig           `json:"HostConfig"`
	NetworkingConfig dockerNetworkingConfig     `json:"NetworkingConfig"`
}

type dockerMount struct {
	Type          string          `json:"Type"`
	Source        string          `json:"Source"`
	Target        string          `json:"Target"`
	ReadOnly      bool            `json:"ReadOnly"`
	Consistency   string          `json:"Consistency"`
	BindOptions   json.RawMessage `json:"BindOptions"`
	VolumeOptions json.RawMessage `json:"VolumeOptions"`
	TmpfsOptions  json.RawMessage `json:"TmpfsOptions"`
}

type dockerEndpointConfig struct {
	Aliases    []string          `json:"Aliases"`
	IPAMConfig json.RawMessage   `json:"IPAMConfig"`
	Links      []string          `json:"Links"`
	DriverOpts map[string]string `json:"DriverOpts"`
}

type dockerNetworkingConfig struct {
	EndpointsConfig map[string]dockerEndpointConfig `json:"EndpointsConfig"`
}

type dockerLogConfig struct {
	Type   string            `json:"Type"`
	Config map[string]string `json:"Config"`
}

type dockerHostConfig struct {
	Binds              []string                       `json:"Binds"`
	Mounts             []dockerMount                  `json:"Mounts"`
	PortBindings       map[string][]dockerPortBinding `json:"PortBindings"`
	PublishAllPorts    bool                           `json:"PublishAllPorts"`
	NetworkMode        string                         `json:"NetworkMode"`
	NetworkAliases     []string                       `json:"NetworkAliases"`
	DNS                []string                       `json:"Dns"`
	DNSSearch          []string                       `json:"DnsSearch"`
	DNSOptions         []string                       `json:"DnsOptions"`
	Ulimits            []dockerUlimit                 `json:"Ulimits"`
	Tmpfs              map[string]string              `json:"Tmpfs"`
	LogConfig          dockerLogConfig                `json:"LogConfig"`
	RestartPolicy      dockerRestartPolicy            `json:"RestartPolicy"`
	Privileged         bool                           `json:"Privileged"`
	CapAdd             []string                       `json:"CapAdd"`
	CapDrop            []string                       `json:"CapDrop"`
	Devices            []json.RawMessage              `json:"Devices"`
	ExtraHosts         []string                       `json:"ExtraHosts"`
	ReadonlyRootfs     bool                           `json:"ReadonlyRootfs"`
	Memory             int64                          `json:"Memory"`
	CPUS               float64                        `json:"NanoCPUs"`
	ShmSize            int64                          `json:"ShmSize"`
	AutoRemove         bool                           `json:"AutoRemove"`
	DeviceRequests     []json.RawMessage              `json:"DeviceRequests"`
	SecurityOpt        []string                       `json:"SecurityOpt"`
	PidMode            string                         `json:"PidMode"`
	IpcMode            string                         `json:"IpcMode"`
	UTSMode            string                         `json:"UTSMode"`
	UsernsMode         string                         `json:"UsernsMode"`
	CgroupnsMode       string                         `json:"CgroupnsMode"`
	Runtime            string                         `json:"Runtime"`
	VolumeDriver       string                         `json:"VolumeDriver"`
	MemorySwap         int64                          `json:"MemorySwap"`
	MemoryReservation  int64                          `json:"MemoryReservation"`
	CPUQuota           int64                          `json:"CpuQuota"`
	CPUPeriod          int64                          `json:"CpuPeriod"`
	CPUShares          int64                          `json:"CpuShares"`
	CpusetCpus         string                         `json:"CpusetCpus"`
	CpusetMems         string                         `json:"CpusetMems"`
	CgroupParent       string                         `json:"CgroupParent"`
	GroupAdd           []string                       `json:"GroupAdd"`
	Links              []string                       `json:"Links"`
	VolumesFrom        []string                       `json:"VolumesFrom"`
	Sysctls            map[string]string              `json:"Sysctls"`
	OomScoreAdj        int                            `json:"OomScoreAdj"`
	PidsLimit          int64                          `json:"PidsLimit"`
	CpuRealtimePeriod  int64                          `json:"CpuRealtimePeriod"`
	CpuRealtimeRuntime int64                          `json:"CpuRealtimeRuntime"`
	BlkioWeight        uint16                         `json:"BlkioWeight"`
	MemorySwappiness   int64                          `json:"MemorySwappiness"`
	Init               *bool                          `json:"Init"`
}

type dockerRestartPolicy struct {
	Name              string `json:"Name"`
	MaximumRetryCount int    `json:"MaximumRetryCount"`
}

type dockerUlimit struct {
	Name string `json:"Name"`
	Soft int64  `json:"Soft"`
	Hard int64  `json:"Hard"`
}

func (value *dockerCreateRequest) UnmarshalJSON(data []byte) error {
	type plain dockerCreateRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := rejectUnknownJSONFields(data, reflect.TypeOf(plain{}), "container config"); err != nil {
		return err
	}
	*value = dockerCreateRequest(decoded)
	return nil
}

func (value *dockerHostConfig) UnmarshalJSON(data []byte) error {
	type plain dockerHostConfig
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := rejectUnknownJSONFields(data, reflect.TypeOf(plain{}), "HostConfig"); err != nil {
		return err
	}
	*value = dockerHostConfig(decoded)
	return nil
}

func (value *dockerMount) UnmarshalJSON(data []byte) error {
	type plain dockerMount
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := rejectUnknownJSONFields(data, reflect.TypeOf(plain{}), "Mount"); err != nil {
		return err
	}
	*value = dockerMount(decoded)
	return nil
}

func (value *dockerEndpointConfig) UnmarshalJSON(data []byte) error {
	type plain dockerEndpointConfig
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := rejectUnknownJSONFields(data, reflect.TypeOf(plain{}), "network endpoint config"); err != nil {
		return err
	}
	*value = dockerEndpointConfig(decoded)
	return nil
}

func (value *dockerNetworkingConfig) UnmarshalJSON(data []byte) error {
	type plain dockerNetworkingConfig
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := rejectUnknownJSONFields(data, reflect.TypeOf(plain{}), "NetworkingConfig"); err != nil {
		return err
	}
	*value = dockerNetworkingConfig(decoded)
	return nil
}

func (value *dockerLogConfig) UnmarshalJSON(data []byte) error {
	type plain dockerLogConfig
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := rejectUnknownJSONFields(data, reflect.TypeOf(plain{}), "LogConfig"); err != nil {
		return err
	}
	*value = dockerLogConfig(decoded)
	return nil
}

func (value *dockerRestartPolicy) UnmarshalJSON(data []byte) error {
	type plain dockerRestartPolicy
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := rejectUnknownJSONFields(data, reflect.TypeOf(plain{}), "RestartPolicy"); err != nil {
		return err
	}
	*value = dockerRestartPolicy(decoded)
	return nil
}

func (value *dockerUlimit) UnmarshalJSON(data []byte) error {
	type plain dockerUlimit
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := rejectUnknownJSONFields(data, reflect.TypeOf(plain{}), "Ulimit"); err != nil {
		return err
	}
	*value = dockerUlimit(decoded)
	return nil
}

func rejectUnknownJSONFields(data []byte, target reflect.Type, label string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	known := make(map[string]struct{}, target.NumField())
	for index := 0; index < target.NumField(); index++ {
		field := target.Field(index)
		if field.PkgPath != "" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" {
			name = field.Name
		}
		if name != "-" {
			known[strings.ToLower(name)] = struct{}{}
		}
	}
	unknown := make([]string, 0)
	for name, raw := range fields {
		if _, ok := known[strings.ToLower(name)]; !ok && !isEmptyJSONValue(raw) {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		return fmt.Errorf("%s field %q is not supported by WSLC v1", label, unknown[0])
	}
	return nil
}

func isEmptyJSONValue(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	if bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("false")) || bytes.Equal(trimmed, []byte("0")) || bytes.Equal(trimmed, []byte("\"\"")) || bytes.Equal(trimmed, []byte("[]")) || bytes.Equal(trimmed, []byte("{}")) {
		return true
	}
	if len(trimmed) == 0 {
		return true
	}
	if trimmed[0] == '{' {
		var values map[string]json.RawMessage
		return json.Unmarshal(trimmed, &values) == nil && len(values) == 0
	}
	if trimmed[0] == '[' {
		var values []json.RawMessage
		return json.Unmarshal(trimmed, &values) == nil && len(values) == 0
	}
	if trimmed[0] == '"' {
		var text string
		return json.Unmarshal(trimmed, &text) == nil && text == ""
	}
	return false
}

var validNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func (h *Handler) createContainer(w http.ResponseWriter, r *http.Request) {
	var request dockerCreateRequest
	if err := decodeRequest(r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}
	request.Image = strings.TrimSpace(request.Image)
	arguments, err := buildCreateArgs(request, r.URL.Query().Get("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// WSLC create implicitly pulls a missing image. Preflight against the guest
	// image API to preserve Docker's create-without-pull behavior in the common case.
	imageInspect := r.Clone(r.Context())
	imageInspect.Method = http.MethodGet
	imageInspect.URL = cloneURL(r.URL)
	imageInspect.URL.Path = "/images/" + request.Image + "/json"
	imageInspect.URL.RawPath = ""
	imageInspect.Body = nil
	imageInspect.ContentLength = 0
	imageResponse, imageErr := h.doGuest(r.Context(), imageInspect)
	if imageErr != nil {
		writeError(w, http.StatusBadGateway, "could not verify image before create: "+guest.Failure(h.guest, imageErr))
		return
	}
	_ = imageResponse.Body.Close()
	if imageResponse.StatusCode == http.StatusNotFound {
		writeError(w, http.StatusNotFound, "image must be pulled before container create")
		return
	}
	if imageResponse.StatusCode < 200 || imageResponse.StatusCode >= 300 {
		writeError(w, http.StatusBadGateway, "guest Docker image inspect returned "+imageResponse.Status)
		return
	}
	id, err := h.native.CreateContainer(r.Context(), arguments)
	if err != nil {
		writeError(w, commandStatus(err), "wslc container create failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"Id": id, "Warnings": []string{}})
}

func buildCreateArgs(request dockerCreateRequest, name string) ([]string, error) {
	image := strings.TrimSpace(request.Image)
	if image == "" {
		return nil, errors.New("Image is required")
	}
	if name != "" && !validNamePattern.MatchString(name) {
		return nil, errors.New("container name contains unsupported characters")
	}
	if hasUnsupportedHostConfig(request.HostConfig) {
		return nil, errors.New("requested host configuration is not supported by WSLC v1")
	}
	if request.NetworkDisabled || request.MacAddress != "" || len(request.OnBuild) > 0 || len(request.Shell) > 0 || request.AttachStdin || request.AttachStdout || request.AttachStderr || request.StdinOnce || request.ArgsEscaped || request.StopTimeout != nil {
		return nil, errors.New("requested container configuration is not supported by WSLC v1")
	}
	if len(request.Healthcheck) > 0 && string(request.Healthcheck) != "null" {
		return nil, errors.New("container health checks are not supported by WSLC 2.9.3")
	}
	if len(request.Volumes) > 0 {
		return nil, errors.New("anonymous volume declarations are not supported; specify a Windows bind or named volume")
	}
	if len(request.Entrypoint) > 1 {
		return nil, errors.New("WSLC supports only a single entrypoint value")
	}
	if request.HostConfig.Memory < 0 || request.HostConfig.CPUS < 0 || request.HostConfig.ShmSize < 0 {
		return nil, errors.New("memory, NanoCPUs, and ShmSize must not be negative")
	}
	if err := validateExposedPorts(request.ExposedPorts, request.HostConfig.PortBindings, request.HostConfig.PublishAllPorts); err != nil {
		return nil, err
	}
	networkMode := strings.TrimSpace(request.HostConfig.NetworkMode)
	if networkMode == "host" || strings.HasPrefix(networkMode, "container:") || networkMode == "none" {
		return nil, fmt.Errorf("network mode %q is not supported", networkMode)
	}
	if networkMode == "default" {
		networkMode = "bridge"
	}
	arguments := []string{"container", "create"}
	if name != "" {
		arguments = append(arguments, "--name", name)
	}
	if request.Hostname != "" {
		arguments = append(arguments, "--hostname", request.Hostname)
	}
	if request.Domainname != "" {
		arguments = append(arguments, "--domainname", request.Domainname)
	}
	if request.User != "" {
		arguments = append(arguments, "--user", request.User)
	}
	if request.WorkingDir != "" {
		arguments = append(arguments, "--workdir", request.WorkingDir)
	}
	if request.StopSignal != "" {
		arguments = append(arguments, "--stop-signal", request.StopSignal)
	}
	if len(request.Entrypoint) == 1 {
		arguments = append(arguments, "--entrypoint", request.Entrypoint[0])
	}
	for _, value := range request.Env {
		if !strings.Contains(value, "=") {
			return nil, fmt.Errorf("environment entry %q must include an explicit value", value)
		}
		arguments = append(arguments, "--env", value)
	}
	labelKeys := make([]string, 0, len(request.Labels))
	for key := range request.Labels {
		labelKeys = append(labelKeys, key)
	}
	sort.Strings(labelKeys)
	for _, key := range labelKeys {
		arguments = append(arguments, "--label", key+"="+request.Labels[key])
	}
	if networkMode != "" && networkMode != "bridge" {
		arguments = append(arguments, "--network", networkMode)
	}
	for _, alias := range request.HostConfig.NetworkAliases {
		arguments = append(arguments, "--network-alias", alias)
	}
	for _, value := range request.HostConfig.DNS {
		arguments = append(arguments, "--dns", value)
	}
	for _, value := range request.HostConfig.DNSSearch {
		arguments = append(arguments, "--dns-search", value)
	}
	for _, value := range request.HostConfig.DNSOptions {
		arguments = append(arguments, "--dns-option", value)
	}
	for _, value := range request.HostConfig.Ulimits {
		if value.Name == "" {
			return nil, errors.New("ulimit Name is required")
		}
		arguments = append(arguments, "--ulimit", fmt.Sprintf("%s=%d:%d", value.Name, value.Soft, value.Hard))
	}
	if len(request.NetworkingConfig.EndpointsConfig) > 1 {
		return nil, errors.New("multiple network attachments are not supported")
	}
	endpointNetworks := make([]string, 0, len(request.NetworkingConfig.EndpointsConfig))
	for network := range request.NetworkingConfig.EndpointsConfig {
		endpointNetworks = append(endpointNetworks, network)
	}
	sort.Strings(endpointNetworks)
	for _, network := range endpointNetworks {
		endpoint := request.NetworkingConfig.EndpointsConfig[network]
		if len(endpoint.Links) > 0 || len(endpoint.DriverOpts) > 0 {
			return nil, errors.New("network endpoint links and driver options are not supported")
		}
		if len(endpoint.IPAMConfig) > 0 && string(endpoint.IPAMConfig) != "null" && string(endpoint.IPAMConfig) != "{}" {
			return nil, errors.New("per-network IPAM settings are not supported")
		}
		if networkMode != "" && networkMode != "bridge" && networkMode != network {
			return nil, errors.New("multiple network attachments are not supported")
		}
		if network != "bridge" && network != networkMode {
			arguments = append(arguments, "--network", network)
		}
		for _, alias := range endpoint.Aliases {
			arguments = append(arguments, "--network-alias", alias)
		}
	}
	if request.HostConfig.PublishAllPorts {
		arguments = append(arguments, "--publish-all")
	}
	portKeys := make([]string, 0, len(request.HostConfig.PortBindings))
	for key := range request.HostConfig.PortBindings {
		portKeys = append(portKeys, key)
	}
	sort.Strings(portKeys)
	for _, key := range portKeys {
		for _, binding := range request.HostConfig.PortBindings[key] {
			publish, err := dockerPublishSpec(key, binding)
			if err != nil {
				return nil, err
			}
			arguments = append(arguments, "--publish", publish)
		}
	}
	for _, bind := range request.HostConfig.Binds {
		mapped, err := mapBind(bind)
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, "--volume", mapped)
	}
	for _, mount := range request.HostConfig.Mounts {
		if mount.Type != "bind" && mount.Type != "volume" {
			return nil, fmt.Errorf("mount type %q is not supported", mount.Type)
		}
		if mount.Source == "" || mount.Target == "" || !strings.HasPrefix(mount.Target, "/") {
			return nil, errors.New("mount Source and Target are required")
		}
		if mount.Consistency != "" || hasMountOptions(mount.BindOptions) || hasMountOptions(mount.VolumeOptions) || hasMountOptions(mount.TmpfsOptions) {
			return nil, errors.New("mount propagation, consistency, volume, and tmpfs options are not supported")
		}
		mode := "rw"
		if mount.ReadOnly {
			mode = "ro"
		}
		arguments = append(arguments, "--volume", mount.Source+":"+mount.Target+":"+mode)
	}
	tmpfsTargets := make([]string, 0, len(request.HostConfig.Tmpfs))
	for target := range request.HostConfig.Tmpfs {
		tmpfsTargets = append(tmpfsTargets, target)
	}
	sort.Strings(tmpfsTargets)
	for _, target := range tmpfsTargets {
		options := request.HostConfig.Tmpfs[target]
		value := target
		if options != "" {
			value += ":" + options
		}
		arguments = append(arguments, "--tmpfs", value)
	}
	if request.HostConfig.Memory > 0 {
		arguments = append(arguments, "--memory", strconv.FormatInt(request.HostConfig.Memory, 10))
	}
	if request.HostConfig.CPUS > 0 {
		arguments = append(arguments, "--cpus", strconv.FormatFloat(request.HostConfig.CPUS/1e9, 'f', -1, 64))
	}
	if request.HostConfig.ShmSize > 0 {
		arguments = append(arguments, "--shm-size", strconv.FormatInt(request.HostConfig.ShmSize, 10))
	}
	if request.OpenStdin {
		arguments = append(arguments, "--interactive")
	}
	if request.Tty {
		arguments = append(arguments, "--tty")
	}
	arguments = append(arguments, image)
	arguments = append(arguments, request.Cmd...)
	return arguments, nil
}

func hasUnsupportedHostConfig(config dockerHostConfig) bool {
	return config.Privileged || config.RestartPolicy.Name != "" || config.RestartPolicy.MaximumRetryCount != 0 ||
		len(config.CapAdd) > 0 || len(config.CapDrop) > 0 || len(config.Devices) > 0 || len(config.DeviceRequests) > 0 ||
		len(config.ExtraHosts) > 0 || config.ReadonlyRootfs || config.AutoRemove || len(config.SecurityOpt) > 0 || config.LogConfig.Type != "" || len(config.LogConfig.Config) > 0 ||
		config.PidMode != "" || config.IpcMode != "" || config.UTSMode != "" || config.UsernsMode != "" || config.CgroupnsMode != "" ||
		config.Runtime != "" || config.VolumeDriver != "" || config.MemorySwap != 0 || config.MemoryReservation != 0 ||
		config.CPUQuota != 0 || config.CPUPeriod != 0 || config.CPUShares != 0 || config.CpusetCpus != "" || config.CpusetMems != "" ||
		config.CgroupParent != "" || len(config.GroupAdd) > 0 || len(config.Links) > 0 || len(config.VolumesFrom) > 0 || len(config.Sysctls) > 0 ||
		config.OomScoreAdj != 0 || config.PidsLimit != 0 || config.CpuRealtimePeriod != 0 || config.CpuRealtimeRuntime != 0 ||
		config.BlkioWeight != 0 || config.MemorySwappiness != 0 || config.Init != nil
}

func hasMountOptions(value json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(value))
	return trimmed != "" && trimmed != "null" && trimmed != "{}"
}

func dockerPublishSpec(key string, binding dockerPortBinding) (string, error) {
	port, protocol, ok := parsePublishPortKey(key)
	if !ok {
		return "", fmt.Errorf("published container port %q is invalid", key)
	}
	hostIP := strings.TrimSpace(binding.HostIP)
	if hostIP == "" {
		hostIP = "0.0.0.0"
	}
	parsedIP := net.ParseIP(hostIP)
	if parsedIP == nil {
		return "", fmt.Errorf("published host IP %q is invalid", hostIP)
	}
	if parsedIP.To4() == nil {
		return "", fmt.Errorf("IPv6 published host IP %q is not supported by WSLC v1", hostIP)
	}
	hostPort := strings.TrimSpace(binding.HostPort)
	if hostPort == "0" {
		hostPort = ""
	}
	if hostPort != "" {
		value, err := strconv.Atoi(hostPort)
		if err != nil || value < 1 || value > 65535 {
			return "", fmt.Errorf("published host port %q must be between 1 and 65535 or empty for dynamic allocation", hostPort)
		}
	}
	return fmt.Sprintf("%s:%s:%d/%s", hostIP, hostPort, port, protocol), nil
}

func parsePublishPortKey(value string) (int, string, bool) {
	parts := strings.SplitN(value, "/", 2)
	if len(parts) != 2 || (parts[1] != "tcp" && parts[1] != "udp") {
		return 0, "", false
	}
	port, err := strconv.Atoi(parts[0])
	return port, parts[1], err == nil && port > 0 && port <= 65535
}

func validateExposedPorts(exposed map[string]json.RawMessage, bindings map[string][]dockerPortBinding, publishAll bool) error {
	for port := range exposed {
		if _, published := bindings[port]; published || publishAll {
			continue
		}
		return fmt.Errorf("exposed port %q has no WSLC CLI mapping; publish it explicitly or use PublishAllPorts", port)
	}
	return nil
}

func mapBind(value string) (string, error) {
	separator := strings.Index(value, ":")
	if separator == 1 && len(value) > 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) {
		separator = strings.Index(value[2:], ":")
		if separator >= 0 {
			separator += 2
		}
	}
	if separator <= 0 || separator == len(value)-1 {
		return "", fmt.Errorf("bind %q must be source:container-path[:ro|rw]", value)
	}
	source := value[:separator]
	parts := strings.Split(value[separator+1:], ":")
	if len(parts) < 1 || len(parts) > 2 || !strings.HasPrefix(parts[0], "/") {
		return "", fmt.Errorf("bind %q must use an absolute container path and optional ro/rw mode", value)
	}
	mode := "rw"
	if len(parts) == 2 {
		mode = parts[1]
	}
	if mode != "ro" && mode != "rw" {
		return "", fmt.Errorf("bind mode %q is unsupported; use ro or rw", mode)
	}
	return source + ":" + parts[0] + ":" + mode, nil
}

func (h *Handler) containerStart(w http.ResponseWriter, r *http.Request, path string) {
	id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/start")
	_, err := h.native.RunWithTimeout(r.Context(), 2*time.Minute, "container", "start", id)
	if err != nil {
		message := strings.ToLower(process.StderrOf(err))
		if strings.Contains(message, "already running") || strings.Contains(message, "already started") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, commandStatus(err), "wslc container start failed: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) containerStop(w http.ResponseWriter, r *http.Request, path string) {
	id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/stop")
	query := r.URL.Query()
	if strings.EqualFold(query.Get("signal"), "") {
		query.Del("signal")
	}
	arguments := []string{"container", "stop"}
	if signal := query.Get("signal"); signal != "" {
		arguments = append(arguments, "--signal", signal)
	}
	if timeout := query.Get("t"); timeout != "" {
		seconds, err := strconv.Atoi(timeout)
		if err != nil || seconds < -1 {
			writeError(w, http.StatusBadRequest, "stop timeout must be -1 or a non-negative number of seconds")
			return
		}
		arguments = append(arguments, "--time", strconv.Itoa(seconds))
	}
	arguments = append(arguments, id)
	_, err := h.native.RunWithTimeout(r.Context(), 2*time.Minute, arguments...)
	if err != nil {
		message := strings.ToLower(process.StderrOf(err))
		if strings.Contains(message, "not running") || strings.Contains(message, "already stopped") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, commandStatus(err), "wslc container stop failed: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) containerRemove(w http.ResponseWriter, r *http.Request, path string) {
	id := strings.TrimPrefix(path, "/containers/")
	query := r.URL.Query()
	if query.Get("v") == "1" || strings.EqualFold(query.Get("v"), "true") {
		writeError(w, http.StatusNotImplemented, "removing associated volumes is not supported by WSLC v1")
		return
	}
	arguments := []string{"container", "remove"}
	if query.Get("force") == "1" || strings.EqualFold(query.Get("force"), "true") {
		arguments = append(arguments, "--force")
	}
	arguments = append(arguments, id)
	_, err := h.native.RunWithTimeout(r.Context(), 2*time.Minute, arguments...)
	if err != nil {
		writeError(w, commandStatus(err), "wslc container remove failed: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeRequest(r *http.Request, destination any) error {
	if r.Body == nil {
		return errors.New("request body is required")
	}
	if err := decodeBody(r, destination); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("request body is required")
		}
		return err
	}
	return nil
}

// decodeOptionalRequest decodes a body that may be absent, such as the exec start
// options. An empty or absent body is valid there; ContentLength is not a
// reliable signal because it is -1 for chunked requests.
func decodeOptionalRequest(r *http.Request, destination any) error {
	if r.Body == nil {
		return nil
	}
	if err := decodeBody(r, destination); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func decodeBody(r *http.Request, destination any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxRequestBodyBytes))
	if err := decoder.Decode(destination); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errRequestEntityTooLarge
		}
		return fmt.Errorf("decode request JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON value")
	}
	return nil
}

func cloneURL(value *url.URL) *url.URL {
	copy := *value
	return &copy
}
