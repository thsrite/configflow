package server

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// O10 R11 source gate: HTTP handlers (internal/api/**) and the packages that
// run background loops must not encode a whole configuration or Agent record
// (Document, System, Shared, Profile, Config, Agent or a list of Agents) with
// a generic encoder or redaction walker. Such an encode costs tens of MB on
// production data (the model codec allocates about 40 bytes per encoded byte)
// and was the root cause of most O10 findings; read the members a route needs
// instead, or take a generation-cached projection (repo config credentials,
// Agent views). Deep copies are gated separately and repository-wide
// (agents.TestAgentDeepCopyBoundary, repo r2 copy gate).
//
// The check is type-aware: it type-checks every package in scope against the
// compiler's export data, so an alias, a method value or a function variable
// holding an encoder does not hide a call. Any reference to an encoder that is
// not a direct call (a function value) is reported too, because the gate
// cannot see what it is later called with.

// o10WholeEncodeAllowed lists the functions allowed to encode a whole record,
// as "path:function" relative to server/, each with its reason. Entries
// marked "debt:" are read paths that existed when the gate was added (none on
// an O10-measured route above its ceiling); they may be removed, not copied.
var o10WholeEncodeAllowed = map[string]string{
	// Publish and deploy sources: the whole configuration is the content.
	"internal/agents/dnsfollow/adapter.go:config":          "DNS-follow deploy source (whole configuration is published)",
	"internal/agents/dnsfollow/adapter.go:snapshot":        "DNS-follow publish snapshot with its revision",
	"internal/agents/dnsha/adapter.go:source":              "OxiDNS HA publish source",
	"internal/agents/ipv6/adapter.go:snapshot":             "IPv6 verification snapshot",
	"internal/agents/rulesync/adapter.go:repositorySource": "rule-sync deploy source with its revision",
	"internal/api/routers/adapters.go:publishSnapshot":     "router publish snapshot with its revision",
	"internal/routers/publish.go:Publish":                  "router publish: the configuration written to the router",
	// Write transactions on the legacy JSON boundary.
	"internal/agents/dnsfollow/adapter.go:saveDraft":                "write transaction (profile draft)",
	"internal/agents/metrics/flush.go:SetFlushInterval":             "settings write transaction",
	"internal/agents/metrics/traffic_native.go:SetTrafficRetention": "settings write transaction",
	"internal/api/config/custom.go:saveCustom":                      "write: change baseline of the custom-config editor",
	"internal/api/clientdevices/handlers.go:devices":                "write transaction (fixed-IP apply)",
	"internal/api/nodes/adapter.go:Wire":                            "node deletion planner inside the write transaction",
	"internal/api/profiles/profiles.go:importClient":                "write: client profile import",
	"internal/routers/management.go:replaceManagementRouter":        "router write transaction",
	"internal/routers/management.go:replaceManagementRawRouter":     "router write transaction",
	"internal/routers/management_repo.go:managementTransaction":     "router write transaction (legacy JSON boundary)",
	// Rare paths.
	"internal/agents/dnsfollow/adapter.go:FailureMessage":  "error path only: credentials of every Agent before a refusal is shown",
	"internal/api/agents/client_binding.go:warnClientPeer": "rate-limited warning log (once per Agent and interval)",
	"internal/routers/inspect.go:inspectionDNS":            "manual router inspection, only when the DNS Agent has no typed DNS settings",
	"internal/agents/rulesync/adapter.go:profileAgents":    "history-free Agent view (repo.AgentHistoryFields cleared) for rule-sync hooks",
	// Generic JSON helpers (native, stored, same, raw) taking `any`.
	"internal/api/rulelibrary/adapter.go:editWithProfiles":    "write transaction: rule library edit through the generic JSON helper",
	"internal/api/rulelibrary/adapter.go:editSystem":          "write transaction through the generic JSON helper",
	"internal/api/rulelibrary/adapter.go:editShared":          "write transaction through the generic JSON helper",
	"internal/api/rules/compose_adapter.go:editSharedProfile": "write transaction through the generic JSON helper",
	"internal/api/rules/adapter.go:editProfile":               "write transaction through the generic JSON helper",
	"internal/api/proxygroups/adapter.go:edit":                "write transaction through the generic JSON helper",
	"internal/api/proxygroups/adapter.go:relink":              "write transaction through the generic JSON helper",
	"internal/api/ipv6/routes.go:operation":                   "IPv6 check compares two publish snapshots (rare, operator action)",
	"internal/agents/ipv6/service.go:Host":                    "debt: IPv6 host check sends the Agent as a generic document",
	"internal/api/aggregations/adapter.go:resourceConfig":     "debt: aggregation preview/count read the generic configuration (the provider download does not)",
	"internal/api/rules/adapter.go:loadProfile":               "debt: rules page reads the generic configuration",
	"internal/api/rulelibrary/adapter.go:sharedConfig":        "debt: rule library page reads the generic shared record",
	"internal/api/proxygroups/adapter.go:sharedConfig":        "debt: proxy groups page reads the generic shared record",
	"internal/api/proxygroups/adapter.go:load":                "debt: proxy groups page reads the generic configuration",
	// Debt: read paths still encoding whole records.
	"internal/agents/dnsfollow/adapter.go:secrets":                     "debt: DNS-follow credential collection per Agent (whole record)",
	"internal/agents/ipv6/adapter.go:agentDocument":                    "debt: IPv6 verification reads the Agent as a generic document",
	"internal/api/agents/client_tools_adapter.go:clientRuleDeps":       "debt: client tools (not an O10 route) read the generic configuration",
	"internal/api/agents/client_tools_adapter.go:clientTargetsPayload": "debt: client tools (not an O10 route) read the generic configuration",
	"internal/api/agents/public_agent_adapter.go:publicAgentModel":     "debt: client-tools Agent projection (O10 routes use the member-wise projection)",
	"internal/api/clientdevices/handlers.go:config":                    "debt: client devices page reads the generic configuration",
	"internal/api/config/custom.go:readCustom":                         "debt: custom-config editor reads the generic profile",
	"internal/routers/management.go:Encoded":                           "debt: manual router inspection reads the generic configuration",
	"internal/routers/pending.go:pendingTargets":                       "debt: upstream-routers pending check (O10 route, under its ceiling)",
	"internal/routers/rule_sync_adapter.go:syncConfig":                 "debt: router rule sync reads the generic configuration",
	"internal/routers/workspace.go:encodedRuleSets":                    "debt: policy-settings workspace reads the generic profile (O10 route, under its ceiling)",
}

