package ippresence

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
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
	f, err := os.Open("../routersplit/testdata/r02_recorded.jsonl.gz")
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
		ctx, problem := rowRepository(t, rules)
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
			class := "?"
			var classed interface{ PythonClass() string }
			if errors.As(err, &classed) {
				class = classed.PythonClass()
			}
			if err == nil || class != want.GetString("class") || err.Error() != want.GetString("msg") {
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
	t.Logf("classical_ip_lines replay: %d identical, %d differ, %d not expressible; from %d tests in %d files; outcomes %v",
		identical, differ, notExpressible, len(tests), len(files), outcomes)
}

// rowRepository builds the repository of a recorded call.
func rowRepository(t *testing.T, rules *jsonx.Object) (context.Context, string) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	if library, ok := rules.Value("library").([]any); ok && len(library) > 0 {
		if _, err := r.SaveShared(ctx, jsonx.Obj("rule_library", library), nil); err != nil {
			return nil, "cannot store the recorded rule library: " + err.Error()
		}
	}
	root, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range rules.GetObject("files").All() {
		raw, err := hex.DecodeString(data.(string))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ctx, ""
}
