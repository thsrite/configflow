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

	"configflow/internal/jsonx"
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

	data := parse(t, `{"名称": "值", "n": 1.5, "list": []}`)
	path = must(r.WriteProfileJSON(bg, "default", "providers/a.json", data))(t)
	if text := must(readFileString(path))(t); text != "{\n  \"名称\": \"值\",\n  \"n\": 1.5,\n  \"list\": []\n}\n" {
		t.Fatalf("text %q", text)
	}
	wantEqual(t, must(r.ReadProfileJSON("default", "providers/a.json"))(t), data)
	_, err = r.ReadProfileJSON("default", "providers/missing.json")
	wantError(t, err, ErrRepository, "Cannot read configuration missing.json:")
	noError(t, os.WriteFile(path, []byte("[1]"), 0o600))
	_, err = r.ReadProfileJSON("default", "providers/a.json")
	wantError(t, err, ErrValidation, "JSON object expected in a.json")
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
	must(r.UpdateSharedTransaction(bg, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Update(parse(t, `{"subscriptions": [{"id": "s", "name": "S", "url": "https://secret.test/sub"}],
			"nodes": [{"id": "n", "name": "N", "type": "ss", "server": "secret.test", "port": 1, "password": "pw", "params": {"k": "v"}}]}`))
		return nil, nil
	}))(t)
	_, _, err := r.UpdateProfileFields(bg, "default", parse(t, `{"mihomo": {"custom_config": "secret", "linux_gateway_restore_point": {"x": 1}, "easytier": {"secret": "s"}},
		"loon": {"custom_config": "secret"}}`), nil)
	noError(t, err)
	scAppendAgent(t, r, `{"id": "a", "name": "A", "profile_id": "default", "token": "agent-token"}`)

	defer SetHooks(SetHooks(Hooks{}))
	_, err = r.ExportAll(bg, true)
	wantError(t, err, ErrRepository, "脱敏")

	var sanitizedWith *jsonx.Object
	SetHooks(Hooks{
		SanitizeExternalPayload: func(_ context.Context, payload, systemConfig *jsonx.Object) (*jsonx.Object, error) {
			sanitizedWith = systemConfig
			return payload, nil
		},
		RedactOxidnsSettings:   func(any) any { return "oxidns-redacted" },
		RedactRouters:          func(any) any { return "routers-redacted" },
		RedactEasytierSettings: func(any) any { return "easytier-redacted" },
	})
	result := must(r.ExportAll(bg, true))(t)
	system := result.GetObject("system")
	settings := system.GetObject("system_config")
	if settings.Has("rule_proxy_token") || settings.Has("retired_rule_proxy_tokens") || settings.Has("config_token") {
		t.Fatal("tokens exported")
	}
	if !sanitizedWith.Has("rule_proxy_token") {
		t.Fatal("the sanitizer must receive the original system_config")
	}
	wantEqual(t, system.Value("backup"), jsonx.NewObject())
	wantEqual(t, system.Value("agents"), []any{})
	shared := result.GetObject("shared")
	wantEqual(t, shared.GetList("subscriptions")[0].(*jsonx.Object).Value("url"), "[REDACTED]")
	node := shared.GetList("nodes")[0].(*jsonx.Object)
	wantEqual(t, []any{node.Value("server"), node.Value("password"), node.Value("params"), node.Value("name")},
		[]any{"[REDACTED]", "[REDACTED]", jsonx.NewObject(), "N"})
	profile := result.GetObject("profiles").GetObject("default")
	mihomo := profile.GetObject("mihomo")
	wantEqual(t, mihomo, parse(t, `{"custom_config": "", "easytier": "easytier-redacted"}`))
	wantEqual(t, profile.Value("oxidns"), "oxidns-redacted")
	wantEqual(t, profile.Value("routers"), "routers-redacted")
	wantEqual(t, profile.GetObject("loon").Value("custom_config"), "")
	// The stored document is untouched.
	if !must(r.GetSystemSettings(bg))(t).Has("rule_proxy_token") {
		t.Fatal("export modified the document")
	}
}

