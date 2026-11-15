package repo

// C04 review: cross-check of the public API (config_repository.py part B)
// and the config.py module functions against the real Python code. The
// operations are in testdata/repo_crosscheck_c04_ops.json; the Python side is
// execute_c04() in testdata/repo_crosscheck.py. config.py's ContextVars are
// the main thread's on the Python side and one Scope per opened repository
// here (set_repository resets the ContextVars).

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"configflow/internal/jsonx"
)

var xcScopes = map[*Repository]context.Context{}

func xcScope(r *Repository) context.Context {
	ctx, ok := xcScopes[r]
	if !ok {
		ctx = WithScope(WithRepository(bg, r))
		xcScopes[r] = ctx
	}
	return ctx
}

func xcOptionalString(op *jsonx.Object, key string) *string {
	value, ok := op.Get(key)
	if !ok || value == nil {
		return nil
	}
	text := value.(string)
	return &text
}

func xcRelative(t *testing.T, r *Repository, path string, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return must(filepath.Rel(r.DataDir(), path))(t), nil
}

func xcPlainAgents(agents []any) []any {
	result := []any{}
	for _, item := range agents {
		agent := item.(*jsonx.Object)
		plain := jsonx.NewObject()
		for key, value := range agent.All() {
			if value == AgentHistoryNotRead {
				value = "<not read>"
			}
			plain.Set(key, value)
		}
		result = append(result, plain)
	}
	return result
}

func xcSortedTokens(tokens TokenSet) []any {
	values := make([]string, 0, len(tokens))
	for token := range tokens {
		values = append(values, token)
	}
	sort.Strings(values)
	return jsonx.ListOf(values)
}

