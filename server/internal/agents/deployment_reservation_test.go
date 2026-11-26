package agents

import (
	"testing"
)

func TestTypedReservationOwnsReturnedJournal(t *testing.T) {
	e := newEnv(t)
	const id = "owned-reservation"
	state, err := e.m.ReserveDeployment(e.ctx, e.id(), id)
	if err != nil || state == nil || !flagSet(state.Success) || state.Status != "preparing" || len(state.Events) != 1 {
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
	raw, present := existing.MemberJSON("existing")
	if !present || string(raw) != `true` {
		t.Fatal("idempotent reservation lost existing marker")
	}
}
