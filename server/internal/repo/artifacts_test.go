package repo

// Artifact file helpers and the hook-backed operations (desensitized export,
// node deletion). The Python tests that exercise these go through routes of
// other packages; these cover the repository's own part.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/model"
)

func TestArtifactWritesAreAtomicAndKeepABackup(t *testing.T) {
	r := newRepo(t)
	path := must(r.WriteGenerated(bg, "default", "config.yaml", []byte("first\n")))(t)
	if path != filepath.Join(r.ProfilesDir(), "default", "generated", "config.yaml") {
		t.Fatalf("path %s", path)
	}
	if lexists(path + ".bak") {
		t.Fatal("backup of nothing")
	}
	must(r.WriteGenerated(bg, "default", "config.yaml", []byte("second\n")))(t)
	if must(readFileString(path))(t) != "second\n" || must(readFileString(path+".bak"))(t) != "first\n" {
		t.Fatal("content or backup")
	}
	if mode := must(os.Stat(path))(t).Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode %o", mode)
	}
	for _, entry := range must(os.ReadDir(filepath.Dir(path)))(t) {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary left: %s", entry.Name())
		}
	}
	for _, name := range []string{"other.yaml", "../config.yaml", "sub/config.yaml", ""} {
		_, err := r.WriteGenerated(bg, "default", name, nil)
		wantError(t, err, ErrValidation, "Invalid generated filename")
	}
	_, err := r.WriteGenerated(bg, "missing", "config.conf", nil)
	wantError(t, err, ErrNotFound, "")
	if lexists(filepath.Join(r.ProfilesDir(), "missing", "generated")) {
		t.Fatal("wrote for a missing profile")
	}

	data := parseJSONObj(t, []byte(`{"名称": "值", "n": 1.5, "list": []}`))
	path = must(r.WriteProfileJSON(bg, "default", "providers/a.json", data))(t)
	if text := must(readFileString(path))(t); text != "{\n  \"名称\": \"值\",\n  \"n\": 1.5,\n  \"list\": []\n}\n" {
		t.Fatalf("text %q", text)
	}
	wantJSONEqual(t, must(os.ReadFile(path))(t), data)
	must(r.WriteProfileText(bg, "default", "rules/x.list", []byte("a.test\n")))(t)
	_, err = r.WriteProfileText(bg, "default", "../x", nil)
	wantError(t, err, ErrValidation, "")
	if must(r.CacheDir("default"))(t) != filepath.Join(r.DataDir(), "shared", "subscribes") ||
		must(r.SharedRulesDir())(t) != filepath.Join(r.DataDir(), "shared", "rules") {
		t.Fatal("shared directories")
	}
}

