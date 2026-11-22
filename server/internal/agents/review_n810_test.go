package agents

import (
	"testing"

	"configflow/internal/model"
)

// Python persists heartbeat[key] when it differs from agent.get(key) by type
// or presence, so stored numeric/missing values are replaced by reported text.
func TestReviewN810HeartbeatPersistsReportedTextOverCoercedStoredValue(t *testing.T) {
	e, id := setupRuntime(t)
	e.editSystem(func(system *model.System) {
		a := e.firstAgent(system)
		_ = model.SetStoredMember(&a, "version", []byte(`2`))
		_ = model.ZeroMember(&a, "config_version")
	})
	if ok, err := e.m.ObserveHeartbeat(e.ctx, id, &model.Agent{Version: "2", ConfigVersion: ""}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	// ConfigVersion "" is the zero value and therefore absent from a bare
	// struct payload; send it through the reported JSON path instead.
	payload := &model.Agent{}
	if err := model.SetStoredMember(&payload, "config_version", []byte(`""`)); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.m.ObserveHeartbeat(e.ctx, id, payload); err != nil || !ok {
		t.Fatal(ok, err)
	}
	stored := e.stored(id)
	if v, ok := storedText(model.EncodeMember(&stored, "version")); !ok || v != "2" {
		raw, _, _ := model.EncodeMember(&stored, "version")
		t.Fatalf("numeric stored version kept: %s", raw)
	}
	// An empty reported config_version is not a report (O18 T1b), and an
	// empty stored one is a missing member.
}
