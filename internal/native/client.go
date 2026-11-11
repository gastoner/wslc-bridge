package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"wslc-bridge/internal/logging"
	"wslc-bridge/internal/process"
	"wslc-bridge/internal/wslutil"
)

type Client struct {
	Runner *process.Runner
}

type PortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

func NewClient(wslcPath, wslPath string) *Client {
	return &Client{Runner: process.NewRunner(wslcPath, wslPath)}
}

func (c *Client) Run(ctx context.Context, args ...string) (process.Result, error) {
	return c.Runner.RunWSLC(ctx, args...)
}

func (c *Client) RunWithTimeout(ctx context.Context, timeout time.Duration, args ...string) (process.Result, error) {
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.Run(commandCtx, args...)
}

func (c *Client) ListContainers(ctx context.Context) ([]ContainerListItem, error) {
	result, err := c.RunWithTimeout(ctx, 15*time.Second, "container", "list", "--all", "--format", "json")
	if err != nil {
		return nil, err
	}
	if result.Truncated {
		return nil, fmt.Errorf("wslc container list output exceeded %d bytes", process.MaxOutputBytes)
	}
	return DecodeContainerList([]byte(wslutil.DecodeWindowsOutput(result.Stdout)))
}

var containerIDPattern = regexp.MustCompile(`^[0-9a-f]{12,64}$`)

func (c *Client) CreateContainer(ctx context.Context, args []string) (string, error) {
	result, err := c.RunWithTimeout(ctx, 2*time.Minute, args...)
	if err != nil {
		return "", err
	}
	identifier, ok := selectContainerID(wslutil.DecodeWindowsOutput(result.Stdout))
	if !ok {
		return "", errors.New("wslc container create returned an empty container ID")
	}
	if !containerIDPattern.MatchString(identifier) {
		logging.Errorf("wslc container create returned an unexpected container ID %q; using it unchanged", identifier)
	}
	return identifier, nil
}

// selectContainerID picks the container ID from wslc output. Trailing non-ID
// lines are skipped so a warning appended to stdout cannot become the ID.
func selectContainerID(output string) (string, bool) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	fallback := ""
	for i := len(lines) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(lines[i])
		if candidate == "" {
			continue
		}
		if containerIDPattern.MatchString(candidate) {
			return candidate, true
		}
		if fallback == "" {
			fallback = candidate
		}
	}
	return fallback, fallback != ""
}

func (c *Client) PullImage(ctx context.Context, reference string) error {
	_, err := c.RunWithTimeout(ctx, 30*time.Minute, "image", "pull", reference)
	return err
}

func (c *Client) ExecContainer(ctx context.Context, id string, command []string, stdout, stderr io.Writer) (int, error) {
	arguments := append([]string{"container", "exec", id}, command...)
	return c.Runner.RunWSLCStreaming(ctx, stdout, stderr, arguments...)
}

func (c *Client) CreateNetwork(ctx context.Context, args []string, name string) (string, error) {
	_, err := c.RunWithTimeout(ctx, 2*time.Minute, args...)
	if err != nil {
		return "", err
	}
	result, err := c.RunWithTimeout(ctx, 15*time.Second, "network", "inspect", name)
	if err != nil {
		return "", err
	}
	var entries []struct {
		ID    string `json:"Id"`
		IDAlt string `json:"ID"`
	}
	if err := json.Unmarshal([]byte(wslutil.DecodeWindowsOutput(result.Stdout)), &entries); err != nil {
		return "", fmt.Errorf("decode wslc network inspect: %w", err)
	}
	if len(entries) == 0 {
		return "", errors.New("wslc network inspect returned no network ID")
	}
	if entries[0].ID == "" {
		entries[0].ID = entries[0].IDAlt
	}
	if entries[0].ID == "" {
		return "", errors.New("wslc network inspect returned no network ID")
	}
	return entries[0].ID, nil
}

type ContainerListItem struct {
	ID    string
	State string
	Ports map[string][]PortBinding
}

