package skipaudit

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// toolHint marks a skip that depends on an interpreter, a reference runtime or
// another external tool being present, judged from the skip message or the
// condition that guards it.
var toolHint = regexp.MustCompile(`(?i)python|pyyaml|interpreter|\.venv|venv/|webdav3|LookPath|` +
	`not installed|not found|unavailable|is required|CONFIGFLOW_(DIFF|PY|REPO|YAML_PYTHON|DIFF_PYTHON)\b`)

// finding is one tool-dependent skip, keyed by file and enclosing function.
type finding struct{ file, function string }

func (f finding) String() string { return f.file + ":" + f.function }

// TestNoNewToolMissingSkips fails when a test skips because an interpreter or
// tool is missing, unless that file:function is listed in allowed.txt. Make a
// missing tool fail the test instead (t.Fatal), or implement the dependency in
// Go. Entries that no longer match must be removed from allowed.txt.
func TestNoNewToolMissingSkips(t *testing.T) {
	found, err := scan("../..")
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SKIPAUDIT_PRINT") == "1" {
		for _, f := range found {
			fmt.Println(f)
		}
		return
	}
	allowed, err := readAllowed("allowed.txt")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range found {
		seen[f.String()] = true
		if !allowed[f.String()] {
			t.Errorf("%s skips when an interpreter or tool is missing; fail instead (t.Fatal) or implement it in Go", f)
		}
	}
	for entry := range allowed {
		if !seen[entry] {
			t.Errorf("allowed.txt: stale entry %s (no tool-dependent skip there any more); remove it", entry)
		}
	}
}

// TestToolHintClassifiesSkips keeps the classifier honest on small fixtures.
func TestToolHintClassifiesSkips(t *testing.T) {
	tests := []struct {
		name, source string
		want         []string
	}{
		{"lookpath guard", `func TestA(t *testing.T) { if _, err := exec.LookPath("jq"); err != nil { t.Skip("x") } }`, []string{"TestA"}},
		{"previous statement", "func TestB(t *testing.T) {\n p, err := exec.LookPath(\"sh\")\n if err != nil { t.Skip(p) }\n}", []string{"TestB"}},
		{"message", `func helper(t *testing.T) { t.Skipf("python3 is required") }`, []string{"helper"}},
		{"env gate", `func TestC(t *testing.T) { if os.Getenv("CONFIGFLOW_DIFF") == "" { t.Skip("set it") } }`, []string{"TestC"}},
		{"closure", `func TestD(t *testing.T) { t.Run("x", func(t *testing.T) { t.Skip("interpreter unavailable") }) }`, []string{"TestD"}},
		{"unrelated skip", `func TestE(t *testing.T) { if testing.Short() { t.Skip("short mode") } }`, nil},
		{"opt-in container test", `func TestF(t *testing.T) { if os.Getenv("CONFIGFLOW_L1_CONTAINERS") == "" { t.Skip("set CONFIGFLOW_L1_CONTAINERS=1") } }`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "x_test.go")
			if err := os.WriteFile(path, []byte("package x\n"+tc.source+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			found, err := scan(dir)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range found {
				got = append(got, f.function)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// scan returns the tool-dependent skips in every Go file under root, sorted.
func scan(root string) ([]finding, error) {
	set := map[finding]bool{}
	// Other tests in the same go test run create and remove scratch files
	// under the module (testgolden's negative controls), so files that vanish
	// or do not parse are ignored: such a file cannot be a test that runs.
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == filepath.Join(root, "third_party") {
				return filepath.SkipDir
			}
			if name := d.Name(); name == "testdata" || name == "node_modules" || (strings.HasPrefix(name, ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if toolSkips(fset, src, fn.Body) {
				set[finding{filepath.ToSlash(rel), funcName(fn)}] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]finding, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// toolSkips reports whether body contains a Skip/Skipf/SkipNow call whose
// message, guarding if (init and condition) or the statement just before that
// if mentions a tool hint.
func toolSkips(fset *token.FileSet, src []byte, body *ast.BlockStmt) bool {
	text := func(n ast.Node) string {
		if n == nil {
			return ""
		}
		return string(src[fset.Position(n.Pos()).Offset:fset.Position(n.End()).Offset])
	}
	found := false
	var walk func(n ast.Node, guards []string)
	// list walks statements, giving each if body its guard: the init, the
	// condition and the statement just before the if.
	list := func(stmts []ast.Stmt, guards []string) {
		for i, stmt := range stmts {
			ifs, ok := stmt.(*ast.IfStmt)
			if !ok {
				walk(stmt, guards)
				continue
			}
			g := text(ifs.Init) + "\n" + text(ifs.Cond)
			if i > 0 {
				g += "\n" + text(stmts[i-1])
			}
			walk(ifs.Body, append(guards, g))
			if ifs.Else != nil {
				walk(ifs.Else, append(guards, g))
			}
		}
	}
	walk = func(n ast.Node, guards []string) {
		ast.Inspect(n, func(node ast.Node) bool {
			if found {
				return false
			}
			switch v := node.(type) {
			case *ast.BlockStmt:
				list(v.List, guards)
				return false
			case *ast.CaseClause:
				list(v.Body, guards)
				return false
			case *ast.CommClause:
				list(v.Body, guards)
				return false
			case *ast.CallExpr:
				sel, ok := v.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Skip" && sel.Sel.Name != "Skipf" && sel.Sel.Name != "SkipNow") {
					return true
				}
				context := text(v)
				if len(guards) > 0 {
					context += "\n" + guards[len(guards)-1]
				}
				if toolHint.MatchString(context) {
					found = true
				}
				return false
			}
			return true
		})
	}
	walk(body, nil)
	return found
}

func readAllowed(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	allowed := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if allowed[line] {
			return nil, fmt.Errorf("%s: duplicate entry %s", path, line)
		}
		allowed[line] = true
	}
	return allowed, scanner.Err()
}
