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
	// Member-by-member reads over lists that are not constant literals.
	"internal/api/agents/agent_view_cache.go:liveFingerprint":      "one EncodeMember per live member (core.RuntimeFields + last_heartbeat/status), not the stored history",
	"internal/api/agents/public_agent_adapter.go:liveAgentSecrets": "one EncodeMember per live member (liveMembers), for the credentials they carry",
	"internal/api/agents/public_agent_adapter.go:publicAgentView":  "debt: public Agent projection copies every non-private member one by one (histories projected from typed values)",
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
	// YAML encoders encode a typed record just as whole (their usual YAML
	// node arguments are not records and are not reported).
	"configflow/internal/yamlx.Marshal":  true,
	"configflow/internal/yamlx.Encode":   true,
	"gopkg.in/yaml.v3.Marshal":           true,
	"(*gopkg.in/yaml.v3.Encoder).Encode": true,
	"(*gopkg.in/yaml.v3.Node).Encode":    true,
	// Not gated: model.EncodeMember (one member).
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

// o10MemberReaders read one member of a record named by their second
// argument (model.EncodeMember encodes only that field). They count as a
// member read only when the name is fixed (see o10FixedNames): a computed
// name could walk every member.
var o10MemberReaders = map[string]bool{
	"configflow/internal/model.EncodeMember": true,
}

func o10MemberRead(info *types.Info, fixed *o10FixedNames, call *ast.CallExpr) bool {
	fn := o10CalledFunc(info, call.Fun)
	if fn == nil || !o10MemberReaders[fn.FullName()] || len(call.Args) < 2 {
		return false
	}
	return fixed.fixed(info, call.Args[1])
}

// o10FixedNames are the string variables of one package that only ever hold
// constants: range variables over a literal list of constants (inline, or a
// package-level variable initialised with one and never assigned), and
// parameters of unexported functions that every call in the package passes a
// constant for (the function never used as a value).
type o10FixedNames struct{ vars map[*types.Var]bool }

func (f *o10FixedNames) fixed(info *types.Info, e ast.Expr) bool {
	e = ast.Unparen(e)
	if tv, ok := info.Types[e]; ok && tv.Value != nil {
		return true
	}
	ident, ok := e.(*ast.Ident)
	if !ok || f == nil {
		return false
	}
	v, ok := info.Uses[ident].(*types.Var)
	return ok && f.vars[v]
}

