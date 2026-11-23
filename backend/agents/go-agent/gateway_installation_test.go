package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const gatewayInstallationID = "install-session-0123456789"
const gatewayInstallationToken = "one-time-installation-secret"

func gatewayTestConfig(t *testing.T, session bool) *Config {
	t.Helper()
	values := map[string]any{
		"server_url": "http://unused.test", "agent_name": "gateway-mihomo", "agent_ip": "192.168.8.2",
		"agent_host": "0.0.0.0", "agent_port": 8080, "service_type": "mihomo", "deployment_method": "shell",
		"native_aio": true, "agent_service_unit": "configflow-agent-mihomo", "config_path": "/etc/mihomo/config.yaml",
		"future_extension": map[string]any{"enabled": true, "policy": "keep me"},
	}
	if session {
		values["installation_id"] = gatewayInstallationID
		values["installation_token"] = gatewayInstallationToken
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func gatewaySavedConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestGatewayInstallationRegistrationScopeAndDurableEnrollment(t *testing.T) {
	for _, session := range []bool{false, true} {
		name := "ordinary"
		if session {
			name = "gateway"
		}
		t.Run(name, func(t *testing.T) {
			cfg := gatewayTestConfig(t, session)
			var request map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/agents/register" {
					t.Errorf("unexpected registration request: %s %s", r.Method, r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":true,"id":"registered-mihomo","token":"durable-agent-token"}`))
			}))
			defer server.Close()
			cfg.ServerURL = server.URL
			if err := cfg.RegisterAgent(); err != nil {
				t.Fatal(err)
			}
			if session {
				if request["installation_id"] != gatewayInstallationID || request["installation_token"] != gatewayInstallationToken {
					t.Fatalf("gateway registration lost its scoped installation proof")
				}
			} else {
				if _, present := request["installation_id"]; present {
					t.Fatal("ordinary registration unexpectedly sends installation_id")
				}
				if _, present := request["installation_token"]; present {
					t.Fatal("ordinary registration unexpectedly sends installation_token")
				}
			}
			if request["host"] != "192.168.8.2" || request["service_type"] != "mihomo" || request["port"] != float64(8080) {
				t.Fatal("registration no longer carries the explicit host/service binding")
			}
			if cfg.InstallationID != "" || cfg.InstallationToken != "" {
				t.Fatal("successful registration retained the one-time installation capability")
			}
			saved := gatewaySavedConfig(t, cfg.filePath)
			for _, key := range []string{"installation_id", "installation_token"} {
				if _, present := saved[key]; present {
					t.Errorf("omitempty left a stale %s in the existing JSON file", key)
				}
			}
			// The shell installer enrolls with these durable fields only. Removing
			// installation fields must not break its authenticated pair enrollment.
			if saved["agent_id"] != "registered-mihomo" || saved["token"] != "durable-agent-token" {
				t.Fatal("enrollment credentials were not persisted")
			}
			wantExtension := map[string]any{"enabled": true, "policy": "keep me"}
			if !reflect.DeepEqual(saved["future_extension"], wantExtension) {
				t.Fatalf("unrelated extension changed: %#v", saved["future_extension"])
			}
		})
	}
}

func TestGatewayInstallationReregistrationUsesOnlyDurableCredential(t *testing.T) {
	cfg := gatewayTestConfig(t, true)
	var requests []map[string]any
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests = append(requests, request)
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		if len(requests) == 1 {
			_, _ = w.Write([]byte(`{"success":true,"id":"registered-mihomo","token":"durable-agent-token"}`))
		} else {
			_, _ = w.Write([]byte(`{"success":true,"id":"registered-mihomo"}`))
		}
	}))
	defer server.Close()
	cfg.ServerURL = server.URL
	if err := cfg.RegisterAgent(); err != nil {
		t.Fatal(err)
	}
	// Exercise a process restart, not merely the in-memory zero values.
	reloaded, err := LoadConfig(cfg.filePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.RegisterAgent(); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || authorizations[0] != "" || authorizations[1] != "Bearer durable-agent-token" {
		t.Fatalf("unexpected authentication transition: %#v", authorizations)
	}
	if _, present := requests[1]["installation_id"]; present {
		t.Fatal("re-registration carried an expired installation_id")
	}
	if _, present := requests[1]["installation_token"]; present {
		t.Fatal("re-registration carried an expired installation_token")
	}
	if reloaded.Token != "durable-agent-token" {
		t.Fatal("re-registration without a rotated token lost the durable token")
	}
}

func TestGatewayInstallationFailedRegistrationRetainsRetryCapability(t *testing.T) {
	for _, response := range []struct {
		name   string
		status int
		body   string
	}{
		{"rejected", http.StatusForbidden, `{"success":false}`},
		{"unsuccessful", http.StatusOK, `{"success":false}`},
		{"malformed", http.StatusOK, `not-json`},
		{"missing-id", http.StatusOK, `{"success":true,"token":"new-token"}`},
		{"missing-token", http.StatusOK, `{"success":true,"id":"new-id"}`},
	} {
		t.Run(response.name, func(t *testing.T) {
			cfg := gatewayTestConfig(t, true)
			before, err := os.ReadFile(cfg.filePath)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(response.status)
				_, _ = w.Write([]byte(response.body))
			}))
			defer server.Close()
			cfg.ServerURL = server.URL
			if err := cfg.RegisterAgent(); err == nil {
				t.Fatal("unsuccessful registration was accepted")
			}
			if cfg.InstallationID != gatewayInstallationID || cfg.InstallationToken != gatewayInstallationToken {
				t.Fatal("failed registration discarded the retry capability")
			}
			after, err := os.ReadFile(cfg.filePath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("failed registration changed the local installation config")
			}
		})
	}
}

func TestGatewayInstallationSaveFailureRestoresRetryState(t *testing.T) {
	cfg := gatewayTestConfig(t, true)
	// A missing parent consistently forces persistence failure without relying
	// on Unix permission behavior (which differs for root CI workers).
	cfg.filePath = filepath.Join(t.TempDir(), "missing-directory", "config.json")
	if err := cfg.handleRegisterResponse(&RegisterResponse{Success: true, ID: "new-id", Token: "new-token"}); err == nil {
		t.Fatal("registration unexpectedly persisted into a missing parent")
	}
	if cfg.InstallationID != gatewayInstallationID || cfg.InstallationToken != gatewayInstallationToken {
		t.Fatal("save failure discarded the one-time retry capability")
	}
	if cfg.AgentID != "" || cfg.Token != "" {
		t.Fatal("save failure left an unpersisted registered identity in memory")
	}
}
