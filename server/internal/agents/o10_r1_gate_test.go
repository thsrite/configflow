package agents

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// deepCopyAllowed lists every function allowed to deep-copy an Agent record
// (copyAgent or CloneAgent), as "file:function". Reads borrow the immutable
// registry generation ("Borrowed records" in registry.go); a deep copy is only
// for write-transaction working records and write results.
var deepCopyAllowed = map[string]string{
	"internal/agents/registry.go:readerAgent":     "write-transaction working records may still change",
	"internal/agents/registry.go:CloneAgent":      "the explicit deep copy itself",
	"internal/agents/mutations.go:setAgentSwitch": "result copied out of the write transaction",
	"internal/agents/mutations.go:DeleteAgent":    "deleted record copied out of the write transaction",
}

type deepCopySite struct {
	key, pos string
}

// deepCopySites reports every reference to copyAgent or CloneAgent, not only
// direct calls: a method value, a function variable or a package-level
// initializer would otherwise hide a deep copy from the gate. References in a
// function (including its closures) are keyed by that function; references
// outside any function by "<package-level>".
func deepCopySites(fset *token.FileSet, name string, data []byte) ([]deepCopySite, error) {
	file, err := parser.ParseFile(fset, name, data, 0)
	if err != nil {
		return nil, err
	}
	var sites []deepCopySite
	for _, decl := range file.Decls {
		owner := "<package-level>"
		var root ast.Node = decl
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Body == nil {
				continue
			}
			owner, root = fn.Name.Name, fn.Body
		}
		// Every identifier, including a selector's Sel, is visited once.
		ast.Inspect(root, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok && (ident.Name == "copyAgent" || ident.Name == "CloneAgent") {
				sites = append(sites, deepCopySite{key: name + ":" + owner, pos: fset.Position(ident.Pos()).String()})
			}
			return true
		})
	}
	return sites, nil
}

// TestAgentDeepCopyBoundary keeps the O10-R1 default: API handlers and
// background loops read borrowed Agent records and never deep-copy them.
func TestAgentDeepCopyBoundary(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	used := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == filepath.Join(root, "third_party") {
				return filepath.SkipDir
			}
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "node_modules" || entry.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sites, err := deepCopySites(fset, name, data)
		if err != nil {
			return err
		}
		for _, site := range sites {
			used[site.key] = true
			if _, ok := deepCopyAllowed[site.key]; !ok {
				t.Errorf("%s: Agent deep copy in %s; borrow the record (registry.go \"Borrowed records\") or add a reasoned exemption", site.pos, site.key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var stale []string
	for key := range deepCopyAllowed {
		if !used[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("stale deep-copy exemption %s", key)
	}
}

func TestAgentDeepCopyBoundaryDetectsHandlers(t *testing.T) {
	tests := []struct {
		name, file, src string
		want            []string
	}{
		{"api handler", "internal/api/agents/x.go", "package x\nfunc List() { agents.CloneAgent(nil) }\n", []string{"internal/api/agents/x.go:List"}},
		{"loop", "internal/agents/rulesync/x.go", "package x\nfunc (w *worker) run() { a, _ := agents.CloneAgent(r); _ = a }\n", []string{"internal/agents/rulesync/x.go:run"}},
		{"private copy", "internal/agents/x.go", "package agents\nfunc read() { copyAgent(nil, true) }\n", []string{"internal/agents/x.go:read"}},
		{"borrowed read", "internal/api/agents/y.go", "package x\nfunc Get() { m.ReadAgent(ctx, id, true) }\n", nil},
		{"function value", "internal/api/agents/z.go", "package x\nfunc Get() { clone := agents.CloneAgent; clone(nil) }\n", []string{"internal/api/agents/z.go:Get"}},
		{"closure", "internal/agents/x.go", "package agents\nfunc read() { f := func() { _, _ = copyAgent(nil, true) }; f() }\n", []string{"internal/agents/x.go:read"}},
		{"package-level alias", "internal/agents/x.go", "package agents\nvar dup = copyAgent\n", []string{"internal/agents/x.go:<package-level>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, err := deepCopySites(token.NewFileSet(), tt.file, []byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, site := range sites {
				got = append(got, site.key)
				if _, ok := deepCopyAllowed[site.key]; ok {
					t.Errorf("%s unexpectedly exempt", site.key)
				}
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("sites = %v, want %v", got, tt.want)
			}
		})
	}
}
