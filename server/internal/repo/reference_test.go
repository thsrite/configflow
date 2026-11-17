package repo

// Reference results of config_repository.py part A, produced by the real
// Python functions (testdata/repo_reference.py → testdata/repo_reference.json,
// C03 review). Regenerate on the test server:
//
//	rt --back server/internal/repo/testdata/repo_reference.json -- bash -lc \
//	  'CONFIGFLOW_REPO=$(pwd) ~/cf-test/.venv/bin/python server/internal/repo/testdata/repo_reference.py \
//	   server/internal/repo/testdata/repo_reference.json'
//
// A Python exception of the ProfileRepositoryError family must come back as
// the same class with the same message and usages. Other Python exceptions
// (TypeError, AttributeError, KeyError: a 500 in Flask) only require Go to
// fail; the Go error is logged.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"

	"configflow/internal/jsonx"
)

var (
	refOnce sync.Once
	refData *jsonx.Object
	refErr  error
)

func refLoad(t *testing.T) *jsonx.Object {
	t.Helper()
	refOnce.Do(func() {
		var data []byte
		if data, refErr = os.ReadFile("testdata/repo_reference.json"); refErr == nil {
			var value any
			if value, refErr = jsonx.Parse(data); refErr == nil {
				refData = value.(*jsonx.Object)
			}
		}
	})
	if refErr != nil {
		t.Fatal(refErr)
	}
	return refData
}

// refPatch is apply_patch of repo_reference.py.
func refPatch(t *testing.T, value any, patch []any) {
	t.Helper()
	for _, item := range patch {
		step := item.(*jsonx.Object)
		path := step.GetList("path")
		target := value
		for _, key := range path[:len(path)-1] {
			target = refIndex(t, target, key)
		}
		last := path[len(path)-1]
		switch {
		case step.Has("set"):
			refAssign(t, target, last, jsonx.DeepCopy(step.Value("set")))
		case step.Has("append"):
			list := refIndex(t, target, last).([]any)
			refAssign(t, target, last, append(list, jsonx.DeepCopy(step.Value("append"))))
		case jsonx.Truthy(step.Value("delete")):
			target.(*jsonx.Object).Delete(last.(string))
		}
	}
}

func refIndex(t *testing.T, target, key any) any {
	if object, ok := target.(*jsonx.Object); ok {
		return object.Value(key.(string))
	}
	index, _ := key.(jsonx.Number).Int64()
	return target.([]any)[index]
}

func refAssign(t *testing.T, target, key, value any) {
	if object, ok := target.(*jsonx.Object); ok {
		object.Set(key.(string), value)
		return
	}
	index, _ := key.(jsonx.Number).Int64()
	target.([]any)[index] = value
}

var refKinds = map[string]error{
	"ProfileRepositoryError": ErrRepository, "ProfileValidationError": ErrValidation, "ProfileInUse": ErrInUse,
	"ProfileNotFound": ErrNotFound, "ProfileExists": ErrExists, "NodeDeletionChanged": ErrNodeDeletionChanged,
}

func refText(t *testing.T, value any) string {
	t.Helper()
	return string(must(jsonx.Compact(value))(t))
}

// refOutcome compares a Go result/error with a Python outcome record.
func refOutcome(t *testing.T, label string, record *jsonx.Object, got any, err error) {
	t.Helper()
	pyError, failed := record.Get("error")
	if !failed {
		if err != nil {
			t.Errorf("%s: Python succeeded, Go failed: %v", label, err)
			return
		}
		if record.Has("result") && got != nil {
			if left, right := refText(t, got), refText(t, record.Value("result")); left != right {
				t.Errorf("%s: result differs\n  go: %s\n  py: %s", label, left, right)
			}
		}
		return
	}
	failure := pyError.(*jsonx.Object)
	class := failure.GetString("class")
	if err == nil {
		t.Errorf("%s: Python raised %s(%s), Go succeeded", label, class, failure.GetString("message"))
		return
	}
	kind, known := refKinds[class]
	if !known {
		t.Logf("%s: Python crash %s(%s); Go: %T %v", label, class, failure.GetString("message"), err, err)
		return
	}
	var repoError *Error
	if !errors.As(err, &repoError) || repoError.Kind != kind {
		t.Errorf("%s: Python %s, Go %T %v", label, class, err, err)
		return
	}
	if err.Error() != failure.GetString("message") {
		t.Errorf("%s: message differs\n  go: %s\n  py: %s", label, err.Error(), failure.GetString("message"))
	}
	if usages := failure.Value("usages"); usages != nil || kind == ErrInUse {
		want := usages
		if want == nil {
			want = []any{}
		}
		if left, right := refText(t, repoError.Usages), refText(t, want); left != right {
			t.Errorf("%s: usages differ\n  go: %s\n  py: %s", label, left, right)
		}
	}
}

