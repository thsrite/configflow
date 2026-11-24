package agents

import (
	core "configflow/internal/agents"
	"configflow/internal/deploy"
	"context"
	"encoding/json"
	"testing"
)

func TestDeployOwnersFinishDisconnectedRequests(t *testing.T) {
	s, _, _ := newDeployHTTP(t)
	m, e := core.Get(s.Context())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(s.Context())
	cancel()
	dp := deploy.New(deploy.Options{})
	generated := dp.GenerateFinal(ctx, m, "publish-agent", deploy.GenerateRequest{Body: []byte(`{"expected_profile_id":"default","expected_service_type":"mihomo","base_url":"https://panel.example.com"}`), HostURL: "https://panel.example.com"})
	if generated.Err != nil || generated.Status != 200 {
		t.Fatal(generated)
	}
	body := generated.Body.(map[string]any)
	key := body["id"].(string)
	raw, _ := json.Marshal(map[string]any{"generation_id": key, "restart": false})
	pushed := dp.PushConfig(ctx, m, "publish-agent", deploy.PushRequest{Body: raw, Scheme: "https", Host: "panel.example.com"})
	if pushed.Err != nil || pushed.Status != 202 {
		t.Fatal(pushed)
	}
	key = body["deployment_id"].(string)
	state, e := m.GetDeploymentModel(ctx, "publish-agent", key)
	if e != nil || state.Status != "ready" {
		t.Fatal(state, e)
	}
}
