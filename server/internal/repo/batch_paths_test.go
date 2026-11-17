package repo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitBatchMarkersMatchDesign(t *testing.T) {
	allowed := map[string]int{"DeleteProfile": 0, "ImportAll": 0, "CreateRuleLibraryItem": 0}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			owner := ""
			if fn, ok := declaration.(*ast.FuncDecl); ok {
				owner = fn.Name.Name
			}
			ast.Inspect(declaration, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name, ok := call.Fun.(*ast.Ident)
				if !ok || name.Name != "beginCommitBatch" {
					return true
				}
				if _, found := allowed[owner]; !found {
					t.Errorf("batch marker in unapproved path %s: update Clarified in O3-6c", owner)
				} else {
					allowed[owner]++
				}
				return true
			})
		}
	}
	design, err := os.ReadFile(filepath.Join("..", "..", "design", "o3-concurrency.md"))
	if err != nil {
		t.Fatal(err)
	}
	for name, count := range allowed {
		if count != 1 || !strings.Contains(string(design), name) {
			t.Errorf("batch path %s: markers=%d, must appear once and be documented", name, count)
		}
	}
}
