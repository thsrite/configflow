package repo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCommitBatchMarkersMatchDesign(t *testing.T) {
	// One canonical whitelist for the three O3-6c repo batches and the fourth
	// explicitly approved domain exception, Clarified in R12. The AST proves
	// location and cardinality; Runtime tests prove the side-effect condition.
	allowed := map[string]int{
		"repo:DeleteProfile:beginCommitBatch":                 0,
		"repo:ImportAllJSON:beginCommitBatch":                 0,
		"repo:CreateRuleLibraryRecord:beginCommitBatch":       0,
		"routers:saveAbortedPublication:ctxx.Uninterruptible": 0,
	}
	for _, pkg := range []string{"repo", "routers"} {
		dir := "."
		if pkg != "repo" {
			dir = filepath.Join("..", pkg)
		}
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
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
			ctxxAliases := map[string]bool{}
			dotCtxx := false
			for _, imp := range file.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if path != "configflow/internal/ctxx" {
					continue
				}
				alias := "ctxx"
				if imp.Name != nil {
					alias = imp.Name.Name
				}
				if alias == "." {
					dotCtxx = true
				} else {
					ctxxAliases[alias] = true
				}
			}
			for _, decl := range file.Decls {
				owner := ""
				if fn, ok := decl.(*ast.FuncDecl); ok {
					owner = fn.Name.Name
					// The repo batches are *Repository methods (O3-6c); in routers a
					// method must not pass for the approved plain function.
					if fn.Recv != nil && pkg != "repo" {
						owner = "method:" + owner
					}
				}
				ast.Inspect(decl, func(node ast.Node) bool {
					callee := ""
					switch value := node.(type) {
					case *ast.CallExpr:
						if name, ok := value.Fun.(*ast.Ident); ok && pkg == "repo" && name.Name == "beginCommitBatch" {
							callee = "beginCommitBatch"
						}
						if name, ok := value.Fun.(*ast.Ident); ok && pkg == "routers" && dotCtxx && name.Name == "Uninterruptible" {
							callee = "ctxx.Uninterruptible"
						}
					case *ast.SelectorExpr:
						// Count references as well as direct calls: assigning the
						// function to a variable must not bypass the whitelist.
						if pkg == "routers" && value.Sel.Name == "Uninterruptible" {
							if alias, ok := value.X.(*ast.Ident); ok && ctxxAliases[alias.Name] {
								callee = "ctxx.Uninterruptible"
							}
						}
					}
					if callee == "" {
						return true
					}
					key := pkg + ":" + owner + ":" + callee
					if _, ok := allowed[key]; !ok {
						t.Errorf("unapproved batch marker %s in %s: update the coordinator-approved design", key, name)
					} else {
						allowed[key]++
					}
					return true
				})
			}
		}
	}
	design, err := os.ReadFile(filepath.Join("..", "..", "design", "o3-concurrency.md"))
	if err != nil {
		t.Fatal(err)
	}
	for name, count := range allowed {
		documentedName := name
		// N8-11 only renamed these owners; the O3-6c batch contract is unchanged.
		switch name {
		case "repo:ImportAllJSON:beginCommitBatch":
			documentedName = "repo:ImportAll:beginCommitBatch"
		case "repo:CreateRuleLibraryRecord:beginCommitBatch":
			documentedName = "repo:CreateRuleLibraryItem:beginCommitBatch"
		}
		if count != 1 || !strings.Contains(string(design), documentedName) {
			t.Errorf("batch path %s: markers=%d, must appear once and be documented", name, count)
		}
	}
}
