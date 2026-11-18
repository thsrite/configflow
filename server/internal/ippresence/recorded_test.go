package ippresence

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/repo"
)

// Replay of every rule_ip_presence.classical_ip_lines call the Python suite
// made (routersplit/testdata/r02_record.py → r02_recorded.jsonl.gz). Each
// row gets a fresh repository whose stored rule library and rules directory
// are the snapshot taken when Python made the call; the memo is cleared
// first, as the recorder cleared Python's.

func encode(v any) string {
	b, err := jsonx.Compact(v)
	if err != nil {
		return fmt.Sprintf("<%v>", err)
	}
	return string(b)
}

func TestRecordedPythonTests(t *testing.T) {
	t.Parallel()
	replay(t, "../routersplit/testdata/r02_recorded.jsonl.gz")
}

// TestCorpus replays the classical_ip_lines rows of the random corpus
// (routersplit/testdata/gen_r02.py): random libraries, declarations,
// inline and cached contents (payload YAML, BOM, invalid UTF-8, line
// separators), mrs formats, legacy cache names.
func TestCorpus(t *testing.T) {
	t.Parallel()
	replay(t, "../routersplit/testdata/r02_corpus.jsonl.gz")
}

func replay(t *testing.T, path string) {
	fixture := newReplayRepository(t)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	identical, differ, notExpressible := 0, 0, 0
	tests, files := map[string]bool{}, map[string]bool{}
	outcomes := map[string]int{}
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		row := value.(*jsonx.Object)
		if row.GetString("fn") != "classical_ip_lines" {
			continue
		}
		test := row.GetString("test")
		rules := row.GetObject("rules")
		if _, bad := rules.Get("root_error"); bad {
			notExpressible++
			continue
		}
		if _, bad := rules.Get("library_error"); bad {
			notExpressible++
			continue
		}
		tests[test] = true
		files[strings.SplitN(test, "::", 2)[0]] = true
		ctx, problem := fixture.reset(t, rules)
		if problem != "" {
			t.Errorf("%s: %s", test, problem)
			differ++
			continue
		}
		ClearMemo()
		args := row.GetObject("args")
		out, err := ClassicalIPLines(ctx, args.Value("item"), args.Value("config"))
		var problems []string
		if want := row.GetObject("err"); want != nil {
			class := pyClass(err)
			if test == "corpus::classical_ip_lines::1589" {
				// N7-3c §5.7: this one scalar-payload refusal uses the native
				// validator, without reproducing Python's internal TypeError.
				if want.GetString("class") != "TypeError" || err == nil || err.Error() != mosdns.PayloadCollectionMessage {
					problems = append(problems, fmt.Sprintf("native scalar-payload refusal: %v", err))
				}
				outcomes["native scalar-payload refusal"]++
			} else if err == nil || class != want.GetString("class") || err.Error() != want.GetString("msg") {
				problems = append(problems, fmt.Sprintf("python raised %s, go %v (%s)", encode(want), err, class))
			}
			outcomes["raise "+want.GetString("class")]++
		} else if err != nil {
			problems = append(problems, "go error "+err.Error())
		} else if encode(out) != encode(row.Value("out")) {
			problems = append(problems, fmt.Sprintf("python %s go %s", encode(row.Value("out")), encode(out)))
		} else {
			outcomes[encode(out)]++
		}
		if len(problems) > 0 {
			differ++
			if differ <= 20 {
				t.Errorf("%s classical_ip_lines(%s): %s", test, encode(args.Value("item")), strings.Join(problems, "; "))
			}
			continue
		}
		identical++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s classical_ip_lines: %d identical, %d differ, %d not expressible; from %d tests in %d files; outcomes %v",
		path, identical, differ, notExpressible, len(tests), len(files), outcomes)
}

// replayRepository shares only schema/default initialization. Each recorded
// call replaces the library and cache files, and replay clears the content memo.
// Reusing this fixture must not let a previous row supply undeclared input.
type replayRepository struct {
	r       *repo.Repository
	ctx     context.Context
	root    string
	library string
}

func newReplayRepository(t *testing.T) *replayRepository {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	root, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	return &replayRepository{r: r, ctx: repo.WithRepository(context.Background(), r), root: root}
}

func (f *replayRepository) reset(t *testing.T, rules *jsonx.Object) (context.Context, string) {
	t.Helper()
	library, _ := rules.Value("library").([]any)
	if library == nil {
		library = []any{}
	}
	encoded := encode(library)
	if encoded != f.library {
		if _, err := f.r.SaveShared(f.ctx, jsonx.Obj("rule_library", library), nil); err != nil {
			return nil, "cannot store the recorded rule library: " + err.Error()
		}
		f.library = encoded
	}
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.root, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range rules.GetObject("files").All() {
		raw, err := hex.DecodeString(data.(string))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(f.root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return f.ctx, ""
}

// TestEdgeCorpus replays the classical_ip_lines rows of the hand-picked edge
// corpus (routersplit/testdata/gen_r02_edge.py, review R02): legacy-name
// cache path with malformed library entries, by-id caches, library_rule_id
// shapes, url/source_type shapes, Unicode contents and invalid UTF-8.
func TestEdgeCorpus(t *testing.T) {
	t.Parallel()
	replay(t, "../routersplit/testdata/r02_edge.jsonl.gz")
}

// TestReplayRepositoryDoesNotLeakInputs guards the shared fixture reset itself.
// A missing cache-directory reset leaves the first row's extra file behind.
func TestReplayRepositoryDoesNotLeakInputs(t *testing.T) {
	f := newReplayRepository(t)
	first := jsonx.Obj("library", []any{jsonx.Obj("id", "old", "name", "old")},
		"files", jsonx.Obj("old.list", hex.EncodeToString([]byte("192.0.2.0/24\n"))))
	if _, problem := f.reset(t, first); problem != "" {
		t.Fatal(problem)
	}
	if _, err := os.Stat(filepath.Join(f.root, "old.list")); err != nil {
		t.Fatal(err)
	}
	second := jsonx.Obj("library", []any{}, "files", jsonx.Obj())
	if _, problem := f.reset(t, second); problem != "" {
		t.Fatal(problem)
	}
	shared, err := f.r.GetShared(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.GetList("rule_library")) != 0 {
		t.Fatal("previous row library leaked")
	}
	files, err := os.ReadDir(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatal("previous row files leaked", files)
	}
}
