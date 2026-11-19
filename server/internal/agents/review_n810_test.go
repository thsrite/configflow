package agents

import (
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// Python compares state['profile_id'] with the prepared proof by type and
// value; a numeric stored deployment identity is not the text profile "7".
func TestReviewN810PreparedOwnershipRejectsCoercedStoredIdentity(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(agent *jsonx.Object, deploymentID string)
		accept  bool
	}{
		{"text control", func(*jsonx.Object, string) {}, true},
		{"numeric deployment profile", func(a *jsonx.Object, id string) {
			a.GetObject("deployments").GetObject(id).Set("profile_id", jsonx.Int(7))
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.createProfile("7")
			e.editSystem(func(system *jsonx.Object) { e.firstAgent(system).Set("profile_id", "7") })
			id := "coerced-proof"
			e.begin(id)
			defer e.finish(id)
			e.editSystem(func(system *jsonx.Object) { tt.corrupt(e.firstAgent(system), id) })
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
	e.editSystem(func(system *jsonx.Object) {
		a := e.firstAgent(system)
		a.Set("version", jsonx.Int(2))
		a.Delete("config_version")
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
	if v, ok := stored.Value("version").(string); !ok || v != "2" {
		t.Fatalf("numeric stored version kept: %#v", stored.Value("version"))
	}
	if v, ok := stored.Get("config_version"); !ok || v != "" {
		t.Fatalf("missing config_version not persisted: %#v %v", v, ok)
	}
}
