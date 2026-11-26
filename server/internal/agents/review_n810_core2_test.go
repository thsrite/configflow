package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

// Python register_agent rebuilds an existing Agent from a fixed member list.
// Members outside it (the Docker HA enrollment marker, runtime metrics,
// unknown extensions, a falsy gateway marker) do not survive a re-registration.
// Keeping docker_ha_installation_id would let the enrollment credential keep
// replaying the Agent token after the device itself has registered.
func TestReviewN810ReregistrationKeepsOnlyPythonMembers(t *testing.T) {
	e, id := setupRuntime(t)
	if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, id)
		for _, tt := range []struct{ name, raw string }{
			{"docker_ha_installation_id", `"enrollment-session"`},
			{"node_test_dns_endpoints", `{"endpoint":{"address":"192.0.2.53"}}`},
			{"service_status", `"running"`},
			{"gateway_installation_id", `""`},
			{"config_version", `"7"`},
			{"rule_sync", `{"settings":{"library":true}}`},
		} {
			if err := model.SetStoredMember(&a, tt.name, []byte(tt.raw)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"name": a.Name, "host": a.Host, "port": a.Port, "service_type": a.ServiceType})
	if err != nil {
		t.Fatal(err)
	}
	input, err := DecodeRegistrationInput(data)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := e.m.RegisterIdentity(e.ctx, input, RegistrationOptions{ExistingToken: a.Token}); err != nil || result.IsNew {
		t.Fatal(result, err)
	}
	var stored *model.Agent
	if err := e.repo.ViewAgents(e.ctx, func(records []model.Agent) error {
		stored, err = copyAgent(registryAgent(records, id), true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docker_ha_installation_id", "node_test_dns_endpoints", "service_status", "gateway_installation_id", "future"} {
		raw, present, err := model.EncodeMember(&stored, name)
		if err != nil || present {
			t.Errorf("re-registration kept %s=%s err=%v; Python drops it", name, raw, err)
		}
	}
	for _, tt := range []struct{ name, raw string }{{"config_version", `"7"`}, {"rule_sync", `{"settings":{"library":true}}`}} {
		raw, present, err := model.EncodeMember(&stored, tt.name)
		if err != nil || !present || !bytes.Equal(raw, []byte(tt.raw)) {
			t.Errorf("re-registration lost %s: %s %v %v", tt.name, raw, present, err)
		}
	}
}
