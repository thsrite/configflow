package repo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// O10-R2 source gate: deep copies of configuration records are explicit.
//
// Inside this package copyRecord/copyValue may only be called from the write
// paths and explicit owned reads listed in r2OwnedCopies. Outside it the
// owned reads (Clone*, System, PublishConfig) may only be called from the
// sites in r2OwnedReads, each with the reason it needs its own copy; in
// particular no HTTP read route (internal/api/**) or background loop may start
// deep-copying again by default. Borrowing reads (Config, Shared, Profile, …)
// are free to use anywhere.

// r2OwnedCopies are the functions of package repo allowed to deep-copy, as
// "file:function".
var r2OwnedCopies = map[string]string{
	"read.go:System":                               "owned System (Agents included); proddiff tools and tests only",
	"resolve.go:resolveConfig":                     "owned mode for writers' working copies and PublishConfig",
	"config_borrow.go:lend":                        "deep copy only for writers' working copies and Clone*",
	"sections.go:applySharedIn":                    "write transaction working copy",
	"sections.go:updateProfileIn":                  "write transaction working copy",
	"sections.go:UpdateSharedAndProfiles":          "write transaction working copies",
	"system.go:applySystemIn":                      "write transaction working copy",
	"config_edits.go:updateSharedConfigs":          "write transaction working copies",
	"profile_records.go:CreateProfileRecord":       "write: copies a profile into a new one",
	"profile_records.go:ImportClientProfileRecord": "write result handed to the caller",
	"profile_records.go:ImportProfileJSON":         "write result handed to the caller",
	"profile_records.go:SaveProfileFieldsJSON":     "write result handed to the caller",
	"shared.go:CreateRuleLibraryRecord":            "write result handed to the caller",
	"section_snapshots.go:SaveSharedRecord":        "write result handed to the caller",
	"section_snapshots.go:SaveSystemRecord":        "write result handed to the caller",
	"section_snapshots.go:SaveSystemConfig":        "write result handed to the caller",
	"section_snapshots.go:SaveBackup":              "write result handed to the caller",
	"import_conversion.go:copyImportResource":      "import conversion of a document being written",
	"import_conversion.go:importOldProfile":        "import conversion of a document being written",
	"repository.go:openLocked":                     "startup token initialisation edits a system copy",
}

// r2OwnedReadNames are the repository reads that return a deep copy. System
// is a common name, so only its calls with arguments count (see r2CopySites).
var r2OwnedReadNames = map[string]bool{
	"CloneShared": true, "CloneProfile": true, "CloneSystemConfig": true, "CloneBackup": true,
	"PublishConfig": true,
}

// r2OwnedReads are the call sites outside package repo allowed to use an owned
// read, as "path:function" relative to server/.
var r2OwnedReads = map[string]string{
	"internal/api/settings/owner.go:loadSystemConfig":        "settings editor saves the copy through SaveSystemConfig",
	"internal/api/settings/owner.go:loadBackup":              "backup editor saves the copy through SaveBackup",
	"internal/api/routers/adapters.go:publishSnapshot":       "router publish snapshot with its revision",
	"internal/api/ipv6/routes.go:operation":                  "IPv6 check compares two publish snapshots with full Agents",
	"internal/agents/dnsfollow/adapter.go:snapshot":          "DNS-follow publish snapshot with its revision",
	"internal/agents/dnsha/adapter.go:source":                "OxiDNS HA publish snapshot with its revision",
	"internal/agents/ipv6/adapter.go:snapshot":               "IPv6 verification snapshot with full Agents",
	"internal/agents/rulesync/adapter.go:repositorySource":   "rule-sync deploy source with its revision",
	"internal/configgen/candidate.go:Capture":                "publish candidate with its revision",
	"internal/deploy/adapter.go:previewSnapshot":             "deploy preview source with its revision",
	"internal/deploy/orchestrate_adapter.go:publishSource":   "deploy job source with its revision",
	"internal/gateway/draft/adapter.go:Execute":              "gateway draft with full Agents",
	"internal/gateway/installation/adapter.go:savedSnapshot": "gateway installation with full Agents",
	"internal/gateway/onboarding/service.go:Execute":         "gateway onboarding with full Agents",
	"internal/setup/completion.go:checkInstallation":         "setup completion fingerprint of the published profile",
	"internal/setup/completion.go:finish":                    "setup completion fingerprint of the published profile",
	"internal/setup/prepare.go:prepare":                      "setup preparation builds an edit from the published profile",
	"tools/proddiff/baseline/main.go:run":                    "offline proddiff tool: owned System with Agents",
	"tools/proddiff/generator/main.go:generate":              "offline proddiff tool: owned System with Agents",
	"tools/proddiff/generator/bundle.go:deploymentBundle":    "offline proddiff tool: owned System with Agents",
}

type r2CopySite struct{ key, pos string }