func o10FixedNamesOf(files []*ast.File, info *types.Info) *o10FixedNames {
	assigned := map[*types.Var]bool{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			var lhs []ast.Expr
			switch n := node.(type) {
			case *ast.AssignStmt:
				lhs = n.Lhs
			case *ast.IncDecStmt:
				lhs = []ast.Expr{n.X}
			case *ast.UnaryExpr: // &v lets anything change v
				if n.Op == token.AND {
					lhs = []ast.Expr{n.X}
				}
			}
			for _, l := range lhs {
				if ident, ok := ast.Unparen(l).(*ast.Ident); ok {
					if v, ok := info.Uses[ident].(*types.Var); ok {
						assigned[v] = true
					}
				}
			}
			return true
		})
	}
	constList := func(e ast.Expr) bool {
		lit, ok := ast.Unparen(e).(*ast.CompositeLit)
		if !ok {
			return false
		}
		for _, elt := range lit.Elts {
			if tv, ok := info.Types[elt]; !ok || tv.Value == nil {
				return false
			}
		}
		return true
	}
	lists := map[*types.Var]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if v, ok := info.Defs[name].(*types.Var); ok && i < len(vs.Values) && constList(vs.Values[i]) && !assigned[v] {
						lists[v] = true
					}
				}
			}
		}
	}
	f := &o10FixedNames{vars: map[*types.Var]bool{}}
	// Range variables over constant lists.
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			r, ok := node.(*ast.RangeStmt)
			if !ok || r.Tok != token.DEFINE {
				return true
			}
			over := constList(r.X)
			if ident, ok := ast.Unparen(r.X).(*ast.Ident); ok {
				if v, ok := info.Uses[ident].(*types.Var); ok && lists[v] {
					over = true
				}
			}
			if value, ok := r.Value.(*ast.Ident); ok && over {
				if v, ok := info.Defs[value].(*types.Var); ok && !assigned[v] {
					f.vars[v] = true
				}
			}
			return true
		})
	}
	// Parameters every call passes a constant for.
	called := map[*ast.Ident]bool{}
	notFixed := map[*types.Var]bool{}
	candidates := map[*types.Var]bool{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := ast.Unparen(call.Fun).(*ast.Ident)
			if !ok {
				return true
			}
			fn, ok := info.Uses[ident].(*types.Func)
			if !ok || fn.Exported() || fn.Pkg() == nil {
				return true
			}
			called[ident] = true
			params := fn.Type().(*types.Signature).Params()
			for i := range params.Len() {
				p := params.At(i)
				candidates[p] = true
				if i >= len(call.Args) || call.Ellipsis.IsValid() {
					notFixed[p] = true
					continue
				}
				if tv, ok := info.Types[call.Args[i]]; !ok || tv.Value == nil {
					notFixed[p] = true
				}
			}
			return true
		})
	}
	escaped := map[*types.Func]bool{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok && !called[ident] {
				if fn, ok := info.Uses[ident].(*types.Func); ok {
					escaped[fn] = true
				}
			}
			return true
		})
	}
	for _, file := range files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil {
				continue
			}
			fn, ok := info.Defs[fd.Name].(*types.Func)
			if !ok || escaped[fn] {
				continue
			}
			params := fn.Type().(*types.Signature).Params()
			for i := range params.Len() {
				if p := params.At(i); candidates[p] && !notFixed[p] && !assigned[p] {
					f.vars[p] = true
				}
			}
		}
	}
	return f
}

func o10CalledFunc(info *types.Info, fun ast.Expr) *types.Func {
	var ident *ast.Ident
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		ident = f
	case *ast.SelectorExpr:
		ident = f.Sel
	default:
		return nil
	}
	fn, _ := info.Uses[ident].(*types.Func)
	return fn
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

// o10WholeRecord reports whether t is a whole record, or a value an encoder
// writes one from: a pointer, slice, array or map of one, or a struct whose
// encoded fields (exported or embedded) hold one, so that wrapping a record
// in a response struct or map does not hide its encode.
func o10WholeRecord(t types.Type) bool {
	return o10HoldsRecord(t, map[types.Type]bool{})
}

func o10HoldsRecord(t types.Type, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	switch u := t.(type) {
	case *types.Alias:
		return o10HoldsRecord(types.Unalias(u), seen)
	case *types.Pointer:
		return o10HoldsRecord(u.Elem(), seen)
	case *types.Slice:
		return o10HoldsRecord(u.Elem(), seen)
	case *types.Array:
		return o10HoldsRecord(u.Elem(), seen)
	case *types.Map:
		return o10HoldsRecord(u.Key(), seen) || o10HoldsRecord(u.Elem(), seen)
	case *types.Named:
		obj := u.Obj()
		if obj.Pkg() != nil && o10WholeRecords[obj.Pkg().Path()+"."+obj.Name()] {
			return true
		}
		// A type with its own encoder decides what it writes.
		if o10OwnMarshalJSON(u) {
			return false
		}
		return o10HoldsRecord(u.Underlying(), seen)
	case *types.Struct:
		for i := range u.NumFields() {
			if f := u.Field(i); o10EncodedField(f) && o10HoldsRecord(f.Type(), seen) {
				return true
			}
		}
	}
	return false
}

// o10EncodedField reports whether encoding/json writes the field.
func o10EncodedField(f *types.Var) bool { return f.Exported() || f.Embedded() }

