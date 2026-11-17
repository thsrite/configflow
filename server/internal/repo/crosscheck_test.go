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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/store"
)

func xcPython(t *testing.T, command, directory, mode string, extra ...string) any {
	t.Helper()
	interpreter, repo := os.Getenv("CONFIGFLOW_PY"), os.Getenv("CONFIGFLOW_REPO")
	if interpreter == "" || repo == "" {
		t.Skip("CONFIGFLOW_PY and CONFIGFLOW_REPO are not set")
	}
	script := must(filepath.Abs("testdata/repo_crosscheck.py"))(t)
	output := filepath.Join(t.TempDir(), command+".json")
	run := exec.Command(interpreter, append([]string{script, command, directory, output, mode}, extra...)...)
	run.Env = append(os.Environ(), "CONFIGFLOW_REPO="+repo, "PYTHONDONTWRITEBYTECODE=1")
	if text, err := run.CombinedOutput(); err != nil {
		t.Fatalf("repo_crosscheck.py %s: %v\n%s", command, err, text)
	}
	return must(jsonx.Parse(must(os.ReadFile(output))(t)))(t)
}

func xcOpen(t *testing.T, directory, mode string) *Repository {
	t.Helper()
	if mode == "factories" {
		return must(OpenDefault(directory))(t)
	}
	return openRepo(t, directory)
}

// xcDump reads the same rows as dump() in repo_crosscheck.py.
func xcDump(t *testing.T, directory string) *jsonx.Object {
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
					row[i] = jsonx.Int(v)
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
	return jsonx.Obj(
		"meta", table("SELECT key, value FROM meta ORDER BY key", 2),
		"sections", table("SELECT scope, owner, position FROM sections ORDER BY scope, owner", 3),
		"fields", table("SELECT scope, owner, field, position, value, revision FROM fields ORDER BY scope, owner, field", 6))
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
		if entry.IsDir() {
			relative += "/"
		}
		entries = append(entries, relative)
		return nil
	}))
	sort.Strings(entries)
	return jsonx.ListOf(entries)
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
	for _, entry := range record.(*jsonx.Object).GetList("result") {
		if xcRetiredLock.MatchString(entry.(string)) {
			if !python {
				t.Fatalf("Go created the retired lock file %s", entry)
			}
			continue
		}
		entries = append(entries, xcOrdinal.ReplaceAllString(xcMask(entry.(string)), "<random>"))
	}
	sort.Strings(entries)
	return jsonx.ListOf(entries)
}

func xcText(t testing.TB, value any) string {
	t.Helper()
	return xcMask(string(must(jsonx.Encode(value, jsonx.Options{}))(t)))
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
		return strings.Split(xcMask(string(must(jsonx.Indent2(value))(t))), "\n")
	}
	a, b := indent(goValue), indent(pyValue)
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			from := max(0, i-6)
			t.Errorf("%s differs at line %d:\n  go: %s\n  py: %s\ncontext (go):\n%s", label, i+1, a[i], b[i],
				strings.Join(a[from:i+1], "\n"))
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

func TestConfigTemplateMatchesBackend(t *testing.T) {
	repo := os.Getenv("CONFIGFLOW_REPO")
	if repo == "" {
		t.Skip("CONFIGFLOW_REPO is not set")
	}
	backend := must(os.ReadFile(filepath.Join(repo, "backend", "config_template.json")))(t)
	if !bytes.Equal(backend, configTemplate) {
		t.Fatal("server/internal/repo/config_template.json differs from backend/config_template.json")
	}
}

func TestCrossCheckFreshInit(t *testing.T) {
	for _, mode := range []string{"factories", "plain"} {
		t.Run(mode, func(t *testing.T) {
			pyDir, goDir := t.TempDir(), t.TempDir()
			python := xcPython(t, "init", pyDir, mode).(*jsonx.Object)
			r := xcOpen(t, goDir, mode)
			xcSame(t, "export", must(r.ExportAll(bg, false))(t), python.Value("export"))
			xcSame(t, "rows", xcDump(t, goDir), python.Value("dump"))
			xcSame(t, "layout", xcLayout(t, goDir), python.Value("layout"))
		})
	}
}

