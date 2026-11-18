package testgolden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sourceFixture(t *testing.T) (string, sourceSet) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"backend/reference.py":                  "# synthetic reference\n",
		"server/go.mod":                         "module configflow\n",
		"server/internal/example/value.go":      "package example\n",
		"server/internal/example/oracle.py":     "print('reference')\n",
		"server/internal/example/input.fixture": "synthetic input\n",
		"server/internal/example/driver_test.go": `package example
import ("testing"; tg "configflow/internal/testgolden")
func TestOracle(t *testing.T) { helper(t); tg.NewLines(t,"oracle.py") }
`,
		"server/internal/example/helper_test.go": `package example
import ("testing"; h "configflow/internal/inputhelper"; tg "configflow/internal/testgolden")
func helper(t *testing.T) { h.Value(); tg.ReadFile(t,"input.fixture") }
`,
		"server/internal/inputhelper/helper.go": `package inputhelper
import _ "embed"
//go:embed value.fixture
var value string
func Value() string { return value }
`,
		"server/internal/inputhelper/value.fixture": "imported fixture\n",
		"server/internal/testgolden/golden.go":      "package testgolden\n",
	}
	for path, content := range files {
		writeSourceFixture(t, root, path, content)
	}
	set := sourceSet{Package: "server/internal/example", Test: "TestOracle"}
	paths, err := newSourceEngine(root).required(set)
	if err != nil {
		t.Fatal(err)
	}
	set.Sources = paths
	writeSourceManifest(t, root, set)
	return root, set
}

func writeSourceFixture(t *testing.T, root, path, content string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeSourceManifest(t *testing.T, root string, set sourceSet) {
	t.Helper()
	data, err := json.Marshal([]sourceSet{set})
	if err != nil {
		t.Fatal(err)
	}
	writeSourceFixture(t, root, sourceManifest, string(data))
}

func TestSourceAuditNarrowAndComplete(t *testing.T) {
	root, set := sourceFixture(t)
	scopes := []string{set.Package}
	if err := AuditSources(root, scopes); err != nil {
		t.Fatal(err)
	}
	writeSourceFixture(t, root, set.Package+"/unrelated_test.go", "package example\nimport \"testing\"\nfunc TestUnrelated(t *testing.T){t.Helper()}\n")
	required, err := newSourceEngine(root).required(set)
	if err != nil || !reflect.DeepEqual(required, set.Sources) {
		t.Fatalf("unrelated test changed reference dependencies: %v\n%v", err, required)
	}
	if err := AuditSources(root, scopes); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{
		set.Package + "/helper_test.go",
		"server/internal/inputhelper/helper.go",
		"server/internal/inputhelper/value.fixture",
		set.Package + "/input.fixture",
	} {
		t.Run(missing, func(t *testing.T) {
			incomplete := set
			incomplete.Sources = nil
			for _, path := range set.Sources {
				if path != missing {
					incomplete.Sources = append(incomplete.Sources, path)
				}
			}
			writeSourceManifest(t, root, incomplete)
			if err := AuditSources(root, scopes); err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("missing dependency was not caught: %s: %v", missing, err)
			}
		})
	}
	writeSourceManifest(t, root, set)
	writeSourceFixture(t, root, set.Package+"/new_test.go", `package example
import ("testing"; tg "configflow/internal/testgolden")
func TestNewOracle(t *testing.T){tg.NewLines(t,"oracle.py")}
`)
	if err := AuditSources(root, scopes); err == nil || !strings.Contains(err.Error(), "undeclared golden constructor") {
		t.Fatal("new reference bypassed source registration", err)
	}
}

func TestSourceAuditDetectsNewHelperAndRawRead(t *testing.T) {
	root, set := sourceFixture(t)
	writeSourceFixture(t, root, set.Package+"/helper_test.go", `package example
import "testing"
func helper(t *testing.T){ newInputHelper(t) }
`)
	writeSourceFixture(t, root, set.Package+"/new_helper_test.go", `package example
import "testing"
func newInputHelper(t *testing.T){ t.Helper() }
`)
	if err := newSourceEngine(root).audit(set); err == nil || !strings.Contains(err.Error(), "new_helper_test.go") {
		t.Fatal("new called helper bypassed source registration", err)
	}
	writeSourceFixture(t, root, set.Package+"/new_helper_test.go", `package example
import ("testing"; alias "os")
func newInputHelper(t *testing.T){ read:=alias.ReadFile; read("input.fixture") }
`)
	if err := newSourceEngine(root).audit(set); err == nil || !strings.Contains(err.Error(), "unaudited fixture read") {
		t.Fatal("function-value raw file read bypassed fixture guard", err)
	}
}