func TestDesensitizedExportNeedsRedactionHooks(t *testing.T) {
	r := newRepo(t)
	noError(t, r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
		if err := shared.SetJSON("subscriptions", []byte(`[{"id": "s", "name": "S", "url": "https://secret.test/sub"}]`)); err != nil {
			return err
		}
		return shared.SetJSON("nodes", []byte(`[{"id": "n", "name": "N", "type": "ss", "server": "secret.test", "port": 1, "password": "pw", "params": {"k": "v"}}]`))
	}))
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", []byte(`{"mihomo": {"custom_config": "secret", "linux_gateway_restore_point": {"x": 1}, "easytier": {"options": {"network-secret": "s"}}}, "loon": {"custom_config": "secret"}}`), nil)
	noError(t, err)
	scAppendAgent(t, r, `{"id": "a", "name": "A", "profile_id": "default", "token": "agent-token"}`)

	defer SetHooks(SetHooks(Hooks{}))
	_, err = r.ExportAllJSON(bg, true)
	wantError(t, err, ErrRepository, "脱敏")

	var sanitizedWith model.SystemConfig
	SetHooks(Hooks{RecordHooks: RecordHooks{
		SanitizeExternalDocument: func(_ context.Context, payload *model.Document, systemConfig model.SystemConfig) (*model.Document, error) {
			sanitizedWith = systemConfig
			return payload, nil
		},
		RedactOxidnsJSON:  func(json.RawMessage) (json.RawMessage, error) { return []byte(`"oxidns-redacted"`), nil },
		RedactRoutersJSON: func(json.RawMessage) (json.RawMessage, error) { return []byte(`{"items":[],"redacted":true}`), nil },
	}})
	result := parseJSONObj(t, must(r.ExportAllJSON(bg, true))(t))
	system := result.GetObject("system")
	settings := system.GetObject("system_config")
	if _, has := settings.Get("rule_proxy_token"); has {
		t.Fatal("rule_proxy_token exported")
	}
	if _, has := settings.Get("retired_rule_proxy_tokens"); has {
		t.Fatal("retired_rule_proxy_tokens exported")
	}
	if _, has := settings.Get("config_token"); has {
		t.Fatal("config_token exported")
	}
	if sanitizedWith.RuleProxyToken == "" {
		t.Fatal("the sanitizer must receive the original system_config")
	}
	wantJSONEqual(t, system.Value("backup"), map[string]any{})
	wantJSONEqual(t, system.Value("agents"), []any{})
	shared := result.GetObject("shared")
	wantJSONEqual(t, shared.GetList("subscriptions")[0].(*testObj).Value("url"), "[REDACTED]")
	node := shared.GetList("nodes")[0].(*testObj)
	wantJSONEqual(t, []any{node.Value("server"), node.Value("password"), node.Value("params"), node.Value("name")},
		[]any{"[REDACTED]", "[REDACTED]", map[string]any{}, "N"})
	profile := result.GetObject("profiles").GetObject("default")
	mihomo := profile.GetObject("mihomo")
	wantJSONEqual(t, mihomo, json.RawMessage(`{"custom_config": "", "easytier": {"options": {"network-secret": "[REDACTED]"}}}`))
	wantJSONEqual(t, profile.Value("oxidns"), "oxidns-redacted")
	// Routers is a plain record: the hook returns an object (O18 T1a) whose
	// undeclared members are dropped (O18 T1c).
	wantJSONEqual(t, profile.Value("routers"), json.RawMessage(`{"items":[]}`))
	wantJSONEqual(t, profile.GetObject("loon").Value("custom_config"), "")
	// The stored document is untouched.
	if must(r.System(bg))(t).SystemConfig.RuleProxyToken == "" {
		t.Fatal("export modified the document")
	}
}

func TestNodeDeletionPreviewAndCommit(t *testing.T) {
	r := newRepo(t)
	noError(t, r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
		return shared.SetJSON("nodes", []byte(`[{"id": "n1", "name": "N1"}, {"id": "n2", "name": "N2"}]`))
	}))
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", []byte(`{"proxy_groups": [{"id": "g", "name": "G", "type": "select", "manual_nodes": ["n1", "n2"]}]}`), nil)
	noError(t, err)

	preview := must(r.PreviewNodeDeletionIDs(bg, []string{"n1"}))(t)
	if len(preview.Blockers) != 0 || preview.Confirmation == "" {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	_, err = r.DeleteNodesWithReferencesIDs(bg, []string{"n1"}, "not-a-digest")
	wantError(t, err, ErrValidation, "请先查看并确认删除影响")
	_, err = r.DeleteNodesWithReferencesIDs(bg, []string{"n1"}, strings.Repeat("b", 64))
	wantError(t, err, ErrNodeDeletionChanged, "节点或引用关系已变化")
	wantError(t, err, ErrInUse, "")

	revision := must(r.ConfigRevision(bg, "default"))(t)
	must(r.DeleteNodesWithReferencesIDs(bg, []string{"n1"}, preview.Confirmation))(t)
	wantJSONEqual(t, must(r.Shared(bg))(t).Nodes, json.RawMessage(`[{"id": "n2", "name": "N2"}]`))
	wantJSONEqual(t, *must(r.Profile(bg, "default"))(t).ProxyGroups[0].ManualNodes, []string{"n2"})
	// One commit: the profile and the shared section each advance once.
	if got := must(r.ConfigRevision(bg, "default"))(t); got != revision+2 {
		t.Fatalf("revision=%d, want %d", got, revision+2)
	}
}