// o10LoopPackages are the packages that register background loops
// (lifecycle.RegisterPeriodic), besides internal/lifecycle itself.
var o10LoopPackages = []string{
	"internal/agents/dnsfollow", "internal/agents/dnsha", "internal/agents/ipv6", "internal/agents/metrics",
	"internal/agents/monitor", "internal/agents/rulesync", "internal/discovery", "internal/refresh",
	"internal/routers", "internal/substore",
}

// o10Encoders are the generic encoders and redaction walkers that encode
// their whole argument.
var o10Encoders = map[string]bool{
	"configflow/internal/model.Marshal":              true,
	"configflow/internal/model.EncodeRecord":         true,
	"configflow/internal/model.EncodeMembers":        true,
	"configflow/internal/jsonresp.Encode":            true,
	"configflow/internal/jsonresp.EncodeUTF8":        true,
	"configflow/internal/redact.CollectCredentials":  true,
	"configflow/internal/redact.SanitizeCredentials": true,
	"configflow/internal/redact.SanitizeWithSecrets": true,
	"encoding/json.Marshal":                          true,
	"encoding/json.MarshalIndent":                    true,
	"(*encoding/json.Encoder).Encode":                true,
	"encoding/json/v2.Marshal":                       true,
	// Not gated: model.EncodeMember (one member), yamlx.Marshal (YAML nodes).
}

