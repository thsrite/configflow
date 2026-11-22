package testgolden

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const sourceManifest = "server/tools/pyref/sources.json"

var sourceAudits sync.Map

type cachedSourceAudit struct {
	sync.Mutex
	engine *sourceEngine
}

type sourceSet struct {
	Package        string   `json:"package"`
	Test           string   `json:"test"`
	Sources        []string `json:"sources"`
	ProductSymbols []string `json:"product_symbols,omitempty"`
	ProductImports []string `json:"product_imports,omitempty"`
	ProductMembers []string `json:"product_members,omitempty"`
}

func readSourceSets(root string) ([]sourceSet, error) {
	data, err := os.ReadFile(filepath.Join(root, sourceManifest))
	if err != nil {
		return nil, err
	}
	var sets []sourceSet
	if err := json.Unmarshal(data, &sets); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, set := range sets {
		key := set.Package + ":" + set.Test
		if set.Package == "" || set.Test == "" || seen[key] {
			return nil, fmt.Errorf("invalid/duplicate pyref source declaration %s", key)
		}
		seen[key] = true
	}
	return sets, nil
}

// Sources returns the explicitly declared sources for the current golden test.
// A native dependency audit rejects missing test helpers, imports and fixtures.
// required contains the oracle and backend roots supplied by the caller.
func Sources(t testing.TB, required ...string) []string {
	t.Helper()
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	set, found, err := currentSourceSet(root, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("pyref: missing source declaration for %s", t.Name())
	}
	// The dependency audit only guards the source hashes that record/check
	// modes compare; ordinary replay reads just the committed golden.
	if os.Getenv("PYREF_MODE") != "" {
		// Go source is fixed for this test binary. Share parsed ASTs across parallel
		// references; sourceMetadata still reads and hashes the actual bytes afresh.
		value, _ := sourceAudits.LoadOrStore(root, &cachedSourceAudit{engine: newSourceEngine(root)})
		audit := value.(*cachedSourceAudit)
		audit.Lock()
		err = audit.engine.audit(set)
		audit.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	declared := stringSet(set.Sources)
	for _, path := range required {
		if !declared[path] {
			t.Fatalf("pyref: required oracle/source is undeclared: %s", path)
		}
	}
	return append([]string(nil), set.Sources...)
}

func currentSourceSet(root, name string) (sourceSet, bool, error) {
	sets, err := readSourceSets(root)
	if err != nil {
		return sourceSet{}, false, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return sourceSet{}, false, err
	}
	pkg, err := filepath.Rel(root, cwd)
	if err != nil {
		return sourceSet{}, false, err
	}
	name, _, _ = strings.Cut(name, "/")
	for _, set := range sets {
		if set.Package == filepath.ToSlash(pkg) && set.Test == name {
			return set, true, nil
		}
	}
	return sourceSet{}, false, nil
}

// Open opens a fixture and rejects repository reads omitted from this golden's
// declaration. Non-golden callers of shared replay helpers keep ordinary I/O.
func Open(t testing.TB, path string) (*os.File, error) {
	t.Helper()
	if err := checkFixtureRead(t, path); err != nil {
		return nil, err
	}
	return os.Open(path)
}

// ReadFile reads a fixture with the same declaration check as Open.
func ReadFile(t testing.TB, path string) ([]byte, error) {
	t.Helper()
	if err := checkFixtureRead(t, path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func checkFixtureRead(t testing.TB, path string) error {
	root, err := Root()
	if err != nil {
		return err
	}
	set, found, err := currentSourceSet(root, t.Name())
	if err != nil || !found {
		return err
	}
	return declaredRead(root, set, path)
}

func declaredRead(root string, set sourceSet, path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// Check both spellings: neither a temporary alias into the repository nor a
	// repository alias pointing out to a temporary fixture can hide a read.
	paths := []string{absolute}
	if resolved, e := filepath.EvalSymlinks(absolute); e == nil && resolved != absolute {
		paths = append(paths, resolved)
	}
	declared := stringSet(set.Sources)
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if !declared[filepath.ToSlash(rel)] {
			return fmt.Errorf("pyref: undeclared fixture read: %s", filepath.ToSlash(rel))
		}
	}
	return nil
}

type sourceFile struct {
	path        string
	ast         *ast.File
	imports     map[string]string
	calls       map[*ast.SelectorExpr]*ast.CallExpr
	definitions map[*ast.Ident]bool
}
type sourceDecl struct {
	file *sourceFile
	node ast.Node
}
type sourceEngine struct {
	root           string
	files          map[string]*sourceFile
	needed         map[string]bool
	packages       map[string]bool
	construct      map[string]bool
	productImports map[string]bool
	productSymbols map[string]bool
	usedImports    map[string]bool
	usedSymbols    map[string]bool
	unclassified   map[string]string
	productMembers map[string]bool
	usedMembers    map[string]bool
}

func newSourceEngine(root string) *sourceEngine {
	return &sourceEngine{root: root, files: map[string]*sourceFile{}, needed: map[string]bool{}, packages: map[string]bool{}, construct: map[string]bool{}}
}
func stringSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}
func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sourceImport(file *sourceFile, id *ast.Ident) string {
	if id.Obj != nil && id.Obj.Kind != ast.Pkg {
		return "" // A local identifier shadows the import alias.
	}
	return file.imports[id.Name]
}

func receiverImport(file *sourceFile, id *ast.Ident) string {
	if id.Obj == nil {
		return ""
	}
	var expression ast.Expr
	switch decl := id.Obj.Decl.(type) {
	case *ast.Field:
		expression = decl.Type
	case *ast.ValueSpec:
		expression = decl.Type
		if expression == nil && len(decl.Values) == 1 {
			expression = decl.Values[0]
		}
	case *ast.AssignStmt:
		for i, left := range decl.Lhs {
			if i >= len(decl.Rhs) {
				continue
			}
			if name, ok := left.(*ast.Ident); ok && name.Name == id.Name {
				expression = decl.Rhs[i]
			}
		}
	}
	for {
		switch value := expression.(type) {
		case *ast.StarExpr:
			expression = value.X
		case *ast.UnaryExpr:
			expression = value.X
		case *ast.CompositeLit:
			expression = value.Type
		case *ast.CallExpr:
			expression = value.Fun
		case *ast.SelectorExpr:
			if owner, ok := value.X.(*ast.Ident); ok {
				return sourceImport(file, owner)
			}
			return ""
		default:
			return ""
		}
	}
}

func indexSourceDecls(file *sourceFile, index map[string][]sourceDecl) {
	for _, node := range file.ast.Decls {
		switch decl := node.(type) {
		case *ast.FuncDecl:
			index[decl.Name.Name] = append(index[decl.Name.Name], sourceDecl{file, decl})
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					index[spec.Name.Name] = append(index[spec.Name.Name], sourceDecl{file, spec})
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						index[name.Name] = append(index[name.Name], sourceDecl{file, spec})
					}
				}
			}
		}
	}
}

