package agents

import (
	"context"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func getAllAgentsInspectLegacy(m *Manager, ctx context.Context, history bool, inspect func(*jsonx.Object)) ([]*jsonx.Object, error) {
	agents, err := m.agentsInspect(ctx, history, inspect)
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		agent.Set("status", OnlineStatus(agent, nil))
	}
	return agents, nil
}

func withHistory(t *testing.T, m *Manager, id string) {
	t.Helper()
	_, err := m.UpdateAgents(context.Background(), "", func(_ context.Context, agents *[]any) (any, error) {
		var agent *jsonx.Object
		for _, item := range *agents {
			if a, ok := item.(*jsonx.Object); ok && a.GetString("id") == id {
				agent = a
				break
			}
		}
		deployments := jsonx.NewObject()
		for i := 0; i < 5; i++ {
			key := "release-" + string(rune('0'+i))
			deployments.Set(key, jsonx.Obj("deployment_id", key, "status", "succeeded", "events", []any{jsonx.Obj("message", strings.Repeat("x", 200))}))
		}
		agent.Set("deployments", deployments)
		agent.Set("latest_deployment", deployments.GetObject("release-4").DeepCopy())
		agent.Set("ipv6_verifications", jsonx.Obj("release-4", jsonx.Obj("status", "ok")))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func withoutHistory(agent *jsonx.Object) *jsonx.Object {
	out := agent.Clone()
	out.Delete("deployments")
	out.Delete("ipv6_verifications")
	return out
}

// TestReviewInspectionKeepsRuntimeOverlay checks the Manager's unchanged live view.
func TestReviewInspectionKeepsRuntimeOverlay(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	e.beat(e.id(), map[string]any{"version": "9.9.9-go", "service_status": "running", "system_metrics": map[string]any{"cpu": 12.5}})
	seen := 0
	light, err := getAllAgentsInspectLegacy(e.m, e.ctx, false, func(raw *jsonx.Object) {
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

// Legacy unread-history sentinel tests (W9: remove when GetAgentByID/GetAllAgents/UpdateAgents/repo.GetAgents facades are deleted).

func TestManagerReadsLayHeartbeatStateOverBothKindsOfRecord(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	e.beat(e.id(), map[string]any{"version": "9.9.9-go", "service_status": "running", "system_metrics": map[string]any{"cpu": 12.5}})
	full := must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, e.id(), true))
	light := must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, e.id(), false))
	if light.Value("service_status") != "running" || !jsonx.Equal(light.Value("cpu"), 12.5) || light.Value("status") != "online" {
		t.Fatalf("light %v", light)
	}
	if !jsonx.Equal(withoutHistory(light), withoutHistory(full)) || full.GetObject("deployments").Len() != 5 || light.Value("deployments") != repo.AgentHistoryNotRead {
		t.Fatal("history")
	}
	if must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, "missing", false)) != nil {
		t.Fatal("missing")
	}
}

func TestUnreadHistoryIsNeverSaved(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	light := must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, e.id(), false))
	head, _ := e.repo.Head(e.ctx)
	_, err := e.m.UpdateAgents(e.ctx, "", func(_ context.Context, agents *[]any) (any, error) {
		(*agents)[0] = light
		return nil, nil
	})
	if err == nil {
		t.Fatal("a record without history was saved")
	}
	if after, _ := e.repo.Head(e.ctx); after != head || e.get(e.id()).Deployments.Len() != 5 {
		t.Fatal("changed")
	}
}

