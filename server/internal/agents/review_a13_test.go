package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/model"
)

func withHistory(t *testing.T, m *Manager, id string) {
	t.Helper()
	err := m.EditAgents(context.Background(), "", func(_ context.Context, list *[]model.Agent) error {
		for i := range *list {
			if (*list)[i].ID != id {
				continue
			}
			deployments := map[string]any{}
			for j := 0; j < 5; j++ {
				key := "release-" + string(rune('0'+j))
				deployments[key] = map[string]any{
					"deployment_id": key,
					"status":        "succeeded",
					"events":        []any{map[string]any{"message": strings.Repeat("x", 200)}},
				}
			}
			depRaw, err := json.Marshal(deployments)
			if err != nil {
				return err
			}
			latestRaw, err := json.Marshal(deployments["release-4"])
			if err != nil {
				return err
			}
			ipv6Raw := json.RawMessage(`{"release-4":{"status":"ok"}}`)
			if err := (*list)[i].SetJSON("deployments", depRaw); err != nil {
				return err
			}
			if err := (*list)[i].SetJSON("latest_deployment", latestRaw); err != nil {
				return err
			}
			return (*list)[i].SetJSON("ipv6_verifications", ipv6Raw)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestReviewInspectionKeepsRuntimeOverlay checks the Manager's unchanged live view.
func TestReviewInspectionKeepsRuntimeOverlay(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	e.beat(e.id(), map[string]any{"version": "9.9.9-go", "service_status": "running", "system_metrics": map[string]any{"cpu": 12.5}})
	seen := 0
	light, err := e.m.ReadAgents(e.ctx, false, func(raw *model.Agent) {
		seen++
		if raw.Deployments.Len() != 5 {
			t.Error("Manager inspection did not see raw history")
		}
	})
	if err != nil || seen != len(light) || len(light) == 0 {
		t.Fatal("Manager inspection lost records")
	}
	cpuRaw, cpuOK, err := light[0].MemberJSON("cpu")
	if err != nil || !cpuOK || string(cpuRaw) != "12.5" {
		t.Fatalf("inspection changed runtime cpu overlay: %s %v %v", cpuRaw, cpuOK, err)
	}
	if light[0].Deployments.Len() != 0 || light[0].Version != "9.9.9-go" || !FreshHeartbeat(light[0].LastHeartbeat, nil) {
		t.Fatal("inspection changed runtime overlay or copied history")
	}
}