// r2CopySites lists every reference to a deep-copy name in a source file,
// keyed by "prefix:function", not only direct calls: a function value, a
// method value or a package-level alias would otherwise hide a deep copy from
// the gate. References in a function (including its closures) are keyed by
// that function; references outside any function by "<package-level>".
// Inside package repo the names are copyRecord and copyValue; elsewhere the
// owned reads of r2OwnedReadNames and calls of a method System with arguments
// (Repository.System; a bare "System" also names fields and types).
func r2CopySites(fset *token.FileSet, name string, data []byte, inRepo bool) ([]r2CopySite, error) {
	file, err := parser.ParseFile(fset, name, data, 0)
	if err != nil {
		return nil, err
	}
	prefix := name
	if inRepo {
		prefix = filepath.Base(name)
	}
	var sites []r2CopySite
	for _, declaration := range file.Decls {
		owner := "<package-level>"
		var root ast.Node = declaration
		if function, ok := declaration.(*ast.FuncDecl); ok {
			if function.Body == nil {
				continue
			}
			owner, root = function.Name.Name, function.Body
		}
		key := prefix + ":" + owner
		if key == "read.go:copyRecord" || key == "resolve.go:copyValue" {
			continue // the helpers themselves
		}
		ast.Inspect(root, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.Ident:
				if inRepo && (node.Name == "copyRecord" || node.Name == "copyValue") || !inRepo && r2OwnedReadNames[node.Name] {
					sites = append(sites, r2CopySite{key, fset.Position(node.Pos()).String()})
				}
			case *ast.CallExpr:
				if selector, ok := node.Fun.(*ast.SelectorExpr); ok && !inRepo && selector.Sel.Name == "System" && len(node.Args) > 0 {
					sites = append(sites, r2CopySite{key, fset.Position(node.Pos()).String()})
				}
			}
			return true
		})
	}
	return sites, nil
}

func TestO10R2DeepCopiesAreExplicit(t *testing.T) {
	root := filepath.Join("..", "..")
	used := map[string]bool{}
	fset := token.NewFileSet()
	for _, top := range []string{"internal", "cmd", "tools"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata" || entry.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			name, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			name = filepath.ToSlash(name)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			inRepo := filepath.ToSlash(filepath.Dir(name)) == "internal/repo"
			sites, err := r2CopySites(fset, name, data, inRepo)
			if err != nil {
				return err
			}
			allowed := r2OwnedReads
			if inRepo {
				allowed = r2OwnedCopies
			}
			for _, site := range sites {
				used[site.key] = true
				if _, ok := allowed[site.key]; !ok {
					t.Errorf("%s: deep copy in %s; borrow the snapshot (Config, Shared, Profile, …) or add a reasoned exemption", site.pos, site.key)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, list := range []map[string]string{r2OwnedCopies, r2OwnedReads} {
		for key, reason := range list {
			if strings.TrimSpace(reason) == "" || !used[key] {
				t.Errorf("empty or stale deep-copy exemption: %s", key)
			}
		}
	}
}

// TestO10R2DeepCopyGateDetectsIndirectUse is the gate's self-test: function
// values, method values, closures and package-level aliases count.
func TestO10R2DeepCopyGateDetectsIndirectUse(t *testing.T) {
	tests := []struct {
		name, file, src string
		inRepo          bool
		want            []string
	}{
		{"api call", "internal/api/x/x.go", "package x\nfunc Get() { r.CloneShared(ctx) }\n", false, []string{"internal/api/x/x.go:Get"}},
		{"method value", "internal/api/x/x.go", "package x\nfunc Get() { clone := r.PublishConfig; clone(ctx, id, false) }\n", false, []string{"internal/api/x/x.go:Get"}},
		{"closure", "internal/api/x/x.go", "package x\nfunc Get() { f := func() { r.CloneProfile(ctx, id) }; f() }\n", false, []string{"internal/api/x/x.go:Get"}},
		{"package-level alias", "internal/api/x/x.go", "package x\nvar clone = (*repo.Repository).CloneBackup\n", false, []string{"internal/api/x/x.go:<package-level>"}},
		{"System call", "internal/api/x/x.go", "package x\nfunc Get() { r.System(ctx) }\n", false, []string{"internal/api/x/x.go:Get"}},
		{"System field", "internal/api/x/x.go", "package x\nfunc Get() { _ = document.System; _ = model.System{} }\n", false, nil},
		{"borrowed read", "internal/api/x/x.go", "package x\nfunc Get() { r.Config(ctx, id, nil); r.Shared(ctx) }\n", false, nil},
		{"repo function value", "internal/repo/x.go", "package repo\nfunc read() { dup := copyRecord[model.Shared]; dup(nil) }\n", true, []string{"x.go:read"}},
		{"repo package-level alias", "internal/repo/x.go", "package repo\nvar dup = copyValue[model.Backup]\n", true, []string{"x.go:<package-level>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, err := r2CopySites(token.NewFileSet(), tt.file, []byte(tt.src), tt.inRepo)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, site := range sites {
				got = append(got, site.key)
				if _, ok := r2OwnedReads[site.key]; ok {
					t.Errorf("%s unexpectedly exempt", site.key)
				}
				if _, ok := r2OwnedCopies[site.key]; ok {
					t.Errorf("%s unexpectedly exempt", site.key)
				}
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("sites = %v, want %v", got, tt.want)
			}
		})
	}
}
