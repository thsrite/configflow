package repo

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

// damageSettings stores system_config as a non-object, the way an old or
// hand-edited store can hold it.
func damageSettings(t *testing.T, r *Repository, settings string) {
	t.Helper()
	base := must(r.store.Load())(t)
	for i := range base.Document.System.Fields {
		if base.Document.System.Fields[i].Name == "system_config" {
			base.Document.System.Fields[i].Value = json.RawMessage(settings)
		}
	}
	_, _, err := r.store.Commit(base, base.Document, nil)
	noError(t, err)
	r.dropCache()
}

// HR O18 T1a: an unrelated write must not repair damaged settings either.
// Token maintenance runs in every transaction; on the empty settings a
// damaged record reads as it would mint a rule-proxy token and write a fresh
// settings object with an empty config token, turning damaged credentials
// into anonymous access.
func TestReviewO18T1aUnrelatedWriteKeepsDamagedSettings(t *testing.T) {
	for _, settings := range []string{`null`, `7`, `"malformed"`, `[]`} {
		t.Run(settings, func(t *testing.T) {
			r := newRepo(t)
			damageSettings(t, r, settings)
			noError(t, r.Update(bg, func(_ context.Context, d *model.Document) error {
				d.Profiles[0].Description = "changed"
				return nil
			}))
			// A system write that leaves the settings alone (for example an
			// Agent edit) runs token maintenance in applySystemIn too.
			noError(t, r.UpdateSystem(bg, func(context.Context, *model.System) error { return nil }))
			load := must(r.store.Load())(t)
			for _, field := range load.Document.System.Fields {
				if field.Name == "system_config" && string(field.Value) != settings {
					t.Fatalf("system_config rewritten to %s", field.Value)
				}
			}
			if !must(r.SystemSettingsDamaged(bg))(t) {
				t.Fatal("settings no longer damaged after an unrelated write")
			}
			if must(r.Profile(bg, DefaultProfileID))(t).Description != "changed" {
				t.Fatal("unrelated write lost")
			}
		})
	}
}

// An explicit settings save is the one write that repairs damaged settings,
// and it gets a rule-proxy token like any other settings write.
func TestReviewO18T1aExplicitSettingsSaveRepairsDamagedSettings(t *testing.T) {
	r := newRepo(t)
	damageSettings(t, r, `"malformed"`)
	noError(t, r.UpdateSystem(bg, func(_ context.Context, s *model.System) error {
		return s.SystemConfig.SetJSON("server_domain", json.RawMessage(`"https://example.com"`))
	}))
	if must(r.SystemSettingsDamaged(bg))(t) {
		t.Fatal("explicit save did not repair the settings")
	}
	settings := must(r.SystemConfig(bg))(t)
	if settings.ServerDomain != "https://example.com" || settings.RuleProxyToken == "" {
		t.Fatalf("repaired settings %+v", settings)
	}
}
