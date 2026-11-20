package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoveryFollowsHelpersAndIgnoresFixtureText(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"helper_test.go": `package fixture
import ("os"; "testing")
func oracle(t *testing.T) { if os.Getenv("CONFIGFLOW_DIFF") != "1" { t.Skip() } }
func intermediate(t *testing.T) { oracle(t) }
`,
		"review_test.go": `package fixture
import "testing"
func TestNewBoundary(t *testing.T) { intermediate(t) }
func TestUnrelated(t *testing.T) { _ = "func TestNotReal(t *testing.T) {}" }
func TestMain(m *testing.M) {}
`,
		// External test packages and methods may reuse helper names. They
		// must not overwrite another helper's environment dependency.
		"external_test.go": `package fixture_test
import "testing"
func intermediate(t *testing.T) {}
`,
	}
	names := []string{}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	pkg := testPackage{ImportPath: "configflow/internal/newpackage", Dir: dir, TestGoFiles: names}
	tests, sources, err := discoverPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tests, []string{"TestNewBoundary"}) || !reflect.DeepEqual(sources, []string{"helper_test.go"}) {
		t.Fatalf("tests=%v sources=%v", tests, sources)
	}
	pkg.ImportPath += "/difftest"
	tests, _, err = discoverPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tests, []string{"TestNewBoundary", "TestUnrelated"}) {
		t.Fatalf("difftest manifest=%v", tests)
	}
}