func refBase(t *testing.T) *jsonx.Object {
	return refLoad(t).GetObject("base").DeepCopy()
}

func TestReferenceThreeWayMerge(t *testing.T) {
	// Python compared with ==, so 1, 1.0 and True were one value. The Go merge
	// compares JSON values by type (yamlx.Equal); these Python answers change.
	goAnswers := map[int]string{
		6:  `{"a":true}`,
		7:  `{"a":1.0}`,
		16: `[{"id":1,"v":"x"}]`,
	}
	cases := refLoad(t).GetList("three_way")
	if len(cases) != 30 {
		t.Fatalf("reference corpus has %d cases", len(cases))
	}
	for index, item := range cases {
		values := item.([]any)
		got, err := threeWayMerge(jsonx.DeepCopy(values[0]), jsonx.DeepCopy(values[1]), jsonx.DeepCopy(values[2]))
		noError(t, err)
		want := refText(t, values[3])
		if answer, changed := goAnswers[index]; changed {
			want = refText(t, must(jsonx.ParseString(answer))(t))
			if want == refText(t, values[3]) {
				t.Fatalf("case %d: listed as changed but Python agrees", index)
			}
		}
		if left := refText(t, got); left != want {
			t.Errorf("case %d %s: go %s want %s", index, refText(t, values[:3]), left, want)
		}
	}
}

func TestReferenceDeepMerge(t *testing.T) {
	for index, item := range refLoad(t).GetList("deep_merge") {
		values := item.([]any)
		got := DeepMerge(values[0].(*jsonx.Object), values[1].(*jsonx.Object))
		if left, right := refText(t, got), refText(t, values[2]); left != right {
			t.Errorf("case %d: go %s py %s", index, left, right)
		}
	}
}

func TestReferenceLockTimeout(t *testing.T) {
	for _, item := range refLoad(t).GetList("lock_timeout") {
		values := item.([]any)
		if values[0] == nil {
			os.Unsetenv(LockTimeoutEnv)
		} else {
			t.Setenv(LockTimeoutEnv, values[0].(string))
		}
		want := secondsDuration(values[1].(jsonx.Number).Float64())
		if got := LockTimeout(); got != want {
			t.Errorf("%s: go %v, py %v", jsonx.Repr(values[0]), got, want)
		}
	}
	os.Unsetenv(LockTimeoutEnv)
}

func TestReferenceLegacyDefaults(t *testing.T) {
	want := refLoad(t).GetObject("legacy_defaults")
	plain := newRepo(t)
	factories := must(OpenDefault(t.TempDir()))(t)
	if left, right := refText(t, plain.legacyDefaults()), refText(t, want.Value("plain")); left != right {
		t.Errorf("plain defaults differ\n  go: %s\n  py: %s", left, right)
	}
	if left, right := refText(t, factories.legacyDefaults()), refText(t, want.Value("factories")); left != right {
		t.Errorf("factory defaults differ\n  go: %s\n  py: %s", left, right)
	}
}

func TestReferenceValidateDocument(t *testing.T) {
	// R8 (design n7-n8 2.3): an integral JSON number in any notation is a
	// stored integer, so Go accepts the revision 1.0 that Python refused.
	acceptedByGo := map[string]bool{"revision float": true}
	for _, item := range refLoad(t).GetList("validate") {
		record := item.(*jsonx.Object)
		document := refBase(t)
		refPatch(t, document, record.GetList("patch"))
		label := record.GetString("label")
		if acceptedByGo[label] {
			if !record.Has("error") {
				t.Fatalf("%s: Python accepted it too", label)
			}
			noError(t, ValidateDocument(document))
			continue
		}
		refOutcome(t, label, record, nil, ValidateDocument(document))
	}
}

func TestReferenceCollectResourceRefs(t *testing.T) {
	for _, item := range refLoad(t).GetList("collect_refs") {
		record := item.(*jsonx.Object)
		document := refBase(t)
		refPatch(t, document, record.GetList("patch"))
		var roots ResourceRoots
		if object, ok := record.Value("roots").(*jsonx.Object); ok {
			roots = ResourceRoots{}
			for kind, value := range object.All() {
				if value == nil {
					roots[kind] = nil
				} else {
					roots[kind] = append([]string{}, jsonx.Strings(value.([]any))...)
				}
			}
		}
		result, err := func() (any, error) {
			catalogs, err := resourceCatalogs(document.GetObject("shared"), ResourceFields)
			if err != nil {
				return nil, err
			}
			selected, err := collectResourceRefs(document.GetObject("profiles").GetObject("default"), catalogs, roots)
			if err != nil {
				return nil, err
			}
			out := jsonx.NewObject()
			for _, kind := range ResourceFields {
				keys := append([]string{}, selected[kind].keys...)
				sort.Strings(keys)
				out.Set(kind, jsonx.ListOf(keys))
			}
			return out, nil
		}()
		refOutcome(t, record.GetString("label"), record, result, err)
	}
}

