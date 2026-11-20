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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"configflow/internal/model"
	"configflow/internal/store"
	"configflow/internal/testjson"
)

var (
	refOnce sync.Once
	refData *testObj
	refErr  error
)

func refLoad(t *testing.T) *testObj {
	t.Helper()
	refOnce.Do(func() {
		var data []byte
		if data, refErr = os.ReadFile("testdata/repo_reference.json"); refErr == nil {
			refData = parseJSONObj(t, data)
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
		step := item.(*testObj)
		path := step.GetList("path")
		target := value
		for _, key := range path[:len(path)-1] {
			target = refIndex(t, target, key)
		}
		last := path[len(path)-1]
		switch {
		case step.Has("set"):
			refAssign(t, target, last, testDeepCopy(step.Value("set")))
		case step.Has("append"):
			list := refIndex(t, target, last).([]any)
			refAssign(t, target, last, append(list, testDeepCopy(step.Value("append"))))
		case testTruthy(step.Value("delete")):
			target.(*testObj).Delete(last.(string))
		}
	}
}

func refIndex(t *testing.T, target, key any) any {
	if object, ok := target.(*testObj); ok {
		return object.Value(key.(string))
	}
	index, _ := key.(json.Number).Int64()
	return target.([]any)[index]
}

func refAssign(t *testing.T, target, key, value any) {
	if object, ok := target.(*testObj); ok {
		object.Set(key.(string), value)
		return
	}
	index, _ := key.(json.Number).Int64()
	target.([]any)[index] = value
}

var refKinds = map[string]error{
	"ProfileRepositoryError": ErrRepository, "ProfileValidationError": ErrValidation, "ProfileInUse": ErrInUse,
	"ProfileNotFound": ErrNotFound, "ProfileExists": ErrExists, "NodeDeletionChanged": ErrNodeDeletionChanged,
}

func refText(t *testing.T, value any) string {
	t.Helper()
	return testCompact(t, value)
}

func refRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	switch v := value.(type) {
	case json.RawMessage:
		return v
	case []byte:
		return v
	default:
		return json.RawMessage(testCompact(t, value))
	}
}

// refOutcome compares a Go result/error with a Python outcome record.
func refOutcome(t *testing.T, label string, record *testObj, got any, err error) {
	t.Helper()
	pyError, failed := record.Get("error")
	if !failed {
		if err != nil {
			t.Errorf("%s: Python succeeded, Go failed: %v", label, err)
			return
		}
		if record.Has("result") && got != nil {
			left, right := refRaw(t, got), refRaw(t, record.Value("result"))
			if !testjson.Equal(right, left) {
				t.Errorf("%s: result differs\n  go: %s\n  py: %s", label, left, right)
			}
		}
		return
	}
	failure := pyError.(*testObj)
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
		left, right := refRaw(t, repoError.Usages), refRaw(t, want)
		if !testjson.Equal(right, left) {
			t.Errorf("%s: usages differ\n  go: %s\n  py: %s", label, left, right)
		}
	}
}

func refBase(t *testing.T) *testObj {
	return refLoad(t).GetObject("base").DeepCopy()
}

func decodeDocObj(t *testing.T, obj *testObj) (*model.Document, error) {
	t.Helper()
	if obj == nil {
		return nil, &store.Error{Kind: store.ErrDocument, Message: msgStoreDocumentObject}
	}
	encodeFields := func(o *testObj) []store.Field {
		out := make([]store.Field, 0, len(o.keys))
		for k, v := range o.All() {
			out = append(out, store.Field{Name: k, Value: mustMarshalJSON(t, v)})
		}
		return out
	}
	rows := &store.Document{Order: obj.Keys()}
	root := newTestObj()
	for name, value := range obj.All() {
		switch name {
		case "system":
			sub, ok := value.(*testObj)
			if !ok {
				return nil, validationError(msgSectionMissing + name)
			}
			rows.System = &store.Section{Fields: encodeFields(sub)}
		case "shared":
			sub, ok := value.(*testObj)
			if !ok {
				return nil, validationError(msgSectionMissing + name)
			}
			rows.Shared = &store.Section{Fields: encodeFields(sub)}
		case "profiles":
			profiles, ok := value.(*testObj)
			if !ok {
				return nil, validationError(msgSectionMissing + "profiles")
			}
			for id, v := range profiles.All() {
				p, ok := v.(*testObj)
				if !ok {
					return nil, validationError(msgProfileIDMismatch)
				}
				rows.Profiles = append(rows.Profiles, store.ProfileRows{ID: id, Fields: encodeFields(p)})
			}
		default:
			root.Set(name, value)
		}
	}
	rows.Root = encodeFields(root)
	doc, _, err := decodeModelRows(rows)
	if err != nil {
		return nil, &store.Error{Kind: store.ErrDocument, Message: err.Error(), Cause: err}
	}
	return doc, nil
}

