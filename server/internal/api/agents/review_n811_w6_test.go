package agents_test

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
)

type stubPairActivator struct {
	activate func(ctx context.Context, agentID, deploymentID string) (*model.Deployment, error)
}

func (s stubPairActivator) PairDeployment(context.Context, string, string) (*model.Deployment, error) {
	return nil, nil
}
func (s stubPairActivator) ActivatePair(ctx context.Context, agentID, deploymentID string) (*model.Deployment, error) {
	return s.activate(ctx, agentID, deploymentID)
}
func (s stubPairActivator) ServiceOperationJSON(context.Context, string, func() (json.RawMessage, error), bool) (json.RawMessage, error) {
	return nil, nil
}

// HR review N8-11 W6 H1: the activate route must dispatch through the HA owner
// (oxidns_ha_publish.activate), never activate the candidate directly.
func TestReviewN811ActivateRouteDispatchesHAOwner(t *testing.T) {
	calls := 0
	s, _ := newServerWithOptions(t, agents.Options{
		Pair: stubPairActivator{activate: func(_ context.Context, agentID, deploymentID string) (*model.Deployment, error) {
			calls++
			if agentID != "pair-member" || deploymentID != "pair-task" {
				t.Fatalf("arguments: %q %q", agentID, deploymentID)
			}
			var d model.Deployment
			err := json.Unmarshal([]byte(`{"success":true,"status":"succeeded","deployment_id":"pair-task"}`), &d)
			return &d, err
		}},
	})
	response := s.Post("/api/agents/pair-member/deployments/pair-task/activate")
	response.WantStatus(200)
	if calls != 1 {
		t.Fatalf("HA owner calls = %d, want 1", calls)
	}
	if got := response.Object().GetString("deployment_id"); got != "pair-task" {
		t.Fatalf("response did not come from the HA owner: %s", response.Body)
	}
}
