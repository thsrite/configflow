package ipv6

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
)

// Review A23 F1: deployment history is trimmed by insertion order (A14 keeps
// the newest 30). Enrollment must not re-sort the history map.
func TestEnrollKeepsDeploymentHistoryOrder(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	ids := []string{"z-older", "m-middle", "a-newest"}
	e := s.Manager.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		for i := range sys.Agents {
			if sys.Agents[i].ID != "source" {
				continue
			}
			if err := sys.Agents[i].SetJSON("deployments", []byte(`{"z-older":{"deployment_id":"z-older","status":"succeeded"},"m-middle":{"deployment_id":"m-middle","status":"succeeded"},"a-newest":{"deployment_id":"a-newest","status":"succeeded"}}`)); err != nil {
				return err
			}
			if err := sys.Agents[i].SetJSON("latest_deployment", []byte(`{"deployment_id":"a-newest","status":"preparing"}`)); err != nil {
				return err
			}
		}
		return nil
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
	a, e := s.Manager.ReadAgent(ctx, "source", true)
	if e != nil {
		t.Fatal(e)
	}
	if !slices.Equal(a.Deployments.Keys(), ids) {
		t.Fatalf("deployment history reordered: %v", a.Deployments.Keys())
	}
	task, ok := a.Deployments.Get("a-newest")
	if !ok || task.IPv6VerificationRequested == nil || !*task.IPv6VerificationRequested {
		t.Fatal("enrollment not recorded", task)
	}
}

// Review A23 F2: a host prepare failure is Agent-supplied text. It must use
// the captured credential seeds like status/probe, not only the live ones.
func TestPrepareFailureUsesCapturedCredentials(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	e := s.Manager.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		var peer model.Agent
		if err := json.Unmarshal([]byte(`{"id":"peer","profile_id":"default","token":"synthetic-peer-old-token","password":"synthetic-peer-old-password"}`), &peer); err != nil {
			return err
		}
		sys.Agents = append(sys.Agents, peer)
		return nil
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

// Review A23 F3: Python compares the parsed client and gateway addresses, so a
// differently spelled gateway address is never offered as a client source.
func TestPolicyCandidateExcludesGatewayAddressSpelling(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	addClient(t, s, ctx)
	for id, host := range map[string]string{"source": "2001:db8::1", "client": "2001:db8:0::1"} {
		if e := s.update(ctx, id, func(_ context.Context, a Document, _ []Document) error {
			a["host"] = raw(host)
			if id == "source" {
				a["ipv6_verification_settings"] = raw(Settings{ClientAgentID: "client"})
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
	}
	a, e := s.agent(ctx, "source", "default")
	if e != nil {
		t.Fatal(e)
	}
	config, _, e := s.snapshot(ctx, "default")
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range s.policy(ctx, config, a).Clients {
		if c.SourceCandidate {
			t.Fatalf("gateway address offered as client candidate: %+v", c)
		}
	}
}
