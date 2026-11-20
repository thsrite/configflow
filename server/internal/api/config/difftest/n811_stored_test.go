package difftest

import (
	"encoding/json"
	"slices"
	"testing"

	apiclients "configflow/internal/api/clientdevices"
	apirouters "configflow/internal/api/routers"
	"configflow/internal/httpx/httpxtest"
)

func TestN811W5StoredTextDoesNotSelectNumericIdentity(t *testing.T) {
	w := newWorld(t, func(s *httpxtest.Server) {
		// R5 fixtures represent preexisting stored rows, bypassing current write validation.
		entry, err := s.Repo.Store().Load()
		if err != nil {
			t.Fatal(err)
		}
		document := *entry.Document
		section := *document.System
		section.Fields = slices.Clone(section.Fields)
		document.System = &section
		document.Profiles = slices.Clone(document.Profiles)
		found := map[string]bool{}
		for i := range section.Fields {
			if section.Fields[i].Name == "agents" {
				section.Fields[i].Value = json.RawMessage(`[{"id":"raw-agent","name":0,"host":0,"status":0,"profile_id":"default","service_type":"oxidns","port":8080,"token":"synthetic-agent-token"},{"id":"raw-gateway","name":0,"host":0,"status":0,"profile_id":"default","service_type":"mihomo","port":8081,"token":"synthetic-gateway-token"}]`)
				found["agents"] = true
			}
		}
		for i := range document.Profiles {
			if document.Profiles[i].ID == "default" {
				document.Profiles[i].Fields = slices.Clone(document.Profiles[i].Fields)
				for j := range document.Profiles[i].Fields {
					field := &document.Profiles[i].Fields[j]
					switch field.Name {
					case "routers":
						field.Value = json.RawMessage(`{"items":[{"id":"raw-router","name":"Synthetic","type":"routeros","endpoint":"http://192.0.2.1","username":"operator","password":"synthetic-password","verify_tls":false,"proxy_gateway_agent_id":"","dns_agent_id":""}]}`)
						found["routers"] = true
					}
				}
			}
		}
		if len(found) != 2 {
			t.Fatalf("missing R5 seed fields: %v", found)
		}
		if _, written, err := s.Repo.Store().CommitContext(t.Context(), entry, &document, nil); err != nil || !written {
			t.Fatalf("seed R5 rows: written=%v err=%v", written, err)
		}
	})
	apiclients.Register(w.srv.App.Router)
	apirouters.Register(w.srv.App.Router)
	for _, path := range []string{"/api/client-devices", "/api/client-devices?agent=raw-agent", "/api/routers/raw-router/policy-settings"} {
		answer := w.check(request{Method: "GET", Path: path})
		compare(t, "stored text GET status", answer.Status, 200)
		if path == "/api/routers/raw-router/policy-settings" {
			workspace := answer.Body.(map[string]any)["workspace"].(map[string]any)
			sources := workspace["source_agents"].([]any)
			gateways := workspace["gateways"].([]any)
			compare(t, "source agent name", sources[0].(map[string]any)["name"], "raw-agent")
			compare(t, "source agent host", sources[0].(map[string]any)["host"], "")
			compare(t, "source agent status", sources[0].(map[string]any)["status"], "offline")
			var found bool
			for _, row := range gateways {
				item := row.(map[string]any)
				if item["id"] == "raw-gateway" {
					found = true
					compare(t, "gateway name", item["name"], "raw-gateway")
					compare(t, "gateway host", item["host"], "")
				}
			}
			if !found {
				t.Fatalf("missing raw-gateway in %v", gateways)
			}
		}
	}
}