func TestNodeDeletionGoesThroughThePlanHook(t *testing.T) {
	r := newRepo(t)
	must(r.UpdateSharedTransaction(bg, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("nodes", []any{parse(t, `{"id": "n1", "name": "N1"}`), parse(t, `{"id": "n2", "name": "N2"}`)})
		return nil, nil
	}))(t)
	_, _, err := r.UpdateProfileFields(bg, "default", parse(t, `{"proxy_groups": [{"id": "g", "name": "G", "type": "select", "manual_nodes": ["n1", "n2"]}]}`), nil)
	noError(t, err)

	defer SetHooks(SetHooks(Hooks{}))
	_, err = r.PreviewNodeDeletion(bg, []any{"n1"})
	wantError(t, err, ErrRepository, "PlanNodeDeletion")

	confirmation := strings.Repeat("a", 64)
	detach := true
	SetHooks(Hooks{PlanNodeDeletion: func(document *jsonx.Object, nodeIDs any) (*jsonx.Object, *jsonx.Object, error) {
		candidate := document.DeepCopy()
		candidate.GetObject("shared").Set("nodes", []any{parse(t, `{"id": "n2", "name": "N2"}`)})
		if detach {
			candidate.GetObject("profiles").GetObject("default").GetList("proxy_groups")[0].(*jsonx.Object).Set("manual_nodes", []any{"n2"})
		}
		preview := jsonx.Obj("blockers", []any{}, "usages", []any{}, "confirmation", confirmation,
			"groups", []any{jsonx.Obj("id", "g", "name", "G", "profile_id", "default", "profile_name", "默认配置")})
		return preview, candidate, nil
	}})
	_, err = r.DeleteNodesWithReferences(bg, []any{"n1"}, "not-a-digest")
	wantError(t, err, ErrValidation, "请先查看并确认删除影响")
	_, err = r.DeleteNodesWithReferences(bg, []any{"n1"}, strings.Repeat("b", 64))
	wantError(t, err, ErrNodeDeletionChanged, "节点或引用关系已变化")
	wantError(t, err, ErrInUse, "")

	// A candidate that does not validate becomes a blocker of the preview.
	detach = false
	preview := must(r.PreviewNodeDeletion(bg, []any{"n1"}))(t)
	if blockers := preview.GetList("blockers"); len(blockers) != 1 || !strings.Contains(blockers[0].(string), "引用的共享资源不存在：nodes/n1") {
		t.Fatalf("blockers %s", jsonx.Repr(blockers))
	}
	_, err = r.DeleteNodesWithReferences(bg, []any{"n1"}, confirmation)
	wantError(t, err, ErrInUse, "无法删除：")
	wantEqual(t, len(must(r.GetShared(bg))(t).GetList("nodes")), 2)

	detach = true
	revision := must(r.ConfigRevision(bg, "default"))(t)
	must(r.DeleteNodesWithReferences(bg, []any{"n1"}, confirmation))(t)
	wantEqual(t, must(r.GetShared(bg))(t).Value("nodes"), []any{parse(t, `{"id": "n2", "name": "N2"}`)})
	wantEqual(t, must(r.GetProfile(bg, "default"))(t).GetList("proxy_groups")[0].(*jsonx.Object).Value("manual_nodes"), []any{"n2"})
	// One commit: the profile and the shared section each advance once.
	wantEqual(t, must(r.ConfigRevision(bg, "default"))(t), revision+2)
}

// Export hooks must not resolve credentials from a different repository or
// generation after the export has removed its own agents and capability keys.
func TestExportSanitizerUsesExportRepositorySnapshot(t *testing.T) {
	r, other := newRepo(t), newRepo(t)
	scAppendAgent(t, r, `{"id":"export-agent","name":"Export","profile_id":"default","token":"synthetic-export-token"}`)
	scAppendAgent(t, other, `{"id":"other-agent","name":"Other","profile_id":"default","token":"synthetic-other-token"}`)
	called := false
	defer SetHooks(SetHooks(Hooks{
		SanitizeExternalPayload: func(ctx context.Context, payload, settings *jsonx.Object) (*jsonx.Object, error) {
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
			if len(payload.GetObject("system").GetList("agents")) != 0 {
				t.Fatal("export must omit agents before sanitization")
			}
			original := must(bound.GetSystemSettings(ctx))(t)
			wantEqual(t, settings, original)
			return payload, nil
		},
		RedactOxidnsSettings:   func(v any) any { return v },
		RedactRouters:          func(v any) any { return v },
		RedactEasytierSettings: func(v any) any { return v },
	}))
	must(r.ExportAll(WithRepository(bg, other), true))(t)
	if !called {
		t.Fatal("sanitizer not called")
	}
	called = false
	full := must(r.ExportAll(WithRepository(bg, other), false))(t)
	if called || len(full.GetObject("system").GetList("agents")) != 1 {
		t.Fatal("full restorable export was sanitized")
	}
}

