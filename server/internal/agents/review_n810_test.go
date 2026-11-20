package agents

import (
	"testing"

	"configflow/internal/model"
)

// Python compares state['profile_id'] with the prepared proof by type and
// value; a numeric stored deployment identity is not the text profile "7".
func TestReviewN810PreparedOwnershipRejectsCoercedStoredIdentity(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(agent *model.Agent, deploymentID string)
		accept  bool
	}{
		{"text control", func(*model.Agent, string) {}, true},
		{"numeric deployment profile", func(a *model.Agent, id string) {
			d, _ := a.Deployments.Get(id)
			_ = d.SetJSON("profile_id", []byte(`7`))
			a.Deployments.Set(id, d)
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.createProfile("7")
			e.editSystem(func(system *model.System) { e.firstAgent(system).ProfileID = "7" })
			id := "coerced-proof"
			e.begin(id)
			defer e.finish(id)
			e.editSystem(func(system *model.System) { tt.corrupt(e.firstAgent(system), id) })
			before, err := e.repo.Head(e.ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = e.m.RememberPreparedContent(e.ctx, e.id(), id, []byte(`{"profile_id":"7","service_type":"mihomo"}`), nil)
			after, headErr := e.repo.Head(e.ctx)
			if headErr != nil {
				t.Fatal(headErr)
			}
			if tt.accept && (err != nil || after == before) {
				t.Fatalf("control rejected: %v", err)
			}
			if !tt.accept && (err == nil || after != before) {
				t.Fatalf("coerced stored identity accepted: %v", err)
			}
		})
	}
}

// Python persists heartbeat[key] when it differs from agent.get(key) by type
// or presence, so stored numeric/missing values are replaced by reported text.
func TestReviewN810HeartbeatPersistsReportedTextOverCoercedStoredValue(t *testing.T) {
	e, id := setupRuntime(t)
	e.editSystem(func(system *model.System) {
		a := e.firstAgent(system)
		_ = a.SetJSON("version", []byte(`2`))
		_ = a.Clear("config_version")
	})
	if ok, err := e.m.ObserveHeartbeat(e.ctx, id, &model.Agent{Version: "2", ConfigVersion: ""}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	// ConfigVersion "" is the zero value and therefore absent from a bare
	// struct payload; send it through the reported JSON path instead.
	payload := &model.Agent{}
	if err := payload.SetJSON("config_version", []byte(`""`)); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.m.ObserveHeartbeat(e.ctx, id, payload); err != nil || !ok {
		t.Fatal(ok, err)
	}
	stored := e.stored(id)
	if v, ok := storedText(stored.MemberJSON("version")); !ok || v != "2" {
		raw, _, _ := stored.MemberJSON("version")
		t.Fatalf("numeric stored version kept: %s", raw)
	}
	if v, ok := storedText(stored.MemberJSON("config_version")); !ok || v != "" {
		raw, present, _ := stored.MemberJSON("config_version")
		t.Fatalf("missing config_version not persisted: %s %v", raw, present)
	}
}
