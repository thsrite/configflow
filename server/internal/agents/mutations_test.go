package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

func TestTypedAgentEditsKeepOriginalEvidenceAndExactKeys(t *testing.T) {
	e, id := setupRuntime(t)
	var stored model.Agent
	if err := json.Unmarshal([]byte(`{"id":"device","name":"original","host":"192.0.2.8","port":"8080","service_type":"mihomo","profile_id":"default","enabled":"false","future":{"x":2.00},"deployments":{"old":{"status":"succeeded","future":3e0}}}`), &stored); err != nil {
		t.Fatal(err)
	}
	stored.ID = id
	if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, records *[]model.Agent) error { *records = []model.Agent{stored}; return nil }); err != nil {
		t.Fatal(err)
	}
	edits, err := DecodeAgentEdits([]byte(`{"name":"changed","Name":"forged","port":8080.0,"enabled":false,"token":"forged","future":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := e.m.EditAgent(e.ctx, id, edits); err != nil || !ok {
		t.Fatal(ok, err)
	}
	a, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ key, want string }{
		{"name", `"changed"`}, {"port", `8080.0`}, {"enabled", `false`}, {"future", `{"x":2.00}`}, {"deployments", `{"old":{"status":"succeeded","future":3e0}}`},
	} {
		raw, present, err := a.MemberJSON(tt.key)
		if err != nil || !present || !bytes.Equal(raw, []byte(tt.want)) {
			t.Fatalf("field=%s got=%s present=%v err=%v want=%s", tt.key, raw, present, err, tt.want)
		}
	}
	if raw, present, _ := a.MemberJSON("token"); present {
		t.Fatalf("uneditable token inserted: %s", raw)
	}
}

func TestTypedAgentGuardsBindOriginalPortTypes(t *testing.T) {
	for _, tt := range []struct {
		stored, patch string
		conflict      bool
	}{
		{`8080`, `8080.0`, false}, {`8080.0`, `8.08e3`, false},
		{`"8080"`, `8080`, true}, {`1`, `true`, true}, {`true`, `1`, true},
	} {
		t.Run(tt.stored+"_"+tt.patch, func(t *testing.T) {
			e, id := setupRuntime(t)
			if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, records *[]model.Agent) error {
				a := registryAgent(*records, id)
				if err := a.SetJSON("port", []byte(tt.stored)); err != nil {
					return err
				}
				a.OxiDNSHAPublication = &model.HAJournal{Status: "preparing"}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			head, _ := e.repo.Head(e.ctx)
			edits, err := DecodeAgentEdits([]byte(`{"port":` + tt.patch + `}`))
			if err != nil {
				t.Fatal(err)
			}
			_, err = e.m.EditAgent(e.ctx, id, edits)
			var conflict *DeploymentConflict
			if errors.As(err, &conflict) != tt.conflict || !tt.conflict && err != nil {
				t.Fatal(err)
			}
			if tt.conflict {
				if after, _ := e.repo.Head(e.ctx); after != head {
					t.Fatal("rejected edit committed")
				}
			}
		})
	}
}

func TestTypedTokenLookupRejectsProjectedStoredIdentity(t *testing.T) {
	e, id := setupRuntime(t)
	for _, tt := range []struct {
		raw   string
		match bool
	}{
		{`"7"`, true}, {`7`, false}, {`null`, false}, {`false`, false},
	} {
		if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, records *[]model.Agent) error {
			return registryAgent(*records, id).SetJSON("token", []byte(tt.raw))
		}); err != nil {
			t.Fatal(err)
		}
		a, err := e.m.ReadAgentByToken(e.ctx, "7")
		if err != nil || (a != nil) != tt.match {
			t.Fatalf("stored=%s matched=%v err=%v", tt.raw, a != nil, err)
		}
	}
}

func TestTypedAgentMutationsDetachButRespectShutdown(t *testing.T) {
	e, id := setupRuntime(t)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(e.ctx)
	defer rt.Shutdown(context.Background())
	request, disconnect := context.WithCancel(rt.BindRequest(e.ctx))
	disconnect()
	edits, err := DecodeAgentEdits([]byte(`{"name":"disconnected"}`))
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := e.m.EditAgent(request, id, edits); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if a, err := e.m.SetDomainDiscoveryModel(request, id, true); err != nil || a == nil || a.DomainDiscoveryEnabled == nil || !*a.DomainDiscoveryEnabled {
		t.Fatal(a, err)
	}
	if a, err := e.m.SetTrafficHistoryModel(request, id, true); err != nil || a == nil || a.TrafficHistorySince == nil {
		t.Fatal(a, err)
	}
	bound := rt.BindRequest(e.ctx)
	rt.Shutdown(context.Background())
	head, _ := e.repo.Head(e.ctx)
	if ok, err := e.m.EditAgent(bound, id, edits); ok || !errors.Is(err, context.Canceled) {
		t.Fatal(ok, err)
	}
	if a, err := e.m.SetDomainDiscoveryModel(bound, id, false); a != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(a, err)
	}
	if a, err := e.m.SetTrafficHistoryModel(bound, id, false); a != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(a, err)
	}
	if ok, err := e.m.DeleteAgent(bound, id); ok || !errors.Is(err, context.Canceled) {
		t.Fatal(ok, err)
	}
	if after, _ := e.repo.Head(e.ctx); after != head {
		t.Fatal("shutdown wrote")
	}
}
