package tasks

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The migration boundary is architectural: dynamic compatibility belongs to
// legacy.go, and every consumer's business logic has one typed implementation.
func TestNativeConsumerBoundaries(t *testing.T) {
	for _, pkg := range []string{"taskstatus", "tasks", "notifications", "weblistener", "api/logs", "api/stats", "api/tasks", "api/notifications"} {
		path := filepath.Join("..", pkg)
		entries, err := os.ReadDir(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "legacy.go" {
				continue
			}
			filePath := filepath.Join(path, name)
			file, err := parser.ParseFile(token.NewFileSet(), filePath, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range file.Imports {
				if imp.Path.Value == `"configflow/internal/jsonx"` {
					t.Errorf("native core imports jsonx: %s", filePath)
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				if ident, ok := node.(*ast.Ident); ok && ident.Name == "any" {
					t.Errorf("native core uses any: %s", filePath)
				}
				return true
			})
		}
	}
}