// o10OwnMarshalJSON reports whether a named type declares MarshalJSON itself
// (a method promoted from an embedded field is that field's encoder, and the
// field is checked as a field).
func o10OwnMarshalJSON(t *types.Named) bool {
	for i := range t.NumMethods() {
		if t.Method(i).Name() == "MarshalJSON" {
			return true
		}
	}
	return false
}

// o10ArgRecord returns the type of a whole record an argument carries: the
// argument itself, or a record placed in an encoded slot of a composite
// literal (map, slice, or an exported or embedded struct field) or converted
// to an interface (any(c)).
func o10ArgRecord(info *types.Info, arg ast.Expr) types.Type {
	arg = ast.Unparen(arg)
	if t := info.TypeOf(arg); t != nil && !types.IsInterface(t) && o10WholeRecord(t) {
		return t
	}
	switch e := arg.(type) {
	case *ast.CompositeLit:
		litType := info.TypeOf(e)
		if named, ok := types.Unalias(litType).(*types.Named); ok && o10OwnMarshalJSON(named) {
			return nil
		}
		fields, _ := litType.Underlying().(*types.Struct)
		for i, elt := range e.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if fields != nil {
					if key, ok := kv.Key.(*ast.Ident); ok {
						if f, ok := info.Uses[key].(*types.Var); ok && !o10EncodedField(f) {
							continue
						}
					}
				} else if t := o10ArgRecord(info, kv.Key); t != nil {
					return t
				}
				elt = kv.Value
			} else if fields != nil && i < fields.NumFields() && !o10EncodedField(fields.Field(i)) {
				continue
			}
			if t := o10ArgRecord(info, elt); t != nil {
				return t
			}
		}
	case *ast.UnaryExpr:
		return o10ArgRecord(info, e.X)
	case *ast.CallExpr:
		if tv, ok := info.Types[e.Fun]; ok && tv.IsType() && len(e.Args) == 1 {
			return o10ArgRecord(info, e.Args[0])
		}
	}
	return nil
}

type o10EncodeSite struct{ key, pos, what string }