// Export hooks must not resolve credentials from a different repository or
// generation after the export has removed its own agents and capability keys.
func TestExportSanitizerUsesExportRepositorySnapshot(t *testing.T) {
	r, other := newRepo(t), newRepo(t)
	scAppendAgent(t, r, `{"id":"export-agent","name":"Export","profile_id":"default","token":"synthetic-export-token"}`)
	scAppendAgent(t, other, `{"id":"other-agent","name":"Other","profile_id":"default","token":"synthetic-other-token"}`)
	called := false
	pass := func(v json.RawMessage) (json.RawMessage, error) { return v, nil }
	defer SetHooks(SetHooks(Hooks{RecordHooks: RecordHooks{
		SanitizeExternalDocument: func(ctx context.Context, payload *model.Document, settings model.SystemConfig) (*model.Document, error) {
			called = true
			bound, err := GetRepository(ctx)
			noError(t, err)
			if bound != r || r.lockedSnapshot(ctx) == nil {
				t.Fatal("sanitizer lost export repository snapshot")
			}
			tokens := must(bound.AgentTokensForSanitization(ctx))(t)
			if !tokens.Has("synthetic-export-token") || tokens.Has("synthetic-other-token") {
				t.Fatal("sanitizer used another repository")
			}
			if len(payload.System.Agents) != 0 {
				t.Fatal("export must omit agents before sanitization")
			}
			original := must(bound.System(ctx))(t).SystemConfig
			wantJSONEqual(t, settings, original)
			return payload, nil
		},
		RedactOxidnsJSON:  pass,
		RedactRoutersJSON: pass,
	}}))
	must(r.ExportAllJSON(WithRepository(bg, other), true))(t)
	if !called {
		t.Fatal("sanitizer not called")
	}
	called = false
	full := parseJSONObj(t, must(r.ExportAllJSON(WithRepository(bg, other), false))(t))
	if called || len(full.GetObject("system").GetList("agents")) != 1 {
		t.Fatal("full restorable export was sanitized")
	}
}

func TestN811TypedExportHooksRunOnce(t *testing.T) {
	r := newRepo(t)
	_, err := r.UpdateProfile(bg, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("mihomo", []byte(`{"custom_config":"private","easytier":{"enabled":true},"linux_gateway_restore_point":{"private":"snapshot"}}`))
	})
	noError(t, err)
	calls := map[string]int{}
	pass := func(name string) func(json.RawMessage) (json.RawMessage, error) {
		return func(raw json.RawMessage) (json.RawMessage, error) { calls[name]++; return raw, nil }
	}
	defer SetHooks(SetHooks(Hooks{
		RecordHooks: RecordHooks{
			RedactOxidnsJSON: pass("oxidns"), RedactRoutersJSON: pass("routers"),
			SanitizeExternalDocument: func(_ context.Context, d *model.Document, s model.SystemConfig) (*model.Document, error) {
				calls["sanitize"]++
				if s.RuleProxyToken == "" || d.System.SystemConfig.RuleProxyToken != "" {
					t.Fatal("sanitizer did not receive original settings and redacted payload")
				}
				return d, nil
			},
		},
	}))
	raw := must(r.ExportAllJSON(bg, true))(t)
	for _, name := range []string{"oxidns", "routers", "sanitize"} {
		if calls[name] != 1 {
			t.Fatalf("%s calls=%d", name, calls[name])
		}
	}
	if strings.Contains(string(raw), "linux_gateway_restore_point") || strings.Contains(string(raw), "snapshot") {
		t.Fatal("restore point exported")
	}
}

func TestN811TypedCommitHooksRunOnceInOrder(t *testing.T) {
	defer SetHooks(SetHooks(Hooks{}))
	r := newRepo(t)
	_, err := r.UpdateProfile(bg, "default", func(_ context.Context, profile *model.Profile) error {
		return profile.SetJSON("routers", []byte(`{"items":[{"id":"router","name":"Router","type":"routeros","endpoint":"http://192.0.2.1","username":"operator","password":"old"}]}`))
	})
	noError(t, err)
	err = r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		return system.SetJSON("backup", []byte(`{"auto_backup":true}`))
	})
	noError(t, err)
	locks, releases, commits := 0, 0, 0
	SetHooks(Hooks{
		RecordHooks: RecordHooks{
			JobLockRecord: func(_ context.Context, profileID, routerID string, router model.Router) (func(), bool) {
				locks++
				if profileID != "default" || routerID != "router" || router.Password != "old" {
					t.Fatal("job lock did not receive committed router identity")
				}
				return func() { releases++ }, true
			},
			ConfigurationCommittedRecords: func(repository *Repository, previous, current *model.Document) {
				commits++
				if repository != r || previous.Profiles[0].Routers.Items[0].Password != "old" || current.Profiles[0].Routers.Items[0].Password != "new" {
					t.Fatal("commit observer did not receive old and committed generations")
				}
				if releases != 1 {
					t.Fatal("commit observer ran before router lock release")
				}
			},
		},
	})
	_, err = r.UpdateProfile(bg, "default", func(_ context.Context, profile *model.Profile) error {
		profile.Routers.Items[0].Password = "new"
		return nil
	})
	noError(t, err)
	if locks != 1 || releases != 1 || commits != 1 {
		t.Fatalf("locks=%d releases=%d commits=%d; want one each", locks, releases, commits)
	}
}
