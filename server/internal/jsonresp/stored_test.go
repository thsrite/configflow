package jsonresp

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeStoredPreservesRecordsAndOnlyRepairsNumbers(t *testing.T) {
	var got struct {
		Values []json.RawMessage `json:"values"`
		Text   string            `json:"text"`
		Exact  json.Number       `json:"exact"`
	}
	if err := DecodeStored([]byte(`{"values":[NaN,Infinity,-Infinity,1.25,null],"text":"NaN Infinity \"NaN","exact":100000000000000000001}`), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Values) != 5 || string(got.Values[0]) != "null" || string(got.Values[1]) != "null" || string(got.Values[2]) != "null" || string(got.Values[3]) != "1.25" || string(got.Exact) != "100000000000000000001" {
		t.Fatalf("%+v", got)
	}
	for _, bad := range []string{`{"x":NotNaN}`, `{"x":Infinity1}`, `{"x":+Infinity}`, `{"x":NaN} {}`} {
		if err := DecodeStored([]byte(bad), new(json.RawMessage)); err == nil {
			t.Fatal(bad)
		}
	}
}

// Persistence compatibility cannot weaken request validation accidentally.
func TestRequestPackagesDoNotRepairStoredJSON(t *testing.T) {
	for _, root := range []string{"../httpx", "../api"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			aliases := map[string]bool{}
			for _, imp := range file.Imports {
				if imp.Path.Value == `"configflow/internal/jsonresp"` {
					alias := "jsonresp"
					if imp.Name != nil {
						alias = imp.Name.Name
					}
					aliases[alias] = true
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				s, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := s.X.(*ast.Ident)
				if ok && aliases[ident.Name] && s.Sel.Name == "DecodeStored" {
					t.Errorf("request package calls stored-data repair: %s", path)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
