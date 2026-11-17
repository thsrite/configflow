package store

// Cross-check against the real Python store. It needs the interpreter and
// the repository:
//
//	CONFIGFLOW_PY=~/cf-test/.venv/bin/python CONFIGFLOW_REPO=<repo root> go test ./internal/store/
//
// and is skipped when either variable is missing. testdata/crosscheck.py is
// the Python side.

import (
	"bufio"
	"database/sql"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

// python runs testdata/crosscheck.py and returns the JSON it wrote.
func python(t *testing.T, command, target string) *jsonx.Object {
	t.Helper()
	interpreter, repo := os.Getenv("CONFIGFLOW_PY"), os.Getenv("CONFIGFLOW_REPO")
	if interpreter == "" || repo == "" {
		t.Skip("CONFIGFLOW_PY and CONFIGFLOW_REPO are not set")
	}
	script := must(filepath.Abs("testdata/crosscheck.py"))(t)
	output := filepath.Join(t.TempDir(), command+".json")
	run := exec.Command(interpreter, script, command, target, output)
	run.Env = append(os.Environ(), "CONFIGFLOW_REPO="+repo, "PYTHONDONTWRITEBYTECODE=1")
	if text, err := run.CombinedOutput(); err != nil {
		t.Fatalf("crosscheck.py %s: %v\n%s", command, err, text)
	}
	value := must(jsonx.Parse(must(os.ReadFile(output))(t)))(t)
	return value.(*jsonx.Object)
}

// goDump reads the same tables and pragmas as dump() in crosscheck.py.
func goDump(t *testing.T, path string) *jsonx.Object {
	t.Helper()
	connection := must(connect(path, false, time.Second))(t)
	defer connection.close()
	table := func(statement string, columns int) []any {
		var result []any
		err := connection.query(statement, nil, func(row *sql.Rows) error {
			values := make([]any, columns)
			targets := make([]any, columns)
			for i := range values {
				targets[i] = &values[i]
			}
			if err := row.Scan(targets...); err != nil {
				return err
			}
			result = append(result, values)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	pragmas := jsonx.NewObject()
	for _, name := range []string{"application_id", "journal_mode", "user_version", "encoding", "auto_vacuum"} {
		pragmas.Set(name, table("PRAGMA "+name, 1)[0].([]any)[0])
	}
	return jsonx.Obj(
		"meta", table("SELECT key, value FROM meta ORDER BY key", 2),
		"sections", table("SELECT scope, owner, position FROM sections ORDER BY scope, owner", 3),
		"fields", table("SELECT scope, owner, field, position, value, revision FROM fields ORDER BY scope, owner, field", 6),
		"schema", table("SELECT type, name, tbl_name, sql FROM sqlite_master ORDER BY name", 4),
		"pragmas", pragmas,
	)
}

// sameTables compares the row dumps; with masked set, the values that are
// random or a timestamp (store_id, commit_id, created_at) are left out.
func sameTables(t *testing.T, what string, got, want *jsonx.Object, masked bool) {
	t.Helper()
	meta := func(dump *jsonx.Object) string {
		var rows []any
		for _, row := range dump.GetList("meta") {
			key := row.([]any)[0]
			if masked && (key == "store_id" || key == "commit_id" || key == "created_at") {
				row = []any{key, "<masked>"}
			}
			rows = append(rows, row)
		}
		return compact(t, rows)
	}
	if meta(got) != meta(want) {
		t.Errorf("%s: meta differs\n  go %s\n  py %s", what, meta(got), meta(want))
	}
	for _, table := range []string{"sections", "fields", "schema", "pragmas"} {
		if a, b := compact(t, got.Value(table)), compact(t, want.Value(table)); a != b {
			t.Errorf("%s: %s differs\n  go %s\n  py %s", what, table, a, b)
		}
	}
}

func copyTo(t *testing.T, source, target string) string {
	t.Helper()
	input := must(os.Open(source))(t)
	defer input.Close()
	output := must(os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600))(t)
	defer output.Close()
	if _, err := io.Copy(output, input); err != nil {
		t.Fatal(err)
	}
	return target
}

func parseObject(t *testing.T, text any) *jsonx.Object {
	t.Helper()
	if text == nil {
		return nil
	}
	return must(jsonx.ParseString(text.(string)))(t).(*jsonx.Object)
}


func TestCrossCheckPython(t *testing.T) {
	directory := t.TempDir()
	pythonDB := filepath.Join(directory, "python.sqlite3")
	created := python(t, "create", pythonDB)
	pythonDump := created.GetObject("dump")

	// 1. Go reads the database Python built.
	target := legacyNew(copyTo(t, pythonDB, filepath.Join(directory, Filename)), seconds(2))
	if err := target.Check(); err != nil {
		t.Fatal(err)
	}
	entry := must(target.Load())(t)
	if got := compact(t, entry.Document); got != pythonDump.GetString("document") {
		t.Fatalf("document read by Go differs\n  go %s\n  py %s", got, pythonDump.GetString("document"))
	}
	head := pythonDump.GetList("head")
	if entry.Head.StoreID != head[0] || !jsonx.Equal(entry.Head.Generation, head[1]) || entry.Head.CommitID != head[2] || entry.Size != pythonDump.GetInt("size") {
		t.Fatalf("head %v size %d, Python %s size %d", entry.Head, entry.Size, compact(t, head), pythonDump.GetInt("size"))
	}
	sameTables(t, "Go's view of the Python database", goDump(t, target.Path()), pythonDump, false)

	// 2. The same sequence of commits through the Go store gives the same rows.
	replay := legacyNew(filepath.Join(t.TempDir(), Filename), seconds(2))
	var current legacyEntry
	for i, item := range created.GetList("steps") {
		step := item.(*jsonx.Object)
		document, meta := parseObject(t, step.Value("document")), parseObject(t, step.Value("meta"))
		committed := true
		if i == 0 {
			current = must(replay.Create(document, meta))(t)
		} else {
			next, done, err := replay.Commit(current, document, meta)
			if err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
			if committed = done; done {
				current = next
			}
		}
		if committed != step.GetBool("committed") || current.Head.Generation != step.GetInt("generation") {
			t.Fatalf("step %d: committed=%v generation=%d, Python committed=%v generation=%d",
				i, committed, current.Head.Generation, step.GetBool("committed"), step.GetInt("generation"))
		}
		if loaded := must(replay.Load())(t); loaded.Head != current.Head || compact(t, loaded.Document) != compact(t, current.Document) ||
			loaded.Size != current.Size || (committed && compact(t, loaded.Document) != step.GetString("document")) {
			t.Fatalf("step %d: the entry returned by Commit is not what is stored", i)
		}
	}
	sameTables(t, "replayed commits", goDump(t, replay.Path()), pythonDump, true)
	if current.Size != pythonDump.GetInt("size") {
		t.Fatalf("size %d, Python %d", current.Size, pythonDump.GetInt("size"))
	}
	identifier := regexp.MustCompile(`^[0-9a-f]{32}$`)
	if !identifier.MatchString(current.Head.StoreID) || !identifier.MatchString(current.Head.CommitID) {
		t.Fatal(current.Head)
	}

	// 3. Python accepts the database Go created, and agrees on every row.
	replayDump := python(t, "dump", replay.Path())
	sameTables(t, "Python's view of the Go database", goDump(t, replay.Path()), replayDump, false)
	if replayDump.GetString("document") != pythonDump.GetString("document") {
		t.Fatalf("Python reads another document from the Go database\n  %s", replayDump.GetString("document"))
	}

	// 4. Go commits to the Python database; Python checks and reads it.
	document := entry.Document.DeepCopy()
	document.GetObject("shared").Set("go_was_here", []any{jsonx.Float(2.5), jsonx.Int(1 << 62), "日本語 ✓", nil, jsonx.Obj("z", 1, "a", jsonx.Float(1e21))})
	document.GetObject("profiles").Delete("alpha")
	document.GetObject("profiles").Set("made_by_go", jsonx.Obj("id", "made_by_go", "name", "Go", "_revision", 0))
	document.Set("go_root", true)
	entry = mustCommit(t, target, entry, document, jsonx.Obj("go_meta", jsonx.Obj("at", "now")))
	afterGo := python(t, "dump", target.Path())
	if afterGo.GetString("document") != compact(t, document) {
		t.Fatalf("Python reads another document after a Go commit\n  py %s\n  go %s", afterGo.GetString("document"), compact(t, document))
	}
	sameTables(t, "after a Go commit", goDump(t, target.Path()), afterGo, false)
	if generation := afterGo.GetList("head")[1]; !jsonx.Equal(generation, entry.Head.Generation) || afterGo.GetList("head")[2] != entry.Head.CommitID {
		t.Fatalf("head after a Go commit: %s", compact(t, afterGo.Value("head")))
	}
	revisions := 0
	for _, row := range afterGo.GetList("fields") {
		if jsonx.Equal(row.([]any)[5], entry.Head.Generation) {
			revisions++
		}
	}
	if revisions != 5 { // shared.go_was_here, root go_root, three fields of made_by_go
		t.Fatalf("%d rows carry the new revision", revisions)
	}

	// 5. Python commits on top of that; Go reads the result.
	afterPython := python(t, "commit", target.Path())
	if err := target.Check(); err != nil {
		t.Fatal(err)
	}
	final := must(target.Load())(t)
	if compact(t, final.Document) != afterPython.GetString("document") || final.Head.Generation != entry.Head.Generation+1 {
		t.Fatalf("after a Python commit: %v", final.Head)
	}
	if list := final.Document.GetObject("shared").GetList("python_was_here"); compact(t, list) != `[1.5,"🚀",{"generation":`+string(jsonx.Int(entry.Head.Generation))+`}]` {
		t.Fatal(compact(t, list))
	}
	if _, _, err := target.Commit(entry, document, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("a commit based on the entry before Python's commit: %v", err)
	}
	sameTables(t, "after a Python commit", goDump(t, target.Path()), afterPython, false)
}

// TestCrossCheckPythonErrors: for damaged, locked and inconsistent
// databases both stores fail (or succeed) the same way, with the same
// exception class and message.
func TestCrossCheckPythonErrors(t *testing.T) {
	pythonDirectory := t.TempDir()
	scenarios := python(t, "errors", pythonDirectory)
	kinds := map[string]error{
		"StoreCorrupt": ErrCorrupt, "StoreUnsupported": ErrUnsupported, "StoreError": ErrStore, "StoreConflict": ErrConflict,
		"ConfigStorageError": ErrStorage, "OperationalError": ErrSQLite, "DatabaseError": ErrSQLite,
	}
	directory := t.TempDir()
	count := 0
	for _, item := range scenarios.GetObject("scenarios").GetList("items") {
		scenario := item.(*jsonx.Object)
		name := scenario.GetString("name")
		count++
		path := filepath.Join(directory, name+".sqlite3")
		var holder *Conn
		switch setup := scenario.Value("setup"); setup {
		case "garbage":
			copyTo(t, filepath.Join(pythonDirectory, name+".sqlite3"), path)
		case "missing":
		default:
			copyTo(t, filepath.Join(pythonDirectory, "base.sqlite3"), path)
			if setup == "locked" {
				holder = must(connect(path, false, 0))(t)
				if err := holder.begin("BEGIN EXCLUSIVE"); err != nil {
					t.Fatal(err)
				}
			} else {
				execSQL(t, path, jsonx.Strings(setup.([]any))...)
			}
		}
		target := legacyNew(path, seconds(scenario.GetFloat("timeout")))
		compare := func(operation string, value any, err error) {
			t.Helper()
			want := scenario.GetObject(operation)
			if want.Value("type") == nil {
				if err != nil {
					t.Errorf("%s/%s: Go fails with %v, Python succeeds", name, operation, err)
				} else if !jsonx.Equal(value, want.Value("value")) {
					t.Errorf("%s/%s:\n  go %s\n  py %s", name, operation, compact(t, value), compact(t, want.Value("value")))
				}
				return
			}
			kind, known := kinds[want.GetString("type")]
			if !known {
				t.Fatalf("%s/%s: unexpected Python exception %s", name, operation, want.GetString("type"))
			}
			var typed *Error
			if err == nil || !errors.As(err, &typed) || typed.Kind != kind || !sameBoundaryError(name, err.Error(), want.GetString("message")) {
				t.Errorf("%s/%s:\n  go %v (%v)\n  py %s: %s", name, operation, err, kindOf(err), want.GetString("type"), want.GetString("message"))
			}
		}
		entry, err := target.Load()
		var document, head any
		if err == nil {
			document = compact(t, entry.Document)
			head = []any{entry.Head.StoreID, entry.Head.Generation, nil}
			if entry.Head.CommitID != "" {
				head.([]any)[2] = entry.Head.CommitID
			}
		}
		compare("load", document, err)
		compare("head", head, err)
		compare("check", nil, target.Check())
		var salvaged any
		if meta := target.SalvageMeta(); meta != nil {
			salvaged = compact(t, meta)
		}
		compare("salvage", salvaged, nil)
		// commit_new_root_key in crosscheck.py; last, it may change the file.
		var committedValue any
		err = nil
		if entry, err = target.Load(); err == nil {
			document := entry.Document.Clone()
			document.Set("added_by_commit", []any{jsonx.Int(1), "例"})
			var next legacyEntry
			var committed bool
			if next, committed, err = target.Commit(entry, document, nil); err == nil && committed {
				committedValue = []any{jsonx.Int(next.Head.Generation), compact(t, next.Document), jsonx.Int(next.Size)}
			}
		}
		compare("commit", committedValue, err)
		if holder != nil {
			holder.close()
		}
	}
	if count < 30 {
		t.Fatalf("only %d scenarios", count)
	}
}

func kindOf(err error) error {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Kind
	}
	return nil
}

// TestCrossCheckPythonConcurrentCommits: a Go and a Python writer commit
// from the same base at the same time. SQLite's locks work across the two
// SQLite builds, the second writer waits for the first (busy timeout) and
// then fails with the conflict, never overwriting; with a short timeout it
// fails with Python's "database is locked" storage error.
func TestCrossCheckPythonConcurrentCommits(t *testing.T) {
	interpreter, repo := os.Getenv("CONFIGFLOW_PY"), os.Getenv("CONFIGFLOW_REPO")
	if interpreter == "" || repo == "" {
		t.Skip("CONFIGFLOW_PY and CONFIGFLOW_REPO are not set")
	}
	script := must(filepath.Abs("testdata/crosscheck.py"))(t)
	start := func(t *testing.T, path, mode string, timeout string) (*exec.Cmd, io.WriteCloser, *bufio.Reader) {
		run := exec.Command(interpreter, script, "race", path, mode, timeout)
		run.Env = append(os.Environ(), "CONFIGFLOW_REPO="+repo, "PYTHONDONTWRITEBYTECODE=1")
		run.Stderr = os.Stderr
		stdin := must(run.StdinPipe())(t)
		stdout := bufio.NewReader(must(run.StdoutPipe())(t))
		if err := run.Start(); err != nil {
			t.Fatal(err)
		}
		return run, stdin, stdout
	}
	expect := func(t *testing.T, reader *bufio.Reader, want string) string {
		t.Helper()
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("python: %v (read %q)", err, line)
		}
		line = strings.TrimSuffix(line, "\n")
		if want != "" && line != want {
			t.Fatalf("python said %q, want %q", line, want)
		}
		return line
	}
	fresh := func(t *testing.T) *legacyStore {
		target := newStore(t)
		must(target.Create(sampleDocument(t), nil))(t)
		return target
	}

	t.Run("go first, python waits then conflicts", func(t *testing.T) {
		target := fresh(t)
		run, stdin, stdout := start(t, target.Path(), "wait", "5")
		expect(t, stdout, "loaded")
		base := must(target.Load())(t)
		target.BeforeCommit = func() error {
			stdin.Write([]byte("go\n")) // Python now blocks in BEGIN IMMEDIATE
			time.Sleep(600 * time.Millisecond)
			return nil
		}
		document := base.Document.Clone()
		document.Set("go_commit", true)
		entry := mustCommit(t, target, base, document, nil)
		if got := expect(t, stdout, ""); got != "StoreConflict 配置数据库在事务外被修改" {
			t.Errorf("python: %q", got)
		}
		run.Wait()
		final := must(target.Load())(t)
		if final.Head != entry.Head || !final.Document.Has("go_commit") || final.Document.Has("python_commit") {
			t.Errorf("final document %s", compact(t, final.Document))
		}
	})

	t.Run("python first, go waits then conflicts", func(t *testing.T) {
		target := fresh(t)
		base := must(target.Load())(t)
		run, _, stdout := start(t, target.Path(), "hold", "5")
		expect(t, stdout, "loaded")
		expect(t, stdout, "holding")
		document := base.Document.Clone()
		document.Set("go_commit", true)
		target.timeout = seconds(5)
		_, committed, err := target.Commit(base, document, nil)
		if !errors.Is(err, ErrConflict) || committed || err.Error() != "配置数据库在事务外被修改" {
			t.Errorf("Go commit: committed=%v err=%v (%v)", committed, err, kindOf(err))
		}
		if got := expect(t, stdout, ""); got != "ok 2" {
			t.Errorf("python: %q", got)
		}
		run.Wait()
		final := must(target.Load())(t)
		if final.Head.Generation != 2 || final.Document.Has("go_commit") || !final.Document.Has("python_commit") {
			t.Errorf("final document %s", compact(t, final.Document))
		}
	})

	t.Run("python holds the lock, go times out", func(t *testing.T) {
		target := fresh(t)
		base := must(target.Load())(t)
		run, _, stdout := start(t, target.Path(), "hold", "5")
		expect(t, stdout, "loaded")
		expect(t, stdout, "holding")
		target.timeout = seconds(0.1)
		document := base.Document.Clone()
		document.Set("go_commit", true)
		_, _, err := target.Commit(base, document, nil)
		if !errors.Is(err, ErrStorage) || err.Error() != "配置数据库访问失败：database is locked" {
			t.Errorf("Go commit: %v (%v)", err, kindOf(err))
		}
		expect(t, stdout, "ok 2")
		run.Wait()
	})
}

// TestCrossCheckPythonOddDocuments replays ODD_SEQUENCES of crosscheck.py
// (empty documents and sections, empty keys, profiles without fields,
// reordered roots, sections and profiles) with the Go store and compares
// every step: committed or not, the returned and the reloaded document, the
// generation, the size and every row (random ids and timestamps masked).
func TestCrossCheckPythonOddDocuments(t *testing.T) {
	result := python(t, "odd", t.TempDir())
	sequences := result.GetList("sequences")
	if len(sequences) < 2 {
		t.Fatalf("only %d sequences", len(sequences))
	}
	for index, item := range sequences {
		target := legacyNew(filepath.Join(t.TempDir(), Filename), seconds(2))
		var entry legacyEntry
		for number, raw := range item.([]any) {
			step := raw.(*jsonx.Object)
			name := "sequence " + strconv.Itoa(index) + " step " + strconv.Itoa(number)
			document := parseObject(t, step.GetString("document"))
			committed := true
			if number == 0 {
				entry = must(target.Create(document, nil))(t)
			} else {
				next, ok, err := target.Commit(entry, document, nil)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				committed = ok
				if ok {
					entry = next
				}
			}
			loaded := must(target.Load())(t)
			if committed != step.GetBool("committed") {
				t.Errorf("%s: committed %v, Python %v", name, committed, step.GetBool("committed"))
			}
			for what, value := range map[string]*jsonx.Object{"entry": entry.Document, "loaded": loaded.Document} {
				if got := compact(t, value); got != step.GetString(what) {
					t.Errorf("%s: %s\n  go %s\n  py %s", name, what, got, step.GetString(what))
				}
			}
			if loaded.Head.Generation != step.GetInt("generation") || loaded.Size != step.GetInt("size") {
				t.Errorf("%s: generation %d size %d, Python %d %d", name, loaded.Head.Generation, loaded.Size,
					step.GetInt("generation"), step.GetInt("size"))
			}
			sameTables(t, name, goDump(t, target.Path()), step.GetObject("dump"), true)
		}
	}
}

// Syntax diagnostics come from encoding/json; user-facing Chinese categories stay.
func sameBoundaryError(scenario, got, want string) bool {
	switch scenario {
	case "field-not-json":
		return strings.HasPrefix(got, "字段 shared//nodes 不是有效 JSON")
	case "meta-not-json", "generation-not-json":
		return strings.HasPrefix(got, "数据库元信息无法解析：")
	case "version-string", "version-bool", "version-float", "version-null", "version-list":
		return strings.HasPrefix(got, "数据库存储版本无效：")
	}
	return got == want
}