func TestDeclaredReadRejectsDynamicAndSymlinkAliases(t *testing.T) {
	root, set := sourceFixture(t)
	fixture := filepath.Join(root, set.Package, "input.fixture")
	if err := declaredRead(root, set, fixture); err != nil {
		t.Fatal(err)
	}
	set.Sources = nil
	if err := declaredRead(root, set, fixture); err == nil || !strings.Contains(err.Error(), "undeclared fixture read") {
		t.Fatal("dynamic undeclared input was accepted", err)
	}
	outside := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(fixture, outside); err != nil {
		t.Fatal(err)
	}
	if err := declaredRead(root, set, outside); err == nil {
		t.Fatal("temporary symlink hid repository fixture")
	}
	temporary := filepath.Join(t.TempDir(), "external.fixture")
	if err := os.WriteFile(temporary, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, set.Package, "external-alias")
	if err := os.Symlink(temporary, inside); err != nil {
		t.Fatal(err)
	}
	if err := declaredRead(root, set, inside); err == nil {
		t.Fatal("repository symlink escaped fixture declaration")
	}
	if err := declaredRead(root, set, filepath.Join(t.TempDir(), "generated-input")); err != nil {
		t.Fatal("temporary materialized payload rejected", err)
	}
}

func TestSourceAuditRejectsConstructorBypasses(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"alias", `start:=tg.NewLines; start(t,"oracle.py")`, "indirect golden constructor"},
		{"nested-sources", `tg.New(t,tg.Spec{Sources:[]string{"oracle.py"},Run:func()([]byte,error){_=tg.Spec{Sources:tg.Sources(t)};return nil,nil}})`, "New must use audited Sources"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, set := sourceFixture(t)
			writeSourceFixture(t, root, set.Package+"/driver_test.go", "package example\nimport (\"testing\"; tg \"configflow/internal/testgolden\")\nfunc TestOracle(t *testing.T){helper(t);"+tc.body+"}\n")
			if err := AuditSources(root, []string{set.Package}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal("source boundary bypass accepted", err)
			}
		})
	}
}

func TestSourceAuditRequiresClassificationForNewOrdinaryHelper(t *testing.T) {
	root, set := sourceFixture(t)
	writeSourceFixture(t, root, set.Package+"/helper_test.go", `package example
import "testing"
func helper(t *testing.T){ ordinaryInputHelper() }
`)
	writeSourceFixture(t, root, set.Package+"/ordinary_helper.go", "package example\nfunc ordinaryInputHelper(){}\n")
	if err := newSourceEngine(root).audit(set); err == nil || !strings.Contains(err.Error(), "ordinaryInputHelper") {
		t.Fatal("ordinary Go helper was silently treated as product code", err)
	}
	if _, err := SuggestedSources(root); err == nil || !strings.Contains(err.Error(), "manual helper/product classification") {
		t.Fatal("suggestion silently approved an omitted helper as product", err)
	}
	set.Sources = append(set.Sources, set.Package+"/ordinary_helper.go")
	if err := newSourceEngine(root).audit(set); err != nil {
		t.Fatal(err)
	}
}

func TestSourceAuditDoesNotExemptNewMemberOfProductImport(t *testing.T) {
	root, set := sourceFixture(t)
	set.ProductImports = []string{"configflow/internal/inputhelper"}
	set.ProductMembers = []string{"configflow/internal/inputhelper.Value"}
	if err := newSourceEngine(root).audit(set); err != nil {
		t.Fatal(err)
	}
	writeSourceFixture(t, root, set.Package+"/helper_test.go", `package example
import ("testing"; h "configflow/internal/inputhelper")
func helper(t *testing.T) { h.NewInputHelper() }
`)
	if err := newSourceEngine(root).audit(set); err == nil || !strings.Contains(err.Error(), "unclassified imported product member") {
		t.Fatal("new helper hidden behind an existing product import", err)
	}
}
