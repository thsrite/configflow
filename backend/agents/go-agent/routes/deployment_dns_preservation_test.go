package routes

import (
	"agent/preflight"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func aioPublicationConfig(t *testing.T, mode string) *Config {
	t.Helper()
	t.Setenv("ENABLE_OXIDNS", "false")
	t.Setenv("ENABLE_MOSDNS", "false")
	cfg := testDeploymentConfig(t)
	cfg.NativeAIO = mode == "native"
	if cfg.NativeAIO {
		cfg.AgentIP = "192.168.0.33"
		stubAgentIPv4(t, func() (string, error) { return "192.168.0.33", nil })
	}
	if mode == "docker-oxidns" {
		t.Setenv("ENABLE_OXIDNS", "true")
	} else if mode == "docker-mosdns" {
		t.Setenv("ENABLE_MOSDNS", "true")
	}
	return cfg
}

func assertOriginalDNSDocument(t *testing.T, cfg *Config, got, original []byte) {
	t.Helper()
	var actual, expected map[string]any
	if err := yaml.Unmarshal(got, &actual); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(original, &expected); err != nil {
		t.Fatal(err)
	}
	// Native AIO adds host exclusions only when TUN auto-redirect is enabled.
	// Every original DNS value, omitted/null field and other TUN entry survives.
	if tun, ok := expected["tun"].(map[string]any); ok && cfg.NativeAIO && tun["enable"] == true && tun["auto-redirect"] == true {
		tun["route-exclude-address"] = []any{"127.0.0.0/8", cfg.AgentIP + "/32"}
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("DNS configuration changed:\n%s", got)
	}
}

func assertDeploymentOriginal(t *testing.T, cfg *Config, state *DeploymentState, original []byte) {
	t.Helper()
	name := cfg.ConfigPath
	manifestPath := filepath.Join(deploymentRoot(cfg), "active-manifest.json")
	if state.Status == "ready" {
		name = filepath.Join(deploymentPath(cfg, state.DeploymentID), "stage", "config.yaml")
		manifestPath = filepath.Join(deploymentPath(cfg, state.DeploymentID), "effective-manifest.json")
	}
	published, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	assertOriginalDNSDocument(t, cfg, published, original)
	digest, size, err := hashFile(name)
	if err != nil {
		t.Fatal(err)
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest DeploymentManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, file := range manifest.Files {
		if file.Path == "config.yaml" {
			if file.SHA256 != digest || file.Size != size {
				t.Fatal("deployment manifest does not describe final configuration bytes")
			}
			return
		}
	}
	t.Fatal("configuration missing from deployment manifest")
}

func TestAIOPublishesOriginalMihomoDNSConfiguration(t *testing.T) {
	fixtures := []struct{ name, content string }{
		{"53", "# Keep my listen address and port.\ndns: {enable: true, listen: '127.0.0.2:53'}\ntun: {enable: true, dns-hijack: ['any:53', 'tcp://any:53']}\n"},
		{"custom-ports", "dns: {enable: true, listen: '127.0.0.2:5353'}\ntun: {enable: true, auto-route: false, dns-hijack: ['any:5353', 'tcp://any:7874', '192.0.2.1:5533']}\n"},
		{"auto-redirect", "dns: {enable: true, listen: '127.0.0.2:5353'}\ntun: {enable: true, auto-route: true, auto-redirect: true, auto-detect-interface: false, dns-hijack: ['any:5353', 'tcp://any:7874']}\n"},
		{"ipv6-listen", "dns: {enable: true, listen: '[::1]:7874'}\ntun: {enable: true, dns-hijack: ['[2001:db8::1]:5533']}\n"},
		{"disabled", "dns: {enable: false, listen: '127.0.0.1:53'}\ntun: {enable: false, dns-hijack: ['any:53', 'tcp://any:53']}\n"},
		{"missing", "mode: rule\n"},
		{"null", "mode: rule\ndns: null\ntun: null\n"},
		{"empty", "mode: rule\ndns: {}\ntun: {}\n"},
		{"listeners", "dns: {enable: false}\nlisteners:\n  - {name: custom-tun, type: tun, dns-hijack: ['any:53', 'tcp://any:5353']}\n  - {name: disabled-tun, type: tun, disabled: true, dns-hijack: ['any:53']}\n"},
	}
	for _, mode := range []string{"native", "docker-oxidns", "docker-mosdns", "single"} {
		for _, fixture := range fixtures {
			t.Run(mode+"/"+fixture.name, func(t *testing.T) {
				cfg := aioPublicationConfig(t, mode)
				original := []byte(fixture.content)
				checked := false
				useDNSCheck(t, func(candidate preflight.DNSCandidate, _ preflight.DNSOptions) preflight.Report {
					checked = true
					assertOriginalDNSDocument(t, cfg, candidate.Content, original)
					return nil
				})
				activate := fixture.name == "custom-ports"
				s := submitBundle(t, cfg, "original-dns", map[string][]byte{"config.yaml": original}, activate)
				if s.Status != "ready" && s.Status != "succeeded" || !checked {
					t.Fatalf("publication did not finish DNS validation: %+v", s)
				}
				assertDeploymentOriginal(t, cfg, s, original)
			})
		}
	}
}

func TestAIODNSPreservesCandidateThroughReadyActivation(t *testing.T) {
	cfg := aioPublicationConfig(t, "native")
	content := []byte("dns: {enable: true, listen: '127.0.0.2:5353'}\ntun: {enable: true, dns-hijack: ['any:53', 'tcp://any:7874']}\n")
	calls := 0
	useDNSCheck(t, func(candidate preflight.DNSCandidate, _ preflight.DNSOptions) preflight.Report {
		calls++
		assertOriginalDNSDocument(t, cfg, candidate.Content, content)
		return nil
	})
	s := submitBundle(t, cfg, "preserved-ready", map[string][]byte{"config.yaml": content}, false)
	if s.Status != "ready" {
		t.Fatalf("prepare failed: %+v", s)
	}
	// Activation may repeat native validation when additional Geo files arrive.
	if err := loadEffectiveManifest(cfg, s); err != nil {
		t.Fatal(err)
	}
	if err := validateStagedDeployment(cfg, s); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	DeploymentHandler(cfg)(w, httptest.NewRequest(http.MethodPost, "/api/deployments/preserved-ready/activate", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("activation: %d %s", w.Code, w.Body.String())
	}
	s = waitDeploymentState(t, cfg, "preserved-ready", true, func(st *DeploymentState) bool {
		return st.Status == "succeeded" || st.Status == "failed"
	}, "activation preserved-ready")
	if s == nil || s.Status != "succeeded" || calls != 2 {
		t.Fatalf("activation did not recheck DNS: %+v (%d calls)", s, calls)
	}
	assertDeploymentOriginal(t, cfg, s, content)
}

func TestAIOPublishesDNSHijackWarningsWithoutChangingSettings(t *testing.T) {
	for _, mode := range []string{"native", "docker-oxidns", "docker-mosdns", "single"} {
		for _, fixture := range []struct {
			name, tun string
			warn      bool
		}{
			{"top-level-53", "tun: {enable: true, dns-hijack: ['any:53', 'tcp://any:53']}\n", true},
			{"listener-53", "listeners: [{type: tun, name: custom-tun, dns-hijack: ['tcp://any:53']}]\n", true},
			{"custom", "tun: {enable: true, dns-hijack: ['any:5353', 'tcp://any:7874']}\n", false},
			{"disabled", "tun: {enable: false, dns-hijack: ['any:53']}\nlisteners: [{type: tun, disabled: true, dns-hijack: ['tcp://any:53']}]\n", false},
			{"missing", "", false},
		} {
			t.Run(mode+"/"+fixture.name, func(t *testing.T) {
				cfg := aioPublicationConfig(t, mode)
				// Disabled external DNS avoids any dependency on host listeners or probes.
				useDNSCheck(t, preflight.CheckDNS)
				content := []byte("dns: {enable: false, listen: '127.0.0.2:53'}\n" + fixture.tun)
				activate := fixture.name == "top-level-53"
				s := submitBundle(t, cfg, "hijack-risk", map[string][]byte{"config.yaml": content}, activate)
				if s.Status != "ready" && s.Status != "succeeded" {
					t.Fatalf("DNS hijack warning blocked publication: %+v", s)
				}
				assertDeploymentOriginal(t, cfg, s, content)
				found := false
				for _, check := range s.DNSChecks {
					if strings.Contains(check.Name, "自动调整") {
						t.Fatalf("reported a configuration change that was not made: %+v", check)
					}
					if check.Name == "DNS 劫持范围" && check.Level == "warn" {
						found = true
					}
				}
				if expected := fixture.warn && mode != "single"; found != expected {
					t.Fatalf("DNS hijack warning = %v, want %v: %+v", found, expected, s.DNSChecks)
				}
				w := httptest.NewRecorder()
				DeploymentHandler(cfg)(w, httptest.NewRequest(http.MethodGet, "/api/deployments/hijack-risk", nil))
				var response DeploymentState
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || len(response.DNSChecks) != len(s.DNSChecks) {
					t.Fatalf("DNS report not retained in publication history: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestAIODNSPeerConfigurationPreservesCustomListenersAndUpstreams(t *testing.T) {
	content := []byte("plugins:\n  - {tag: dns, type: udp_server, args: {listen: '127.0.0.2:5353', entry: main}}\n  - {tag: main, type: sequence, args: []}\n  - {tag: remote, type: forward, args: {upstreams: [{addr: '127.0.0.1:7874'}]}}\n")
	for _, service := range []string{"oxidns", "mosdns"} {
		t.Run(service, func(t *testing.T) {
			cfg := aioPublicationConfig(t, "native")
			cfg.ServiceType = service
			s := submitBundle(t, cfg, "peer-original", map[string][]byte{"config.yaml": content}, false)
			if s.Status != "ready" {
				t.Fatalf("prepare failed: %+v", s)
			}
			staged, err := os.ReadFile(filepath.Join(deploymentPath(cfg, s.DeploymentID), "stage", "config.yaml"))
			if err != nil || !bytes.Equal(staged, content) {
				t.Fatalf("DNS peer configuration changed: %v\n%s", err, staged)
			}
		})
	}
}
