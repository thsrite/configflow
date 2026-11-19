package agents

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestTypedReservationOwnsReturnedJournal(t *testing.T) {
	e := newEnv(t)
	const id = "owned-reservation"
	state, err := e.m.ReserveDeployment(e.ctx, e.id(), id)
	if err != nil || state == nil || !state.Success || state.Status != "preparing" || len(state.Events) != 1 {
		t.Fatal(state, err)
	}
	defer e.finish(id)
	state.Status = "succeeded"
	state.Events[0].Status = "forged"
	a, err := e.m.ReadAgent(e.ctx, e.id(), true)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := a.Deployments.Get(id)
	if stored.Status != "preparing" || len(stored.Events) != 1 || stored.Events[0].Status != "preparing" || a.LatestDeployment.Status != "preparing" {
		t.Fatal("caller mutated stored journal")
	}
	existing, err := e.m.ReserveDeployment(e.ctx, e.id(), id)
	if err != nil {
		t.Fatal(err)
	}
	raw, present := existing.Fields.Get("existing")
	if !present || string(raw) != `true` {
		t.Fatal("idempotent reservation lost existing marker")
	}
}

func TestTypedReservationRequiresOriginalHAChildText(t *testing.T) {
	for _, tt := range []struct {
		child    string
		accepted bool
	}{{`"7"`, true}, {`7`, false}} {
		t.Run(tt.child, func(t *testing.T) {
			e := newEnv(t)
			if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, records *[]model.Agent) error {
				a := registryAgent(*records, e.id())
				data, err := json.Marshal(map[string]any{"status": "ready", "children": map[string]json.RawMessage{a.ID: json.RawMessage(tt.child)}})
				if err != nil {
					return err
				}
				return a.SetJSON("oxidns_ha_publication", data)
			}); err != nil {
				t.Fatal(err)
			}
			state, err := e.m.ReserveDeployment(e.ctx, e.id(), "7")
			defer e.finish("7")
			if err != nil {
				t.Fatal(err)
			}
			if tt.accepted {
				if !state.Success || state.Status != "preparing" {
					t.Fatal(state)
				}
			} else if state.Success || state.HTTPStatus == nil || *state.HTTPStatus != 409 || state.HAPublication == nil {
				t.Fatal("numeric child became ownership", state)
			}
		})
	}
}