func productSymbol(node ast.Node, name string) string {
	fn, ok := node.(*ast.FuncDecl)
	if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
		return name
	}
	expression := fn.Recv.List[0].Type
	for {
		switch value := expression.(type) {
		case *ast.Ident:
			return value.Name + "." + name
		case *ast.StarExpr:
			expression = value.X
		case *ast.IndexExpr:
			expression = value.X
		case *ast.IndexListExpr:
			expression = value.X
		default:
			return name
		}
	}
}

// Unreferenced pure constructor results do not affect a reference. Their
// callbacks are traversed if the variable is actually used. Unknown package
// initialization and explicit init/TestMain remain conservative roots.
func pureInitializer(file *sourceFile, spec *ast.ValueSpec) bool {
	pure := true
	ast.Inspect(spec, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
			if owner, ok := selector.X.(*ast.Ident); ok {
				switch sourceImport(file, owner) + "." + selector.Sel.Name {
				case "sync.OnceValues", "sync.OnceValue", "sync.OnceFunc", "strings.Fields", "strings.Repeat", "regexp.MustCompile", "regexp.Compile":
					return true
				}
			}
		}
		pure = false
		return false
	})
	return pure
}
func (e *sourceEngine) parse(path string) (*sourceFile, error) {
	if file := e.files[path]; file != nil {
		return file, nil
	}
	data, err := os.ReadFile(filepath.Join(e.root, path))
	if err != nil {
		return nil, err
	}
	tree, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	file := &sourceFile{path: path, ast: tree, imports: map[string]string{}, calls: map[*ast.SelectorExpr]*ast.CallExpr{}, definitions: map[*ast.Ident]bool{}}
	ast.Inspect(tree, func(n ast.Node) bool {
		switch value := n.(type) {
		case *ast.FuncDecl:
			file.definitions[value.Name] = true
		case *ast.TypeSpec:
			file.definitions[value.Name] = true
		case *ast.ValueSpec:
			for _, name := range value.Names {
				file.definitions[name] = true
			}
		case *ast.Field:
			for _, name := range value.Names {
				file.definitions[name] = true
			}
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
				file.calls[selector] = call
			}
		}
		return true
	})
	for _, spec := range tree.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		alias := filepath.Base(path)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias == "." {
			return nil, fmt.Errorf("pyref: dot import prevents source audit: %s", file.path)
		}
		file.imports[alias] = path
	}
	e.files[file.path] = file
	return file, nil
}