func TestReferencePruneRuleDNS(t *testing.T) {
	for index, item := range refLoad(t).GetList("prune_rule_dns") {
		values := item.([]any)
		profile := values[0].(*jsonx.Object).DeepCopy()
		noError(t, pruneRuleDNS(profile))
		if left, right := refText(t, profile), refText(t, values[1]); left != right {
			t.Errorf("case %d: go %s py %s", index, left, right)
		}
	}
}

func TestReferenceResourceUsage(t *testing.T) {
	for _, item := range refLoad(t).GetList("resource_usage") {
		record := item.(*jsonx.Object)
		document := refBase(t)
		refPatch(t, document, record.GetList("patch"))
		usages, err := resourceUsage(document, record.GetString("kind"), record.GetString("id"))
		refOutcome(t, record.GetString("kind")+"/"+record.GetString("id"), record, usages, err)
	}
}

func TestReferenceRouterCommitGuard(t *testing.T) {
	r := newRepo(t)
	base := refBase(t)
	routers := func(items any) *jsonx.Object {
		document := base.DeepCopy()
		document.GetObject("profiles").GetObject("default").Set("routers", jsonx.Obj("items", jsonx.DeepCopy(items)))
		return document
	}
	for _, item := range refLoad(t).GetList("router_guard") {
		record := item.(*jsonx.Object)
		label := record.GetString("label")
		refuse := map[routerKey]bool{}
		for _, key := range record.GetList("refuse") {
			pair := jsonx.Strings(key.([]any))
			refuse[routerKey{pair[0], pair[1]}] = true
		}
		locked := map[routerKey]bool{}
		for _, key := range record.GetList("locked") {
			pair := jsonx.Strings(key.([]any))
			locked[routerKey{pair[0], pair[1]}] = true
		}
		var policy *routerKey
		if pair := jsonx.Strings(record.GetList("policy")); len(pair) == 2 {
			policy = &routerKey{pair[0], pair[1]}
		}
		calls := []any{}
		held := 0
		previousHooks := SetHooks(Hooks{JobLock: func(_ context.Context, profileID, routerID string, router *jsonx.Object) (func(), bool) {
			calls = append(calls, []any{profileID, routerID})
			if refuse[routerKey{profileID, routerID}] {
				return nil, false
			}
			held++
			return func() { held-- }, true
		}})
		release, err := r.routerCommitGuard(bg, routers(record.Value("old")), routers(record.Value("new")), locked, policy)
		if release != nil {
			release()
		}
		SetHooks(previousHooks)
		refOutcome(t, label, record, nil, err)
		if left, right := refText(t, calls), refText(t, record.Value("calls")); left != right {
			t.Errorf("%s: job lock calls differ\n  go: %s\n  py: %s", label, left, right)
		}
		if held != 0 {
			t.Errorf("%s: %d job locks not released", label, held)
		}
	}
}

func TestReferenceConvertImport(t *testing.T) {
	r := must(OpenDefault(t.TempDir()))(t)
	counter := 0
	defer func(hex func(int) string, now func() string) { randomHex, nowISO = hex, now }(randomHex, nowISO)
	randomHex = func(length int) string {
		counter++
		text := fmt.Sprintf("%012x", counter)
		return (text + text + text)[:length]
	}
	nowISO = func() string { return "2026-01-02T03:04:05.000006Z" }
	for _, item := range refLoad(t).GetList("convert_import") {
		record := item.(*jsonx.Object)
		counter = 0
		source, _ := jsonx.DeepCopy(record.Value("source")).(*jsonx.Object)
		if record.GetString("label") == "initial config" {
			source = r.initialConfig()
		}
		document, err := r.convertImport(source, nil)
		var got any
		if document != nil {
			got = document
		}
		refOutcome(t, record.GetString("label"), record, got, err)
	}
}

func TestReferenceFileIsCurrent(t *testing.T) {
	// The generator's own sanity check: the base document validates in Go too.
	noError(t, ValidateDocument(refBase(t)))
}