// o10InterfaceReaders take a record as an interface value without encoding
// it: decoding into it, or reading its provenance.
var o10InterfaceReaders = map[string]bool{
	"encoding/json.Unmarshal":                  true,
	"configflow/internal/jsondecode.Unmarshal": true,
	"configflow/internal/model.Issues":         true,
	"configflow/internal/model.IssueUnder":     true,
	"configflow/internal/model.SwitchEnabled":  true,
}

func o10InterfaceReader(info *types.Info, fun ast.Expr) bool {
	var ident *ast.Ident
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		ident = f
	case *ast.SelectorExpr:
		ident = f.Sel
	default:
		return false
	}
	fn, ok := info.Uses[ident].(*types.Func)
	return ok && o10InterfaceReaders[fn.FullName()]
}

// o10WholeRecords are the record types whose whole encode is gated.
var o10WholeRecords = map[string]bool{
	"configflow/internal/model.Document": true, "configflow/internal/model.System": true,
	"configflow/internal/model.Shared": true, "configflow/internal/model.Profile": true,
	"configflow/internal/model.Config": true, "configflow/internal/model.Agent": true,
}

// o10WholeRecord reports whether t is a whole record, a pointer to one or a
// slice/array of either.
func o10WholeRecord(t types.Type) bool {
	for {
		switch u := t.(type) {
		case *types.Pointer:
			t = u.Elem()
			continue
		case *types.Slice:
			t = u.Elem()
			continue
		case *types.Array:
			t = u.Elem()
			continue
		case *types.Alias:
			t = types.Unalias(u)
			continue
		case *types.Named:
			obj := u.Obj()
			return obj.Pkg() != nil && o10WholeRecords[obj.Pkg().Path()+"."+obj.Name()]
		}
		return false
	}
}

type o10EncodeSite struct{ key, pos, what string }

