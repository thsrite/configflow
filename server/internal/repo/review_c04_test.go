package repo

// Regression tests of the C04 review (public API, config.py). Expectations
// marked "Python" come from runs of the Python code; the corpus is
// testdata/repo_c04_reference.json (repo_c04_reference.py).

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/rules"
)

func c04Reference(t *testing.T, section string) []any {
	t.Helper()
	data := must(os.ReadFile("testdata/repo_c04_reference.json"))(t)
	return must(jsonx.Parse(data))(t).(*jsonx.Object).GetList(section)
}

// _read_json (read_profile_json, read_shared_json) decodes in text mode:
// CPython's UnicodeDecodeError texts and universal newlines (error
// positions after "\r\n" -> "\n").
func TestReferenceReadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "case.json")
	for _, item := range c04Reference(t, "read_json") {
		row := item.(*jsonx.Object)
		data := must(hex.DecodeString(row.GetString("hex")))(t)
		noError(t, os.WriteFile(path, data, 0o600))
		value, err := readJSON(path)
		got := jsonx.NewObject()
		if err != nil {
			got.Set("class", xcErrorClass(err))
			got.Set("message", err.Error())
		} else {
			got.Set("value", value)
		}
		want := row.GetObject("outcome")
		if string(must(jsonx.Compact(got))(t)) != string(must(jsonx.Compact(want))(t)) {
			t.Errorf("%s: got %s, Python %s", row.GetString("hex"), must(jsonx.Compact(got))(t), must(jsonx.Compact(want))(t))
		}
	}
}

func c04RouterProfile(t *testing.T) (*Repository, *jsonx.Object) {
	t.Helper()
	r := newRepo(t)
	router := jsonx.Obj("id", "rt1", "name", "R", "type", "routeros", "endpoint", "192.0.2.1",
		"username", "u", "password", "p", "verify_tls", false)
	_, _, err := r.SaveProfile(bg, "default", jsonx.Obj("routers", jsonx.Obj("items", []any{router.DeepCopy()})), nil)
	noError(t, err)
	return r, router
}

// save_router_settings / save_router_publication with a non-string expected
// id: Python finds no record (ProfileInUse), raises TypeError for an
// unhashable id and KeyError without one.
func TestSaveRouterWithNonStringExpectedID(t *testing.T) {
	r, router := c04RouterProfile(t)
	withID := func(id any) *jsonx.Object {
		expected := router.DeepCopy()
		expected.Set("id", id)
		return expected
	}
	wantError(t, r.SaveRouterSettings(bg, "default", withID(jsonx.Int(5)), nil), ErrInUse, "路由器设置已变化，请刷新后重试")
	wantError(t, r.SaveRouterPublication(bg, "default", withID(jsonx.Float(1.5)), RouterPublication{}), ErrInUse,
		"路由器连接已变化，无法保存发布回读记录")
	for _, id := range []any{jsonx.Obj("a", 1), []any{"x"}} {
		err := r.SaveRouterSettings(bg, "default", withID(id), nil)
		if !isHelperClass(err, ClassTypeError) || err.Error() != "unhashable type: '"+pyTypeName(id)+"'" {
			t.Fatalf("settings with id %v: %v", id, err)
		}
		err = r.SaveRouterPublication(bg, "default", withID(id), RouterPublication{})
		if !isHelperClass(err, ClassTypeError) {
			t.Fatalf("publication with id %v: %v", id, err)
		}
	}
	missing := router.DeepCopy()
	missing.Delete("id")
	if err := r.SaveRouterSettings(bg, "default", missing, nil); !isHelperClass(err, ClassKeyError) || err.Error() != "'id'" {
		t.Fatalf("missing id: %v", err)
	}
	// A non-string id on a profile that does not exist: the profile is looked up first.
	wantError(t, r.SaveRouterSettings(bg, "ghost", withID(jsonx.Int(5)), nil), ErrNotFound, "ghost")
	wantEqual(t, must(r.GetProfile(bg, "default"))(t).GetObject("routers").GetList("items")[0], router)
}

// import_client_profile: a non-object "profile" result fails on .items()
// like Python (AttributeError text).
func TestImportClientProfileNonObjectProfileResult(t *testing.T) {
	r := newRepo(t)
	_, err := r.ImportClientProfile(bg, jsonx.Obj("id", "c", "name", "C"),
		func(context.Context, *jsonx.Object) (*jsonx.Object, error) {
			return jsonx.Obj("profile", []any{"mihomo"}), nil
		})
	if !isHelperClass(err, ClassAttributeError) || err.Error() != "'list' object has no attribute 'items'" {
		t.Fatalf("got %v", err)
	}
}