func TestRecordsWithoutHistoryEqualTheFullOnesInEveryOtherField(t *testing.T) {
	e := newEnv(t)
	e.register(map[string]any{"name": "second", "host": "second.test", "port": 8080, "service_type": "oxidns"}, "")
	withHistory(t, e.m, e.id())
	full := must[[]any](t)(e.repo.GetAgents(e.ctx, true))
	light := must[[]any](t)(e.repo.GetAgents(e.ctx, false))
	sys, err := e.repo.GetSystem(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonx.Equal(full, sys.GetList("agents")) || len(full) != 2 || len(light) != 2 {
		t.Fatalf("full %v", full)
	}
	for i := range full {
		if !jsonx.Equal(withoutHistory(light[i].(*jsonx.Object)), withoutHistory(full[i].(*jsonx.Object))) {
			t.Fatalf("%d: light %v full %v", i, light[i], full[i])
		}
	}
	first := full[0].(*jsonx.Object)
	if first.GetObject("deployments").Len() != 5 || !first.GetObject("ipv6_verifications").Has("release-4") {
		t.Fatalf("first %v", first)
	}
	for _, item := range light {
		for _, field := range repo.AgentHistoryFields {
			if item.(*jsonx.Object).Value(field) != repo.AgentHistoryNotRead {
				t.Fatalf("%s of %v", field, item)
			}
		}
	}
	light[0].(*jsonx.Object).GetObject("latest_deployment").Set("status", "edited")
	first.GetObject("deployments").GetObject("release-0").Set("status", "edited")
	again := must[[]any](t)(e.repo.GetAgents(e.ctx, true))[0].(*jsonx.Object)
	if again.GetObject("latest_deployment").Value("status") != "succeeded" ||
		again.GetObject("deployments").GetObject("release-0").Value("status") != "succeeded" {
		t.Fatalf("again %v", again)
	}
}

func TestManagerListsLayHeartbeatStateOverBothKindsOfRecord(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	e.beat(e.id(), map[string]any{"version": "9.9.9-go", "service_status": "running"})
	listedFull := must[[]*jsonx.Object](t)(e.m.GetAllAgents(e.ctx, true))
	listedLight := must[[]*jsonx.Object](t)(e.m.GetAllAgents(e.ctx, false))
	if len(listedFull) != len(listedLight) || len(listedFull) != 1 {
		t.Fatal("length")
	}
	for i := range listedFull {
		if !jsonx.Equal(withoutHistory(listedLight[i]), withoutHistory(listedFull[i])) || listedLight[i].Value("service_status") != "running" {
			t.Fatalf("light %v full %v", listedLight[i], listedFull[i])
		}
	}
}

func TestEveryUseOfUnreadHistoryFails(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	light := must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, e.id(), false))
	for _, field := range repo.AgentHistoryFields {
		if _, err := repo.AgentHistory(light, field); err == nil || !strings.Contains(err.Error(), "history=True") {
			t.Fatalf("%s: %v", field, err)
		}
		value := light.Value(field)
		if _, ok := value.(*jsonx.Object); ok {
			t.Fatalf("%s reads as an object", field)
		}
		if _, ok := value.([]any); ok {
			t.Fatalf("%s reads as a list", field)
		}
		for name, encode := range map[string]func(any) ([]byte, error){"Compact": jsonx.Compact, "Default": jsonx.Default, "Sorted": jsonx.Sorted} {
			if _, err := encode(value); err == nil {
				t.Fatalf("%s: %s encoded the sentinel", field, name)
			}
		}
	}
	full := must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, e.id(), true))
	if deployments, err := repo.AgentHistory(full, "deployments"); err != nil || deployments.(*jsonx.Object).Len() != 5 {
		t.Fatalf("full history %v %v", deployments, err)
	}
}

func TestUnreadHistorySurvivesCopiesButIsNeverSerialized(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	light := must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, e.id(), false))
	for name, copied := range map[string]*jsonx.Object{"deep": light.DeepCopy(), "shallow": light.Clone(),
		"value": jsonx.DeepCopy(light).(*jsonx.Object)} {
		for _, field := range repo.AgentHistoryFields {
			if copied.Value(field) != repo.AgentHistoryNotRead {
				t.Fatalf("%s copy lost %s", name, field)
			}
		}
	}
	if _, err := jsonx.Default(light); err == nil {
		t.Fatal("json.dumps(light) must fail")
	}
	if _, err := jsonx.Response(light); err == nil {
		t.Fatal("jsonify(light) must fail")
	}
}
