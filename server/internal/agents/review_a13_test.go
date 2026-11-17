package agents

import (
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// TestReviewInspectionKeepsRuntimeOverlay checks the Manager's unchanged live view.
func TestReviewInspectionKeepsRuntimeOverlay(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	must[bool](t)(e.m.UpdateHeartbeat(e.ctx, e.id(), jsonx.Obj("version", "9.9.9-go", "service_status", "running", "system_metrics", jsonx.Obj("cpu", 12.5))))
	seen := 0
	light, err := e.m.GetAllAgentsInspect(e.ctx, false, func(raw *jsonx.Object) {
		seen++
		if raw.GetObject("deployments").Len() != 5 {
			t.Error("Manager inspection did not see raw history")
		}
	})
	if err != nil || seen != len(light) || len(light) == 0 {
		t.Fatal("Manager inspection lost records")
	}
	if light[0].Value("deployments") != repo.AgentHistoryNotRead || light[0].GetString("version") != "9.9.9-go" || light[0].GetString("status") != "online" || !jsonx.Equal(light[0].Value("cpu"), 12.5) {
		t.Fatal("inspection changed runtime overlay or copied history")
	}
}
