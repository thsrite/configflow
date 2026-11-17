package yamlx_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestYAMLFacadeBoundary prevents new user-text readers from bypassing shared
// policies. It also checks function references, so assigning a forbidden helper
// to another variable cannot sidestep the gate. Test-only Clone is permitted.
func TestYAMLFacadeBoundary(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("missing source location")
	}
	server := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	facade := filepath.Join(server, "internal/yamlx")
	for _, relative := range []string{"internal", "tools", "cmd"} {
		err := filepath.WalkDir(filepath.Join(server, relative), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path == facade {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			positions := token.NewFileSet()
			source, err := parser.ParseFile(positions, path, nil, 0)
			if err != nil {
				return err
			}
			imports := map[string]string{}
			for _, spec := range source.Imports {
				imported, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				if imported == "configflow/internal/yamlx/yamlxtest" && strings.HasPrefix(path, filepath.Join(server, "internal")+string(filepath.Separator)) && !strings.HasSuffix(path, "_test.go") {
					t.Errorf("runtime import of migration test facade: %s", positions.Position(spec.Pos()))
				}
				if imported == "configflow/internal/pyyaml" || imported == "configflow/internal/yamlx/yamltest" {
					t.Errorf("retired YAML package at %s", positions.Position(spec.Pos()))
				}
				if imported != "gopkg.in/yaml.v3" && imported != "configflow/internal/yamlx" {
					continue
				}
				name := "yaml"
				if imported == "configflow/internal/yamlx" {
					name = "yamlx"
				}
				if spec.Name != nil {
					name = spec.Name.Name
				}
				if name == "." {
					t.Errorf("dot-import bypasses YAML facade audit: %s", path)
				}
				imports[name] = imported
			}
			ast.Inspect(source, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				identifier, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				imported := imports[identifier.Name]
				if imported == "gopkg.in/yaml.v3" && (selector.Sel.Name == "Unmarshal" || selector.Sel.Name == "NewDecoder") {
					t.Errorf("raw YAML reader outside facade: %s", positions.Position(selector.Pos()))
				}
				if imported == "configflow/internal/yamlx" && selector.Sel.Name == "Clone" && !strings.HasSuffix(path, "_test.go") {
					t.Errorf("runtime YAML clone: %s", positions.Position(selector.Pos()))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"load.go", "construct.go", "resolve.go", "timestamp.go", "libyaml.go"} {
		if _, err := os.Stat(filepath.Join(facade, name)); !os.IsNotExist(err) {
			t.Errorf("retired YAML loader remains: %s (%v)", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(server, "internal/pyyaml")); !os.IsNotExist(err) {
		t.Errorf("retired parser remains: %v", err)
	}
}
