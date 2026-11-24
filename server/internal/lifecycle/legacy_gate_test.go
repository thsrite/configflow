package lifecycle_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Each key is a check category plus an exact server-relative file:symbol.
// Directory/file checks use <directory>/<file> as their symbol.
type legacySite struct {
	kind, key string
}

func (s legacySite) String() string { return s.kind + " " + s.key }

var retiredImports = map[string]bool{
	"configflow/internal/jsonx":  true,
	"configflow/internal/pystr":  true,
	"configflow/internal/pyyaml": true,
}

var retiredSymbols = map[string]bool{
	"PythonClass": true, "PyError": true, "pyError": true, "PyErrorClass": true,
	"raise": true, "catch": true, "must": true, "mustValue": true,
	"dictGet": true, "dictItem": true, "PyTypeName": true, "PyParseInt": true, "pyTruthy": true,
}

// Test tools are exact locations, not a blanket tools/ exception: a new command
// or a production package named "exampletest" cannot silently use testjson.
func testJSONConsumer(name, pkg string) bool {
	if strings.HasSuffix(name, "_test.go") {
		return true
	}
	dir := filepath.ToSlash(filepath.Dir(name))
	base := filepath.Base(dir)
	if strings.HasPrefix(dir, "internal/") &&
		((strings.HasSuffix(base, "test") && strings.HasSuffix(pkg, "test")) || (base == "testgolden" && pkg == "testgolden")) {
		return true
	}
	switch dir {
	case "tools/proddiff/generator", "tools/proddiff/baseline", "tools/faults/fixture",
		"tools/yamldiagnosticoracle", "tools/yamlreaderoracle", "tools/yamlwriteroracle",
		"tools/pyref", "tools/jsonaudit":
		return true
	}
	return false
}

func legacySites(name string, data []byte) ([]legacySite, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
	if err != nil {
		return nil, err
	}
	var sites []legacySite
	add := func(kind, symbol string) { sites = append(sites, legacySite{kind, name + ":" + symbol}) }
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		if retiredImports[path] {
			add("retired-import", path)
		}
		if path == "configflow/internal/testjson" && !testJSONConsumer(name, file.Name.Name) {
			add("testjson-import", path)
		}
	}
	if strings.HasSuffix(name, "_test.go") {
		return sites, nil
	}
	base := filepath.Base(name)
	if base == "legacy.go" || strings.HasSuffix(base, "_legacy.go") {
		add("legacy-file", "<file>")
	}
	// Inspect identifiers, not only CallExpr: declarations, interface methods,
	// method expressions/values, selectors and assigned function references count.
	// Strings and comments cannot create false positives.
	ast.Inspect(file, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok && retiredSymbols[id.Name] {
			add("retired-symbol", id.Name)
		}
		return true
	})
	return sites, nil
}

func scanLegacyTree(root string) (map[legacySite]bool, error) {
	found := map[legacySite]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if entry.IsDir() {
			if name == "third_party" {
				return filepath.SkipDir
			}
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			switch name {
			case "internal/jsonx", "internal/pystr", "internal/pyyaml":
				found[legacySite{"retired-directory", name + ":<directory>"}] = true
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sites, err := legacySites(name, data)
		if err != nil {
			return err
		}
		for _, site := range sites {
			found[site] = true
		}
		return nil
	})
	return found, err
}

func legacyFailures(found map[legacySite]bool) []string {
	var failures []string
	for site := range found {
		failures = append(failures, "legacy violation: "+site.String())
	}
	sort.Strings(failures)
	return failures
}

// TestLegacyFacadeBoundary enforces the hardened N8-11 legacy boundary: zero
// violations are tolerated across all five checks, and no allowlist file may exist.
func TestLegacyFacadeBoundary(t *testing.T) {
	if _, err := os.Stat("legacy_gate_allow.txt"); !os.IsNotExist(err) {
		t.Fatalf("legacy allowlist file must not exist after N8-11 hardening (err=%v)", err)
	}
	found, err := scanLegacyTree(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range legacyFailures(found) {
		t.Error(failure)
	}
	t.Logf("legacy gate hardened: %d violations (retired-import=0 retired-directory=0 retired-symbol=0 legacy-file=0 testjson-import=0)", len(found))
}

func TestLegacyGateNegativeControls(t *testing.T) {
	root := t.TempDir()
	data, err := os.ReadFile("testdata/legacy_gate/violation.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "internal/example/bad_legacy.go")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"internal/jsonx", "internal/pystr", "internal/pyyaml"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	found, err := scanLegacyTree(root)
	if err != nil {
		t.Fatal(err)
	}
	failures := legacyFailures(found)
	joined := strings.Join(failures, "\n")
	for _, kind := range []string{"retired-import", "testjson-import", "retired-symbol", "legacy-file", "retired-directory"} {
		if !strings.Contains(joined, "legacy violation: "+kind+" ") {
			t.Errorf("negative control missed %s: %v", kind, failures)
		}
	}
	for symbol := range retiredSymbols {
		if !found[legacySite{"retired-symbol", "internal/example/bad_legacy.go:" + symbol}] {
			t.Errorf("negative control missed symbol %s", symbol)
		}
	}
	for _, dir := range []string{"internal/jsonx", "internal/pystr", "internal/pyyaml"} {
		if !found[legacySite{"retired-directory", dir + ":<directory>"}] {
			t.Errorf("negative control missed retired directory %s", dir)
		}
	}
	t.Logf("synthetic source rejected (%d exact sites across all 5 categories)", len(found))
}

func TestLegacyGateTestOnlyConsumers(t *testing.T) {
	tests := []struct {
		name, pkg string
		allowed   bool
	}{
		{"internal/example/value_test.go", "example_test", true},
		{"internal/yamlx/yamlxtest/value.go", "yamlxtest", true},
		{"internal/httpx/httpxtest/value.go", "httpxtest", true},
		{"internal/testgolden/value.go", "testgolden", true},
		{"internal/api/agents/difftest/value.go", "difftest", true},
		{"tools/proddiff/generator/main.go", "main", true},
		{"tools/faults/fixture/main.go", "main", true},
		{"tools/yamldiagnosticoracle/main.go", "main", true},
		{"internal/example/value.go", "exampletest", false},
		{"internal/example/value.go", "example", false},
		{"cmd/example/main.go", "main", false},
		{"tools/new-command/main.go", "main", false},
		{"internal/exampletest/value.go", "example", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := "package " + tt.pkg + "; import _ \"configflow/internal/testjson\""
			sites, err := legacySites(tt.name, []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if (len(sites) == 0) != tt.allowed {
				t.Fatalf("sites=%v, allowed=%t", sites, tt.allowed)
			}
		})
	}
	// Test files may contain old helper declarations, but cannot import retired packages.
	sites, err := legacySites("internal/example/value_test.go", []byte(`package example; import j "configflow/internal/jsonx"; var must = j.Parse`))
	if err != nil || len(sites) != 1 || sites[0].kind != "retired-import" {
		t.Fatalf("test import boundary: %v, %v", sites, err)
	}
	// A package-wide function value is a violation even without a call.
	sites, err = legacySites("internal/example/value.go", []byte(`package example; var callback = other.PythonClass; const text = "must()"`))
	if err != nil || len(sites) != 1 || sites[0].key != "internal/example/value.go:PythonClass" {
		t.Fatalf("function reference escaped: %v, %v", sites, err)
	}
}