// Both old and typed registrations may coexist during N8-11. Each operation
// selects the typed collaborator exactly once and keeps the reviewed inputs.
func TestN811TypedHooksTakePrecedence(t *testing.T) {
	r := newRepo(t)
	noError(t, r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
		shared.Nodes = []model.Node{{ID: "n", Name: "Node"}}
		return nil
	}))
	calls := 0
	preview := &NodeDeletionPreview{Nodes: []DeletionNode{{ID: "n", Name: "Node"}}, Usages: []DeletionUsage{}, Groups: []DeletionGroup{}, Blockers: []string{}, EmptySources: []string{}, Confirmation: strings.Repeat("a", 64)}
	defer SetHooks(SetHooks(Hooks{
		PlanNodeDeletion: func(*jsonx.Object, any) (*jsonx.Object, *jsonx.Object, error) {
			t.Fatal("old planner invoked")
			return nil, nil, nil
		},
		RecordHooks: RecordHooks{PlanNodeDeletionRecords: func(document *model.Document, ids []string) (*NodeDeletionPreview, *model.Document, error) {
			calls++
			if len(ids) != 1 || ids[0] != "n" || len(document.Shared.Nodes) != 1 {
				t.Fatal("planner inputs changed")
			}
			document.Shared.Nodes = []model.Node{}
			return preview, document, nil
		}},
	}))
	must(r.PreviewNodeDeletionIDs(bg, []string{"n"}))(t)
	must(r.DeleteNodesWithReferencesIDs(bg, []string{"n"}, preview.Confirmation))(t)
	if calls != 2 {
		t.Fatalf("planner calls=%d, want one per operation", calls)
	}
}

func TestN811TypedExportHooksTakePrecedence(t *testing.T) {
	r := newRepo(t)
	_, err := r.UpdateProfile(bg, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("mihomo", []byte(`{"custom_config":"private","easytier":{"enabled":true},"linux_gateway_restore_point":{"private":"snapshot"}}`))
	})
	noError(t, err)
	calls := map[string]int{}
	pass := func(name string) func(json.RawMessage) (json.RawMessage, error) {
		return func(raw json.RawMessage) (json.RawMessage, error) { calls[name]++; return raw, nil }
	}
	old := func(any) any { t.Fatal("old redaction invoked"); return nil }
	defer SetHooks(SetHooks(Hooks{
		SanitizeExternalPayload: func(context.Context, *jsonx.Object, *jsonx.Object) (*jsonx.Object, error) {
			t.Fatal("old sanitizer invoked")
			return nil, nil
		},
		RedactOxidnsSettings: old, RedactRouters: old, RedactEasytierSettings: old,
		RecordHooks: RecordHooks{
			RedactOxidnsJSON: pass("oxidns"), RedactRoutersJSON: pass("routers"), RedactEasytierJSON: pass("easytier"),
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
	for _, name := range []string{"oxidns", "routers", "easytier", "sanitize"} {
		if calls[name] != 1 {
			t.Fatalf("%s calls=%d", name, calls[name])
		}
	}
	if strings.Contains(string(raw), "linux_gateway_restore_point") || strings.Contains(string(raw), "snapshot") {
		t.Fatal("restore point exported")
	}
}

func TestN811TypedPathsDoNotMaterializeLegacyDocument(t *testing.T) {
	r := newRepo(t)
	assertNative := func() {
		t.Helper()
		entry := r.cached()
		if entry == nil || entry.legacy == nil || entry.legacy.document != nil {
			t.Fatal("native path materialized a legacy document")
		}
	}
	assertNative()
	must(r.ExportAllJSON(bg, false))(t)
	assertNative()
	noError(t, r.UpdateConfig(bg, "default", func(_ context.Context, config *model.Config) error {
		config.Profile.Mihomo.CustomConfig = "port: 17890"
		return nil
	}))
	assertNative()
}

func TestN811TypedCommitHooksTakePrecedence(t *testing.T) {
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
		JobLock: func(context.Context, string, string, *jsonx.Object) (func(), bool) {
			t.Fatal("old job lock invoked")
			return nil, false
		},
		ConfigurationCommitted: func(*Repository, *jsonx.Object, *jsonx.Object) {
			t.Fatal("old commit observer invoked")
		},
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
