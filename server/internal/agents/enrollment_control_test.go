package agents

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestTypedEnrollmentExactInstallationReplayKeepsOwnership(t *testing.T) {
	e, id := setupRuntime(t)
	if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, id)
		return model.SetStoredMember(&a, "gateway_installation_id", []byte(`"installation-proof"`))
	}); err != nil {
		t.Fatal(err)
	}
	a, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"name": a.Name, "host": a.Host, "port": a.Port, "service_type": a.ServiceType, "deployment_method": a.DeploymentMethod, "profile_id": a.ProfileID})
	if err != nil {
		t.Fatal(err)
	}
	input, err := DecodeRegistrationInput(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.m.RegisterIdentity(e.ctx, input, RegistrationOptions{InstallationID: "installation-proof", ReplayInstallation: true})
	if err != nil || result.ID != id || result.IsNew || result.Token == nil || *result.Token != a.Token {
		t.Fatal(result, err)
	}
	after, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil || after.Token != a.Token || after.GatewayInstallationID == nil || *after.GatewayInstallationID != "installation-proof" {
		t.Fatal(after, err)
	}
}
