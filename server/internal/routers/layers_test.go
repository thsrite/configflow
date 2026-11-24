package routers_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestRouterPackageLayers enforces the 13-package import DAG from
// server/design/o17-wiring.md §1.2 using go/parser (no exec go) and verifies
// that external packages agents/ipv6 and clients do not import internal/routers.
func TestRouterPackageLayers(t *testing.T) {
	const prefix = "configflow/internal/routers"

	// Allowed internal/routers/... imports per package (exact §1.2 DAG).
	allowed := map[string][]string{
		"device":     {},
		"ros":        {"device"},
		"ikuai":      {"device"},
		"spec":       {"device"},
		"oplock":     {"spec", "device"},
		"dnsguard":   {"ros", "spec", "device"},
		"fixedip":    {"ros", "ikuai", "spec", "device"},
		"clientlist": {"ros", "ikuai", "spec", "device"},
		"routeedit":  {"oplock", "ros", "ikuai", "spec", "device"},
		"rospolicy":  {"routeedit", "ros", "spec", "device"},
		"ipv6":       {"rospolicy", "dnsguard", "ros", "spec", "device"},
		".":          {"ipv6", "rospolicy", "routeedit", "dnsguard", "fixedip", "clientlist", "oplock", "ros", "ikuai", "spec", "device"},
		"rulesync":   {".", "ipv6", "rospolicy", "routeedit", "oplock", "ros", "spec", "device"},
	}

	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	// Verify every non-test package directory under internal/routers (excluding
	// routerfake and testdata) is listed in allowed.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "routerfake" || entry.Name() == "testdata" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(root, entry.Name(), "*.go"))
		if len(matches) == 0 {
			continue
		}
		if _, ok := allowed[entry.Name()]; !ok {
			t.Errorf("unexpected subpackage internal/routers/%s not declared in layer table", entry.Name())
		}
	}

	readImports := func(dir string) []string {
		t.Helper()
		fset := token.NewFileSet()
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		seen := map[string]bool{}
		out := []string{}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(fset, filepath.Join(dir, f.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", f.Name(), err)
			}
			for _, spec := range parsed.Imports {
				imp, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", spec.Path.Value, err)
				}
				if !seen[imp] {
					seen[imp] = true
					out = append(out, imp)
				}
			}
		}
		return out
	}

	for pkg, okList := range allowed {
		dir := root
		if pkg != "." {
			dir = filepath.Join(root, pkg)
		}
		for _, imp := range readImports(dir) {
			if imp != prefix && !strings.HasPrefix(imp, prefix+"/") {
				continue
			}
			rel := "."
			if imp != prefix {
				rel = strings.TrimPrefix(imp, prefix+"/")
			}
			if !slices.Contains(okList, rel) {
				t.Errorf("layer violation: internal/routers/%s imports %s (allowed: %v)", pkg, imp, okList)
			}
		}
	}

	// External packages that must NOT import the orchestration package internal/routers.
	internalRoot := filepath.Dir(root)
	for _, ext := range []string{"agents/ipv6", "clients"} {
		for _, imp := range readImports(filepath.Join(internalRoot, ext)) {
			if imp == prefix {
				t.Errorf("external layer violation: internal/%s must not import %s", ext, prefix)
			}
		}
	}
}