// xcExecuteC04 mirrors execute_c04() in repo_crosscheck.py; handled=false
// for an unknown operation.
func xcExecuteC04(t *testing.T, r *Repository, op *jsonx.Object, arg func(string) *jsonx.Object) (result any, err error, handled bool) {
	id := op.GetString("id")
	sctx := xcScope(r)
	handled = true
	switch op.GetString("op") {
	case "save_router_settings":
		err = r.SaveRouterSettings(bg, id, arg("expected"), arg("replacement"))
	case "save_router_publication":
		fields := arg("fields")
		option := func(key string) Optional {
			if value, ok := fields.Get(key); ok {
				return Some(value)
			}
			return Optional{}
		}
		err = r.SaveRouterPublication(bg, id, arg("expected"), RouterPublication{
			Manifest: option("manifest"), DNSGuard: option("dns_guard"), IPv6FakeIP: option("ipv6_fakeip"),
			IPv6Routes: option("ipv6_routes"), IPv6PDGuard: option("ipv6_pd_guard"), LastRelease: option("last_release")})
	case "import_client_profile":
		seen := jsonx.NewObject()
		var output *jsonx.Object
		output, err = r.ImportClientProfile(bg, arg("metadata"), func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
			seen.Set("shared_revision", shared.Value("_revision"))
			return arg("result").DeepCopy(), nil
		})
		if err == nil {
			result = jsonx.Obj("result", output, "seen", seen)
		}
	case "update_profile_transaction_replace":
		var saved *jsonx.Object
		var revision int64
		saved, revision, err = r.UpdateProfileTransaction(bg, id, func(context.Context, *jsonx.Object) (*jsonx.Object, error) {
			return arg("replacement").DeepCopy(), nil
		})
		if err == nil {
			result = jsonx.Obj("saved", saved, "revision", jsonx.Int(revision))
		}
	case "get_agents":
		var agents []any
		agents, err = r.GetAgents(bg, op.GetOr("history", true) == true)
		if err == nil {
			result = xcPlainAgents(agents)
		}
	case "get_system_settings":
		result, err = r.GetSystemSettings(bg)
	case "export_desensitized":
		previous := SetHooks(Hooks{
			SanitizeExternalPayload: func(payload, systemConfig *jsonx.Object) (*jsonx.Object, error) {
				return jsonx.Obj("sanitized", payload, "system_config", systemConfig), nil
			},
			RedactOxidnsSettings:   func(value any) any { return jsonx.Obj("redacted_oxidns", value) },
			RedactRouters:          func(value any) any { return jsonx.Obj("redacted_routers", value) },
			RedactEasytierSettings: func(value any) any { return jsonx.Obj("redacted_easytier", value) },
		})
		result, err = r.ExportAll(bg, true)
		SetHooks(previous)
	case "sanitization_tokens":
		var ruleProxy, agents TokenSet
		if ruleProxy, err = r.RuleProxyTokensForSanitization(bg); err == nil {
			if agents, err = r.AgentTokensForSanitization(bg); err == nil {
				result = jsonx.Obj("rule_proxy", xcSortedTokens(ruleProxy), "agents", xcSortedTokens(agents))
			}
		}
	case "profile_metadata":
		result, err = r.ProfileMetadata(bg, id)
	case "write_generated":
		path, failure := r.WriteGenerated(bg, id, op.GetString("filename"), []byte(op.GetString("content")))
		result, err = xcRelative(t, r, path, failure)
	case "write_profile_json":
		path, failure := r.WriteProfileJSON(bg, id, op.GetString("path"), arg("data"))
		result, err = xcRelative(t, r, path, failure)
	case "write_profile_text":
		path, failure := r.WriteProfileText(bg, id, op.GetString("path"), []byte(op.GetString("content")))
		result, err = xcRelative(t, r, path, failure)
	case "read_profile_json":
		result, err = r.ReadProfileJSON(id, op.GetString("path"))
	case "write_shared_json":
		path, failure := r.WriteSharedJSON(bg, op.GetString("path"), arg("data"))
		result, err = xcRelative(t, r, path, failure)
	case "write_shared_text":
		path, failure := r.WriteSharedText(bg, op.GetString("path"), []byte(op.GetString("content")))
		result, err = xcRelative(t, r, path, failure)
	case "read_shared_json":
		result, err = r.ReadSharedJSON(bg, op.GetString("path"))
	case "raw_write":
		path := filepath.Join(r.DataDir(), op.GetString("path"))
		noError(t, os.MkdirAll(filepath.Dir(path), 0o777))
		data := must(hex.DecodeString(op.GetString("hex")))(t)
		noError(t, os.WriteFile(path, data, 0o644))
	case "read_file":
		path := filepath.Join(r.DataDir(), op.GetString("path"))
		data := must(os.ReadFile(path))(t)
		info := must(os.Stat(path))(t)
		result = jsonx.Obj("text", string(data), "mode", fmt.Sprintf("0o%o", info.Mode().Perm()))
	case "cfg_get_config":
		var config *jsonx.Object
		if config, err = GetConfig(sctx, id); err == nil {
			xcApplyPatch(t, config, op.Value("patch"))
			result = config.DeepCopy()
		}
	case "cfg_save_config":
		if err = SaveConfig(sctx, arg("config"), id); err == nil {
			result = true
		}
	case "cfg_save_with_guard":
		result, err = SaveConfigWithPublishGuard(sctx, arg("config"), op.GetString("service"), id)
	case "cfg_update_config_transaction":
		result, err = UpdateConfigTransaction(sctx, xcUpdater(t, op.Value("patch")), id)
	case "cfg_safe_import_config":
		err = SafeImportConfig(sctx, arg("data"), id)
	case "cfg_get_shared_config":
		result, err = GetSharedConfig(sctx)
	case "cfg_save_shared_config":
		if err = SaveSharedConfig(sctx, arg("config"), arg("baseline")); err == nil {
			result = true
		}
	case "cfg_update_shared_config_transaction":
		result, err = UpdateSharedConfigTransaction(sctx, xcUpdater(t, op.Value("patch")))
	case "cfg_update_shared_and_config":
		var selector ProfileSelector
		if ids, ok := op.Get("ids"); ok && ids != nil {
			selector = Profiles(jsonx.Strings(ids.([]any))...)
		}
		var shared, profiles *jsonx.Object
		shared, profiles, err = UpdateSharedAndConfigTransaction(sctx, xcUpdater(t, op.Value("shared_patch")),
			func(_ context.Context, _ string, _, after *jsonx.Object) (*jsonx.Object, error) {
				xcApplyPatch(t, after, op.Value("profile_patch"))
				return nil, nil
			}, selector)
		if err == nil {
			result = jsonx.Obj("shared", shared, "profiles", profiles)
		}
	case "cfg_get_system_config":
		result, err = GetSystemConfig(sctx)
	case "cfg_save_system_config":
		if err = SaveSystemConfig(sctx, arg("config"), arg("baseline")); err == nil {
			result = true
		}
	case "cfg_reset":
		ResetConfigContext(sctx)
	case "cfg_resolve":
		result, err = ResolveProfile(sctx, xcOptionalString(op, "id"), xcOptionalString(op, "fallback"))
	case "cfg_get_resource_config":
		result, err = GetResourceConfig(sctx, op.GetString("kind"), xcOptionalString(op, "resource_id"), id)
	default:
		handled = false
	}
	return result, err, handled
}

func TestCrossCheckC04Operations(t *testing.T) {
	xcOperationsFile(t, "testdata/repo_crosscheck_c04_ops.json")
}
