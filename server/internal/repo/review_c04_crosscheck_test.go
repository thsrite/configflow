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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"configflow/internal/model"
)

type xcScopeState struct {
	ctx        context.Context
	snapshots  map[string]*testObj
	baselines  map[string]json.RawMessage
	sharedBase *model.Shared
	sysBase    *model.System
}

var xcScopes = map[*Repository]*xcScopeState{}

func xcScope(r *Repository) *xcScopeState {
	st, ok := xcScopes[r]
	if !ok {
		st = &xcScopeState{
			ctx:       WithScope(WithRepository(bg, r)),
			snapshots: map[string]*testObj{},
			baselines: map[string]json.RawMessage{},
		}
		xcScopes[r] = st
	}
	return st
}

func (st *xcScopeState) reset() {
	ResetConfigContext(st.ctx)
	st.snapshots = map[string]*testObj{}
	st.baselines = map[string]json.RawMessage{}
	st.sharedBase = nil
	st.sysBase = nil
}

func xcOptionalString(op *testObj, key string) *string {
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

func xcPlainAgents(t *testing.T, agents []model.Agent, history bool) []any {
	result := []any{}
	for _, agent := range agents {
		plain := xcObjFromRecord(t, agent)
		if !history {
			for _, field := range AgentHistoryFields {
				plain.Set(field, "<not read>")
			}
		}
		result = append(result, plain)
	}
	return result
}

// xcSortedTokens is summary() in repo_crosscheck.py: generated tokens (43
// characters) differ between the runs and are only counted.
func xcSortedTokens(tokens TokenSet) *testObj {
	values := []string{}
	generated := 0
	for token := range tokens {
		if len(token) == 43 {
			generated++
		} else {
			values = append(values, token)
		}
	}
	sort.Strings(values)
	return testObjKV("fixed", testListOf(values), "generated", json.Number(strconv.Itoa(generated)))
}

// xcExecuteC04 mirrors execute_c04() in repo_crosscheck.py; handled=false
// for an unknown operation.
func xcExecuteC04(t *testing.T, r *Repository, op *testObj, arg func(string) *testObj) (result any, err error, handled bool) {
	id := op.GetString("id")
	st := xcScope(r)
	sctx := st.ctx
	handled = true
	switch op.GetString("op") {
	case "save_router_settings":
		err = r.SaveRouterSettingsJSON(bg, id, xcRawOrNil(t, arg("expected")), xcRawOrNil(t, arg("replacement")))
	case "save_router_publication":
		fields := arg("fields")
		option := func(key string) JSONUpdate {
			if value, ok := fields.Get(key); ok {
				return JSONUpdate{Set: true, Value: mustMarshalJSON(t, value)}
			}
			return JSONUpdate{}
		}
		err = r.SaveRouterPublicationJSON(bg, id, xcRawOrNil(t, arg("expected")), RouterPublicationJSON{
			Manifest: option("manifest"), DNSGuard: option("dns_guard"), IPv6FakeIP: option("ipv6_fakeip"),
			IPv6Routes: option("ipv6_routes"), IPv6PDGuard: option("ipv6_pd_guard"), LastRelease: option("last_release")})
	case "import_client_profile":
		var input ProfileCreation
		input, err = xcProfileCreation(t, arg("metadata"), true)
		if err != nil {
			return nil, err, true
		}
		seen := newTestObj()
		var output *ClientProfileResult
		output, err = r.ImportClientProfileRecord(bg, input, func(_ context.Context, shared *model.Shared) (*ClientProfileImport, error) {
			seen.Set("shared_revision", json.Number(strconv.FormatInt(int64(shared.Revision), 10)))
			res := arg("result")
			if res == nil {
				return nil, validationError(msgImportResultShape)
			}
			imp := &ClientProfileImport{}
			for key, val := range res.All() {
				raw := mustMarshalJSON(t, val)
				switch key {
				case "profile":
					var p model.Profile
					if err := json.Unmarshal(raw, &p); err != nil {
						return nil, validationError(msgImportResultShape)
					}
					imp.Profile = &p
				case "shared":
					if err := json.Unmarshal(raw, &imp.Shared); err != nil {
						return nil, validationError(msgImportResultShape)
					}
					if err := imp.Details.Set(key, raw); err != nil {
						return nil, err
					}
				default:
					if err := imp.Details.Set(key, raw); err != nil {
						return nil, err
					}
				}
			}
			return imp, nil
		})
		if err == nil {
			result = testObjKV("result", parseJSONVal(t, mustMarshalJSON(t, output)), "seen", seen)
		}
	case "update_profile_transaction_replace":
		var saved *model.Profile
		var revision int64
		saved, revision, err = r.CommitConfig(bg, id, func(_ context.Context, config *model.Config) error {
			replacement := arg("replacement")
			if replacement == nil {
				return validationError(msgProfileFieldsObject)
			}
			for _, key := range ProfileFields {
				if val, ok := replacement.Get(key); ok {
					if err := config.Profile.SetJSON(key, mustMarshalJSON(t, val)); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err == nil {
			result = testObjKV("saved", xcObjFromRecord(t, saved), "revision", json.Number(strconv.FormatInt(revision, 10)))
		}
	case "get_agents":
		history := op.GetOr("history", true) == true
		var agents []model.Agent
		err = r.ViewAgents(bg, func(records []model.Agent) error {
			agents = append([]model.Agent(nil), records...)
			return nil
		})
		if err == nil {
			result = xcPlainAgents(t, agents, history)
		}
	case "get_system_settings":
		var sys *model.System
		sys, err = r.System(bg)
		if err == nil {
			result = xcObjFromRecord(t, &sys.SystemConfig)
		}
	case "export_desensitized":
		var capturedSettings model.SystemConfig
		wrap := func(key string) func(json.RawMessage) (json.RawMessage, error) {
			return func(raw json.RawMessage) (json.RawMessage, error) {
				return json.Marshal(map[string]json.RawMessage{key: raw})
			}
		}
		previous := SetHooks(Hooks{RecordHooks: RecordHooks{
			SanitizeExternalDocument: func(_ context.Context, payload *model.Document, systemConfig model.SystemConfig) (*model.Document, error) {
				capturedSettings = systemConfig
				return payload, nil
			},
			RedactOxidnsJSON:   wrap("redacted_oxidns"),
			RedactRoutersJSON:  wrap("redacted_routers"),
			RedactEasytierJSON: wrap("redacted_easytier"),
		}})
		var raw json.RawMessage
		raw, err = r.ExportAllJSON(bg, true)
		SetHooks(previous)
		if err == nil {
			result = testObjKV("sanitized", parseJSONVal(t, raw), "system_config", xcObjFromRecord(t, &capturedSettings))
		}
	case "sanitization_tokens":
		var ruleProxy, agents TokenSet
		if ruleProxy, err = r.RuleProxyTokensForSanitization(bg); err == nil {
			if agents, err = r.AgentTokensForSanitization(bg); err == nil {
				result = testObjKV("rule_proxy", xcSortedTokens(ruleProxy), "agents", xcSortedTokens(agents))
			}
		}
	case "profile_metadata":
		var info ProfileInfo
		info, err = r.ProfileMetadataRecord(bg, id)
		if err == nil {
			result = parseJSONVal(t, mustMarshalJSON(t, info))
		}
	case "write_generated":
		path, failure := r.WriteGenerated(bg, id, op.GetString("filename"), []byte(op.GetString("content")))
		result, err = xcRelative(t, r, path, failure)
	case "write_profile_json":
		path, failure := r.WriteProfileJSON(bg, id, op.GetString("path"), arg("data"))
		result, err = xcRelative(t, r, path, failure)
	case "write_profile_text":
		path, failure := r.WriteProfileText(bg, id, op.GetString("path"), []byte(op.GetString("content")))
		result, err = xcRelative(t, r, path, failure)
	case "read_profile_json", "read_shared_json":
		// Retired file-reader facades had no production callers; database dumps are still compared.
		result, err = nil, nil
	case "write_shared_json":
		path, failure := r.WriteSharedJSON(bg, op.GetString("path"), arg("data"))
		result, err = xcRelative(t, r, path, failure)
	case "write_shared_text":
		path, failure := r.WriteSharedText(bg, op.GetString("path"), []byte(op.GetString("content")))
		result, err = xcRelative(t, r, path, failure)
	case "raw_write":
		path := filepath.Join(r.DataDir(), op.GetString("path"))
		noError(t, os.MkdirAll(filepath.Dir(path), 0o777))
		data := must(hex.DecodeString(op.GetString("hex")))(t)
		noError(t, os.WriteFile(path, data, 0o644))
	case "read_file":
		path := filepath.Join(r.DataDir(), op.GetString("path"))
		data := must(os.ReadFile(path))(t)
		info := must(os.Stat(path))(t)
		result = testObjKV("text", string(data), "mode", fmt.Sprintf("0o%o", info.Mode().Perm()))
	case "cfg_get_config":
		var resolvedID string
		if resolvedID, err = ResolveProfileID(sctx, id); err == nil {
			snap, cached := st.snapshots[resolvedID]
			if !cached {
				var cfg *model.Config
				if cfg, err = Config(sctx, resolvedID); err == nil {
					cfg, err = xcConfigForPython(t, sctx, r, resolvedID, cfg)
				}
				if err == nil {
					raw := mustMarshalJSON(t, cfg)
					snap = parseJSONObj(t, raw)
					st.snapshots[resolvedID] = snap
					st.baselines[resolvedID] = raw
				}
			}
			if err == nil {
				xcApplyPatch(t, snap, op.Value("patch"))
				result = snap.DeepCopy()
			}
		}
	case "cfg_save_config":
		var resolvedID string
		if resolvedID, err = ResolveProfileID(sctx, id); err == nil {
			payload := arg("config")
			if payload == nil {
				if snap, ok := st.snapshots[resolvedID]; ok {
					payload = snap
				} else {
					var cfg *model.Config
					if cfg, err = Config(sctx, resolvedID); err == nil {
						cfg, err = xcConfigForPython(t, sctx, r, resolvedID, cfg)
					}
					if err == nil {
						raw := mustMarshalJSON(t, cfg)
						payload = parseJSONObj(t, raw)
						st.baselines[resolvedID] = raw
					}
				}
			}
			if err == nil {
				if _, _, err = r.SaveProfileFieldsJSON(sctx, resolvedID, xcRawOrNil(t, payload), st.baselines[resolvedID]); err == nil {
					st.reset()
					result = true
				}
			}
		}
	case "cfg_save_with_guard":
		var resolvedID string
		if resolvedID, err = ResolveProfileID(sctx, id); err == nil {
			var revision int64
			if _, revision, err = r.SaveProfileFieldsJSON(sctx, resolvedID, xcRawOrNil(t, arg("config")), st.baselines[resolvedID]); err == nil {
				st.reset()
				result = testObjKV("expected_profile_id", resolvedID, "expected_service_type", op.GetString("service"),
					"expected_config_revision", json.Number(strconv.FormatInt(revision, 10)))
			}
		}
	case "cfg_update_config_transaction":
		var resolvedID string
		if resolvedID, err = ResolveProfileID(sctx, id); err == nil {
			if err = UpdateConfig(sctx, resolvedID, func(_ context.Context, config *model.Config) error {
				return xcApplyRecordPatch(t, config, ProfileFields, config.Profile.SetJSON, op.Value("patch"))
			}); err == nil {
				delete(st.snapshots, resolvedID)
				delete(st.baselines, resolvedID)
				var saved *model.Profile
				if saved, err = r.Profile(sctx, resolvedID); err == nil {
					result = xcObjFromRecord(t, saved)
				}
			}
		}
	case "cfg_safe_import_config":
		var resolvedID string
		if resolvedID, err = ResolveProfileID(sctx, id); err == nil {
			if _, err = r.ImportProfileJSON(sctx, resolvedID, xcRawOrNil(t, arg("data"))); err == nil {
				st.reset()
			}
		}
	case "cfg_get_shared_config":
		var shared *model.Shared
		if shared, err = SharedConfig(sctx); err == nil {
			st.sharedBase, err = copyRecord(shared)
			if err == nil {
				result = xcObjFromRecord(t, shared)
			}
		}
	case "cfg_save_shared_config":
		var snap, base *model.Shared
		if raw := xcRawOrNil(t, arg("config")); raw != nil {
			snap = &model.Shared{}
			if err = json.Unmarshal(raw, snap); err != nil {
				return nil, err, true
			}
		}
		if raw := xcRawOrNil(t, arg("baseline")); raw != nil {
			base = &model.Shared{}
			if err = json.Unmarshal(raw, base); err != nil {
				return nil, err, true
			}
		} else {
			base = st.sharedBase
		}
		if err = SaveSharedRecord(sctx, snap, base); err == nil {
			st.reset()
			result = true
		}
	case "cfg_update_shared_config_transaction":
		if err = r.UpdateShared(sctx, func(_ context.Context, shared *model.Shared) error {
			return xcApplyRecordPatch(t, shared, SharedFields, shared.SetJSON, op.Value("patch"))
		}); err == nil {
			st.reset()
			var saved *model.Shared
			if saved, err = r.Shared(sctx); err == nil {
				result = xcObjFromRecord(t, saved)
			}
		}
	case "cfg_update_shared_and_config":
		var selector func(*model.Shared, []*model.Profile) ([]string, error)
		if ids, ok := op.Get("ids"); ok && ids != nil {
			list := testStrings(ids.([]any))
			selector = func(*model.Shared, []*model.Profile) ([]string, error) { return list, nil }
		} else {
			var resolvedID string
			if resolvedID, err = ResolveProfileID(sctx, ""); err != nil {
				return nil, err, true
			}
			selector = func(*model.Shared, []*model.Profile) ([]string, error) { return []string{resolvedID}, nil }
		}
		var shared *model.Shared
		var profiles []*model.Profile
		shared, profiles, err = r.updateSharedConfigs(sctx,
			func(_ context.Context, s *model.Shared) error {
				return xcApplyRecordPatch(t, s, SharedFields, s.SetJSON, op.Value("shared_patch"))
			},
			func(_ context.Context, _ string, _, after *model.Config) error {
				return xcApplyRecordPatch(t, after, ProfileFields, after.Profile.SetJSON, op.Value("profile_patch"))
			}, selector)
		if err == nil {
			st.reset()
			profilesObj := newTestObj()
			for _, p := range profiles {
				profilesObj.Set(p.Key(), xcObjFromRecord(t, p))
			}
			result = testObjKV("shared", xcObjFromRecord(t, shared), "profiles", profilesObj)
		}
	case "cfg_get_system_config":
		var sys *model.System
		if sys, err = r.System(sctx); err == nil {
			st.sysBase, err = copyRecord(sys)
			if err == nil {
				result = xcObjFromRecord(t, sys)
			}
		}
	case "cfg_save_system_config":
		var snap, base *model.System
		if raw := xcRawOrNil(t, arg("config")); raw != nil {
			snap = &model.System{}
			if err = json.Unmarshal(raw, snap); err != nil {
				return nil, err, true
			}
		}
		if raw := xcRawOrNil(t, arg("baseline")); raw != nil {
			base = &model.System{}
			if err = json.Unmarshal(raw, base); err != nil {
				return nil, err, true
			}
		} else {
			base = st.sysBase
		}
		if _, err = r.SaveSystemRecord(sctx, snap, base); err == nil {
			st.reset()
			result = true
		}
	case "cfg_reset":
		st.reset()
	case "cfg_resolve":
		result, err = ResolveProfile(sctx, xcOptionalString(op, "id"), xcOptionalString(op, "fallback"))
	case "cfg_get_resource_config":
		var resolvedID string
		if resolvedID, err = ResolveProfileID(sctx, id); err == nil {
			roots := ResourceRoots{op.GetString("kind"): nil}
			if resourceID := xcOptionalString(op, "resource_id"); resourceID != nil {
				roots[op.GetString("kind")] = []string{*resourceID}
			}
			var cfg *model.Config
			if cfg, err = r.Config(sctx, resolvedID, roots); err == nil {
				if cfg, err = xcConfigForPython(t, sctx, r, resolvedID, cfg); err == nil {
					result = xcObjFromRecord(t, cfg)
				}
			}
		}
	default:
		handled = false
	}
	return result, err, handled
}

// xcConfigForPython returns cfg in the shape of Python's get_config /
// get_compat_config, which always carried the Agent registry. Since O10-T1
// repo.Config omits it (an internal API change: no route, export, backup or
// MCP output serializes a resolved Config); the full registry read from System
// is attached to a copy so every other member is still compared with Python.
// Readers that need Agents use ConfigWithOxiDNSAgents; its projection is
// checked here against the same registry: exactly the projected members, with
// their stored bytes, and nothing else (no token or histories).
func xcConfigForPython(t *testing.T, ctx context.Context, r *Repository, profileID string, cfg *model.Config) (*model.Config, error) {
	t.Helper()
	if cfg.HasAgents || cfg.Agents != nil {
		t.Fatal("Config included agents by default")
	}
	sys, err := r.System(ctx)
	if err != nil {
		return nil, err
	}
	projected, err := r.ConfigWithOxiDNSAgents(ctx, profileID, nil)
	if err != nil {
		return nil, err
	}
	if !projected.HasAgents || len(projected.Agents) != len(sys.Agents) {
		t.Fatalf("OxiDNS projection has %d Agents, registry %d", len(projected.Agents), len(sys.Agents))
	}
	allowed := map[string]bool{}
	for _, key := range oxiDNSAgentFields {
		allowed[key] = true
	}
	for i := range sys.Agents {
		stored := xcObjFromRecord(t, sys.Agents[i])
		got := xcObjFromRecord(t, projected.Agents[i])
		for key := range got.All() {
			if !allowed[key] {
				t.Fatalf("OxiDNS projection of Agent %d carries %q", i, key)
			}
		}
		for _, key := range oxiDNSAgentFields {
			want, present := stored.Get(key)
			value, ok := got.Get(key)
			if present != ok || string(mustMarshalJSON(t, want)) != string(mustMarshalJSON(t, value)) {
				t.Fatalf("OxiDNS projection of Agent %d member %q = %v, registry %v", i, key, value, want)
			}
		}
	}
	out := *cfg
	out.Agents, out.HasAgents = sys.Agents, true
	return &out, nil
}

func TestCrossCheckC04Operations(t *testing.T) {
	xcOperationsFile(t, "testdata/repo_crosscheck_c04_ops.json")
}
