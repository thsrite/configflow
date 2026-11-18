package ipv6

import (
	"context"
	"slices"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

// Review A23 F1: deployment history is trimmed by insertion order (A14 keeps
// the newest 30). Enrollment must not re-sort the history map.
func TestEnrollKeepsDeploymentHistoryOrder(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	ids := []string{"z-older", "m-middle", "a-newest"}
	_, e := s.Manager.UpdateAgents(ctx, "", func(_ context.Context, rows *[]any) (any, error) {
		for _, row := range *rows {
			a := row.(*jsonx.Object)
			if a.GetString("id") != "source" {
				continue
			}
			history := jsonx.NewObject()
			for _, id := range ids {
				history.Set(id, jsonx.Obj("deployment_id", id, "status", "succeeded"))
			}
			a.Set("deployments", history)
			a.Set("latest_deployment", jsonx.Obj("deployment_id", "a-newest", "status", "preparing"))
		}
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	config, _, e := s.snapshot(ctx, "default")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Enroll(ctx, "source", "a-newest", config); e != nil {
		t.Fatal(e)
	}
	a, e := s.Manager.GetAgentByID(ctx, "source", true)
	if e != nil {
		t.Fatal(e)
	}
	history, _ := a.Value("deployments").(*jsonx.Object)
	if history == nil || !slices.Equal(history.Keys(), ids) {
		t.Fatalf("deployment history reordered: %v", history)
	}
	task, _ := history.Value("a-newest").(*jsonx.Object)
	if task == nil || !jsonx.Truthy(task.Value("ipv6_verification_requested")) {
		t.Fatal("enrollment not recorded", task)
	}
}

// Review A23 F2: a host prepare failure is Agent-supplied text. It must use
// the captured credential seeds like status/probe, not only the live ones.
func TestPrepareFailureUsesCapturedCredentials(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	_, e := s.Manager.UpdateAgents(ctx, "", func(_ context.Context, rows *[]any) (any, error) {
		*rows = append(*rows, jsonx.Obj("id", "peer", "profile_id", "default", "token", "synthetic-peer-old-token", "password", "synthetic-peer-old-password"))
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	first, e := s.Status(ctx, "source", "default", true)
	if e != nil || !first.CanPrepare {
		t.Fatalf("status=%+v err=%v", first, e)
	}
	s.Manager.SetClient(clientFunc(func(ctx context.Context, r agents.Request) (*agents.Response, error) {
		if !strings.HasSuffix(r.URL, "/prepare") {
			return response(hostStatus()), nil
		}
		if e := s.update(ctx, "peer", func(_ context.Context, a Document, _ []Document) error {
			a["token"] = raw("synthetic-peer-new-token")
			a["password"] = raw("synthetic-peer-new-password")
			return nil
		}); e != nil {
			t.Error(e)
		}
		return &agents.Response{StatusCode: 422, Body: []byte(`{"success":false,"message":"synthetic-peer-old-token synthetic-peer-old-password"}`)}, nil
	}))
	_, e = s.Prepare(ctx, "source", "default", raw(Document{"interface": raw(first.PrepareInterface), "check_token": raw(first.CheckToken)}))
	requireCode(t, e, "PREPARE_FAILED", 502)
	// The HTTP owner projects the boundary error with the live seeds afterwards.
	public, err := s.Public(ctx, "default", Document{"message": raw(e.Error())})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), "synthetic-peer-old") {
		t.Fatal("prepare failure leaked replaced peer credentials", string(public))
	}
}