func xcPatch(t testing.TB, value any, path []any, step *jsonx.Object) any {
	t.Helper()
	if len(path) > 1 {
		switch container := value.(type) {
		case *jsonx.Object:
			key := path[0].(string)
			container.Set(key, xcPatch(t, container.Value(key), path[1:], step))
		case []any:
			index := int(must(strconv.ParseInt(jsonx.Str(path[0]), 10, 64))(t))
			container[index] = xcPatch(t, container[index], path[1:], step)
		default:
			t.Fatalf("patch path %v through %T", path, value)
		}
		return value
	}
	switch container := value.(type) {
	case *jsonx.Object:
		key := path[0].(string)
		if replacement, ok := step.Get("set"); ok {
			container.Set(key, jsonx.DeepCopy(replacement))
		} else if item, ok := step.Get("append"); ok {
			container.Set(key, append(container.GetList(key), jsonx.DeepCopy(item)))
		} else if step.GetBool("delete") {
			container.Delete(key)
		}
		return container
	case []any:
		index := int(must(strconv.ParseInt(jsonx.Str(path[0]), 10, 64))(t))
		if replacement, ok := step.Get("set"); ok {
			container[index] = jsonx.DeepCopy(replacement)
		} else if item, ok := step.Get("append"); ok {
			container[index] = append(container[index].([]any), jsonx.DeepCopy(item))
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
		step := item.(*jsonx.Object)
		value = xcPatch(t, value, step.GetList("path"), step)
	}
	return value
}

func xcUpdater(t testing.TB, patch any) Updater {
	return func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		xcApplyPatch(t, current, patch)
		return nil, nil
	}
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
	var helper *HelperError
	if errors.As(err, &helper) {
		return helper.Class
	}
	if errors.Is(err, store.ErrStorage) {
		return "ConfigStorageError"
	}
	return fmt.Sprintf("%T", err)
}