// imported follows module-local compile dependencies, including ordinary Go
// helper packages and their embedded inputs. External modules are pinned by
// go.mod/go.sum. Imported packages' tests are not compile dependencies.
func (e *sourceEngine) imported(path string) error {
	if e.productImports[path] {
		e.usedImports[path] = true
		return nil
	}
	if !strings.HasPrefix(path, "configflow/") || e.packages[path] {
		return nil
	}
	e.packages[path] = true
	dir := "server/" + strings.TrimPrefix(path, "configflow/")
	paths, err := filepath.Glob(filepath.Join(e.root, dir, "*.go"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("pyref: missing imported source package %s", path)
	}
	for _, absolute := range paths {
		if strings.HasSuffix(absolute, "_test.go") {
			continue
		}
		rel, _ := filepath.Rel(e.root, absolute)
		file, err := e.parse(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		e.needed[file.path] = true
		for _, imported := range file.imports {
			if err := e.imported(imported); err != nil {
				return err
			}
		}
		if err := e.resources(file, file.ast); err != nil {
			return err
		}
	}
	return nil
}

func (e *sourceEngine) resources(file *sourceFile, node ast.Node) error {
	ast.Inspect(node, func(n ast.Node) bool {
		literal, ok := n.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil || strings.Contains(value, "\n") || value == "" || strings.Contains(value, "://") {
			return true
		}
		path := filepath.Join(filepath.Dir(file.path), value)
		if strings.HasPrefix(value, "server/") {
			path = value
		}
		if path == sourceManifest || strings.Contains(path, "/testdata/pyref/") {
			return true // Declarations bind each resolved set; never hash goldens.
		}
		if info, err := os.Stat(filepath.Join(e.root, path)); err == nil && !info.IsDir() {
			e.needed[filepath.ToSlash(path)] = true
		}
		return true
	})
	for _, group := range file.ast.Comments {
		for _, comment := range group.List {
			if !strings.HasPrefix(comment.Text, "//go:embed ") {
				continue
			}
			for _, pattern := range strings.Fields(strings.TrimPrefix(comment.Text, "//go:embed ")) {
				pattern = strings.TrimPrefix(strings.Trim(pattern, "\"`"), "all:")
				paths, err := filepath.Glob(filepath.Join(e.root, filepath.Dir(file.path), pattern))
				if err != nil || len(paths) == 0 {
					return fmt.Errorf("pyref: unresolved embed %s in %s", pattern, file.path)
				}
				for _, path := range paths {
					err = filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
						if err != nil || d.IsDir() {
							return err
						}
						rel, _ := filepath.Rel(e.root, path)
						e.needed[filepath.ToSlash(rel)] = true
						return nil
					})
					if err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (e *sourceEngine) required(set sourceSet) ([]string, error) {
	e.needed, e.packages = map[string]bool{}, map[string]bool{}
	e.productImports, e.productSymbols = stringSet(set.ProductImports), stringSet(set.ProductSymbols)
	e.usedImports, e.usedSymbols, e.unclassified = map[string]bool{}, map[string]bool{}, map[string]string{}
	e.productMembers, e.usedMembers = stringSet(set.ProductMembers), map[string]bool{}
	e.needed["backend"] = true
	for _, path := range []string{"server/go.mod", "server/go.sum"} {
		if _, err := os.Stat(filepath.Join(e.root, path)); err == nil {
			e.needed[path] = true
		}
	}
	products := map[string][]sourceDecl{}
	ordinary, err := filepath.Glob(filepath.Join(e.root, set.Package, "*.go"))
	if err != nil {
		return nil, err
	}
	for _, absolute := range ordinary {
		if strings.HasSuffix(absolute, "_test.go") {
			continue
		}
		rel, _ := filepath.Rel(e.root, absolute)
		file, err := e.parse(filepath.ToSlash(rel))
		if err != nil {
			return nil, err
		}
		indexSourceDecls(file, products)
	}
	paths, err := filepath.Glob(filepath.Join(e.root, set.Package, "*_test.go"))
	if err != nil {
		return nil, err
	}
	decls := map[string][]sourceDecl{}
	var roots []sourceDecl
	for _, absolute := range paths {
		rel, _ := filepath.Rel(e.root, absolute)
		file, err := e.parse(filepath.ToSlash(rel))
		if err != nil {
			return nil, err
		}
		for _, node := range file.ast.Decls {
			switch value := node.(type) {
			case *ast.FuncDecl:
				decl := sourceDecl{file, value}
				decls[value.Name.Name] = append(decls[value.Name.Name], decl)
				if value.Name.Name == set.Test || value.Name.Name == "init" || value.Name.Name == "TestMain" {
					roots = append(roots, decl)
				}
			case *ast.GenDecl:
				for _, spec := range value.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						decls[spec.Name.Name] = append(decls[spec.Name.Name], sourceDecl{file, spec})
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							decls[name.Name] = append(decls[name.Name], sourceDecl{file, spec})
						}
						if value.Tok == token.VAR && !pureInitializer(file, spec) {
							ast.Inspect(spec, func(n ast.Node) bool {
								if _, ok := n.(*ast.CallExpr); ok {
									// Package initialization executes expressions, but
									// sync.OnceValues only stores its callback. Visit
									// that body later if the variable is referenced.
									initial := &ast.ValueSpec{Type: spec.Type}
									for _, value := range spec.Values {
										if call, ok := value.(*ast.CallExpr); ok {
											if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
												if id, ok := sel.X.(*ast.Ident); ok && file.imports[id.Name] == "sync" && (sel.Sel.Name == "OnceValues" || sel.Sel.Name == "OnceValue" || sel.Sel.Name == "OnceFunc") {
													copy := *call
													copy.Args = nil
													value = &copy
												}
											}
										}
										initial.Values = append(initial.Values, value)
									}
									roots = append(roots, sourceDecl{file, initial})
									return false
								}
								return true
							})
						}
					}
				}
			}
		}
	}
	if len(decls[set.Test]) != 1 {
		return nil, fmt.Errorf("pyref: missing/ambiguous test entry %s:%s", set.Package, set.Test)
	}
	visited := map[ast.Node]bool{}
	var visit func(sourceDecl) error
	visit = func(decl sourceDecl) error {
		if visited[decl.node] {
			return nil
		}
		visited[decl.node], e.needed[decl.file.path] = true, true
		if err := e.resources(decl.file, decl.node); err != nil {
			return err
		}
		var failure error
		ast.Inspect(decl.node, func(n ast.Node) bool {
			if failure != nil {
				return false
			}
			if selector, ok := n.(*ast.SelectorExpr); ok {
				if owner, ok := selector.X.(*ast.Ident); ok {
					if imported := sourceImport(decl.file, owner); imported != "" {
						if e.productImports[imported] {
							e.usedMembers[imported+"."+selector.Sel.Name] = true
						}
						if imported == "configflow/internal/testgolden" && (selector.Sel.Name == "New" || selector.Sel.Name == "NewLines") {
							e.construct[fmt.Sprintf("%s:%d", decl.file.path, selector.Pos())] = true
						}
						if imported == "os" || imported == "io/ioutil" || imported == "io/fs" {
							switch selector.Sel.Name {
							case "Open", "OpenFile", "ReadFile", "ReadDir":
								failure = fmt.Errorf("pyref: unaudited fixture read %s.%s in %s; use testgolden.Open/ReadFile", owner.Name, selector.Sel.Name, decl.file.path)
								return false
							}
						}
						failure = e.imported(imported)
						return false
					}
					if imported := receiverImport(decl.file, owner); imported != "" {
						if e.productImports[imported] {
							e.usedMembers[imported+"."+selector.Sel.Name] = true
						}
						failure = e.imported(imported)
						return false
					}
				}
			}
			if name, ok := n.(*ast.Ident); ok {
				if decl.file.definitions[name] {
					return true
				}
				if name.Obj != nil {
					global := false
					for _, next := range decls[name.Name] {
						if next.node == name.Obj.Decl {
							global = true
						}
					}
					if !global {
						product := false
						for _, next := range products[name.Name] {
							if next.node == name.Obj.Decl {
								product = true
							}
						}
						if !product {
							return true
						} // A local binding shadows package helpers.
					}
				}
				for _, next := range decls[name.Name] {
					if err := visit(next); err != nil {
						failure = err
						break
					}
				}
				for _, next := range products[name.Name] {
					key := productSymbol(next.node, name.Name)
					if e.productSymbols[key] {
						e.usedSymbols[key] = true
					} else if stringSet(set.Sources)[next.file.path] {
						if err := visit(next); err != nil {
							failure = err
							break
						}
					} else {
						e.unclassified[key] = next.file.path
					}
				}
			}
			return failure == nil
		})
		return failure
	}
	for _, root := range roots {
		if err := visit(root); err != nil {
			return nil, err
		}
	}
	return sortedKeys(e.needed), nil
}

func (e *sourceEngine) audit(set sourceSet) error {
	required, err := e.required(set)
	if err != nil {
		return err
	}
	if len(e.unclassified) > 0 {
		return fmt.Errorf("pyref: unclassified package symbols for %s:%s: %v; declare helper sources or review exact product boundaries", set.Package, set.Test, e.unclassified)
	}
	for member := range e.usedMembers {
		if !e.productMembers[member] {
			return fmt.Errorf("pyref: unclassified imported product member %s; declare helper sources or review the exact product boundary", member)
		}
	}
	for _, member := range set.ProductMembers {
		if !e.usedMembers[member] {
			return fmt.Errorf("pyref: unused product member boundary %s", member)
		}
	}
	for _, path := range set.ProductImports {
		if !e.usedImports[path] {
			return fmt.Errorf("pyref: unused product import boundary %s", path)
		}
	}
	for _, name := range set.ProductSymbols {
		if !e.usedSymbols[name] {
			return fmt.Errorf("pyref: unused product symbol boundary %s", name)
		}
	}
	declared := stringSet(set.Sources)
	for _, path := range set.Sources {
		if filepath.IsAbs(path) || filepath.Clean(path) != path || strings.HasPrefix(path, "..") {
			return fmt.Errorf("pyref: invalid declared source %s", path)
		}
		if _, err := os.Stat(filepath.Join(e.root, path)); err != nil {
			return err
		}
	}
	var missing []string
	for _, path := range required {
		if !declared[path] {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("pyref: undeclared sources for %s:%s:\n%s", set.Package, set.Test, strings.Join(missing, "\n"))
	}
	return nil
}

// AuditSources checks explicit reference declarations and detects new golden
// constructors with no reachable declared test entry. It runs without Go/Python
// subprocesses, both from the repository audit and at each reference boundary.
func AuditSources(root string, scopes []string) error {
	sets, err := readSourceSets(root)
	if err != nil {
		return err
	}
	e := newSourceEngine(root)
	inScope := func(path string) bool {
		for _, scope := range scopes {
			if path == scope || strings.HasPrefix(path, scope+"/") {
				return true
			}
		}
		return false
	}
	for _, set := range sets {
		if inScope(set.Package) {
			if err := e.audit(set); err != nil {
				return err
			}
		}
	}
	for _, scope := range scopes {
		err := filepath.WalkDir(filepath.Join(root, scope), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if err == nil && d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return err
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			file, err := e.parse(filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			var failure error
			ast.Inspect(file.ast, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				owner, ok := selector.X.(*ast.Ident)
				if !ok || sourceImport(file, owner) != "configflow/internal/testgolden" || (selector.Sel.Name != "New" && selector.Sel.Name != "NewLines") {
					return true
				}
				if !e.construct[fmt.Sprintf("%s:%d", file.path, selector.Pos())] {
					failure = fmt.Errorf("pyref: undeclared golden constructor in %s", file.path)
				}
				call := file.calls[selector]
				if call == nil {
					failure = fmt.Errorf("pyref: indirect golden constructor in %s; use a direct audited call", file.path)
					return false
				}
				if selector.Sel.Name == "New" {
					declared := false
					if len(call.Args) == 2 {
						if spec, ok := call.Args[1].(*ast.CompositeLit); ok {
							for _, field := range spec.Elts {
								if key, ok := field.(*ast.KeyValueExpr); ok {
									if name, ok := key.Key.(*ast.Ident); ok && name.Name == "Sources" {
										if value, ok := key.Value.(*ast.CallExpr); ok {
											if sel, ok := value.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sources" {
												if id, ok := sel.X.(*ast.Ident); ok && sourceImport(file, id) == "configflow/internal/testgolden" {
													declared = true
												}
											}
										}
									}
								}
							}
						}
					}
					if !declared {
						failure = fmt.Errorf("pyref: New must use audited Sources in %s", file.path)
					}
				}
				return true
			})
			return failure
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// SuggestedSources prints a reviewable source inventory without changing it or
// recording outputs. Existing explicit backend/oracle/fixture entries are kept.
func SuggestedSources(root string) ([]byte, error) {
	sets, err := readSourceSets(root)
	if err != nil {
		return nil, err
	}
	e := newSourceEngine(root)
	for i := range sets {
		paths, err := e.required(sets[i])
		if err != nil {
			return nil, err
		}
		if len(e.unclassified) != 0 {
			return nil, fmt.Errorf("pyref: source suggestion needs manual helper/product classification: %v", e.unclassified)
		}
		for member := range e.usedMembers {
			if !e.productMembers[member] {
				return nil, fmt.Errorf("pyref: source suggestion needs manual helper/product classification for %s", member)
			}
		}
		declared := stringSet(sets[i].Sources)
		for _, path := range paths {
			declared[path] = true
		}
		sets[i].Sources = sortedKeys(declared)
		sets[i].ProductSymbols = sortedKeys(e.usedSymbols)
		sets[i].ProductImports = sortedKeys(e.usedImports)
		sets[i].ProductMembers = sortedKeys(e.usedMembers)
	}
	return json.MarshalIndent(sets, "", "  ")
}
