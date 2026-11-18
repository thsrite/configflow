package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistrationBootstrapProofIsSentOnceThenRemoved(t *testing.T) {
	for _, field := range []string{"registration_token", "registration_key"} {
		t.Run(field, func(t *testing.T) {
			cfg := gatewayTestConfig(t, false)
			if field == "registration_token" {
				cfg.RegistrationToken = "synthetic-once-proof"
			} else {
				cfg.RegistrationKey = "synthetic-once-proof"
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/agents/register" || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("incorrect raw request metadata")
				}
				if calls == 1 {
					if r.Header.Get("Authorization") != "" || !strings.Contains(string(body), `"`+field+`":"synthetic-once-proof"`) {
						t.Errorf("initial proof missing: %s", body)
					}
				} else if r.Header.Get("Authorization") != "Bearer durable-agent-token" || strings.Contains(string(body), "registration_") {
					t.Errorf("bootstrap proof retained: %s", body)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"success":true,"id":"new-device","token":"durable-agent-token"}`)
			}))
			defer server.Close()
			cfg.ServerURL = server.URL
			if err := cfg.RegisterAgent(); err != nil {
				t.Fatal(err)
			}
			saved := gatewaySavedConfig(t, cfg.filePath)
			if _, exists := saved[field]; exists {
				t.Fatal("proof persisted after successful registration")
			}
			if err := cfg.RegisterAgent(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestFailedRegistrationSaveRetainsBootstrapProof(t *testing.T) {
	cfg := gatewayTestConfig(t, false)
	cfg.RegistrationToken = "synthetic-once-proof"
	cfg.filePath = "/nonexistent/config.json"
	if err := cfg.handleRegisterResponse(&RegisterResponse{Success: true, ID: "new-device", Token: "durable-agent-token"}); err == nil {
		t.Fatal("missing save error")
	}
	if cfg.RegistrationToken != "synthetic-once-proof" || cfg.Token != "" {
		t.Fatal("in-memory registration proof lost on save failure")
	}
}
