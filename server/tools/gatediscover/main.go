// Command gatediscover finds live Python tests, including callers of test helpers.
// It reads the active Go test files; it does not execute tests or reference code.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type testPackage struct {
	ImportPath, Dir           string
	TestGoFiles, XTestGoFiles []string
}

type discovery struct {
	Diff     map[string][]string `json:"diff"`
	Live     map[string][]string `json:"live"`
	Sources  map[string][]string `json:"sources"`
	Excluded map[string]string   `json:"excluded"`
}

// These tests need private snapshot input, not the synthetic compare environment.
var excluded = map[string]string{
	"configflow/internal/repo/TestN82ReviewPrivateRepositoryInterchange": "requires CONFIGFLOW_SNAPSHOT",
	"configflow/internal/model/TestTypesPrivateSnapshot":                 "requires CONFIGFLOW_SNAPSHOT",
	"configflow/internal/model/TestHRSnapshotSchema":                     "requires CONFIGFLOW_SNAPSHOT",
}

func isEnvironment(value string) bool {
	name := strings.SplitN(value, "=", 2)[0]
	switch name {
	case "CONFIGFLOW_DIFF", "CONFIGFLOW_DIFF_PYTHON", "CONFIGFLOW_PY", "CONFIGFLOW_REPO", "CONFIGFLOW_YAML_PYTHON":
		return true
	}
	return false
}

func discoverPackage(pkg testPackage) (tests, sources []string, err error) {
	functions := map[string][]*ast.FuncDecl{}
	marked := map[string]bool{}
	global := false
	for _, name := range append(append([]string{}, pkg.TestGoFiles...), pkg.XTestGoFiles...) {
		file, e := parser.ParseFile(token.NewFileSet(), filepath.Join(pkg.Dir, name), nil, 0)
		if e != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", name, e)
		}
		contains := func(node ast.Node) bool {
			found := false
			ast.Inspect(node, func(n ast.Node) bool {
				if literal, ok := n.(*ast.BasicLit); ok && literal.Kind == token.STRING {
					value, e := strconv.Unquote(literal.Value)
					found = found || (e == nil && isEnvironment(value))
				}
				return true
			})
			return found
		}
		if contains(file) {
			sources = append(sources, name)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				functions[fn.Name.Name] = append(functions[fn.Name.Name], fn)
				marked[fn.Name.Name] = marked[fn.Name.Name] || contains(fn)
			} else {
				global = global || contains(decl)
			}
		}
	}
	// A helper may be in a different file from its test (e.g. A24 review).
	for changed := true; changed; {
		changed = false
		for name, declarations := range functions {
			if marked[name] {
				continue
			}
			for _, fn := range declarations {
				ast.Inspect(fn, func(n ast.Node) bool {
					// Include function values as well as direct calls. This deliberately
					// over-selects rather than losing callbacks passed through helpers.
					if id, ok := n.(*ast.Ident); ok && marked[id.Name] {
						marked[name] = true
					}
					return true
				})
			}
			if marked[name] {
				changed = true
			}
		}
	}
	for name, declarations := range functions {
		for _, fn := range declarations {
			if fn.Recv != nil || !strings.HasPrefix(name, "Test") || fn.Type.Params.NumFields() != 1 {
				continue
			}
			pointer, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			parameter, ok := pointer.X.(*ast.SelectorExpr)
			if !ok || parameter.Sel.Name != "T" {
				continue
			}
			if global || marked[name] || strings.HasSuffix(pkg.ImportPath, "/difftest") {
				tests = append(tests, name)
				break
			}
		}
	}
	sort.Strings(tests)
	sort.Strings(sources)
	return tests, sources, nil
}

func run(ctx context.Context, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	data, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("list active packages: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	result := discovery{Diff: map[string][]string{}, Live: map[string][]string{}, Sources: map[string][]string{}, Excluded: map[string]string{}}
	for {
		var pkg testPackage
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("decode packages: %w", err)
		}
		tests, sources, err := discoverPackage(pkg)
		if err != nil {
			return err
		}
		if len(sources) > 0 {
			result.Sources[pkg.ImportPath] = sources
		}
		selected := []string{}
		for _, name := range tests {
			key := pkg.ImportPath + "/" + name
			if reason, ok := excluded[key]; ok {
				result.Excluded[key] = reason
			} else {
				selected = append(selected, name)
			}
		}
		if strings.HasSuffix(pkg.ImportPath, "/difftest") {
			result.Diff[pkg.ImportPath] = selected
		} else if len(selected) > 0 {
			result.Live[pkg.ImportPath] = selected
		}
		// Fail closed if an environment-bearing test package has no route.
		if len(sources) > 0 && len(selected) == 0 {
			return fmt.Errorf("environment test package omitted from compare: %s (%v)", pkg.ImportPath, sources)
		}
	}
	return json.NewEncoder(out).Encode(result)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := run(ctx, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