// save_config_with_publish_guard resolves the profile before save_profile
// refuses a missing config: an unknown profile is ProfileNotFound (Python).
func TestSaveConfigWithPublishGuardResolvesBeforeRefusingNil(t *testing.T) {
	r := newRepo(t)
	ctx := WithScope(WithRepository(bg, r))
	_, err := SaveConfigWithPublishGuard(ctx, nil, "mihomo", "ghost")
	wantError(t, err, ErrNotFound, "ghost")
	_, err = SaveConfigWithPublishGuard(ctx, nil, "mihomo", "default")
	wantError(t, err, ErrValidation, "Profile fields and baseline must be objects")
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
	must(r.CreateProfile(bg, jsonx.Obj("id", "work", "name", "W"), ""))(t)
	ctx := WithScope(WithRepository(bg, r))
	SetRequest(ctx, RequestInfo{QueryProfile: c04Text("work")})
	wantEqual(t, c04Resolve(ctx, nil, nil), []any{"work"})
	wantEqual(t, c04Resolve(ctx, c04Text("\x00"), nil), []any{"ProfileValidationError", "Invalid profile id"})
	wantEqual(t, c04Resolve(ctx, c04Text("default"), nil), []any{"default"})
	wantEqual(t, c04Resolve(ctx, nil, nil), []any{"work"})

	ctx = WithScope(WithRepository(bg, r))
	SetRequest(ctx, RequestInfo{})
	wantEqual(t, c04Resolve(ctx, nil, nil), []any{"default"})
	wantEqual(t, c04Resolve(ctx, c04Text("\x00"), nil), []any{"ProfileValidationError", "Invalid profile id"})
	wantEqual(t, c04Resolve(ctx, nil, c04Text("work")), []any{"work"})
	wantEqual(t, c04Resolve(ctx, c04Text(""), nil), []any{"ProfileValidationError", "Invalid profile id"})
}

// A commit between taking the memo and storing a result (another request
// deleting the profile meanwhile) leaves the result untrusted: Python
// stores into the memo it took, which the next lookup discards.
func TestResolveProfileResultValidatedBeforeACommitIsNotTrusted(t *testing.T) {
	r := newRepo(t)
	must(r.CreateProfile(bg, jsonx.Obj("id", "work", "name", "W"), ""))(t)
	ctx := WithScope(WithRepository(bg, r))
	SetRequest(ctx, RequestInfo{})
	scope := ScopeOf(ctx)
	scope.mu.Lock()
	memo := scope.resolvedProfiles(ctx, r)
	scope.mu.Unlock()
	noError(t, r.DeleteProfile(bg, "work", ""))
	scope.remember(memo, resolvedKey{explicit: true, id: "work"}, "work")
	wantEqual(t, c04Resolve(ctx, c04Text("work"), nil), []any{"ProfileNotFound", "'work'"})
}

// create_rule_library_item keeps the cache lock through the commit and the
// clean-up (Python: ExitStack around the transaction): the commit hook,
// which runs after .config.lock is released, still finds it held.
func TestCreateRuleLibraryItemHoldsCacheLockThroughCommit(t *testing.T) {
	r := newRepo(t)
	_, err := r.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetObject("backup").Set("auto_backup", true)
		return nil, nil
	})
	noError(t, err)
	rule := jsonx.Obj("id", "lib9", "name", "L", "source_type", "content")
	path := must(r.SharedPath(filepath.Join("rules", rules.RuleCacheFilename(rule))))(t)
	checked := false
	defer SetHooks(SetHooks(Hooks{ConfigurationCommitted: func(*Repository, *jsonx.Object, *jsonx.Object) {
		handle := must(os.OpenFile(path+".lock", os.O_RDWR, 0))(t)
		defer handle.Close()
		if err := syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
			t.Errorf("cache lock not held after the commit: %v", err)
		}
		checked = true
	}}))
	must(r.CreateRuleLibraryItem(bg, rule, []byte("DOMAIN,a\n")))(t)
	if !checked {
		t.Fatal("commit hook not called")
	}
	handle := must(os.OpenFile(path+".lock", os.O_RDWR, 0))(t)
	defer handle.Close()
	noError(t, syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
}

// A creation whose commit fails removes its cache file and frees the lock.
func TestCreateRuleLibraryItemFailedCommitRemovesCache(t *testing.T) {
	r := newRepo(t)
	rule := jsonx.Obj("id", "lib9", "name", "L", "url", "https://example.test/x", "policy", "DIRECT")
	path := must(r.SharedPath(filepath.Join("rules", rules.RuleCacheFilename(rule))))(t)
	if _, err := r.CreateRuleLibraryItem(bg, rule, []byte("x")); err == nil {
		t.Fatal("a rule source with a policy was accepted")
	}
	if pathExists(path) {
		t.Fatal("cache of the failed creation kept")
	}
	handle := must(os.OpenFile(path+".lock", os.O_RDWR, 0))(t)
	defer handle.Close()
	noError(t, syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
}
