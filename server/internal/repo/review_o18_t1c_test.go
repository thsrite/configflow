package repo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/model"
)

// HR O18 T1c: a config_token stored as another type than text reads as no
// token and marks the settings damaged (fail closed). A settings save of
// another member (server domain) writes the record as read: before this fix
// it dropped the stored token, so the damage was gone and the empty token
// meant anonymous access with login off. The stored value now stays until a
// token is set.
func TestReviewO18T1cSettingsSaveKeepsDamagedConfigToken(t *testing.T) {
	for _, token := range []string{`42`, `{}`, `["x"]`, `true`} {
		t.Run(token, func(t *testing.T) {
			r := newRepo(t)
			damageSettings(t, r, `{"server_domain":"","github_proxy_domain":"","rule_fetch_proxy":"","config_token":`+token+`}`)
			if !must(r.SystemSettingsDamaged(bg))(t) {
				t.Fatal("damaged token not reported")
			}
			noError(t, r.UpdateSystem(bg, func(_ context.Context, s *model.System) error {
				return s.SystemConfig.SetJSON("server_domain", json.RawMessage(`"https://example.com"`))
			}))
			settings := must(r.SystemConfig(bg))(t)
			if settings.ServerDomain != "https://example.com" || settings.RuleProxyToken == "" {
				t.Fatalf("save lost: %+v", settings)
			}
			if !must(r.SystemSettingsDamaged(bg))(t) {
				t.Fatal("an unrelated settings save repaired the damaged token")
			}
			var stored map[string]json.RawMessage
			for _, field := range must(r.store.Load())(t).Document.System.Fields {
				if field.Name == "system_config" {
					noError(t, json.Unmarshal(field.Value, &stored))
				}
			}
			if string(stored["config_token"]) != token {
				t.Fatalf("stored token %s", stored["config_token"])
			}
			// Setting a token is the repair.
			noError(t, r.UpdateSystem(bg, func(_ context.Context, s *model.System) error {
				return s.SystemConfig.SetJSON("config_token", json.RawMessage(`"fresh"`))
			}))
			if must(r.SystemSettingsDamaged(bg))(t) || must(r.SystemConfig(bg))(t).ConfigToken != "fresh" {
				t.Fatal("setting a token did not repair it")
			}
		})
	}
}

// HR O18 C10: a path component that cannot be read (here: a regular file
// used as a directory, ENOTDIR) is kept as given, as realpath did before C10,
// instead of failing the resolution.
func TestReviewO18C10UnreadableComponentKeptAsGiven(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	noError(t, os.WriteFile(file, nil, 0o600))
	base := must(resolve(dir))(t)
	got, err := resolve(filepath.Join(file, "child", "..", "x"))
	if err != nil || got != filepath.Join(base, "file", "x") {
		t.Fatalf("resolve = %q, %v", got, err)
	}
}