// o10EncodeSites reports the gated encodes in one type-checked file. Sites
// in a function (closures included) are keyed by that function, others by
// "<package-level>".
func o10EncodeSites(fset *token.FileSet, name string, file *ast.File, info *types.Info, fixed *o10FixedNames) []o10EncodeSite {
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
			if t := o10ArgRecord(info, call.Args[0]); t != nil {
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
			if !ok || o10InterfaceReader(info, call.Fun) || o10MemberRead(info, fixed, call) {
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
				if t := o10ArgRecord(info, arg); t != nil {
					sites = append(sites, o10EncodeSite{name + ":" + owner, fset.Position(arg.Pos()).String(), t.String() + " passed as an interface value to " + types.ExprString(call.Fun)})
				}
			}
			return true
		})
		// An encoder used as a value may be called with anything later; a
		// record's MarshalJSON taken as a method value encodes that record.
		ast.Inspect(root, func(node ast.Node) bool {
			if sel, ok := node.(*ast.SelectorExpr); ok && !called[sel.Sel] && sel.Sel.Name == "MarshalJSON" {
				if t := info.TypeOf(sel.X); t != nil && o10WholeRecord(t) {
					sites = append(sites, o10EncodeSite{name + ":" + owner, fset.Position(sel.Pos()).String(), "method value MarshalJSON of " + t.String()})
				}
				return true
			}
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
		t.Fatalf("the gate needs the go command for export data: %v", err)
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
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
		if _, err := (&types.Config{Importer: imp}).Check(p.ImportPath, fset, files, info); err != nil {
			t.Fatalf("type-check %s: %v", p.ImportPath, err)
		}
		fixed := o10FixedNamesOf(files, info)
		checked++
		for i, file := range files {
			for _, site := range o10EncodeSites(fset, names[i], file, info, fixed) {
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
func EncodeMember(rec any, name string) ([]byte, bool, error) { return nil, false, nil }
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
		{"map wrapper", "func Get(c *model.Config) { json.Marshal(map[string]any{\"config\": c}) }", []string{"Get"}},
		{"struct wrapper", "type reply struct{ Agents []model.Agent }\nfunc Get(a []model.Agent) { json.Marshal(reply{Agents: a}) }", []string{"Get"}},
		{"anonymous struct literal", "func Get(c *model.Config) { json.Marshal(struct{ C any }{c}) }", []string{"Get"}},
		{"embedded record", "type view struct{ *model.Config; Extra int }\nfunc Get(v view) { model.Marshal(v) }", []string{"Get"}},
		{"interface conversion", "func Get(c *model.Config) { model.Marshal(any(c)) }", []string{"Get"}},
		{"wrapper to helper", "func helper(v any) { _ = v }\nfunc Get(a model.Agent) { helper([]any{a}) }", []string{"Get"}},
		{"method value", "func Get(c model.Config) { f := c.MarshalJSON; f() }", []string{"Get"}},
		{"member by constant name", "func Get(a *model.Agent) { model.EncodeMember(a, \"id\") }", nil},
		{"member by named constant", "const field = \"id\"\nfunc Get(a *model.Agent) { model.EncodeMember(a, field) }", nil},
		{"member by computed name", "func Get(a *model.Agent, name string) { model.EncodeMember(a, name) }", []string{"Get"}},
		{"member over an inline constant list", "func Get(a *model.Agent) { for _, k := range []string{\"id\", \"host\"} { model.EncodeMember(a, k) } }", nil},
		{"member over a constant package list", "var keys = []string{\"id\", \"host\"}\nfunc Get(a *model.Agent) { for _, k := range keys { model.EncodeMember(a, k) } }", nil},
		{"member over a reassigned list", "var keys = []string{\"id\"}\nfunc Set(more []string) { keys = more }\nfunc Get(a *model.Agent) { for _, k := range keys { model.EncodeMember(a, k) } }", []string{"Get"}},
		{"member over a computed list", "func Get(a *model.Agent, keys []string) { for _, k := range keys { model.EncodeMember(a, k) } }", []string{"Get"}},
		{"helper always called with constants", "func member(a *model.Agent, k string) { model.EncodeMember(a, k) }\nfunc Get(a *model.Agent) { member(a, \"id\"); member(a, \"host\") }", nil},
		{"helper called with a variable", "func member(a *model.Agent, k string) { model.EncodeMember(a, k) }\nfunc Get(a *model.Agent, k string) { member(a, \"id\"); member(a, k) }", []string{"member"}},
		{"helper used as a value", "func member(a *model.Agent, k string) { model.EncodeMember(a, k) }\nvar hook = member\nfunc Get(a *model.Agent) { member(a, \"id\") }", []string{"member"}},
		{"member reader still sees whole encode", "func Get(a *model.Agent) { model.EncodeMember(a, \"id\"); model.Marshal(a) }", []string{"Get"}},
		{"decode target", "func Get(c *model.Config) { json.Unmarshal(nil, c) }", nil},
		{"members in a map", "func Get(c *model.Config) { json.Marshal(map[string]any{\"name\": c.Name}) }", nil},
		{"unexported field", "type holder struct{ c *model.Config; N int }\nfunc Get(c *model.Config) { json.Marshal(holder{c: c}) }", nil},
		{"self-encoding wrapper", "type view struct{ c *model.Config }\nfunc (view) MarshalJSON() ([]byte, error) { return nil, nil }\nfunc Get(c *model.Config) { json.Marshal(view{c}) }", nil},
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
			info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
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
			for _, site := range o10EncodeSites(fset, "x.go", file, info, o10FixedNamesOf([]*ast.File{file}, info)) {
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
