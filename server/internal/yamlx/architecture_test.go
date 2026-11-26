package yamlx_test

import (
	"fmt"
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
	violations, err := yamlFacadeViolations(server)
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

func yamlFacadeViolations(server string) ([]string, error) {
	var violations []string
	report := func(format string, args ...any) { violations = append(violations, fmt.Sprintf(format, args...)) }
	facade := filepath.Join(server, "internal/yamlx")
	for _, relative := range []string{"internal", "tools", "cmd"} {
		err := filepath.WalkDir(filepath.Join(server, relative), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {

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
				testOnly := strings.HasSuffix(path, "_test.go")
				relative, err := filepath.Rel(server, path)
				if err != nil {
					return err
				}
				oracle := relative == "tools/yamlreaderoracle/main.go" || relative == "tools/yamlwriteroracle/main.go" || relative == "internal/yamlx/testdata/readers/main.go"
				if imported == "configflow/internal/yamlx/yamlxtest" && !testOnly && !oracle {
					report("runtime import of migration test facade: %s", positions.Position(spec.Pos()))
				}
				if imported == "configflow/internal/pyyaml" || imported == "configflow/internal/yamlx/yamltest" {
					report("retired YAML package at %s", positions.Position(spec.Pos()))
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
					report("dot-import bypasses YAML facade audit: %s", path)
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
				nativeReader := filepath.Dir(path) == facade || filepath.Dir(path) == filepath.Join(facade, "yamlxtest")
				if !nativeReader && imported == "gopkg.in/yaml.v3" && (selector.Sel.Name == "Unmarshal" || selector.Sel.Name == "NewDecoder") {
					report("raw YAML reader outside facade: %s", positions.Position(selector.Pos()))
				}
				if imported == "configflow/internal/yamlx" && selector.Sel.Name == "Clone" && !strings.HasSuffix(path, "_test.go") {
					report("runtime YAML clone: %s", positions.Position(selector.Pos()))
				}
				return true
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"load.go", "construct.go", "resolve.go", "timestamp.go", "libyaml.go"} {
		if _, err := os.Stat(filepath.Join(facade, name)); !os.IsNotExist(err) {
			report("retired YAML loader remains: %s (%v)", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(server, "internal/pyyaml")); !os.IsNotExist(err) {
		report("retired parser remains: %v", err)
	}

	return violations, nil
}

func TestYAMLFacadeNegativeControls(t *testing.T) {
	tests := []struct{ name, path, source, diagnostic string }{
		{"raw reader", "internal/example/reader.go", `package example; import y "gopkg.in/yaml.v3"; var read = y.Unmarshal`, "raw YAML reader"},
		{"decoder reference", "tools/example/reader.go", `package example; import y "gopkg.in/yaml.v3"; var read = y.NewDecoder`, "raw YAML reader"},
		{"dot import", "internal/example/reader.go", `package example; import . "gopkg.in/yaml.v3"`, "dot-import"},
		{"runtime clone", "cmd/example/reader.go", `package example; import y "configflow/internal/yamlx"; var copy = y.Clone`, "runtime YAML clone"},
		{"runtime helper", "internal/example/reader.go", `package example; import _ "configflow/internal/yamlx/yamlxtest"`, "runtime import"},
		{"command helper", "cmd/example/reader.go", `package example; import _ "configflow/internal/yamlx/yamlxtest"`, "runtime import"},
		{"tool helper", "tools/example/reader.go", `package example; import _ "configflow/internal/yamlx/yamlxtest"`, "runtime import"},
		{"facade subpackage", "internal/yamlx/bypass/reader.go", `package bypass; import y "gopkg.in/yaml.v3"; var read = y.Unmarshal`, "raw YAML reader"},
		{"types and writer", "internal/example/writer.go", `package example; import y "gopkg.in/yaml.v3"; var n y.Node; var write = y.NewEncoder`, ""},
		{"test helper", "internal/example/reader_test.go", `package example; import _ "configflow/internal/yamlx/yamlxtest"`, ""},
		{"oracle helper", "tools/yamlreaderoracle/main.go", `package main; import _ "configflow/internal/yamlx/yamlxtest"`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range []string{"internal", "cmd", "tools"} {
				if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(root, tt.path)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tt.source), 0600); err != nil {
				t.Fatal(err)
			}
			violations, err := yamlFacadeViolations(root)
			if err != nil {
				t.Fatal(err)
			}
			if tt.diagnostic == "" {
				if len(violations) != 0 {
					t.Fatalf("valid boundary rejected: %v", violations)
				}
				return
			}
			if len(violations) != 1 || !strings.Contains(violations[0], tt.diagnostic) {
				t.Fatalf("negative control escaped audit: %v", violations)
			}
		})
	}
}
