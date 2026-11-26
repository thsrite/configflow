package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type deploymentReloader interface {
	reload(*Config) error
}

func prepareDeploymentReload(cfg *Config, s *DeploymentState, running bool) deploymentReloader {
	// Avoid putting a typed nil pointer into the interface.
	if cfg.ServiceType == "oxidns" {
		if r := prepareOxiDNSReload(cfg, s, running); r != nil {
			return r
		}
	} else if cfg.ServiceType == "mihomo" {
		if r := prepareMihomoReload(cfg, s, running); r != nil {
			return r
		}
	}
	return nil
}

// A reload must keep the API identity stable so that an acknowledgement and
// subsequent health probes still refer to the same running controller.
var mihomoControllerSections = []string{
	"external-controller", "external-controller-tls", "external-controller-unix",
	"external-controller-pipe", "external-controller-cors", "secret", "tls",
	"external-ui", "external-ui-name", "external-ui-url",
}

var mihomoReloadTimeout = 60 * time.Second

type mihomoReloader struct {
	endpoint string
	secret   string
	client   *http.Client
}

// Only contact this machine. Wildcard listeners are reached through loopback;
// configured hostnames and remote addresses are never resolved or contacted.
// A dedicated transport ignores proxy environment variables and redirects.
func newMihomoReloader(doc map[string]any) (*mihomoReloader, error) {
	secret := ""
	if raw, ok := doc["secret"]; ok && raw != nil {
		var valid bool
		secret, valid = raw.(string)
		if !valid || strings.ContainsAny(secret, "\r\n") {
			return nil, fmt.Errorf("invalid controller secret")
		}
	}
	transport := &http.Transport{Proxy: nil}
	endpoint := ""
	address, _ := doc["external-controller"].(string)
	if address != "" {
		host, port, err := net.SplitHostPort(strings.TrimSpace(address))
		if err != nil {
			return nil, fmt.Errorf("invalid controller address")
		}
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, fmt.Errorf("invalid controller port")
		}
		switch host {
		case "", "0.0.0.0", "localhost":
			host = "127.0.0.1"
		case "::":
			host = "::1"
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("controller is not a loopback listener")
		}
		endpoint = "http://" + net.JoinHostPort(host, strconv.Itoa(number))
	} else if socket, ok := doc["external-controller-unix"].(string); ok && socket != "" {
		if !filepath.IsAbs(socket) || strings.ContainsRune(socket, '\x00') {
			return nil, fmt.Errorf("controller Unix socket must be an absolute path")
		}
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}
		endpoint = "http://localhost"
	} else {
		return nil, fmt.Errorf("no usable local HTTP or Unix controller")
	}
	return &mihomoReloader{
		endpoint: endpoint,
		secret:   secret,
		client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}, nil
}

func prepareMihomoReload(cfg *Config, s *DeploymentState, running bool) *mihomoReloader {
	if cfg.ServiceType != "mihomo" || !running {
		return nil
	}
	skip := func(reason string) *mihomoReloader {
		logger.Infof("Mihomo release %s: native reload unavailable (%s)", s.DeploymentID, reason)
		return nil
	}
	live, _, err := readYAMLDocument(cfg.ConfigPath)
	if err != nil {
		return skip("cannot read the running configuration")
	}
	candidate, _, err := readYAMLDocument(filepath.Join(deploymentPath(cfg, s.DeploymentID), "stage", s.Manifest.ConfigPath))
	if err != nil {
		return skip("cannot read the candidate configuration")
	}
	for _, key := range mihomoControllerSections {
		if !reflect.DeepEqual(live[key], candidate[key]) {
			return skip(key + " changed")
		}
	}
	r, err := newMihomoReloader(live)
	if err != nil {
		return skip(err.Error())
	}
	// Authenticated, bounded preflight happens before stop or live-file writes.
	// Mihomo does not expose a running-config digest; the later synchronous 204
	// acknowledges the explicit Agent-managed path, followed by health checks.
	code, data, err := r.request(http.MethodGet, "/version", nil, 5*time.Second)
	var version struct {
		Version string `json:"version"`
	}
	if err != nil || code != http.StatusOK || json.Unmarshal(data, &version) != nil || version.Version == "" {
		return skip("controller preflight failed")
	}
	code, data, err = r.request(http.MethodGet, "/configs", nil, 5*time.Second)
	var current map[string]any
	if err != nil || code != http.StatusOK || json.Unmarshal(data, &current) != nil || current == nil {
		return skip("configuration API preflight failed")
	}
	return r
}

func (r *mihomoReloader) request(method, path string, body []byte, timeout time.Duration) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, r.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("invalid Mihomo API request")
	}
	if r.secret != "" {
		req.Header.Set("Authorization", "Bearer "+r.secret)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := r.client.Do(req)
	if err != nil {
		r.client.CloseIdleConnections()
		return 0, nil, fmt.Errorf("Mihomo controller unreachable or timed out")
	}
	defer response.Body.Close()
	defer r.client.CloseIdleConnections()
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return 0, nil, fmt.Errorf("Mihomo controller response unreadable or too large")
	}
	return response.StatusCode, data, nil
}

func (r *mihomoReloader) reload(cfg *Config) error {
	configPath, err := filepath.Abs(cfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("invalid managed configuration path")
	}
	// No caller-supplied path/payload is accepted. Explicitly load the deployed
	// file even if the core was originally started with another config path.
	body, err := json.Marshal(map[string]string{"path": configPath, "payload": ""})
	if err != nil {
		return err
	}
	code, _, err := r.request(http.MethodPut, "/configs?force=true", body, mihomoReloadTimeout)
	if err != nil {
		return err
	}
	if code != http.StatusNoContent {
		return fmt.Errorf("Mihomo reload returned HTTP %d", code)
	}
	return nil
}

func mihomoUnixControllerProbe(doc map[string]any) error {
	r, err := newMihomoReloader(doc)
	if err != nil {
		return err
	}
	code, data, err := r.request(http.MethodGet, "/version", nil, 2*time.Second)
	var version struct {
		Version string `json:"version"`
	}
	if err != nil || code != http.StatusOK || json.Unmarshal(data, &version) != nil || version.Version == "" {
		return fmt.Errorf("Mihomo Unix controller health probe failed")
	}
	for kind, endpoint := range map[string]string{"proxy-providers": "/providers/proxies", "rule-providers": "/providers/rules"} {
		expected, _ := doc[kind].(map[string]any)
		if len(expected) == 0 {
			continue
		}
		code, data, err := r.request(http.MethodGet, endpoint, nil, 2*time.Second)
		var response struct {
			Providers map[string]json.RawMessage `json:"providers"`
		}
		if err != nil || code != http.StatusOK || json.Unmarshal(data, &response) != nil {
			return fmt.Errorf("Mihomo Unix controller provider status unavailable")
		}
		for name := range expected {
			if _, ok := response.Providers[name]; !ok {
				return fmt.Errorf("Mihomo provider is not initialized: %s", name)
			}
		}
	}
	return nil
}