// o10EncodeSites reports the gated encodes in one type-checked file. Sites
// in a function (closures included) are keyed by that function, others by
// "<package-level>".
func o10EncodeSites(fset *token.FileSet, name string, file *ast.File, info *types.Info) []o10EncodeSite {
	var sites []o10EncodeSite
	for _, decl := range file.Decls {
		owner := "<package-level>"
		var root ast.Node = decl
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Body == nil {
				continue
			}
			owner, root = fn.Name.Name, fn.Body
		}
		called := map[*ast.Ident]bool{}
		encoded := map[*ast.CallExpr]bool{}
		ast.Inspect(root, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			fun := ast.Unparen(call.Fun)
			if index, ok := fun.(*ast.IndexExpr); ok { // explicit instantiation
				fun = index.X
			}
			var ident *ast.Ident
			switch f := fun.(type) {
			case *ast.Ident:
				ident = f
			case *ast.SelectorExpr:
				ident = f.Sel
			default:
				return true
			}
			fn, ok := info.Uses[ident].(*types.Func)
			if !ok {
				return true
			}
			called[ident] = true
			full := fn.FullName()
			if strings.HasSuffix(full, ".MarshalJSON") {
				if sel, ok := fun.(*ast.SelectorExpr); ok && o10WholeRecord(info.TypeOf(sel.X)) {
					sites = append(sites, o10EncodeSite{name + ":" + owner, fset.Position(call.Pos()).String(), "MarshalJSON of " + info.TypeOf(sel.X).String()})
				}
				return true
			}
			if !o10Encoders[full] || len(call.Args) == 0 {
				return true
			}
			if t := info.TypeOf(call.Args[0]); t != nil && o10WholeRecord(t) {
				encoded[call] = true
				sites = append(sites, o10EncodeSite{name + ":" + owner, fset.Position(call.Pos()).String(), full + " of " + t.String()})
			}
			return true
		})
		// A whole record handed to any function as an interface value (a
		// generic helper like `stored(value any, …)`) is reported too: the
		// gate cannot see what the helper does with it.
		ast.Inspect(root, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || encoded[call] {
				return true
			}
			if tv, ok := info.Types[call.Fun]; !ok || tv.IsType() {
				return true
			}
			sig, ok := types.Unalias(info.TypeOf(call.Fun)).Underlying().(*types.Signature)
			if !ok || o10InterfaceReader(info, call.Fun) {
				return true
			}
			for i, arg := range call.Args {
				var param types.Type
				switch {
				case sig.Variadic() && i >= sig.Params().Len()-1:
					last := sig.Params().At(sig.Params().Len() - 1).Type()
					if call.Ellipsis.IsValid() {
						param = last
					} else if slice, ok := last.(*types.Slice); ok {
						param = slice.Elem()
					}
				case i < sig.Params().Len():
					param = sig.Params().At(i).Type()
				}
				if param == nil || !types.IsInterface(param) {
					continue
				}
				if t := info.TypeOf(arg); t != nil && !types.IsInterface(t) && o10WholeRecord(t) {
					sites = append(sites, o10EncodeSite{name + ":" + owner, fset.Position(arg.Pos()).String(), t.String() + " passed as an interface value to " + types.ExprString(call.Fun)})
				}
			}
			return true
		})
		// An encoder used as a value may be called with anything later.
		ast.Inspect(root, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok || called[ident] {
				return true
			}
			if fn, ok := info.Uses[ident].(*types.Func); ok && o10Encoders[fn.FullName()] {
				sites = append(sites, o10EncodeSite{name + ":" + owner, fset.Position(ident.Pos()).String(), "function value " + fn.FullName()})
			}
			return true
		})
	}
	return sites
}

type o10Package struct {
	ImportPath, Dir, Export string
	GoFiles                 []string
	ImportMap               map[string]string
	DepOnly                 bool
}

// o10ListPackages runs `go list -export -deps` for the gate's scope, with
// -race when this test binary has it so the build cache is reused.
func o10ListPackages(t *testing.T, root string) []o10Package {
	t.Helper()
	args := []string{"list", "-e", "-export", "-deps", "-json=ImportPath,Dir,Export,GoFiles,ImportMap,DepOnly,Error"}
	if o10RaceEnabled {
		args = append(args, "-race")
	}
	args = append(args, "./internal/api/...")
	for _, p := range o10LoopPackages {
		args = append(args, "./"+p)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var packages []o10Package
	decoder := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var p o10Package
		if err := decoder.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		packages = append(packages, p)
	}
	return packages
}

func TestO10WholeRecordEncodeBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks the API and loop packages")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not available")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	packages := o10ListPackages(t, root)
	exports := map[string]string{}
	for _, p := range packages {
		exports[p.ImportPath] = p.Export
	}
	fset := token.NewFileSet()
	used := map[string]bool{}
	checked := 0
	for _, p := range packages {
		if p.DepOnly {
			continue
		}
		imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
			if mapped, ok := p.ImportMap[path]; ok {
				path = mapped
			}
			export := exports[path]
			if export == "" {
				return nil, fmt.Errorf("no export data for %s", path)
			}
			return os.Open(export)
		})
		var files []*ast.File
		var names []string
		for _, name := range p.GoFiles {
			file, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(root, filepath.Join(p.Dir, name))
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, file)
			names = append(names, filepath.ToSlash(rel))
		}
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}}
		if _, err := (&types.Config{Importer: imp}).Check(p.ImportPath, fset, files, info); err != nil {
			t.Fatalf("type-check %s: %v", p.ImportPath, err)
		}
		checked++
		for i, file := range files {
			for _, site := range o10EncodeSites(fset, names[i], file, info) {
				used[site.key] = true
				if _, ok := o10WholeEncodeAllowed[site.key]; !ok {
					t.Errorf("%s: %s in %s; read the members the route needs or use a generation-cached projection (O10 R11), or add a reasoned exemption", site.pos, site.what, site.key)
				}
			}
		}
	}
	if checked < 30 {
		t.Fatalf("only %d packages checked", checked)
	}
	var stale []string
	for key := range o10WholeEncodeAllowed {
		if !used[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("stale whole-encode exemption %s", key)
	}
}

