package dockerapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"wslc-bridge/internal/native"
	"wslc-bridge/internal/process"
)

type fakeNativeRuntime struct {
	containers []native.ContainerListItem
	listError  error
	runArgs    []string
}

func (f *fakeNativeRuntime) ListContainers(context.Context) ([]native.ContainerListItem, error) {
	return f.containers, f.listError
}

func (*fakeNativeRuntime) CreateContainer(context.Context, []string) (string, error) {
	return "container-id", nil
}

func (*fakeNativeRuntime) PullImage(context.Context, string) error { return nil }

func (*fakeNativeRuntime) ExecContainer(context.Context, string, []string, io.Writer, io.Writer) (int, error) {
	return 0, nil
}

func (*fakeNativeRuntime) CreateNetwork(context.Context, []string, string) (string, error) {
	return "network-id", nil
}

func (f *fakeNativeRuntime) RunWithTimeout(_ context.Context, _ time.Duration, args ...string) (process.Result, error) {
	f.runArgs = append([]string(nil), args...)
	return process.Result{}, nil
}

func TestInspectContainerUsesGuestBodyAndWSLCListPorts(t *testing.T) {
	guestBody := `{"Id":"container-id","Config":{"Image":"nginx:alpine"},"State":{"Running":true},"NetworkSettings":{"Ports":{"80/tcp":[{"HostIp":"172.18.0.1","HostPort":"8080"}]}}}`
	guest := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/containers/container-id/json" {
			return nil, errors.New("unexpected guest request path: " + request.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(guestBody))}, nil
	})}
	nativeRuntime := &fakeNativeRuntime{containers: []native.ContainerListItem{{
		ID: "container-id", State: "running",
		Ports: map[string][]native.PortBinding{"80/tcp": {{HostIP: "127.0.0.1", HostPort: "49152"}}},
	}}}
	handler := NewHandler(nativeRuntime, guest)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/containers/container-id/json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("inspect returned status %d: %s", response.Code, response.Body.String())
	}
	var inspect struct {
		Config struct {
			Image string `json:"Image"`
		} `json:"Config"`
		NetworkSettings struct {
			Ports map[string][]native.PortBinding `json:"Ports"`
		} `json:"NetworkSettings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &inspect); err != nil {
		t.Fatal(err)
	}
	if inspect.Config.Image != "nginx:alpine" {
		t.Fatalf("guest inspect fields were not preserved: %#v", inspect.Config)
	}
	if binding := inspect.NetworkSettings.Ports["80/tcp"][0]; binding.HostIP != "127.0.0.1" || binding.HostPort != "49152" {
		t.Fatalf("native Windows port was not overlaid: %#v", binding)
	}
}

func TestInspectContainerFallsBackToGuestWhenWSLCListFails(t *testing.T) {
	guestBody := `{"Id":"container-id","NetworkSettings":{"Ports":{"80/tcp":[{"HostIp":"172.18.0.1","HostPort":"8080"}]}}}`
	guest := &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(guestBody))}, nil
	})}
	handler := NewHandler(&fakeNativeRuntime{listError: errors.New("native list unavailable")}, guest)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/containers/container-id/json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("guest inspect should still be served, got status %d: %s", response.Code, response.Body.String())
	}
	var inspect struct {
		NetworkSettings struct {
			Ports map[string][]native.PortBinding `json:"Ports"`
		} `json:"NetworkSettings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &inspect); err != nil {
		t.Fatal(err)
	}
	if binding := inspect.NetworkSettings.Ports["80/tcp"][0]; binding.HostIP != "" || binding.HostPort != "" {
		t.Fatalf("guest VM port was incorrectly exposed as a Windows host port: %#v", binding)
	}
	if warning := response.Header().Get(warningHeader); warning == "" {
		t.Fatal("a failed native port lookup must be reported to the client")
	}
}

func TestListContainersWarnsWhenTheNativePortLookupFails(t *testing.T) {
	guestBody := `[{"Id":"container-id","State":"running","Ports":[]}]`
	guest := &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(guestBody))}, nil
	})}
	handler := NewHandler(&fakeNativeRuntime{listError: errors.New("native list unavailable")}, guest)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/containers/json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("container list returned %d: %s", response.Code, response.Body.String())
	}
	if warning := response.Header().Get(warningHeader); !strings.Contains(warning, "ports may be missing") {
		t.Fatalf("warning header = %q, want the missing-ports warning", warning)
	}
}

func TestContainerStartUsesNativeStartDirectlyWithoutInspect(t *testing.T) {
	nativeRuntime := &fakeNativeRuntime{}
	guest := &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("start must not make a guest inspect request")
	})}
	handler := NewHandler(nativeRuntime, guest)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/containers/container-id/start", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("container start returned %d: %s", response.Code, response.Body.String())
	}
	if !reflect.DeepEqual(nativeRuntime.runArgs, []string{"container", "start", "container-id"}) {
		t.Fatalf("native start arguments = %#v", nativeRuntime.runArgs)
	}
}