func TestReferenceThreeWayMerge(t *testing.T) {
	t.Parallel()
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
		got, _, err := MergeJSON(refRaw(t, values[0]), refRaw(t, values[1]), refRaw(t, values[2]))
		noError(t, err)
		want := refText(t, values[3])
		if answer, changed := goAnswers[index]; changed {
			want = answer
			if refText(t, values[3]) == want {
				t.Fatalf("case %d: listed as changed but Python agrees", index)
			}
		}
		if string(got) != want {
			t.Errorf("case %d %s: go %s want %s", index, refText(t, values[:3]), got, want)
		}
	}
}

func TestReferenceDeepMerge(t *testing.T) {
	t.Parallel()
	for index, item := range refLoad(t).GetList("deep_merge") {
		values := item.([]any)
		got, err := mergeObjectsJSON(refRaw(t, values[0]), refRaw(t, values[1]))
		noError(t, err)
		want := refRaw(t, values[2])
		if !testjson.Equal(want, got) {
			t.Errorf("case %d: go %s py %s", index, got, want)
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
		f, err := values[1].(json.Number).Float64()
		noError(t, err)
		want := secondsDuration(f)
		if got := LockTimeout(); got != want {
			t.Errorf("%v: go %v, py %v", values[0], got, want)
		}
	}
	os.Unsetenv(LockTimeoutEnv)
}

func TestReferenceLegacyDefaults(t *testing.T) {
	want := refLoad(t).GetObject("legacy_defaults")
	plain := newRepo(t)
	factories := must(OpenDefault(t.TempDir()))(t)
	plainGot := must(plain.profileDefaultsJSON())(t)
	if !testjson.Equal(refRaw(t, want.Value("plain")), plainGot) {
		t.Errorf("plain defaults differ\n  go: %s\n  py: %s", plainGot, refRaw(t, want.Value("plain")))
	}
	factGot := must(factories.profileDefaultsJSON())(t)
	if !testjson.Equal(refRaw(t, want.Value("factories")), factGot) {
		t.Errorf("factory defaults differ\n  go: %s\n  py: %s", factGot, refRaw(t, want.Value("factories")))
	}
}

func TestReferenceValidateDocument(t *testing.T) {
	t.Parallel()
	// R8 (design n7-n8 2.3): an integral JSON number in any notation is a
	// stored integer, so Go accepts the revision 1.0 that Python refused.
	acceptedByGo := map[string]bool{"revision float": true}
	// A name that is not a string appears in messages as its JSON text
	// (Python printed its repr); one representative per kind of value.
	goMessages := map[string]string{
		"agent name none":       "Agent null 绑定的配置不存在",
		"agent name dict":       `Agent {"a":[1,"x"]} 绑定的配置不存在`,
		"agent name bool":       "Agent true 绑定的配置不存在",
		"aggregation name none": "聚合 null 引用了不存在的资源：gone",
		"aggregation name list": `聚合 ["a",1] 的 nodes 只能包含资源 ID，不能设置资源覆盖`,
	}
	for _, item := range refLoad(t).GetList("validate") {
		record := item.(*testObj)
		document := refBase(t)
		refPatch(t, document, record.GetList("patch"))
		label := record.GetString("label")
		typed, err := decodeDocObj(t, document)
		if err == nil {
			err = validateDocument(typed)
		}
		if nativeReferenceScalar(t, record, err) {
			continue
		}
		if acceptedByGo[label] {
			if !record.Has("error") {
				t.Fatalf("%s: Python accepted it too", label)
			}
			noError(t, err)
			continue
		}
		if message, ok := goMessages[label]; ok {
			if err == nil || err.Error() != message || !errors.Is(err, ErrValidation) {
				t.Errorf("%s: go %v", label, err)
			}
			continue
		}
		refOutcome(t, label, record, nil, err)
	}
}

func TestReferenceCollectResourceRefs(t *testing.T) {
	t.Parallel()
	for _, item := range refLoad(t).GetList("collect_refs") {
		record := item.(*testObj)
		document := refBase(t)
		refPatch(t, document, record.GetList("patch"))
		var roots ResourceRoots
		if object, ok := record.Value("roots").(*testObj); ok {
			roots = ResourceRoots{}
			for kind, value := range object.All() {
				if value == nil {
					roots[kind] = nil
				} else {
					roots[kind] = append([]string{}, testStrings(value.([]any))...)
				}
			}
		}
		result, err := func() (any, error) {
			typed, err := decodeDocObj(t, document)
			if err != nil {
				return nil, err
			}
			profile := profileByKey(typed, DefaultProfileID)
			view, err := viewOf(profile)
			if err != nil {
				return nil, err
			}
			groups, ok := view.get("proxy_groups")
			if !ok {
				groups = json.RawMessage("[]")
			}
			if err := checkGroupReferenceShapes(groups); err != nil {
				return nil, err
			}
			selected, err := selectResources(profile, catalogsOf(&typed.Shared), roots)
			if err != nil {
				return nil, err
			}
			out := newTestObj()
			for _, kind := range ResourceFields {
				keys := append([]string{}, selected[kind].keys...)
				sort.Strings(keys)
				out.Set(kind, testListOf(keys))
			}
			return out, nil
		}()
		refOutcome(t, record.GetString("label"), record, result, err)
	}
}

func TestReferencePruneRuleDNS(t *testing.T) {
	t.Parallel()
	for index, item := range refLoad(t).GetList("prune_rule_dns") {
		values := item.([]any)
		var record model.Profile
		noError(t, json.Unmarshal(refRaw(t, values[0]), &record))
		noError(t, pruneProfileDNS(&record))
		got := must(model.Marshal(&record))(t)
		want := refRaw(t, values[1])
		if !testjson.Equal(want, json.RawMessage(got)) {
			t.Errorf("case %d: go %s py %s", index, got, want)
		}
	}
}

func TestReferenceResourceUsage(t *testing.T) {
	t.Parallel()
	// Usages read names by type (R5): a null name shows the id, where
	// Python passed the stored null on.
	goAnswers := map[int]string{
		6: `[{"kind":"aggregation","id":"g1","name":"g1"},{"kind":"profile","id":"default","name":"默认配置","profile_id":"default","profile_name":"默认配置"}]`,
	}
	for index, item := range refLoad(t).GetList("resource_usage") {
		record := item.(*testObj)
		document := refBase(t)
		refPatch(t, document, record.GetList("patch"))
		typed := must(decodeDocObj(t, document))(t)
		label := record.GetString("kind") + "/" + record.GetString("id")
		usages, err := resourceUsages(typed, record.GetString("kind"), record.GetString("id"))
		if answer, changed := goAnswers[index]; changed {
			noError(t, err)
			got := refRaw(t, usages)
			if !testjson.Equal(json.RawMessage(answer), got) || testjson.Equal(refRaw(t, record.Value("result")), got) {
				t.Errorf("%s: go %s", label, got)
			}
			continue
		}
		refOutcome(t, label, record, usages, err)
	}
}

func TestReferenceRouterCommitGuard(t *testing.T) {
	r := newRepo(t)
	base := refBase(t)
	routers := func(items any) *testObj {
		document := base.DeepCopy()
		document.GetObject("profiles").GetObject("default").Set("routers", testObjKV("items", testDeepCopy(items)))
		return document
	}
	for _, item := range refLoad(t).GetList("router_guard") {
		record := item.(*testObj)
		label := record.GetString("label")
		refuse := map[routerKey]bool{}
		for _, key := range record.GetList("refuse") {
			pair := testStrings(key.([]any))
			refuse[routerKey{pair[0], pair[1]}] = true
		}
		locked := map[routerKey]bool{}
		for _, key := range record.GetList("locked") {
			pair := testStrings(key.([]any))
			locked[routerKey{pair[0], pair[1]}] = true
		}
		var policy *routerKey
		if pair := testStrings(record.GetList("policy")); len(pair) == 2 {
			policy = &routerKey{pair[0], pair[1]}
		}
		calls := []any{}
		held := 0
		previousHooks := SetHooks(Hooks{RecordHooks: RecordHooks{JobLockRecord: func(_ context.Context, profileID, routerID string, _ model.Router) (func(), bool) {
			calls = append(calls, []any{profileID, routerID})
			if refuse[routerKey{profileID, routerID}] {
				return nil, false
			}
			held++
			return func() { held-- }, true
		}}})
		previous := must(decodeDocObj(t, routers(record.Value("old"))))(t)
		oldRecords, err := routerRecordsIn(previous)
		var release func()
		if err == nil {
			var newRecords routerRecordSet
			if newRecords, err = routerRecordsIn(must(decodeDocObj(t, routers(record.Value("new"))))(t)); err == nil {
				release, err = r.routerGuard(bg, previous, oldRecords, newRecords, locked, policy)
			}
		}
		if release != nil {
			release()
		}
		SetHooks(previousHooks)
		refOutcome(t, label, record, nil, err)
		wantCalls := refText(t, record.Value("calls"))
		// Stored values compare by JSON type: true and 1 differ, so the guard
		// takes the publication lock where Python's == did not (fail-closed).
		if label == "verify_tls change" {
			wantCalls = `[["default","rt1"]]`
		}
		if left, right := refText(t, calls), wantCalls; left != right {
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
		record := item.(*testObj)
		counter = 0
		var sourceRaw json.RawMessage
		if record.GetString("label") == "initial config" {
			sourceRaw = must(r.initialSettingsJSON())(t)
		} else {
			sourceRaw = refRaw(t, record.Value("source"))
		}
		typed, err := r.convertImportJSON(sourceRaw, nil)
		// Python crashed (unhashable dict/list values used as keys); Go
		// matches values by their JSON text and converts these backups.
		if label := record.GetString("label"); label == "subscription name dict" || label == "rule value list" {
			if err != nil || typed == nil {
				t.Errorf("%s: %v", label, err)
			}
			continue
		}
		var document *testObj
		var got any
		if typed != nil {
			raw, merr := model.Marshal(typed)
			noError(t, merr)
			document = parseJSONObj(t, raw)
			got = document
		}
		outcome := record.DeepCopy()
		if err == nil && document != nil {
			if expected, ok := record.Value("result").(*testObj); ok {
				outcome.Set("result", normalizeImportReplay(t, document, expected))
			}
		}
		refOutcome(t, record.GetString("label"), outcome, got, err)
	}
}

func TestReferenceFileIsCurrent(t *testing.T) {
	t.Parallel()
	// The generator's own sanity check: the base document validates in Go too.
	typed := must(decodeDocObj(t, refBase(t)))(t)
	noError(t, validateDocument(typed))
}

// normalizeImportReplay removes incidental entropy and embedded JSON formatting
// from frozen import answers. Catalog order and all references remain compared.
func normalizeImportReplay(t *testing.T, got, want *testObj) *testObj {
	t.Helper()
	expected := want.DeepCopy()
	replacements := map[string]string{}
	pair := func(left, right []any) {
		if len(left) != len(right) {
			return
		}
		for i, item := range left {
			a, aok := item.(*testObj)
			b, bok := right[i].(*testObj)
			if !aok || !bok {
				continue
			}
			actual, previous := a.GetString("id"), b.GetString("id")
			if actual == previous {
				continue
			}
			x, y := strings.LastIndex(actual, "_"), strings.LastIndex(previous, "_")
			if x < 0 || y < 0 || actual[:x] != previous[:y] || len(actual[x+1:]) != 12 || len(previous[y+1:]) != 12 {
				continue
			}
			if _, err := strconv.ParseUint(actual[x+1:], 16, 64); err != nil {
				continue
			}
			if _, err := strconv.ParseUint(previous[y+1:], 16, 64); err != nil {
				continue
			}
			replacements[previous] = actual
		}
	}
	for _, kind := range SharedFields {
		pair(got.GetObject("shared").GetList(kind), expected.GetObject("shared").GetList(kind))
	}
	for id, value := range got.GetObject("profiles").All() {
		profile, _ := value.(*testObj)
		pair(profile.GetList("rule_configs"), expected.GetObject("profiles").GetObject(id).GetList("rule_configs"))
	}
	var rewrite func(any) any
	rewrite = func(value any) any {
		switch item := value.(type) {
		case string:
			if next, found := replacements[item]; found {
				return next
			}
			return item
		case []any:
			for i, value := range item {
				item[i] = rewrite(value)
			}
		case *testObj:
			for key, value := range item.All() {
				if key == "proxy_string" {
					if text, ok := value.(string); ok {
						if json.Valid([]byte(text)) {
							parsed := parseJSONVal(t, []byte(text))
							value = testCompact(t, rewrite(parsed))
						}
					}
				}
				item.Set(key, rewrite(value))
			}
		}
		return value
	}
	rewrite(expected)
	return expected
}
