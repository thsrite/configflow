package repo

// Cross-check against the real Python ProfileRepository. It needs the
// interpreter and the repository:
//
//	CONFIGFLOW_PY=~/cf-test/.venv/bin/python CONFIGFLOW_REPO=<repo root> go test ./internal/repo/
//
// and is skipped when either variable is missing. testdata/repo_crosscheck.py
// is the Python side; testdata/repo_crosscheck_ops.json is the scripted
// sequence both sides execute.

import (
	"bytes"
	"configflow/internal/model/modeltest"
	"configflow/internal/pathx"
	"configflow/internal/testjson"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo/repotest"
	"configflow/internal/repo/staterows"
	"configflow/internal/store"
)

func xcPython(t *testing.T, command, directory, mode string, extra ...string) any {
	t.Helper()
	interpreter, repo := os.Getenv("CONFIGFLOW_PY"), os.Getenv("CONFIGFLOW_REPO")
	if interpreter == "" || repo == "" {
		t.Skip("CONFIGFLOW_PY and CONFIGFLOW_REPO are not set")
	}
	script := must(filepath.Abs("testdata/repo_crosscheck.py"))(t)
	xcPythonLayout(t, directory)
	output := filepath.Join(t.TempDir(), command+".json")
	run := exec.Command(interpreter, append([]string{script, command, directory, output, mode}, extra...)...)
	run.Env = append(os.Environ(), "CONFIGFLOW_REPO="+repo, "PYTHONDONTWRITEBYTECODE=1")
	if text, err := run.CombinedOutput(); err != nil {
		t.Fatalf("repo_crosscheck.py %s: %v\n%s", command, err, text)
	}
	return parseJSONVal(t, must(os.ReadFile(output))(t))
}

// xcPythonLayout rewrites a Go data directory's database in the version-1
// layout before Python opens it (O17: Python reads version 1 only); Go
// reopens it later and upgrades again. A missing or version-1 database is
// left alone.
func xcPythonLayout(t testing.TB, directory string) {
	t.Helper()
	path := filepath.Join(directory, DatabaseFilename)
	if !pathx.Accessible(path) {
		return
	}
	if o17Version(t, path) >= 2 {
		repotest.DowngradeToV1(t, path)
	}
}

func xcOpen(t *testing.T, directory, mode string) *Repository {
	t.Helper()
	if mode == "factories" {
		return must(OpenDefault(directory))(t)
	}
	return openRepo(t, directory)
}

// xcDump reads the same rows as dump() in repo_crosscheck.py.
func xcDump(t *testing.T, directory string) *testObj {
	t.Helper()
	db := must(sql.Open("sqlite", "file:"+filepath.Join(directory, DatabaseFilename)))(t)
	defer db.Close()
	table := func(statement string, columns int) []any {
		result := []any{}
		rows := must(db.Query(statement))(t)
		defer rows.Close()
		for rows.Next() {
			values := make([]any, columns)
			targets := make([]any, columns)
			for i := range values {
				targets[i] = &values[i]
			}
			noError(t, rows.Scan(targets...))
			row := make([]any, columns)
			for i, value := range values {
				switch v := value.(type) {
				case int64:
					row[i] = json.Number(strconv.FormatInt(v, 10))
				case []byte:
					row[i] = string(v)
				default:
					row[i] = v
				}
			}
			result = append(result, row)
		}
		noError(t, rows.Err())
		return result
	}
	return xcVersionOne(t, db, testObjKV(
		"meta", table("SELECT key, value FROM meta ORDER BY key", 2),
		"sections", table("SELECT scope, owner, position FROM sections ORDER BY scope, owner", 3),
		"fields", table("SELECT scope, owner, field, position, value, revision FROM fields ORDER BY scope, owner, field", 6)))
}

// xcVersionOne shows a storage-version-2 dump as the version-1 rows Python
// keeps (O17, D5): the agent_state rows merged into system.agents and the
// router state rows (last_release) into each profile's routers, the state
// counters left out and storage_version 1.
func xcVersionOne(t *testing.T, db *sql.DB, dump *testObj) *testObj {
	t.Helper()
	var tables int
	noError(t, db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='agent_state'").Scan(&tables))
	if tables == 0 {
		return dump
	}
	var state []store.StateRow
	rows := must(db.Query("SELECT agent_id, member, value FROM agent_state ORDER BY agent_id, member"))(t)
	for rows.Next() {
		var row store.StateRow
		var value string
		noError(t, rows.Scan(&row.Owner, &row.Member, &value))
		if row.Member == "state_restored_at" || row.Member == staterows.RouterStateMemberPrefix+"state_restored_at" {
			continue // Go's restore markers after importing a backup without run records (O17); Python has none
		}
		row.Value = json.RawMessage(value)
		state = append(state, row)
	}
	noError(t, rows.Err())
	rows.Close()
	meta := []any{}
	for _, row := range dump.Value("meta").([]any) {
		values := row.([]any)
		switch values[0] {
		case "state_generation", "state_commit_id":
			continue
		case "storage_version":
			row = []any{values[0], "1"}
		}
		meta = append(meta, row)
	}
	dump.Set("meta", meta)
	for _, row := range dump.Value("fields").([]any) {
		values := row.([]any)
		switch {
		case values[0] == "system" && values[1] == "" && values[2] == "agents":
			values[4] = string(staterows.Merge([]byte(values[4].(string)), state))
		case values[0] == "profile" && values[2] == "routers":
			values[4] = string(staterows.MergeRouters(values[1].(string), []byte(values[4].(string)), state))
		}
	}
	return dump
}

// xcLayout is layout() in repo_crosscheck.py (sorted like pathlib paths).
func xcLayout(t *testing.T, directory string) []any {
	t.Helper()
	var entries []string
	noError(t, filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == directory {
			return err
		}
		relative := must(filepath.Rel(directory, path))(t)
		if relative == BackupsDirName {
			// Go's pre-upgrade backup of a Python-made database (O17).
			return filepath.SkipDir
		}
		if entry.IsDir() {
			relative += "/"
		}
		entries = append(entries, relative)
		return nil
	}))
	sort.Strings(entries)
	return testListOf(entries)
}

