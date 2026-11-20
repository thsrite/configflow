package repo

// Regression tests of the C04 review (public API, config.py). Expectations
// marked "Python" come from runs of the Python code; the corpus is
// testdata/repo_c04_reference.json (repo_c04_reference.py).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/testjson"
)

func c04ReferenceValue(t *testing.T, section string) any {
	t.Helper()
	data := must(os.ReadFile("testdata/repo_c04_reference.json"))(t)
	return parseJSONObj(t, data).Value(section)
}

// get_default_config, _get_initial_config and _empty_profile of the
// application's repository, byte for byte (stored values).
func TestReferenceConfigDefaults(t *testing.T) {
	want := c04ReferenceValue(t, "defaults").(*testObj)
	r := must(OpenDefault(t.TempDir()))(t)
	empty := must(r.emptyProfileRecord("p1", " Name ", ""))(t)
	empty.CreatedAt = "<time>"
	empty.UpdatedAt = "<time>"
	initialRaw, err := mergeObjectsJSON(defaultConfigJSON(), configTemplate)
	noError(t, err)
	for name, got := range map[string]json.RawMessage{
		"default_config": defaultConfigJSON(),
		"initial_config": initialRaw,
		"empty_profile":  mustMarshalJSON(t, empty),
	} {
		if !testjson.Equal(mustMarshalJSON(t, want.Value(name)), got) {
			t.Errorf("%s defaults differ:\n%s", name, testjson.Diff(mustMarshalJSON(t, want.Value(name)), got))
		}
	}
}

func c04RouterProfile(t *testing.T) (*Repository, json.RawMessage) {
	t.Helper()
	r := newRepo(t)
	router := json.RawMessage(`{"id":"rt1","name":"R","type":"routeros","endpoint":"192.0.2.1","username":"u","password":"p","verify_tls":false}`)
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", []byte(`{"routers":{"items":[`+string(router)+`]}}`), nil)
	noError(t, err)
	return r, router
}

// save_router_settings / save_router_publication with a non-string expected
// id: Python finds no record (ProfileInUse), raises TypeError for an
// unhashable id and KeyError without one.
func TestSaveRouterWithNonStringExpectedID(t *testing.T) {
	r, router := c04RouterProfile(t)
	withID := func(id any) json.RawMessage {
		obj := parseJSONObj(t, router)
		obj.Set("id", id)
		return mustMarshalJSON(t, obj)
	}
	wantError(t, r.SaveRouterSettingsJSON(bg, "default", withID(5), nil), ErrInUse, "路由器设置已变化，请刷新后重试")
	wantError(t, r.SaveRouterPublicationJSON(bg, "default", withID(1.5), RouterPublicationJSON{}), ErrInUse,
		"路由器连接已变化，无法保存发布回读记录")
	for _, id := range []any{map[string]any{"a": 1}, []any{"x"}} {
		// A container id is refused before anything is read or written.
		if err := r.SaveRouterSettingsJSON(bg, "default", withID(id), nil); err == nil {
			t.Fatalf("settings with id %v accepted", id)
		}
		if err := r.SaveRouterPublicationJSON(bg, "default", withID(id), RouterPublicationJSON{}); err == nil {
			t.Fatalf("publication with id %v accepted", id)
		}
	}
	missing := parseJSONObj(t, router)
	missing.Delete("id")
	if err := r.SaveRouterSettingsJSON(bg, "default", mustMarshalJSON(t, missing), nil); err == nil {
		t.Fatal("missing id accepted")
	}
	// A non-string id on a profile that does not exist: the profile is looked up first.
	wantError(t, r.SaveRouterSettingsJSON(bg, "ghost", withID(5), nil), ErrNotFound, "ghost")
	wantJSONEqual(t, must(r.Profile(bg, "default"))(t).Routers.Items[0], router)
}

// ImportClientProfileRecord: a nil result from the builder is refused and
// writes nothing.
func TestImportClientProfileNilResult(t *testing.T) {
	r := newRepo(t)
	before := must(r.Store().Fingerprint())(t)
	_, err := r.ImportClientProfileRecord(bg, ProfileCreation{ID: "c", Name: "C"},
		func(context.Context, *model.Shared) (*ClientProfileImport, error) {
			return nil, nil
		})
	if err == nil || must(r.Store().Fingerprint())(t) != before {
		t.Fatalf("got %v", err)
	}
}

func c04Resolve(ctx context.Context, explicit, fallback *string) []any {
	id, err := ResolveProfile(ctx, explicit, fallback)
	if err != nil {
		return []any{xcErrorClass(err), err.Error()}
	}
	return []any{id}
}

func c04Text(text string) *string { return &text }

