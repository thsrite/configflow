package routes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"gopkg.in/yaml.v3"
)

// OxiDNS can re-assemble its plugins in-process (POST /api/reload). A restart
// drops DNS for several seconds; a reload only re-binds listeners. Releases
// prefer a reload whenever the running core can apply the candidate that way
// and fall back to the usual stop/start otherwise.

// Sections the core only reads at process start; a change needs a restart.
var oxidnsRestartOnlySections = []string{"runtime", "api", "log", "include"}

// oxidnsReloadTimeout bounds waiting for an accepted reload; replaceable in tests.
var oxidnsReloadTimeout = 30 * time.Second

type oxidnsReloadStatus struct {
	Status         string  `json:"status"`
	Pending        bool    `json:"pending"`
	InProgress     bool    `json:"in_progress"`
	LastError      *string `json:"last_error"`
	RunningVersion *string `json:"running_version"`
}

type oxidnsReloader struct {
	endpoint string
	username string
	password string
	client   *http.Client
}

func readYAMLDocument(name string) (map[string]interface{}, []byte, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, nil, err
	}
	var doc map[string]interface{}
	if err = yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	return doc, data, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// prepareOxiDNSReload decides, before any mutation, whether this release can be
// applied by reloading the running core. It returns nil (with a logged reason)
// when a restart is required.
func prepareOxiDNSReload(cfg *Config, s *DeploymentState, running bool) *oxidnsReloader {
	if cfg.ServiceType != "oxidns" || !running {
		return nil
	}
	skip := func(reason string) *oxidnsReloader {
		log.Printf("OxiDNS release %s: native reload unavailable (%s)", s.DeploymentID, reason)
		return nil
	}
	api, apiErr := loadOxiDNSMonitorConfig(cfg)
	if apiErr != nil {
		return skip("running core has no usable local HTTP API")
	}
	live, liveData, err := readYAMLDocument(cfg.ConfigPath)
	if err != nil {
		return skip("cannot read the running configuration")
	}
	candidate, _, err := readYAMLDocument(filepath.Join(deploymentPath(cfg, s.DeploymentID), "stage", s.Manifest.ConfigPath))
	if err != nil {
		return skip("cannot read the candidate configuration")
	}
	for _, section := range oxidnsRestartOnlySections {
		if !reflect.DeepEqual(live[section], candidate[section]) {
			return skip(section + " changed")
		}
	}
	reloader := &oxidnsReloader{endpoint: api.endpoint, client: oxMonitorClient()}
	if api.basicAuth {
		reloader.username, reloader.password = api.username, api.password
	}
	status, err := reloader.status()
	if err != nil {
		return skip(err.Error())
	}
	if status.Pending || status.InProgress {
		return skip("another reload is in progress")
	}
	// The core must be running exactly the on-disk file, otherwise the result of
	// a reload cannot be told apart from an older, unapplied state.
	if status.RunningVersion == nil || *status.RunningVersion != sha256Hex(liveData) {
		return skip("running version differs from the on-disk configuration")
	}
	return reloader
}

func (r *oxidnsReloader) request(method, path string) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), oxMonitorTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, r.endpoint+path, nil)
	if err != nil {
		return 0, nil, err
	}
	if r.username != "" {
		req.SetBasicAuth(r.username, r.password)
	}
	req.Header.Set("Accept", "application/json")
	response, err := r.client.Do(req)
	if err != nil {
		return 0, nil, diagnosticFailure("OxiDNS API unreachable", err, nil)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return 0, nil, diagnosticFailure("OxiDNS API response unreadable", err, nil)
	}
	return response.StatusCode, data, nil
}

func (r *oxidnsReloader) status() (*oxidnsReloadStatus, error) {
	code, data, err := r.request(http.MethodGet, "/api/reload/status")
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, diagnosticFailure(fmt.Sprintf("reload status returned HTTP %d", code), nil, data, r.password)
	}
	var status oxidnsReloadStatus
	if err = json.Unmarshal(data, &status); err != nil || status.Status == "" {
		return nil, diagnosticFailure("reload status is not recognized", err, nil)
	}
	return &status, nil
}

// oxidnsReloadSettleTimeout bounds waiting, before a fallback stop, for a reload
// that outlived oxidnsReloadTimeout; replaceable in tests.
var oxidnsReloadSettleTimeout = 90 * time.Second

// settle waits until the core is reachable again and no reload is pending or in
// progress. OxiDNS closes its API while reloading and handles SIGTERM only after
// the reload, so stopping it mid-reload outlasts OpenRC's stop timeout. It
// reports whether the core settled within the timeout.
func (r *oxidnsReloader) settle(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if status, err := r.status(); err == nil && !status.Pending && !status.InProgress {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// reload asks the core to apply the on-disk configuration and waits until it
// reports running exactly that file.
func (r *oxidnsReloader) reload(cfg *Config) error {
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		return err
	}
	want := sha256Hex(data)
	if err = prepareOxiDNSRuntimeDirectories(cfg); err != nil {
		return err
	}
	code, response, err := r.request(http.MethodPost, "/api/reload")
	if err != nil {
		return err
	}
	if code != http.StatusAccepted {
		return diagnosticFailure(fmt.Sprintf("reload request returned HTTP %d", code), nil, response, r.password)
	}
	deadline := time.Now().Add(oxidnsReloadTimeout)
	for {
		time.Sleep(200 * time.Millisecond)
		status, err := r.status()
		if err == nil && !status.Pending && !status.InProgress {
			if status.LastError != nil && *status.LastError != "" {
				return diagnosticFailure("OxiDNS rejected the reload", nil, []byte(*status.LastError), r.password)
			}
			if status.RunningVersion != nil && *status.RunningVersion == want {
				return nil
			}
			return fmt.Errorf("OxiDNS did not report the new configuration as running")
		}
		if time.Now().After(deadline) {
			return diagnosticFailure("OxiDNS reload did not finish in time", err, nil)
		}
	}
}
