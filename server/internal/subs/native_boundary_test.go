package subs

import (
	"configflow/internal/yamlx"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestN89CoreHasNoDynamicOrPrivateCompatibilityOwner(t *testing.T) {
	for _, dir := range []string{".", "../substore"} {
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			name := file.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "legacy") {
				continue
			}
			source, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range source.Imports {
				if strings.Contains(imp.Path.Value, "/jsonx\"") {
					t.Errorf("native owner imports jsonx: %s/%s", dir, name)
				}
			}
			// omap uses any solely as a generic constraint, never as a runtime value.
			ast.Inspect(source, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "any" && name != "omap.go" {
					t.Errorf("dynamic core type: %s/%s", dir, name)
				}
				return true
			})
		}
	}
}

func TestN89HostsRejectsUnresolvedAliasKeyWithoutPanic(t *testing.T) {
	key := &yaml.Node{Kind: yaml.AliasNode, Value: "missing"}
	for _, bad := range []*yaml.Node{nil, key} {
		err := ValidateHostsGraph(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{bad, yamlx.Str("192.0.2.1")}})
		if !errors.Is(err, yamlx.ErrPolicy) {
			t.Fatalf("bad key accepted: %v", err)
		}
	}
}
