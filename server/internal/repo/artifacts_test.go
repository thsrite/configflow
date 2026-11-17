package repo

// Artifact file helpers and the hook-backed operations (desensitized export,
// node deletion). The Python tests that exercise these go through routes of
// other packages; these cover the repository's own part.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
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
	wantError(t, err, ErrRepository, "Cannot read configuration missing.json: [Errno 2] No such file or directory")
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
