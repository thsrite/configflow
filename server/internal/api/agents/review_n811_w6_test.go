package agents_test

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
)

// HR review N8-11 W6 H1: the activate route must dispatch through the HA owner
// (oxidns_ha_publish.activate), never activate the candidate directly.
func TestReviewN811ActivateRouteDispatchesHAOwner(t *testing.T) {
	s := newServer(t)
	calls := 0
	agents.RegisterHooks(agents.Hooks{HAActivate: func(_ context.Context, _ *agents.Manager, agentID, deploymentID string) (*model.Deployment, error) {
		calls++
		if agentID != "pair-member" || deploymentID != "pair-task" {
			t.Fatalf("arguments: %q %q", agentID, deploymentID)
		}
		var d model.Deployment
		err := json.Unmarshal([]byte(`{"success":true,"status":"succeeded","deployment_id":"pair-task"}`), &d)
		return &d, err
	}})
	response := s.Post("/api/agents/pair-member/deployments/pair-task/activate")
	response.WantStatus(200)
	if calls != 1 {
		t.Fatalf("HA owner calls = %d, want 1", calls)
	}
	if got := response.Object().GetString("deployment_id"); got != "pair-task" {
		t.Fatalf("response did not come from the HA owner: %s", response.Body)
	}
}
