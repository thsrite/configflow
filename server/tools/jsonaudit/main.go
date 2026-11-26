// Command jsonaudit inventories typed JSON decoding boundaries.
package main

import (
	"bytes"
	"configflow/internal/logx"
	"context"
	"encoding/json"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type pkg struct {
	ImportPath, Dir, Export string
	GoFiles                 []string
}

func main() {
	if err := run(); err != nil {
		logx.Get("jsonaudit").Errorf("audit JSON: %v", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	targets := append([]string{"./internal/...", "./cmd/..."}, os.Args[1:]...)
	args := append([]string{"list", "-deps", "-export", "-json"}, targets...)
	cmd := exec.CommandContext(ctx, "go", args...)
	data, err := cmd.Output()
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var pkgs []pkg
	exports := map[string]string{}
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			return err
		}
		pkgs = append(pkgs, p)
		exports[p.ImportPath] = p.Export
	}
	fs := token.NewFileSet()
	imp := importer.ForCompiler(fs, "gc", func(path string) (io.ReadCloser, error) { return os.Open(exports[path]) })
	enc := json.NewEncoder(os.Stdout)
	var outputErr error
	for _, p := range pkgs {
		if !strings.HasPrefix(p.ImportPath, "configflow/internal/") && !strings.HasPrefix(p.ImportPath, "configflow/cmd/") {
			continue
		}
		var files []*ast.File
		for _, n := range p.GoFiles {
			f, e := parser.ParseFile(fs, filepath.Join(p.Dir, n), nil, 0)
			if e != nil {
				return e
			}
			files = append(files, f)
		}
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}}
		conf := types.Config{Importer: imp, Error: func(error) {}}
		_, err := conf.Check(p.ImportPath, fs, files, info)
		if err != nil {
			return err
		}
		for _, f := range files {
			ast.Inspect(f, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				s, ok := c.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				name := s.Sel.Name
				idx := -1
				switch name {
				case "Unmarshal", "DecodeJSON", "DecodeRecord":
					idx = 1
				case "Decode", "Bind", "BindSilent", "JSON":
					idx = 0
				}
				if idx < 0 || idx >= len(c.Args) {
					return true
				}
				obj := info.Uses[s.Sel]
				if obj == nil {
					return true
				}
				fn, ok := obj.(*types.Func)
				if !ok {
					return true
				}
				origin := ""
				if fn.Pkg() != nil {
					origin = fn.Pkg().Path()
				}
				if origin != "encoding/json" && origin != "configflow/internal/jsondecode" && origin != "configflow/internal/httpx" && origin != "configflow/internal/devhttp" && !(origin == "configflow/internal/model" && name == "DecodeRecord") {
					return true
				}
				if name == "JSON" && origin != "configflow/internal/devhttp" {
					return true
				}
				if name == "Decode" && origin == "configflow/internal/jsondecode" {
					idx = 1
				}
				t := info.TypeOf(c.Args[idx])
				if t == nil {
					return true
				}
				pos := fs.Position(c.Pos())
				cwd, _ := os.Getwd()
				path, _ := filepath.Rel(cwd, pos.Filename)
				if err := enc.Encode(map[string]any{"file": path, "line": pos.Line, "call": origin + "." + name, "type": types.TypeString(t, func(p *types.Package) string { return p.Name() }), "class": classify(t, map[types.Type]bool{})}); err != nil {
					outputErr = err
					return false
				}
				return true
			})
		}
	}
	return outputErr
}
func classify(t types.Type, seen map[types.Type]bool) string {
	if seen[t] {
		return "recursive"
	}
	seen[t] = true
	defer delete(seen, t)
	if p, ok := t.(*types.Pointer); ok {
		return classify(p.Elem(), seen)
	}
	if _, sel, _ := types.LookupFieldOrMethod(types.NewPointer(t), true, nil, "UnmarshalJSON"); sel != nil {
		return "custom"
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		return "struct"
	case *types.Slice:
		return "list/" + classify(u.Elem(), seen)
	case *types.Array:
		return "array/" + classify(u.Elem(), seen)
	case *types.Map:
		return "map/" + classify(u.Elem(), seen)
	case *types.Interface:
		return "dynamic"
	default:
		return "scalar"
	}
}