var (
	xcTime    = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z|\d{8}T\d{6}Z`)
	xcRandom  = regexp.MustCompile(`[A-Za-z0-9_-]{43}|[0-9a-f]{32}|[0-9a-f]{12}`)
	xcWord    = regexp.MustCompile(`[A-Za-z0-9_-]+`)
	xcHexRun  = regexp.MustCompile(`[0-9a-f]+`)
	xcOrdinal = regexp.MustCompile(`<random\d+>`)
)

// xcMask replaces timestamps by <time> and every distinct random value (rule
// proxy tokens, uuid hex, 12-hex id suffixes) by an ordinal placeholder in
// order of first appearance, so structure and reuse are still compared.
func xcMask(text string) string {
	text = xcTime.ReplaceAllString(text, "<time>")
	seen := map[string]string{}
	return xcWord.ReplaceAllStringFunc(text, func(word string) string {
		mask := func(token string) string {
			if _, ok := seen[token]; !ok {
				seen[token] = "<random" + strconv.Itoa(len(seen)+1) + ">"
			}
			return seen[token]
		}
		if len(word) == 43 {
			return mask(word)
		}
		if replaced := xcHexRun.ReplaceAllStringFunc(word, func(run string) string {
			if len(run) == 32 {
				return mask(run)
			}
			return run
		}); replaced != word {
			return replaced
		}
		// "<prefix>_<12 hex>" ids
		if index := strings.LastIndex(word, "_"); len(word)-index-1 == 12 || (index < 0 && len(word) == 12) {
			suffix := word[index+1:]
			if xcRandom.FindString(suffix) == suffix && strings.ContainsAny(suffix, "0123456789") {
				return word[:index+1] + mask(suffix)
			}
		}
		return word
	})
}

// xcRetiredLock matches the lock files Python creates besides .config.lock:
// per-profile operation locks and per-file "<file>.lock" (profile files and
// shared caches). Go serialises these in process since O3-6a and never
// creates them (O12 ledger), so they are dropped from Python's layout and
// refused in Go's.
var xcRetiredLock = regexp.MustCompile(`^(profiles/\.[^/]+\.operation\.lock|(profiles|shared)/.+[^/]\.lock)$`)

func xcMaskedLayout(t testing.TB, record any, python bool) []any {
	var entries []string
	for _, entry := range record.(*testObj).GetList("result") {
		if xcRetiredLock.MatchString(entry.(string)) {
			if !python {
				t.Fatalf("Go created the retired lock file %s", entry)
			}
			continue
		}
		entries = append(entries, xcOrdinal.ReplaceAllString(xcMask(entry.(string)), "<random>"))
	}
	sort.Strings(entries)
	return testListOf(entries)
}

func xcText(t testing.TB, value any) string {
	t.Helper()
	encoded := must(json.Marshal(xcFiniteJSON(value)))(t)
	var native any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	noError(t, decoder.Decode(&native))
	native = xcNativeRecords(t, native)
	// Plain records (O18 T1a–T1c) leave empty optional members out where
	// Python wrote "", null, {} or []: both sides compare without them.
	native = testjson.WithoutEmptyMembers(native)
	return xcMask(string(must(json.Marshal(native))(t)))
}

// Fresh records may choose native JSON member order and whitespace. Existing
// row byte preservation is independently checked by the raw round-trip audits;
// this cross-language sequence compares logical rows, document _revision values
// and outcomes. Physical row rewrite counters may differ after native formatting.
func xcNativeRecords(t testing.TB, value any) any {
	t.Helper()
	decode := func(text string) any {
		var result any
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.UseNumber()
		if decoder.Decode(&result) != nil {
			return text
		}
		return result
	}
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if key == "message" {
				if text, ok := item.(string); ok && strings.HasPrefix(text, "Cannot read configuration ") {
					if end := strings.Index(text, ": "); end >= 0 {
						item = text[:end+2] + "<native-file-error>"
					}
				}
			}
			if rows, ok := item.([]any); ok && (key == "meta" || key == "fields" || key == "sections") {
				for _, row := range rows {
					values, ok := row.([]any)
					if !ok {
						continue
					}
					switch key {
					case "meta":
						if len(values) == 2 {
							if text, ok := values[1].(string); ok {
								values[1] = decode(text)
							}
						}
					case "fields":
						if len(values) == 6 {
							values[3] = "<native-member-position>"
							values[5] = "<native-field-rewrite-counter>"
							if text, ok := values[4].(string); ok {
								values[4] = decode(text)
							}
						}
					case "sections":
						if len(values) == 3 {
							values[2] = "<native-section-position>"
						}
					}
				}
			}
			if key == "proxy_string" {
				if text, ok := item.(string); ok && json.Valid([]byte(text)) {
					item = decode(text)
				}
			}
			v[key] = xcNativeRecords(t, item)
		}
	case []any:
		for i, item := range v {
			v[i] = xcNativeRecords(t, item)
		}
	}
	return value
}

// xcSame compares two values through their masked JSON text; on a
// difference it reports the first differing line of the indented form.
func xcSame(t *testing.T, label string, goValue, pyValue any) {
	t.Helper()
	left, right := xcText(t, goValue), xcText(t, pyValue)
	if left == right {
		return
	}
	indent := func(value any) []string {
		var pretty bytes.Buffer
		noError(t, json.Indent(&pretty, []byte(xcText(t, value)), "", "  "))
		return strings.Split(pretty.String(), "\n")
	}

	a, b := indent(goValue), indent(pyValue)
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			from := max(0, i-6)
			t.Errorf("%s differs at line %d:\n  go: %s\n  py: %s\ncontext (go):\n%s\ncontext (py):\n%s", label, i+1, a[i], b[i],
				strings.Join(a[from:min(len(a), i+4)], "\n"), strings.Join(b[from:min(len(b), i+4)], "\n"))
			return
		}
	}
	t.Errorf("%s differs in length: go %d lines, py %d lines", label, len(a), len(b))
}

func xcCopyTree(t *testing.T, source, target string) {
	t.Helper()
	noError(t, filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		destination := filepath.Join(target, must(filepath.Rel(source, path))(t))
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	}))
}

func TestCrossCheckFreshInit(t *testing.T) {
	for _, mode := range []string{"factories", "plain"} {
		t.Run(mode, func(t *testing.T) {
			pyDir, goDir := t.TempDir(), t.TempDir()
			python := xcPython(t, "init", pyDir, mode).(*testObj)
			r := xcOpen(t, goDir, mode)
			xcSame(t, "export", parseJSONVal(t, must(r.ExportAllJSON(bg, false))(t)), python.Value("export"))
			xcSame(t, "rows", xcDump(t, goDir), python.Value("dump"))
			xcSame(t, "layout", xcLayout(t, goDir), python.Value("layout"))
		})
	}
}

func xcIndex(t testing.TB, key any) int {
	t.Helper()
	switch k := key.(type) {
	case json.Number:
		n, err := k.Int64()
		noError(t, err)
		return int(n)
	case int:
		return k
	case int64:
		return int(k)
	default:
		n, err := strconv.ParseInt(fmt.Sprint(key), 10, 64)
		noError(t, err)
		return int(n)
	}
}

func xcPatch(t testing.TB, value any, path []any, step *testObj) any {
	t.Helper()
	if len(path) > 1 {
		switch container := value.(type) {
		case *testObj:
			key := path[0].(string)
			container.Set(key, xcPatch(t, container.Value(key), path[1:], step))
		case []any:
			index := xcIndex(t, path[0])
			container[index] = xcPatch(t, container[index], path[1:], step)
		default:
			t.Fatalf("patch path %v through %T", path, value)
		}
		return value
	}
	switch container := value.(type) {
	case *testObj:
		key := path[0].(string)
		if replacement, ok := step.Get("set"); ok {
			container.Set(key, testDeepCopy(replacement))
		} else if item, ok := step.Get("append"); ok {
			container.Set(key, append(container.GetList(key), testDeepCopy(item)))
		} else if step.GetBool("delete") {
			container.Delete(key)
		}
		return container
	case []any:
		index := xcIndex(t, path[0])
		if replacement, ok := step.Get("set"); ok {
			container[index] = testDeepCopy(replacement)
		} else if item, ok := step.Get("append"); ok {
			container[index] = append(container[index].([]any), testDeepCopy(item))
		} else if step.GetBool("delete") {
			return append(container[:index:index], container[index+1:]...)
		}
		return container
	}
	t.Fatalf("patch target %T", value)
	return nil
}

func xcApplyPatch(t testing.TB, value any, patch any) any {
	steps, _ := patch.([]any)
	for _, item := range steps {
		step := item.(*testObj)
		value = xcPatch(t, value, step.GetList("path"), step)
	}
	return value
}

func xcErrorClass(err error) string {
	var failure *Error
	if errors.As(err, &failure) {
		switch failure.Kind {
		case ErrValidation:
			return "ProfileValidationError"
		case ErrNotFound:
			return "ProfileNotFound"
		case ErrExists:
			return "ProfileExists"
		case ErrInUse:
			return "ProfileInUse"
		case ErrNodeDeletionChanged:
			return "NodeDeletionChanged"
		case ErrRuleLibraryCreationUncertain:
			return "RuleLibraryCreationUncertain"
		}
		return "ProfileRepositoryError"
	}
	var missing *MissingError
	if errors.As(err, &missing) {
		return "KeyError"
	}
	if errors.Is(err, store.ErrStorage) {
		return "ConfigStorageError"
	}
	return fmt.Sprintf("%T", err)
}

func xcRawOrNil(t testing.TB, obj *testObj) json.RawMessage {
	if obj == nil {
		return nil
	}
	return mustMarshalJSON(t, obj)
}

func xcObjFromRecord(t testing.TB, record any) *testObj {
	t.Helper()
	raw, err := model.Marshal(record)
	noError(t, err)
	return parseJSONObj(t, raw)
}

func xcValFromJSON(t testing.TB, raw json.RawMessage, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return parseJSONVal(t, raw), nil
}

func xcValFromAny(t testing.TB, val any, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return parseJSONVal(t, mustMarshalJSON(t, val)), nil
}

func xcPyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case json.Number:
		return string(x)
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			if s, ok := item.(string); ok {
				parts[i] = "'" + s + "'"
			} else {
				parts[i] = xcPyStr(item)
			}
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *testObj:
		parts := make([]string, 0, len(x.keys))
		for k, item := range x.All() {
			val := xcPyStr(item)
			if s, ok := item.(string); ok {
				val = "'" + s + "'"
			}
			parts = append(parts, "'"+k+"': "+val)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprint(v)
	}
}

func xcProfileCreation(t testing.TB, metadata *testObj, requireName bool) (ProfileCreation, error) {
	if metadata == nil {
		return ProfileCreation{}, validationError(msgMetadataObject)
	}
	input := ProfileCreation{}
	if value := metadata.Value("id"); testTruthy(value) {
		text, ok := value.(string)
		if !ok {
			return input, validationError("Invalid profile id")
		}
		input.ID = text
	}
	if value := metadata.Value("name"); testTruthy(value) || requireName {
		text, ok := value.(string)
		if !ok {
			return input, validationError(msgProfileNameEmpty)
		}
		input.Name = text
	}
	if value := metadata.Value("description"); testTruthy(value) {
		input.Description = xcPyStr(value)
	}
	return input, nil
}

func xcProfileMetadataUpdate(updates *testObj) (ProfileMetadataUpdate, error) {
	if updates == nil {
		return ProfileMetadataUpdate{}, validationError(msgMetadataFields)
	}
	var input ProfileMetadataUpdate
	for key, value := range updates.All() {
		if key != "name" && key != "description" {
			return ProfileMetadataUpdate{}, validationError(msgMetadataFields)
		}
		text, ok := value.(string)
		if !ok {
			return ProfileMetadataUpdate{}, validationError(msgMetadataStrings)
		}
		if key == "name" {
			input.Name = &text
		} else {
			input.Description = &text
		}
	}
	return input, nil
}

func xcApplyRecordPatch(t testing.TB, record any, keys []string, set func(string, json.RawMessage) error, patch any) error {
	obj := xcObjFromRecord(t, record)
	before := obj.DeepCopy()
	xcApplyPatch(t, obj, patch)
	for _, key := range keys {
		bVal, bHas := before.Get(key)
		aVal, aHas := obj.Get(key)
		if !aHas {
			continue
		}
		if bHas && testCompact(t, bVal) == testCompact(t, aVal) {
			continue
		}
		if err := set(key, mustMarshalJSON(t, aVal)); err != nil {
			return err
		}
	}
	return nil
}

// xcExecute mirrors execute() in repo_crosscheck.py.
func xcExecute(t *testing.T, r *Repository, op *testObj, variables map[string]any) (any, error) {
	arg := func(key string) *testObj {
		value, _ := op.Value(key).(*testObj)
		if value != nil && value.Has("$var") {
			copied := testDeepCopy(variables[value.GetString("$var")])
			value, _ = xcApplyPatch(t, copied, value.Value("patch")).(*testObj)
		}
		return value
	}
	withRevision := func(saved *model.Profile, revision int64, err error) (any, error) {
		if err != nil {
			return nil, err
		}
		return testObjKV("saved", xcObjFromRecord(t, saved), "revision", json.Number(strconv.FormatInt(revision, 10))), nil
	}
	id := op.GetString("id")
	switch op.GetString("op") {
	case "create_profile":
		input, err := xcProfileCreation(t, arg("metadata"), false)
		if err != nil {
			return nil, err
		}
		res, err := r.CreateProfileRecord(bg, input, op.GetString("clone_from"))
		return xcValFromAny(t, res, err)
	case "clone_profile":
		input, err := xcProfileCreation(t, arg("metadata"), false)
		if err != nil {
			return nil, err
		}
		res, err := r.CloneProfileRecord(bg, op.GetString("source"), input)
		return xcValFromAny(t, res, err)
	case "update_profile":
		input, err := xcProfileMetadataUpdate(arg("updates"))
		if err != nil {
			return nil, err
		}
		res, err := r.UpdateProfileMetadataRecord(bg, id, input)
		return xcValFromAny(t, res, err)
	case "update_profile_fields":
		return withRevision(r.SaveProfileFieldsJSON(bg, id, xcRawOrNil(t, arg("fields")), xcRawOrNil(t, arg("baseline"))))
	case "update_profile_transaction":
		return withRevision(r.CommitConfig(bg, id, func(_ context.Context, config *model.Config) error {
			return xcApplyRecordPatch(t, config, ProfileFields, config.Profile.SetJSON, op.Value("patch"))
		}))
	case "import_profile":
		saved, err := r.ImportProfileJSON(bg, id, xcRawOrNil(t, arg("data")))
		if err != nil {
			return nil, err
		}
		return xcObjFromRecord(t, saved), nil
	case "update_system_transaction":
		if err := r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
			// Python's transaction stores what it is given; a section that
			// is an object but holds members of the wrong type is written
			// as stored data (O18 T1c), as if an older version had left it.
			set := func(key string, raw json.RawMessage) error {
				err := system.SetJSON(key, raw)
				var memberType *model.MemberTypeError
				if errors.As(err, &memberType) && jsonKind(raw) == '{' {
					return model.SetStoredMember(system, key, raw)
				}
				return err
			}
			return xcApplyRecordPatch(t, system, systemSections, set, op.Value("patch"))
		}); err != nil {
			return nil, err
		}
		sys, err := r.System(bg)
		if err != nil {
			return nil, err
		}
		return xcObjFromRecord(t, sys), nil
	case "save_system":
		var snap, base *model.System
		if raw := xcRawOrNil(t, arg("snapshot")); raw != nil {
			// A typed snapshot has every member (O18 T1a). Python's partial
			// snapshot leaves absent members as they are, which a Go caller
			// expresses by editing the current record: overlay the given
			// members on it.
			if jsonKind(raw) == '{' {
				current, err := r.System(bg)
				if err != nil {
					return nil, err
				}
				members, err := recordMembers(current)
				if err != nil {
					return nil, err
				}
				for key, value := range viewOfJSON(raw).fields.All() {
					// Python's _deep_merge: an object member is merged
					// into the current one (a plain record writes every
					// always-written member, so replacing it would reset
					// the ones the snapshot left out).
					if existing, present := members.Get(key); present && jsonKind(value) == '{' && jsonKind(existing) == '{' {
						merged := viewOfJSON(existing).fields
						for name, member := range viewOfJSON(value).fields.All() {
							if err := merged.Set(name, member); err != nil {
								return nil, err
							}
						}
						if value, err = merged.MarshalJSON(); err != nil {
							return nil, err
						}
					}
					if err := members.Set(key, value); err != nil {
						return nil, err
					}
				}
				if raw, err = members.MarshalJSON(); err != nil {
					return nil, err
				}
			}
			// Snapshots are input: decoded strictly.
			snap = &model.System{}
			if err := model.DecodeInput("system", raw, snap); err != nil {
				return nil, inputError(err)
			}
		}
		if raw := xcRawOrNil(t, arg("baseline")); raw != nil {
			base = &model.System{}
			if err := model.DecodeInput("system", raw, base); err != nil {
				return nil, inputError(err)
			}
		}
		saved, err := r.SaveSystemRecord(bg, snap, base)
		if err != nil {
			return nil, err
		}
		return xcObjFromRecord(t, saved), nil
	case "update_shared_transaction":
		if err := r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
			return xcApplyRecordPatch(t, shared, SharedFields, shared.SetJSON, op.Value("patch"))
		}); err != nil {
			return nil, err
		}
		shared, err := r.Shared(bg)
		if err != nil {
			return nil, err
		}
		return xcObjFromRecord(t, shared), nil
	case "save_shared":
		var snap, base *model.Shared
		if raw := xcRawOrNil(t, arg("snapshot")); raw != nil {
			snap = &model.Shared{}
			if err := json.Unmarshal(raw, snap); err != nil {
				return nil, err
			}
		}
		if raw := xcRawOrNil(t, arg("baseline")); raw != nil {
			base = &model.Shared{}
			if err := json.Unmarshal(raw, base); err != nil {
				return nil, err
			}
		}
		saved, err := r.SaveSharedRecord(bg, snap, base)
		if err != nil {
			return nil, err
		}
		return xcObjFromRecord(t, saved), nil
	case "update_shared_and_profiles":
		ids := testStrings(op.GetList("ids"))
		shared, profiles, err := r.updateSharedConfigs(bg,
			func(_ context.Context, s *model.Shared) error {
				return xcApplyRecordPatch(t, s, SharedFields, s.SetJSON, op.Value("shared_patch"))
			},
			func(_ context.Context, _ string, _, after *model.Config) error {
				return xcApplyRecordPatch(t, after, ProfileFields, after.Profile.SetJSON, op.Value("profile_patch"))
			},
			func(*model.Shared, []*model.Profile) ([]string, error) { return ids, nil },
		)
		if err != nil {
			return nil, err
		}
		profilesObj := newTestObj()
		for _, p := range profiles {
			profilesObj.Set(p.Key(), xcObjFromRecord(t, p))
		}
		return testObjKV("shared", xcObjFromRecord(t, shared), "profiles", profilesObj), nil
	case "create_rule_library_item":
		raw := xcRawOrNil(t, arg("rule"))
		if raw == nil {
			return nil, validationError(msgResourceObject)
		}
		var item model.RuleLibraryItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		saved, err := r.CreateRuleLibraryRecord(bg, item, []byte(op.GetString("content")))
		if err != nil {
			return nil, err
		}
		return xcObjFromRecord(t, saved), nil
	case "set_default_profile":
		return r.SetDefaultProfile(bg, id)
	case "delete_profile":
		return nil, r.DeleteProfile(bg, id, op.GetString("new_default"))
	case "export_all":
		raw, err := r.ExportAllJSON(bg, false)
		return xcValFromJSON(t, raw, err)
	case "import_all":
		raw, err := r.ImportAllJSON(bg, xcRawOrNil(t, arg("data")))
		return xcValFromJSON(t, raw, err)
	case "reset_all":
		raw, err := r.ResetAllJSON(bg)
		return xcValFromJSON(t, raw, err)
	case "list_profiles":
		res, err := r.ListProfileRecords(bg)
		return xcValFromAny(t, res, err)
	case "get_profile":
		p, err := r.Profile(bg, id)
		if err != nil {
			return nil, err
		}
		return xcObjFromRecord(t, p), nil
	case "get_shared":
		s, err := r.Shared(bg)
		if err != nil {
			return nil, err
		}
		return xcObjFromRecord(t, s), nil
	case "get_system":
		s, err := r.System(bg)
		if err != nil {
			return nil, err
		}
		return xcObjFromRecord(t, s), nil
	case "get_compat_config":
		var roots ResourceRoots
		if value, ok := op.Value("roots").(*testObj); ok {
			roots = ResourceRoots{}
			for kind, ids := range value.All() {
				roots[kind] = nil
				if list, ok := ids.([]any); ok {
					roots[kind] = append([]string{}, testStrings(list)...)
				}
			}
		}
		cfg, err := r.Config(bg, id, roots)
		if err != nil {
			return nil, err
		}
		if cfg, err = xcConfigForPython(t, bg, r, id, cfg); err != nil {
			return nil, err
		}
		return xcObjFromRecord(t, cfg), nil
	case "get_publish_snapshot":
		config, revision, err := r.PublishConfig(bg, id, op.GetOr("agents", true) == true)
		if err != nil {
			return nil, err
		}
		return testObjKV("config", xcObjFromRecord(t, config), "revision", json.Number(strconv.FormatInt(revision, 10))), nil
	case "get_resource_usage":
		res, err := r.ResourceUsage(bg, op.GetString("kind"), id)
		return xcValFromAny(t, res, err)
	case "config_revision":
		revision, err := r.ConfigRevision(bg, id)
		return json.Number(strconv.FormatInt(revision, 10)), err
	case "default_profile_id":
		return r.DefaultProfileID(bg)
	case "get_profile_agent_bindings":
		res, err := r.ProfileAgentBindingsRecord(bg, id)
		return xcValFromAny(t, res, err)
	case "reopen":
		return nil, nil
	}
	if result, err, handled := xcExecuteC04(t, r, op, arg); handled {
		return result, err
	}
	t.Fatalf("unknown operation %s", op.GetString("op"))
	return nil, nil
}

// xcNil turns typed nil results into JSON null.
func xcNil(value any) any {
	if object, ok := value.(*testObj); ok && object == nil {
		return nil
	}
	return value
}

func xcRun(t *testing.T, directory, mode string, operations []any) []any {
	r := xcOpen(t, directory, mode)
	variables := map[string]any{}
	records := []any{}
	for _, item := range operations {
		op := item.(*testObj)
		record := testObjKV("op", op.Value("op"))
		if op.GetString("op") == "reopen" {
			r = xcOpen(t, directory, mode)
		}
		beforeRows := xcFingerprint(t, r.Store())
		result, err := xcExecute(t, r, op, variables)
		if err != nil {
			if (errors.Is(err, ErrValidation) || errors.Is(err, ErrInUse) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrExists)) && !reflect.DeepEqual(beforeRows, xcFingerprint(t, r.Store())) {
				t.Fatal("rejected operation changed stored bytes", op.GetString("op"))
			}
			usages := any(nil)
			if errors.Is(err, ErrInUse) {
				usages = UsagesOf(err)
			}
			message := strings.ReplaceAll(err.Error(), r.DataDir(), "<dir>")
			record.Set("error", testObjKV("class", xcErrorClass(err), "message", message, "usages", usages))
		} else {
			result = xcNil(result)
			if name := op.GetString("save"); name != "" {
				variables[name] = testDeepCopy(result)
			}
			record.Set("result", result)
		}
		record.Set("dump", xcDump(t, directory))
		xcDropRestoreMarker(record)
		records = append(records, record)
	}
	return append(records, testObjKV("op", "layout", "result", xcLayout(t, directory)))
}

// xcDropRestoreMarker removes Go's restore marker (state_restored_at, set
// when a backup without run records is imported, O17) from a record: Python
// has no such member.
func xcDropRestoreMarker(value any) {
	switch v := value.(type) {
	case *testObj:
		if v == nil {
			return
		}
		v.Delete("state_restored_at")
		for _, item := range v.All() {
			xcDropRestoreMarker(item)
		}
	case []any:
		for _, item := range v {
			xcDropRestoreMarker(item)
		}
	case map[string]any:
		delete(v, "state_restored_at")
		for _, item := range v {
			xcDropRestoreMarker(item)
		}
	}
}

// xcPythonCrash are Python's internal exception classes (a 500 there).
var xcPythonCrash = map[string]bool{"AttributeError": true, "TypeError": true, "KeyError": true, "IndexError": true}

// xcOperations runs the scripted sequence on both sides, on copies of one
// data directory created by Python, and returns the two directories.
func xcOperations(t *testing.T) (goDir, pyDir string) {
	t.Helper()
	return xcOperationsFile(t, "testdata/repo_crosscheck_ops.json")
}

// xcOperationsFile is xcOperations with another operations file.
func xcOperationsFile(t *testing.T, file string) (goDir, pyDir string) {
	t.Helper()
	seed := t.TempDir()
	xcPython(t, "init", seed, "plain")
	goDir, pyDir = t.TempDir(), t.TempDir()
	xcCopyTree(t, seed, goDir)
	xcCopyTree(t, seed, pyDir)
	script := must(filepath.Abs(file))(t)
	operations := parseJSONVal(t, must(os.ReadFile(script))(t)).([]any)
	python := xcPython(t, "run", pyDir, "plain", script).([]any)
	golang := xcRun(t, goDir, "plain", operations)
	if len(python) != len(golang) {
		t.Fatalf("records: go %d, python %d", len(golang), len(python))
	}
	// The layout holds random directory names in sorted order: compare it
	// as a sorted list of masked names, apart from the cumulative comparison.
	last := len(golang) - 1
	xcBindNativeCachePath(t, file, goDir, pyDir, python[last].(*testObj))
	goLayout, pyLayout := xcMaskedLayout(t, golang[last], false), xcMaskedLayout(t, python[last], true)
	golang, python = golang[:last], python[:last]
	// Decision (HANDOFF, "Python 500s become proper 4xx"): where Python
	// crashed with an internal exception, Go refuses with a validation or
	// in-use error. Both must leave the database as it was; the record then
	// compares as Python's.
	// Retired file-reader ops (read_profile_json/read_shared_json) still compare
	// their database dumps; their removed-facade results are aligned to Python.
	for index := range golang {
		g, p := golang[index].(*testObj), python[index].(*testObj)
		if name := g.GetString("op"); name == "read_profile_json" || name == "read_shared_json" {
			g.Delete("result")
			g.Delete("error")
			if v, ok := p.Get("result"); ok {
				g.Set("result", v)
			}
			if v, ok := p.Get("error"); ok {
				g.Set("error", v)
			}
			continue
		}
		pyClass, goClass := p.GetObject("error").GetString("class"), g.GetObject("error").GetString("class")
		// O18 T1a strict input: a string where a list belongs is "X 必须是列表";
		// Python iterated the string's characters and reported the first one
		// as an item without an ID. Both refuse without writing.
		if pyClass == "ProfileValidationError" && goClass == pyClass {
			pyMessage, goMessage := p.GetObject("error").GetString("message"), g.GetObject("error").GetString("message")
			if label, ok := strings.CutSuffix(pyMessage, " 中每一项必须有 ID"); ok && goMessage == label+" 必须是列表" {
				t.Logf("step %d (%s): Python iterated a string: %s; Go: %s", index, g.GetString("op"), pyMessage, goMessage)
				g.Set("error", p.Value("error"))
				continue
			}
		}
		if !xcPythonCrash[pyClass] || (goClass != "ProfileValidationError" && goClass != "ProfileInUse") {
			continue
		}
		if index > 0 && (xcText(t, g.Value("dump")) != xcText(t, golang[index-1].(*testObj).Value("dump")) ||
			xcText(t, p.Value("dump")) != xcText(t, python[index-1].(*testObj).Value("dump"))) {
			t.Fatalf("step %d: a refused operation wrote", index)
		}
		t.Logf("step %d (%s): Python %s, Go %s: %s", index, g.GetString("op"), pyClass, goClass,
			g.GetObject("error").GetString("message"))
		g.Set("error", p.Value("error"))
	}
	// O18 T1b (accepted): stored Agents read leniently and drop what their
	// record does not declare; Python's records show them as Go stores them.
	if changed := xcStoredAgents(t, python); changed > 0 {
		t.Logf("O18 T1b: %d stored Agent values in the Python records normalized to the Go codec", changed)
	}
	// O18 T1c (accepted): members no plain record declares are dropped on
	// read; Python's records leave out the ones Go's records do not have.
	for i := range python {
		if i < len(golang) {
			python[i] = xcAlignPlain(t, python[i], golang[i])
		}
	}
	// Random values are numbered in order of first appearance over the
	// whole run, so the records are compared cumulatively: the whole run at
	// once, and step by step only to locate a difference.
	if xcText(t, golang) == xcText(t, python) {
		xcSame(t, "layout", goLayout, pyLayout)
		return goDir, pyDir
	}
	index := sort.Search(len(golang), func(i int) bool {
		return xcText(t, golang[:i+1]) != xcText(t, python[:i+1])
	})
	if index < len(golang) {
		g, p := golang[index].(*testObj), python[index].(*testObj)
		label := fmt.Sprintf("step %d (%s)", index, g.GetString("op"))
		if g.Has("error") != p.Has("error") {
			t.Fatalf("%s: go %s, python %s", label, xcText(t, g.GetOr("error", g.Value("result"))),
				xcText(t, p.GetOr("error", p.Value("result"))))
		}
		xcSame(t, label, golang[:index+1], python[:index+1])
		xcSame(t, label+" outcome", testObjKV("result", g.Value("result"), "error", g.Value("error")),
			testObjKV("result", p.Value("result"), "error", p.Value("error")))
		xcSame(t, label+" rows", g.Value("dump"), p.Value("dump"))
		t.FailNow()
	}
	xcSame(t, "layout", goLayout, pyLayout)
	return goDir, pyDir
}

// xcStoredAgents rewrites every stored Agent in Python's records (the agents
// of a system record, the agents row text and get_agents results) as Go
// stores it: O18 T1b
// drops undeclared members, nulls, empty text and values of another type on
// load (accepted). Only what the Go codec cannot represent is removed, so a
// difference in what Go writes still shows. It returns how many Agents
// changed.
func xcStoredAgents(t *testing.T, v any) int {
	t.Helper()
	_, changed := xcStoredAgentsIn(t, v, false)
	// get_agents returns the stored Agent list itself.
	if records, ok := v.([]any); ok {
		for _, record := range records {
			if record, ok := record.(*testObj); ok && record.GetString("op") == "get_agents" {
				if next, n := xcStoredAgentsIn(t, record.Value("result"), true); n > 0 {
					record.Set("result", next)
					changed += n
				}
			}
		}
	}
	return changed
}

func xcStoredAgentsIn(t *testing.T, v any, agents bool) (any, int) {
	t.Helper()
	if agents {
		list, ok := v.([]any)
		if !ok {
			return v, 0
		}
		changed := 0
		for i, item := range list {
			// History members a read left out are marked "<not read>" on both
			// sides; they are kept as they are.
			unread := map[string]any{}
			if object, ok := item.(*testObj); ok {
				object = object.DeepCopy()
				for key, member := range object.All() {
					if text, ok := member.(string); ok && text == "<not read>" {
						unread[key] = member
					}
				}
				for key := range unread {
					object.Delete(key)
				}
				item = object
			}
			before := mustMarshalJSON(t, item)
			var stored model.Agent
			if json.Unmarshal(before, &stored) != nil {
				continue
			}
			after, err := model.Marshal(stored)
			if err != nil {
				t.Fatal(err)
			}
			var x, y any
			noError(t, json.Unmarshal(before, &x))
			noError(t, json.Unmarshal(after, &y))
			if !reflect.DeepEqual(x, y) {
				next := parseJSONVal(t, after)
				if object, ok := next.(*testObj); ok {
					for key, member := range unread {
						object.Set(key, member)
					}
				}
				list[i] = next
				changed++
			}
		}
		return list, changed
	}
	changed := 0
	switch value := v.(type) {
	case *testObj:
		// Stored Agents are the agents of a system record (binding views
		// also have an "agents" list).
		system := value.Has("system_config")
		for key, member := range value.All() {
			next, n := xcStoredAgentsIn(t, member, system && key == "agents")
			if n > 0 {
				value.Set(key, next)
				changed += n
			}
		}
	case map[string]any:
		_, system := value["system_config"]
		for key, member := range value {
			next, n := xcStoredAgentsIn(t, member, system && key == "agents")
			if n > 0 {
				value[key] = next
				changed += n
			}
		}
	case []any:
		// A fields row: scope, owner, field, position, value text, counter.
		if len(value) == 6 && value[2] == "agents" {
			if text, ok := value[4].(string); ok && json.Valid([]byte(text)) {
				decoded := parseJSONVal(t, []byte(text))
				if next, n := xcStoredAgentsIn(t, decoded, true); n > 0 {
					value[4] = string(mustMarshalJSON(t, next))
					return value, n
				}
				return value, 0
			}
		}
		for i, item := range value {
			next, n := xcStoredAgentsIn(t, item, false)
			if n > 0 {
				value[i] = next
				changed += n
			}
		}
	}
	return v, changed
}

func TestCrossCheckOperations(t *testing.T) {
	xcOperations(t)
}

func xcFingerprint(t *testing.T, s *store.Store) []any {
	t.Helper()
	connection := must(sql.Open("sqlite", s.Path()))(t)
	defer connection.Close()
	var tables []any
	for _, query := range []string{"SELECT * FROM meta ORDER BY key", "SELECT * FROM sections ORDER BY scope, owner", "SELECT * FROM fields ORDER BY scope, owner, field"} {
		rows := must(connection.Query(query))(t)
		columns := must(rows.Columns())(t)
		var values []any
		for rows.Next() {
			row := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range row {
				pointers[i] = &row[i]
			}
			noError(t, rows.Scan(pointers...))
			values = append(values, row)
		}
		noError(t, rows.Err())
		rows.Close()
		tables = append(tables, values)
	}
	return tables
}

func TestCrossCheckInterchange(t *testing.T) {
	goDir, pyDir := xcOperations(t)

	// A database written by Go is opened by Python without changes (in the
	// version-1 layout Python reads, O17).
	xcPythonLayout(t, goDir)
	goStore := store.New(filepath.Join(goDir, DatabaseFilename), LockTimeout)
	written := xcFingerprint(t, goStore)
	python := xcPython(t, "fingerprint", goDir, "plain").(*testObj)
	for _, key := range []string{"before", "opened", "after"} {
		wantJSONEqual(t, python.Value(key), written)
	}
	r := openRepo(t, goDir)
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), python.Value("export"))

	// A database written by Python is opened by Go, which upgrades it to
	// storage version 2 after a backup that equals the Python file (O17);
	// Python reads the same document from it.
	pyStore := store.New(filepath.Join(pyDir, DatabaseFilename), LockTimeout)
	before := xcFingerprint(t, pyStore)
	r = openRepo(t, pyDir)
	exported := must(r.ExportAllJSON(bg, false))(t)
	backups := must(filepath.Glob(filepath.Join(pyDir, BackupsDirName, "configflow-v1-*.sqlite3")))(t)
	if len(backups) != 1 {
		t.Fatalf("backups %v", backups)
	}
	wantJSONEqual(t, xcFingerprint(t, store.New(backups[0], LockTimeout)), before)
	python = xcPython(t, "fingerprint", pyDir, "plain").(*testObj)
	wantJSONEqual(t, exported, python.Value("export"))

	// One more write by each side on the other's database; the other reads it.
	script := filepath.Join(t.TempDir(), "ops.json")
	noError(t, os.WriteFile(script, []byte(`[{"op":"create_profile","metadata":{"id":"frompy","name":"Py"}}]`), 0o600))
	xcPython(t, "run", goDir, "plain", script)
	if got := must(openRepo(t, goDir).ProfileMetadataRecord(bg, "frompy"))(t).Name; got != "Py" {
		t.Fatalf("frompy name %q", got)
	}
	r = openRepo(t, pyDir) // upgrades again after Python opened it
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "fromgo", Name: "Go"}, ""))(t)
	python = xcPython(t, "fingerprint", pyDir, "plain").(*testObj)
	wantJSONEqual(t, python.Value("opened"), python.Value("before"))
	wantJSONEqual(t, python.Value("export"), must(r.ExportAllJSON(bg, false))(t))
}

// Go and Python exclude each other through flock on .config.lock: a writer
// on either side waits for the other until CONFIGFLOW_LOCK_TIMEOUT_SECONDS
// and then fails with Python's message (C03 review).
func TestCrossCheckLockExcludesPythonAndGo(t *testing.T) {
	interpreter, repo := os.Getenv("CONFIGFLOW_PY"), os.Getenv("CONFIGFLOW_REPO")
	if interpreter == "" || repo == "" {
		t.Skip("CONFIGFLOW_PY and CONFIGFLOW_REPO are not set")
	}
	r := newRepo(t)
	addAgent := func(id string) error {
		return r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
			system.Agents = append(system.Agents, model.Agent{ID: id, Name: id, ProfileID: "default"})
			return nil
		})
	}

	// Python holds the lock: a Go write times out, then waits it out.
	script := must(filepath.Abs("testdata/repo_crosscheck.py"))(t)
	xcPythonLayout(t, r.DataDir())
	holder := exec.Command(interpreter, script, "hold", r.DataDir(), filepath.Join(t.TempDir(), "hold.json"), "plain", "1.5")
	holder.Env = append(os.Environ(), "CONFIGFLOW_REPO="+repo, "PYTHONDONTWRITEBYTECODE=1")
	stdout := must(holder.StdoutPipe())(t)
	noError(t, holder.Start())
	line := make([]byte, 7)
	if _, err := io.ReadFull(stdout, line); err != nil || string(line) != "locked\n" {
		t.Fatalf("holder: %q %v", line, err)
	}
	t.Setenv(LockTimeoutEnv, "0.3")
	wantError(t, addAgent("go-1"), ErrRepository, "Timed out waiting for profile repository lock")
	t.Setenv(LockTimeoutEnv, "20")
	noError(t, addAgent("go-2"))
	noError(t, holder.Wait())

	// Go holds the lock: Python's write times out, then succeeds once released.
	held, release := make(chan struct{}), make(chan struct{})
	go func() {
		_ = withLock(bg, r.lockFile, func(context.Context) error { close(held); <-release; return nil })
	}()
	<-held
	t.Setenv(LockTimeoutEnv, "0.3")
	result := xcPython(t, "try-write", r.DataDir(), "plain").(*testObj)
	wantJSONEqual(t, result, testObjKV("class", "ProfileRepositoryError", "message", "Timed out waiting for profile repository lock"))
	close(release)
	t.Setenv(LockTimeoutEnv, "20")
	wantJSONEqual(t, xcPython(t, "try-write", r.DataDir(), "plain"), testObjKV("ok", true))
	var ids []string
	noError(t, r.ViewAgents(bg, func(agents []model.Agent) error {
		for _, agent := range agents {
			ids = append(ids, agent.ID)
		}
		return nil
	}))
	if strings.Join(ids, ",") != "go-2,py-agent" {
		t.Fatalf("agents %v", ids)
	}
}

// A Python writer that dies before COMMIT (statements executed, transaction
// open, .config.lock held) leaves the database readable and writable by Go
// with the previous data (C03 review).
func TestCrossCheckPythonWriterKilledBeforeCommit(t *testing.T) {
	interpreter, repo := os.Getenv("CONFIGFLOW_PY"), os.Getenv("CONFIGFLOW_REPO")
	if interpreter == "" || repo == "" {
		t.Skip("CONFIGFLOW_PY and CONFIGFLOW_REPO are not set")
	}
	r := newRepo(t)
	before := must(r.ExportAllJSON(bg, false))(t)
	script := must(filepath.Abs("testdata/repo_crosscheck.py"))(t)
	xcPythonLayout(t, r.DataDir())
	crash := exec.Command(interpreter, script, "crash-write", r.DataDir(), filepath.Join(t.TempDir(), "x.json"), "plain")
	crash.Env = append(os.Environ(), "CONFIGFLOW_REPO="+repo, "PYTHONDONTWRITEBYTECODE=1")
	output, err := crash.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 9 {
		t.Fatalf("crash-write: %v\n%s", err, output)
	}
	reopened := openRepo(t, r.DataDir())
	wantJSONEqual(t, must(reopened.ExportAllJSON(bg, false))(t), before)
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
	t.Setenv(LockTimeoutEnv, "2")
	noError(t, reopened.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{{ID: "go", Name: "Go", ProfileID: "default"}}
		return nil
	}))
	noError(t, reopened.Store().Check())
	if len(must(r.System(bg))(t).Agents) != 1 {
		t.Fatal("write after the crash not visible")
	}
}

// xcFiniteJSON converts *testObj to map[string]any for canonical JSON encoding.
func xcFiniteJSON(value any) any {
	switch v := value.(type) {
	case *testObj:
		if v == nil {
			return nil
		}
		out := map[string]any{}
		for key, item := range v.All() {
			out[key] = xcFiniteJSON(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = xcFiniteJSON(item)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			out[key] = xcFiniteJSON(item)
		}
		return out
	}
	return value
}

// xcAlignPlain is modeltest.AlignResponse on one step record: Python's
// members that Go's record lacks and no plain record declares are left out.
func xcAlignPlain(t *testing.T, python, golang any) any {
	t.Helper()
	decode := func(value any) any {
		var out any
		decoder := json.NewDecoder(bytes.NewReader(mustMarshalJSON(t, value)))
		decoder.UseNumber() // keep big integers exact
		noError(t, decoder.Decode(&out))
		return out
	}
	decodeText := func(text string) (any, bool) {
		var out any
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.UseNumber()
		return out, decoder.Decode(&out) == nil
	}
	p, g := decode(python), decode(golang)
	// Stored rows ([scope, owner, field, position, value text, counter])
	// carry their value as JSON text: align it with Go's row of the same
	// scope, owner and field.
	goRows := map[string]any{}
	var collect func(any)
	collect = func(v any) {
		switch x := v.(type) {
		case []any:
			if len(x) == 6 {
				if text, ok := x[4].(string); ok {
					if value, ok := decodeText(text); ok {
						goRows[fmt.Sprint(x[0], "|", x[1], "|", x[2])] = value
					}
				}
			}
			for _, item := range x {
				collect(item)
			}
		case map[string]any:
			for _, item := range x {
				collect(item)
			}
		}
	}
	collect(g)
	var rewrite func(any)
	rewrite = func(v any) {
		switch x := v.(type) {
		case []any:
			if len(x) == 6 {
				if text, ok := x[4].(string); ok {
					if value, ok := decodeText(text); ok {
						if other, found := goRows[fmt.Sprint(x[0], "|", x[1], "|", x[2])]; found {
							aligned := modeltest.AlignResponse(value, other)
							xcAlignSwitches(aligned, other)
							x[4] = string(mustMarshalJSON(t, aligned))
						}
					}
				}
			}
			for _, item := range x {
				rewrite(item)
			}
		case map[string]any:
			for _, item := range x {
				rewrite(item)
			}
		}
	}
	rewrite(p)
	aligned := modeltest.AlignResponse(p, g)
	xcRestoreRandomKeys(aligned, p, g)
	xcAlignSwitches(aligned, g)
	return parseJSONVal(t, mustMarshalJSON(t, aligned))
}

// xcRestoreRandomKeys puts back the members AlignResponse left out whose
// name holds a random value (a generated profile ID): Go's record has the
// same member under another random name, so it is user data, not an
// undeclared member. It is aligned with Go's member of the same masked name.
func xcRestoreRandomKeys(aligned, python, golang any) {
	a, ok := aligned.(map[string]any)
	p, pok := python.(map[string]any)
	if !ok || !pok {
		if al, ok := aligned.([]any); ok {
			pl, _ := python.([]any)
			gl, _ := golang.([]any)
			for i := range al {
				if i < len(pl) {
					var other any
					if i < len(gl) {
						other = gl[i]
					}
					xcRestoreRandomKeys(al[i], pl[i], other)
				}
			}
		}
		return
	}
	g, _ := golang.(map[string]any)
	for key, value := range p {
		if kept, present := a[key]; present {
			xcRestoreRandomKeys(kept, value, g[key])
			continue
		}
		masked := xcMask(key)
		if masked == key {
			continue
		}
		var other any
		for goKey, goValue := range g {
			if _, inPython := p[goKey]; !inPython && xcMask(goKey) == masked {
				other = goValue
				break
			}
		}
		restored := modeltest.AlignResponse(value, other)
		xcRestoreRandomKeys(restored, value, other)
		a[key] = restored
	}
}

// xcAlignSwitches: a stored switch of another JSON type than a boolean reads
// by its truth value, or as off for a switch that starts device writes (O18
// T1b/T1c rulings); where Go's record holds a boolean and Python's the stored
// value, Python's side takes Go's reading.
func xcAlignSwitches(python, golang any) {
	switch p := python.(type) {
	case map[string]any:
		g, _ := golang.(map[string]any)
		for key, item := range p {
			other, present := g[key]
			if flag, isBool := other.(bool); present && isBool {
				if _, pythonBool := item.(bool); !pythonBool && item != nil {
					p[key] = flag
					continue
				}
			}
			xcAlignSwitches(item, other)
		}
	case []any:
		g, _ := golang.([]any)
		for i, item := range p {
			if i < len(g) {
				xcAlignSwitches(item, g[i])
			}
		}
	}
}
