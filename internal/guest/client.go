package guest

import (
	"net/http"
	"net/url"
	"time"
)

// Transport is the guest Docker HTTP transport. It keeps the dialer reachable so
// a pipe EOF can be explained with the guest exit status and stderr tail.
type Transport struct {
	*http.Transport
	dialer *StdioDialer
}

// Diagnostic reports why the guest transport last ended abnormally, if it did.
func (t *Transport) Diagnostic() string {
	if t == nil || t.dialer == nil {
		return ""
	}
	return t.dialer.Diagnostic()
}

func NewClient(wslcPath string) *http.Client {
	dialer := &StdioDialer{WSLCPath: wslcPath}
	transport := &Transport{
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			DisableCompression:    true,
			MaxIdleConns:          16,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 0,
			ForceAttemptHTTP2:     false,
			Proxy:                 func(*http.Request) (*url.URL, error) { return nil, nil },
		},
		dialer: dialer,
	}
	return &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

// Diagnostic reports the guest transport failure behind client, if the clients
// were created by NewClient and the transport ended abnormally.
func Diagnostic(client *http.Client) string {
	if client == nil {
		return ""
	}
	reporter, ok := client.Transport.(interface{ Diagnostic() string })
	if !ok {
		return ""
	}
	return reporter.Diagnostic()
}

// Failure describes err and appends the guest transport diagnostic when one is
// available, so a bare EOF becomes an actionable message.
func Failure(client *http.Client, err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if diagnostic := Diagnostic(client); diagnostic != "" {
		message += " (" + diagnostic + ")"
	}
	return message
}
