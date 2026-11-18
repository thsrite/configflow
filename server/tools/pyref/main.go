// Command pyref audits process creation in migrated package trees, including test helpers.
package main

import (
	"configflow/internal/testgolden"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type exception struct {
	ID       string `json:"id"`
	File     string `json:"file"`
	Function string `json:"function"`
	Callee   string `json:"callee"`
	Count    int    `json:"count"`
	Guard    string `json:"guard"`
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
}

func (e exception) key() string { return e.File + ":" + e.Function + ":" + e.Callee }
func inspect(path string, data []byte) (map[string]int, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
	if err != nil {
		return nil, err
	}
	imports := map[string]string{}
	for _, spec := range file.Imports {
		name, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, err
		}
		alias := filepath.Base(name)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias == "." && (name == "os/exec" || name == "os" || name == "syscall" || name == "configflow/internal/yamlx/yamlxtest") {
			return nil, fmt.Errorf("%s: dot-import of process/live-oracle package %s is forbidden", path, name)
		}
		imports[alias] = name
	}
	calls := map[string]int{}
	ast.Inspect(file, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		owner, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		pkg := imports[owner.Name]
		name := selector.Sel.Name
		watched := pkg == "os/exec" && (name == "Command" || name == "CommandContext") || pkg == "os" && name == "StartProcess" || pkg == "syscall" && (name == "Exec" || name == "ForkExec" || name == "StartProcess") || pkg == "configflow/internal/yamlx/yamlxtest" && name == "NewOracle"
		if !watched {
			return true
		}
		function := "<package>"
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Pos() <= selector.Pos() && selector.End() <= fn.End() {
				function = fn.Name.Name
				break
			}
		}
		calls[path+":"+function+":"+filepath.Base(pkg)+"."+name]++
		return true
	})
	// A literal exec.Cmd can bypass Command/CommandContext completely.
	ast.Inspect(file, func(n ast.Node) bool {
		literal, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		typ, ok := literal.Type.(*ast.SelectorExpr)
		if !ok || typ.Sel.Name != "Cmd" {
			return true
		}
		owner, ok := typ.X.(*ast.Ident)
		if !ok || imports[owner.Name] != "os/exec" {
			return true
		}
		function := "<package>"
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Pos() <= literal.Pos() && literal.End() <= fn.End() {
				function = fn.Name.Name
				break
			}
		}
		calls[path+":"+function+":exec.Cmd"]++
		return true
	})
	return calls, nil
}

func audit(root string, scopes []string, registry []exception) error {
	found := map[string]int{}
	for _, scope := range scopes {
		path := filepath.Join(root, scope)
		if err := filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			calls, err := inspect(filepath.ToSlash(rel), data)
			if err != nil {
				return err
			}
			for key, count := range calls {
				found[key] = count
			}
			return nil
		}); err != nil {
			return err
		}
	}
	allowed := map[string]exception{}
	for _, row := range registry {
		inScope := false
		for _, scope := range scopes {
			if strings.HasPrefix(row.File, strings.TrimSuffix(scope, "/")+"/") {
				inScope = true
			}
		}
		if !inScope {
			continue
		}
		if row.ID == "" || row.Guard == "" || row.Kind == "" || row.Reason == "" {
			return fmt.Errorf("incomplete live exception %q", row.ID)
		}
		if _, duplicate := allowed[row.key()]; duplicate {
			return fmt.Errorf("duplicate exception %s", row.key())
		}
		allowed[row.key()] = row
	}
	var problems []string
	for key, count := range found {
		row, ok := allowed[key]
		if !ok {
			problems = append(problems, "unregistered process/live oracle: "+key)
			continue
		}
		expected := row.Count
		if expected == 0 {
			expected = 1
		}
		if count != expected {
			problems = append(problems, fmt.Sprintf("call count changed %s: %d != %d", key, count, expected))
		}
		delete(allowed, key)
	}
	for key := range allowed {
		problems = append(problems, "stale exception: "+key)
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	fmt.Printf("pyref audit: %d registered call sites, %d scopes; no unregistered process constructors/live oracles\n", len(found), len(scopes))
	return nil
}
func main() {
	fingerprintFlag := flag.Bool("fingerprint", false, "print native backend+venv fingerprint for gate integration")
	testFlag := flag.Bool("test", false, "run go test with native external-input cache fingerprint; arguments after --")
	scopesFlag := flag.String("scopes", "server/internal/mosdns,server/internal/oxidns,server/internal/provider,server/internal/deploy,server/internal/discovery,server/internal/routersplit", "comma-separated repository-relative scopes")
	root := flag.String("root", "..", "repository root (run from server)")
	flag.Parse()
	if *fingerprintFlag || *testFlag {
		fingerprint, err := testgolden.Fingerprint()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if *fingerprintFlag {
			fmt.Println(fingerprint)
			return
		}
		command := exec.Command("go", append([]string{"test"}, flag.Args()...)...)
		command.Env = append(os.Environ(), "PYREF_INPUT_FINGERPRINT="+fingerprint)
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				os.Exit(exit.ExitCode())
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	files, err := filepath.Glob(filepath.Join(*root, "server/tools/pyref/live-exceptions-*.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var registry []exception
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var rows []exception
		if err = json.Unmarshal(data, &rows); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		registry = append(registry, rows...)
	}
	if err = audit(*root, strings.Split(*scopesFlag, ","), registry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
