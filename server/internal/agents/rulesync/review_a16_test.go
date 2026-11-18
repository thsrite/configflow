package rulesync

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// Python's worker calls prepare_candidate without force_refresh: the flag a
// failure leaves in the state only resumes an interrupted refresh. Passing it
// to candidate preparation disables live-byte reuse and, with the library scope
// off, unpins and re-downloads rule files the user did not opt in to.
func TestStructurePublicationIgnoresStoredForceRefresh(t *testing.T) {
	f := newWorkerFixture(t, "mihomo")
	if _, e := f.s.Configure(f.ctx, "device", []byte(`{"library":false,"rules":true}`)); e != nil {
		t.Fatal(e)
	}
	var forced []bool
	prepare := f.s.prepare
	f.s.prepare = func(ctx context.Context, target deploy.Target, source *deploy.Source, baseline Baseline, settings Settings, id string, force bool) (*deploy.Bundle, error) {
		forced = append(forced, force)
		return prepare(ctx, target, source, baseline, settings, id, force)
	}
	// A previous round failed: Run's failure path always stores force_refresh.
	past := nowSeconds(f.now.Add(-time.Second))
	if e := persistSync(f.ctx, f.s.m, "device", nil, &model.RuleSyncState{Status: "failed", Message: msgFailed, RetryAt: &past, ForceRefresh: ptr(true)}); e != nil {
		t.Fatal(e)
	}
	_, _, e := f.s.m.Repository().UpdateProfileTransaction(f.ctx, "default", func(_ context.Context, doc *jsonx.Object) (*jsonx.Object, error) {
		for _, entry := range doc.GetList("rule_configs") {
			if r := entry.(*jsonx.Object); r.GetString("id") == "single" {
				r.Set("value", "new-inline.example.com")
			}
		}
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if state := f.state(t); state.Status != "pending" || !value(state.ForceRefresh) {
		t.Fatalf("debounce state %+v", state)
	}
	f.now = f.now.Add(PollInterval)
	if e = f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if len(forced) != 1 {
		t.Fatalf("prepare calls %v, state %+v", forced, f.state(t))
	}
	if forced[0] {
		t.Fatal("stored force_refresh reached candidate preparation (Python passes none)")
	}
	for i := 0; i < 8 && f.state(t).Status == "running"; i++ {
		if e = f.s.Run(f.ctx, "device"); e != nil {
			t.Fatal(e)
		}
	}
	if state := f.state(t); state.Status != "succeeded" || len(forced) != 1 {
		t.Fatalf("publication state %s %s, prepare calls %d", state.Status, state.Message, len(forced))
	}
}

// Python skips a rule whose enabled/library_enabled is falsy (None, 0, "");
// the generators agree. Editing such a rule is not an effective change.
func TestFalsyEnabledRuleIsNotAnEffectiveChange(t *testing.T) {
	source := func(value string, enabled any) *deploy.Source {
		t.Helper()
		raw := jsonx.Obj("profile_id", "default", "rule_library", []any{}, "rule_configs", []any{
			jsonx.Obj("id", "single", "itemType", "rule", "rule_type", "DOMAIN", "value", value, "policy", "DIRECT", "enabled", enabled),
		}, "oxidns", jsonx.Obj("direct_rules", []any{"single"}))
		data, e := json.Marshal(raw)
		if e != nil {
			t.Fatal(e)
		}
		s, e := deploy.SourceFromJSON(data, nil)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	for _, enabled := range []any{nil, 0, ""} {
		kind, _, _, e := RuleChanges(source("old.example.com", enabled), source("new.example.com", enabled), "mihomo", Settings{Rules: true})
		if e != nil {
			t.Fatal(e)
		}
		if kind != None {
			t.Errorf("enabled %#v: kind %s, Python none", enabled, kind)
		}
	}
	kind, _, _, e := RuleChanges(source("old.example.com", true), source("new.example.com", true), "mihomo", Settings{Rules: true})
	if e != nil || kind != Structure {
		t.Fatalf("negative control: kind %s %v", kind, e)
	}
}