// xcExecute mirrors execute() in repo_crosscheck.py.
func xcExecute(t *testing.T, r *Repository, op *jsonx.Object, variables map[string]any) (any, error) {
	arg := func(key string) *jsonx.Object {
		value, _ := jsonx.AsObject(op.Value(key))
		if value != nil && value.Has("$var") {
			copied := jsonx.DeepCopy(variables[value.GetString("$var")])
			value, _ = xcApplyPatch(t, copied, value.Value("patch")).(*jsonx.Object)
		}
		return value
	}
	withRevision := func(saved *jsonx.Object, revision int64, err error) (any, error) {
		if err != nil {
			return nil, err
		}
		return jsonx.Obj("saved", saved, "revision", jsonx.Int(revision)), nil
	}
	id := op.GetString("id")
	switch op.GetString("op") {
	case "create_profile":
		return r.CreateProfile(bg, arg("metadata"), op.GetString("clone_from"))
	case "clone_profile":
		return r.CloneProfile(bg, op.GetString("source"), arg("metadata"))
	case "update_profile":
		return r.UpdateProfile(bg, id, arg("updates"))
	case "update_profile_fields":
		return withRevision(r.UpdateProfileFields(bg, id, arg("fields"), arg("baseline")))
	case "update_profile_transaction":
		return withRevision(r.UpdateProfileTransaction(bg, id, xcUpdater(t, op.Value("patch"))))
	case "import_profile":
		return r.ImportProfile(bg, id, arg("data"))
	case "update_system_transaction":
		return r.UpdateSystemTransaction(bg, xcUpdater(t, op.Value("patch")))
	case "save_system":
		return r.SaveSystem(bg, arg("snapshot"), arg("baseline"))
	case "update_shared_transaction":
		return r.UpdateSharedTransaction(bg, xcUpdater(t, op.Value("patch")))
	case "save_shared":
		return r.SaveShared(bg, arg("snapshot"), arg("baseline"))
	case "update_shared_and_profiles":
		shared, profiles, err := r.UpdateSharedAndProfilesTransaction(bg, xcUpdater(t, op.Value("shared_patch")),
			func(_ context.Context, _ string, _, after *jsonx.Object) (*jsonx.Object, error) {
				xcApplyPatch(t, after, op.Value("profile_patch"))
				return nil, nil
			}, Profiles(jsonx.Strings(op.GetList("ids"))...))
		if err != nil {
			return nil, err
		}
		return jsonx.Obj("shared", shared, "profiles", profiles), nil
	case "create_rule_library_item":
		return r.CreateRuleLibraryItem(bg, arg("rule"), []byte(op.GetString("content")))
	case "set_default_profile":
		return r.SetDefaultProfile(bg, id)
	case "delete_profile":
		return nil, r.DeleteProfile(bg, id, op.GetString("new_default"))
	case "export_all":
		return r.ExportAll(bg, false)
	case "import_all":
		return r.ImportAll(bg, arg("data"))
	case "reset_all":
		return r.ResetAll(bg)
	case "list_profiles":
		return r.ListProfiles(bg)
	case "get_profile":
		return r.GetProfile(bg, id)
	case "get_shared":
		return r.GetShared(bg)
	case "get_system":
		return r.GetSystem(bg)
	case "get_compat_config":
		var roots ResourceRoots
		if value, ok := jsonx.AsObject(op.Value("roots")); ok {
			roots = ResourceRoots{}
			for kind, ids := range value.All() {
				roots[kind] = nil
				if list, ok := ids.([]any); ok {
					roots[kind] = append([]string{}, jsonx.Strings(list)...)
				}
			}
		}
		return r.GetCompatConfig(bg, id, roots)
	case "get_publish_snapshot":
		config, revision, err := r.GetPublishSnapshot(bg, id, op.GetOr("agents", true) == true)
		if err != nil {
			return nil, err
		}
		return jsonx.Obj("config", config, "revision", jsonx.Int(revision)), nil
	case "get_resource_usage":
		return r.GetResourceUsage(bg, op.GetString("kind"), id)
	case "config_revision":
		revision, err := r.ConfigRevision(bg, id)
		return jsonx.Int(revision), err
	case "default_profile_id":
		return r.DefaultProfileID(bg)
	case "get_profile_agent_bindings":
		return r.GetProfileAgentBindings(bg, id)
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
	if object, ok := value.(*jsonx.Object); ok && object == nil {
		return nil
	}
	return value
}

func xcRun(t *testing.T, directory, mode string, operations []any) []any {
	r := xcOpen(t, directory, mode)
	variables := map[string]any{}
	records := []any{}
	for _, item := range operations {
		op := item.(*jsonx.Object)
		record := jsonx.Obj("op", op.Value("op"))
		if op.GetString("op") == "reopen" {
			r = xcOpen(t, directory, mode)
		}
		result, err := xcExecute(t, r, op, variables)
		if err != nil {
			usages := any(nil)
			if errors.Is(err, ErrInUse) {
				usages = UsagesOf(err)
			}
			message := strings.ReplaceAll(err.Error(), r.DataDir(), "<dir>")
			record.Set("error", jsonx.Obj("class", xcErrorClass(err), "message", message, "usages", usages))
		} else {
			result = xcNil(result)
			if name := op.GetString("save"); name != "" {
				variables[name] = jsonx.DeepCopy(result)
			}
			record.Set("result", result)
		}
		record.Set("dump", xcDump(t, directory))
		records = append(records, record)
	}
	return append(records, jsonx.Obj("op", "layout", "result", xcLayout(t, directory)))
}

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
	operations := must(jsonx.Parse(must(os.ReadFile(script))(t)))(t).([]any)
	python := xcPython(t, "run", pyDir, "plain", script).([]any)
	golang := xcRun(t, goDir, "plain", operations)
	if len(python) != len(golang) {
		t.Fatalf("records: go %d, python %d", len(golang), len(python))
	}
	// The layout holds random directory names in sorted order: compare it
	// as a sorted list of masked names, apart from the cumulative comparison.
	last := len(golang) - 1
	xcSame(t, "layout", xcMaskedLayout(t, golang[last], false), xcMaskedLayout(t, python[last], true))
	golang, python = golang[:last], python[:last]
	// Random values are numbered in order of first appearance over the
	// whole run, so the records are compared cumulatively: the whole run at
	// once, and step by step only to locate a difference.
	if xcText(t, golang) == xcText(t, python) {
		return goDir, pyDir
	}
	for index := range golang {
		g, p := golang[index].(*jsonx.Object), python[index].(*jsonx.Object)
		label := fmt.Sprintf("step %d (%s)", index, g.GetString("op"))
		if g.Has("error") != p.Has("error") {
			t.Fatalf("%s: go %s, python %s", label, xcText(t, g.GetOr("error", g.Value("result"))),
				xcText(t, p.GetOr("error", p.Value("result"))))
		}
		before := t.Failed()
		xcSame(t, label, golang[:index+1], python[:index+1])
		if !before && t.Failed() {
			xcSame(t, label+" outcome", jsonx.Obj("result", g.Value("result"), "error", g.Value("error")),
				jsonx.Obj("result", p.Value("result"), "error", p.Value("error")))
			xcSame(t, label+" rows", g.Value("dump"), p.Value("dump"))
			t.FailNow()
		}
	}
	return goDir, pyDir
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

	// A database written by Go is opened by Python without changes.
	goStore := store.New(filepath.Join(goDir, DatabaseFilename), LockTimeout)
	written := xcFingerprint(t, goStore)
	python := xcPython(t, "fingerprint", goDir, "plain").(*jsonx.Object)
	for _, key := range []string{"before", "opened", "after"} {
		wantEqual(t, python.Value(key), written)
	}
	r := openRepo(t, goDir)
	wantEqual(t, must(r.ExportAll(bg, false))(t), python.Value("export"))

	// A database written by Python is opened by Go without changes.
	pyStore := store.New(filepath.Join(pyDir, DatabaseFilename), LockTimeout)
	before := xcFingerprint(t, pyStore)
	r = openRepo(t, pyDir)
	exported := must(r.ExportAll(bg, false))(t)
	wantEqual(t, xcFingerprint(t, pyStore), before)
	python = xcPython(t, "fingerprint", pyDir, "plain").(*jsonx.Object)
	wantEqual(t, python.Value("before"), before)
	wantEqual(t, exported, python.Value("export"))

	// One more write by each side on the other's database; the other reads it.
	script := filepath.Join(t.TempDir(), "ops.json")
	noError(t, os.WriteFile(script, []byte(`[{"op":"create_profile","metadata":{"id":"frompy","name":"Py"}}]`), 0o600))
	xcPython(t, "run", goDir, "plain", script)
	wantEqual(t, must(openRepo(t, goDir).ProfileMetadata(bg, "frompy"))(t).Value("name"), "Py")
	must(r.CreateProfile(bg, jsonx.Obj("id", "fromgo", "name", "Go"), ""))(t)
	python = xcPython(t, "fingerprint", pyDir, "plain").(*jsonx.Object)
	wantEqual(t, python.Value("opened"), python.Value("before"))
	wantEqual(t, python.Value("export"), must(r.ExportAll(bg, false))(t))
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
		_, err := r.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
			system.Set("agents", append(system.GetList("agents"), jsonx.Obj("id", id, "name", id, "profile_id", "default")))
			return nil, nil
		})
		return err
	}

	// Python holds the lock: a Go write times out, then waits it out.
	script := must(filepath.Abs("testdata/repo_crosscheck.py"))(t)
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
	result := xcPython(t, "try-write", r.DataDir(), "plain").(*jsonx.Object)
	wantEqual(t, result, jsonx.Obj("class", "ProfileRepositoryError", "message", "Timed out waiting for profile repository lock"))
	close(release)
	t.Setenv(LockTimeoutEnv, "20")
	wantEqual(t, xcPython(t, "try-write", r.DataDir(), "plain"), jsonx.Obj("ok", true))
	var ids []string
	for _, agent := range must(r.GetAgents(bg, true))(t) {
		ids = append(ids, agent.(*jsonx.Object).GetString("id"))
	}
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
	before := must(r.ExportAll(bg, false))(t)
	script := must(filepath.Abs("testdata/repo_crosscheck.py"))(t)
	crash := exec.Command(interpreter, script, "crash-write", r.DataDir(), filepath.Join(t.TempDir(), "x.json"), "plain")
	crash.Env = append(os.Environ(), "CONFIGFLOW_REPO="+repo, "PYTHONDONTWRITEBYTECODE=1")
	output, err := crash.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 9 {
		t.Fatalf("crash-write: %v\n%s", err, output)
	}
	reopened := openRepo(t, r.DataDir())
	wantEqual(t, must(reopened.ExportAll(bg, false))(t), before)
	wantEqual(t, must(r.ExportAll(bg, false))(t), before)
	t.Setenv(LockTimeoutEnv, "2")
	_, err = reopened.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{jsonx.Obj("id", "go", "name", "Go", "profile_id", "default")})
		return nil, nil
	})
	noError(t, err)
	noError(t, reopened.Store().Check())
	if len(must(r.GetAgents(bg, true))(t)) != 1 {
		t.Fatal("write after the crash not visible")
	}
}