// The gate sees through the usual ways of hiding an encode.
func TestO10WholeRecordEncodeBoundaryDetects(t *testing.T) {
	model := `package model
type Config struct{ Name string }
type Agent struct{ ID string }
type Node struct{ Name string }
func (Config) MarshalJSON() ([]byte, error) { return nil, nil }
func Marshal(v any) ([]byte, error) { return nil, nil }
`
	tests := []struct {
		name, src string
		want      []string
	}{
		{"direct", "func Get(c *model.Config) { model.Marshal(c) }", []string{"Get"}},
		{"agent list", "func List(a []model.Agent) { json.Marshal(a) }", []string{"List"}},
		{"method", "func Get(c model.Config) { c.MarshalJSON() }", []string{"Get"}},
		{"closure", "func Get(c *model.Config) { f := func() { model.Marshal(*c) }; f() }", []string{"Get"}},
		{"function value", "func Get(c *model.Config) { enc := model.Marshal; enc(c) }", []string{"Get"}},
		{"package-level value", "var enc = json.Marshal", []string{"<package-level>"}},
		{"alias type", "type cfg = model.Config\nfunc Get(c *cfg) { model.Marshal(c) }", []string{"Get"}},
		{"named slice", "type agents []model.Agent\nfunc Get(a agents) { model.Marshal([]model.Agent(a)) }", []string{"Get"}},
		{"generic helper", "func helper(v any) { _ = v }\nfunc Get(c *model.Config) { helper(c) }", []string{"Get"}},
		{"variadic helper", "func helper(vs ...any) { _ = vs }\nfunc Get(a model.Agent) { helper(1, a) }", []string{"Get"}},
		{"decode target", "func Get(c *model.Config) { json.Unmarshal(nil, c) }", nil},
		{"member", "func Get(c *model.Config) { model.Marshal(c.Name) }", nil},
		{"small record", "func Get(n model.Node) { model.Marshal(n) }", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			modelFile, err := parser.ParseFile(fset, "model.go", model, 0)
			if err != nil {
				t.Fatal(err)
			}
			modelPkg, err := (&types.Config{}).Check("configflow/internal/model", fset, []*ast.File{modelFile}, nil)
			if err != nil {
				t.Fatal(err)
			}
			src := "package x\nimport (\n\t\"configflow/internal/model\"\n\t\"encoding/json\"\n)\nvar _ = json.Valid\nvar _ = model.Marshal\n" + tt.src + "\n"
			file, err := parser.ParseFile(fset, "x.go", src, 0)
			if err != nil {
				t.Fatal(err)
			}
			std := importer.Default()
			info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}}
			conf := types.Config{Importer: importerFunc(func(path string) (*types.Package, error) {
				if path == "configflow/internal/model" {
					return modelPkg, nil
				}
				return std.Import(path)
			})}
			if _, err := conf.Check("x", fset, []*ast.File{file}, info); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, site := range o10EncodeSites(fset, "x.go", file, info) {
				owner := strings.TrimPrefix(site.key, "x.go:")
				if owner == "<package-level>" && strings.Contains(site.what, "model.Marshal") {
					continue // the probe's own `var _ = model.Marshal`
				}
				if !slices.Contains(got, owner) { // one site may match two rules
					got = append(got, owner)
				}
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("sites = %v, want %v", got, tt.want)
			}
		})
	}
}

type importerFunc func(string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }
