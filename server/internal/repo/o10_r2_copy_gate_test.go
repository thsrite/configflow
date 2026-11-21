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
	"view_guard.go:lend":                           "deep copy only for writers' working copies and Clone*",
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

// r2OwnedReadNames are the repository reads that return a deep copy.
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
}

type r2CopySite struct{ key, pos string }

// r2CopySites lists the calls to names in file, keyed by "file:function".
func r2CopySites(fset *token.FileSet, file *ast.File, prefix string, match func(*ast.CallExpr) bool) []r2CopySite {
	var sites []r2CopySite
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && match(call) {
				sites = append(sites, r2CopySite{prefix + ":" + function.Name.Name, fset.Position(call.Pos()).String()})
			}
			return true
		})
	}
	return sites
}

func r2CalledName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	case *ast.IndexExpr:
		if ident, ok := fun.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

func TestO10R2DeepCopiesAreExplicit(t *testing.T) {
	root := filepath.Join("..", "..")
	used := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
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
		file, err := parser.ParseFile(fset, path, data, 0)
		if err != nil {
			return err
		}
		inRepo := filepath.ToSlash(filepath.Dir(name)) == "internal/repo"
		var sites []r2CopySite
		var allowed map[string]string
		if inRepo {
			allowed = r2OwnedCopies
			sites = r2CopySites(fset, file, filepath.Base(name), func(call *ast.CallExpr) bool {
				called := r2CalledName(call)
				return called == "copyRecord" || called == "copyValue"
			})
			// The helpers themselves.
			kept := sites[:0]
			for _, site := range sites {
				if site.key != "read.go:copyRecord" && site.key != "resolve.go:copyValue" {
					kept = append(kept, site)
				}
			}
			sites = kept
		} else {
			allowed = r2OwnedReads
			sites = r2CopySites(fset, file, name, func(call *ast.CallExpr) bool {
				selector, ok := call.Fun.(*ast.SelectorExpr)
				return ok && r2OwnedReadNames[selector.Sel.Name]
			})
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
	for _, list := range []map[string]string{r2OwnedCopies, r2OwnedReads} {
		for key, reason := range list {
			if strings.TrimSpace(reason) == "" || !used[key] {
				t.Errorf("empty or stale deep-copy exemption: %s", key)
			}
		}
	}
}