func DecodeContainerList(data []byte) ([]ContainerListItem, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return []ContainerListItem{}, nil
	}
	var array []json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &array); err == nil {
		items := make([]ContainerListItem, 0, len(array))
		for _, raw := range array {
			item, err := decodeListItem(raw)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	}
	lines := strings.Split(trimmed, "\n")
	items := make([]ContainerListItem, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		item, err := decodeListItem(json.RawMessage(line))
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func decodeListItem(raw json.RawMessage) (ContainerListItem, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return ContainerListItem{}, fmt.Errorf("decode wslc container list item: %w", err)
	}
	for _, key := range []string{"Id", "ID"} {
		if value, ok := values[key]; ok {
			var id string
			if err := json.Unmarshal(value, &id); err == nil && id != "" {
				item := ContainerListItem{ID: id, Ports: make(map[string][]PortBinding)}
				if state, ok := values["State"]; ok {
					item.State = decodeContainerState(state)
				}
				if ports, ok := values["Ports"]; ok {
					if err := decodeContainerPorts(ports, &item); err != nil {
						return ContainerListItem{}, err
					}
				}
				return item, nil
			}
		}
	}
	return ContainerListItem{}, errors.New("wslc container list item has no ID")
}

func decodeContainerState(raw json.RawMessage) string {
	var state string
	if err := json.Unmarshal(raw, &state); err == nil {
		return state
	}
	var stateValue int
	if err := json.Unmarshal(raw, &stateValue); err == nil {
		switch stateValue {
		case 1:
			return "created"
		case 2:
			return "running"
		case 3:
			return "exited"
		case 4:
			return "deleted"
		}
	}
	return ""
}

func decodeContainerPorts(raw json.RawMessage, item *ContainerListItem) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == `""` {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var ports []struct {
			HostPort       uint16 `json:"HostPort"`
			ContainerPort  uint16 `json:"ContainerPort"`
			Protocol       int    `json:"Protocol"`
			BindingAddress string `json:"BindingAddress"`
		}
		if err := json.Unmarshal(raw, &ports); err != nil {
			return fmt.Errorf("decode structured wslc container ports: %w", err)
		}
		for _, port := range ports {
			protocol, ok := dockerProtocol(port.Protocol)
			if !ok {
				return fmt.Errorf("unsupported WSLC port protocol %d", port.Protocol)
			}
			if port.HostPort == 0 || port.ContainerPort == 0 {
				continue
			}
			key := fmt.Sprintf("%d/%s", port.ContainerPort, protocol)
			item.Ports[key] = append(item.Ports[key], PortBinding{
				HostIP: port.BindingAddress, HostPort: strconv.Itoa(int(port.HostPort)),
			})
		}
		return nil
	}

	var formatted string
	if err := json.Unmarshal(raw, &formatted); err != nil {
		return fmt.Errorf("decode formatted wslc container ports: %w", err)
	}
	for _, entry := range strings.Split(formatted, ", ") {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		binding, portKey, err := parseFormattedPort(entry)
		if err != nil {
			return err
		}
		item.Ports[portKey] = append(item.Ports[portKey], binding)
	}
	return nil
}

func parseFormattedPort(value string) (PortBinding, string, error) {
	arrow := strings.Index(value, "->")
	if arrow < 0 {
		return PortBinding{}, "", fmt.Errorf("invalid formatted WSLC port mapping %q", value)
	}
	endpoint := value[:arrow]
	container := value[arrow+2:]
	slash := strings.LastIndex(container, "/")
	if slash < 0 {
		return PortBinding{}, "", fmt.Errorf("invalid formatted WSLC container port %q", value)
	}
	containerPort, err := strconv.Atoi(container[:slash])
	if err != nil || containerPort < 1 || containerPort > 65535 {
		return PortBinding{}, "", fmt.Errorf("invalid formatted WSLC container port %q", value)
	}
	protocol := container[slash+1:]
	if protocol != "tcp" && protocol != "udp" {
		return PortBinding{}, "", fmt.Errorf("unsupported formatted WSLC port protocol %q", protocol)
	}
	var hostIP, hostPortText string
	if strings.HasPrefix(endpoint, "[") {
		closingBracket := strings.Index(endpoint, "]:")
		if closingBracket < 0 {
			return PortBinding{}, "", fmt.Errorf("invalid formatted WSLC host address %q", endpoint)
		}
		hostIP = endpoint[1:closingBracket]
		hostPortText = endpoint[closingBracket+2:]
	} else {
		separator := strings.LastIndex(endpoint, ":")
		if separator < 0 {
			return PortBinding{}, "", fmt.Errorf("invalid formatted WSLC host address %q", endpoint)
		}
		hostIP = endpoint[:separator]
		hostPortText = endpoint[separator+1:]
	}
	hostPort, err := strconv.Atoi(hostPortText)
	if err != nil || hostPort < 1 || hostPort > 65535 {
		return PortBinding{}, "", fmt.Errorf("invalid formatted WSLC host port %q", hostPortText)
	}
	return PortBinding{HostIP: hostIP, HostPort: strconv.Itoa(hostPort)}, fmt.Sprintf("%d/%s", containerPort, protocol), nil
}

func dockerProtocol(protocol int) (string, bool) {
	switch protocol {
	case 6:
		return "tcp", true
	case 17:
		return "udp", true
	default:
		return "", false
	}
}