// resolve_profile_id's per-request memo keeps "no explicit id" apart from
// every explicit id, "\x00" included (Python probe in a Flask request
// without endpoint, ?profile=work).
func TestResolveProfileMemoKeepsNoneApartFromExplicitIDs(t *testing.T) {
	r := newRepo(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "work", Name: "W"}, ""))(t)
	ctx := WithScope(WithRepository(bg, r))
	SetRequest(ctx, RequestInfo{QueryProfile: c04Text("work")})
	wantJSONEqual(t, c04Resolve(ctx, nil, nil), []any{"work"})
	wantJSONEqual(t, c04Resolve(ctx, c04Text("\x00"), nil), []any{"ProfileValidationError", "Invalid profile id"})
	wantJSONEqual(t, c04Resolve(ctx, c04Text("default"), nil), []any{"default"})
	wantJSONEqual(t, c04Resolve(ctx, nil, nil), []any{"work"})

	ctx = WithScope(WithRepository(bg, r))
	SetRequest(ctx, RequestInfo{})
	wantJSONEqual(t, c04Resolve(ctx, nil, nil), []any{"default"})
	wantJSONEqual(t, c04Resolve(ctx, c04Text("\x00"), nil), []any{"ProfileValidationError", "Invalid profile id"})
	wantJSONEqual(t, c04Resolve(ctx, nil, c04Text("work")), []any{"work"})
	wantJSONEqual(t, c04Resolve(ctx, c04Text(""), nil), []any{"ProfileValidationError", "Invalid profile id"})
}

// A commit between taking the memo and storing a result (another request
// deleting the profile meanwhile) leaves the result untrusted: Python
// stores into the memo it took, which the next lookup discards.
func TestResolveProfileResultValidatedBeforeACommitIsNotTrusted(t *testing.T) {
	r := newRepo(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "work", Name: "W"}, ""))(t)
	ctx := WithScope(WithRepository(bg, r))
	SetRequest(ctx, RequestInfo{})
	scope := ScopeOf(ctx)
	scope.mu.Lock()
	memo := scope.resolvedProfiles(ctx, r)
	scope.mu.Unlock()
	noError(t, r.DeleteProfile(bg, "work", ""))
	scope.remember(memo, resolvedKey{explicit: true, id: "work"}, "work")
	wantJSONEqual(t, c04Resolve(ctx, c04Text("work"), nil), []any{"ProfileNotFound", "'work'"})
}

// create_rule_library_item keeps the cache lock through the commit and the
// clean-up (Python: ExitStack around the transaction): the commit hook,
// which runs after .config.lock is released, still finds it held.
func TestCreateRuleLibraryItemHoldsCacheLockThroughCommit(t *testing.T) {
	r := newRepo(t)
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		return system.SetJSON("backup", []byte(`{"auto_backup":true}`))
	}))
	rule := decodeJSON[model.RuleLibraryItem](t, `{"id":"lib9","name":"L","source_type":"content"}`)
	path := must(r.SharedPath(filepath.Join("rules", rules.CacheFilename(decodeJSON[model.RuleConfig](t, `{"id":"lib9","name":"L","source_type":"content"}`)))))(t)
	checked := false
	defer SetHooks(SetHooks(Hooks{RecordHooks: RecordHooks{ConfigurationCommittedRecords: func(*Repository, *model.Document, *model.Document) {
		_, release, acquired := fileLocks.TryLock(bg, resolvePath(path))
		if acquired {
			release()
			t.Error("cache lock not held after the commit")
		}
		checked = true
	}}}))
	must(r.CreateRuleLibraryRecord(bg, rule, []byte("DOMAIN,a\n")))(t)
	if !checked {
		t.Fatal("commit hook not called")
	}
	_, release, acquired := fileLocks.TryLock(bg, resolvePath(path))
	if !acquired {
		t.Fatal("cache lock retained after successful commit")
	}
	release()
}

// A creation whose commit fails removes its cache file and frees the lock.
func TestCreateRuleLibraryItemFailedCommitRemovesCache(t *testing.T) {
	r := newRepo(t)
	rule := decodeJSON[model.RuleLibraryItem](t, `{"id":"lib9","name":"L","url":"https://example.test/x","policy":"DIRECT"}`)
	path := must(r.SharedPath(filepath.Join("rules", rules.CacheFilename(decodeJSON[model.RuleConfig](t, `{"id":"lib9","name":"L","url":"https://example.test/x","policy":"DIRECT"}`)))))(t)
	if _, err := r.CreateRuleLibraryRecord(bg, rule, []byte("x")); err == nil {
		t.Fatal("a rule source with a policy was accepted")
	}
	if pathExists(path) {
		t.Fatal("cache of the failed creation kept")
	}
	_, release, acquired := fileLocks.TryLock(bg, resolvePath(path))
	if !acquired {
		t.Fatal("cache lock retained after failed commit")
	}
	release()
}
