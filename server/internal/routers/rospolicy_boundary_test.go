package routers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"configflow/internal/routers/routerfake"
)

const r08Gateway = "192.0.2.3"
const r08Target = "198.51.100.0/25"

func r08State() routerfake.State {
	return routerfake.State{PolicyWire: true, Tables: map[string][]routerfake.Row{
		rosTables: {{".id": "*0", "name": "main", "fib": ""}},
		rosRoutes: {
			{".id": "*1", "dst-address": "192.0.2.0/24", "gateway": "bridge", "routing-table": "main", "immediate-gw": "bridge", "active": "true", "connect": "true", "dynamic": "true"},
			{".id": "*2", "dst-address": "198.18.0.0/16", "gateway": r08Gateway, "routing-table": "main", "active": "true", "immediate-gw": r08Gateway + "%bridge", "disabled": "false", "comment": "cf"},
		},
		rosNAT: {{".id": "*3", "chain": "srcnat", "action": "masquerade", "out-interface": "wan", "disabled": "false"}},
	}}
}

func r08Desired(selected bool) *Desired {
	d := &Desired{FakeIPRange: "198.18.0.0/16", ProxyGateway: r08Gateway, ProxyCIDRs: []ProxyCIDR{{"198.18.0.0/16", []string{"fake-ip"}}}, Warnings: []string{}}
	p := &ROSPolicyDesired{Selected: []string{}, Sources: []string{}, CIDRs: []ProxyCIDR{}, Owner: "cf-ros-policy-test"}
	if selected {
		p.Selected = []string{"synthetic"}
		p.Sources = []string{"192.0.2.101/32"}
		p.CIDRs = []ProxyCIDR{{r08Target, []string{"Synthetic"}}}
	} else {
		d.ProxyCIDRs = append(d.ProxyCIDRs, ProxyCIDR{r08Target, []string{"Synthetic"}})
	}
	d.ROSPolicy = p
	return d
}

func r08Plan(ctx context.Context, d *RouterOS, desired *Desired) (*ROSPolicyPlan, error) {
	desired.ROSPolicy.Previous = slices.Clone(d.policyManifest)
	state, err := r08InspectState(ctx, d)
	if err != nil {
		return nil, err
	}
	return BuildROSPolicy(desired, state), nil
}

func r08InspectState(ctx context.Context, d *RouterOS) (*RouterState, error) {
	state, err := d.ReadState(ctx)
	if err != nil {
		return nil, err
	}
	state.ROSPolicy, err = SnapshotROSPolicy(ctx, d)
	if err != nil {
		return nil, err
	}
	state.NatContext.RoutingRules = (*state.ROSPolicy)["/routing/rule"]
	state.NatContext.VRFs = (*state.ROSPolicy)["/ip/vrf"]
	if state.NatContext.Mangle == nil {
		state.NatContext.Mangle = (*state.ROSPolicy)[rosMangle]
	}
	return state, nil
}

type r08Step struct {
	Action   string                    `json:"action"`
	Desired  *Desired                  `json:"desired,omitempty"`
	Mutation *routerfake.ExternalWrite `json:"mutation,omitempty"`
	Prepare  json.RawMessage           `json:"prepare,omitempty"`
	Rows     json.RawMessage           `json:"rows,omitempty"`
	Gateway  string                    `json:"gateway,omitempty"`
	Config   json.RawMessage           `json:"config,omitempty"`
	Router   *Router                   `json:"router,omitempty"`
	Contents map[string]string         `json:"contents,omitempty"`
}
type r08Case struct {
	Name  string
	State routerfake.State `json:"state"`
	Steps []r08Step        `json:"steps"`
}
type r08Error struct {
	Class   string `json:"class"`
	Message string `json:"message"`
}
type r08Output struct {
	Results  []json.RawMessage `json:"results"`
	Error    *r08Error         `json:"error"`
	Manifest json.RawMessage   `json:"manifest"`
}

func r08Run(t *testing.T, f *routerfake.Fake, endpoint string, tc r08Case) r08Output {
	t.Helper()
	f.Reset(tc.State)
	d := NewRouterOS(endpoint, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	ctx := context.Background()
	out := r08Output{Results: []json.RawMessage{}}
	appendResult := func(value any) {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		out.Results = append(out.Results, data)
	}
	var undo *ROSPolicyUndo
	var err error
	for _, step := range tc.Steps {
		if step.Mutation != nil {
			f.ApplyExternal(*step.Mutation)
		}
		switch step.Action {
		case "report":
			step.Desired.ROSPolicy.Previous = slices.Clone(d.policyManifest)
			var state *RouterState
			state, err = r08InspectState(ctx, d)
			if err == nil {
				var report *Report
				report, err = BuildReport(step.Desired, state)
				if err == nil {
					appendResult(report)
				}
			}
		case "legacy":
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(d.policyManifest, &fields)
			manifest, _ := policyManifest(d.policyManifest)
			rename := map[string]string{"CF_Source_Proxy_IP_01": "cfp_0123456789abcdef_src", "CF_Destination_Proxy_IP_01": "cfp_0123456789abcdef_dst", "CF_Bypass_IP_01": "cfp_0123456789abcdef_bypass", "CF_Proxy_Routing_01": "cfp_0123456789abcdef"}
			patches := map[string]map[string]routerfake.Row{}
			for i, item := range manifest.Resources {
				config := cloneRow(item.Config)
				for key, value := range config {
					if name, ok := rename[value]; ok {
						config[key] = name
					}
				}
				if item.Path == rosMangle {
					if config.is("action", "jump") {
						config["routing-mark"] = "main"
					}
					config["invalid"] = "true"
				}
				manifest.Resources[i].Config = config
				patch := cloneRow(config)
				patch["invalid"] = "false"
				if patches[item.Path] == nil {
					patches[item.Path] = map[string]routerfake.Row{}
				}
				patches[item.Path][item.ID] = fakeRow(patch)
			}
			var spec map[string]json.RawMessage
			_ = json.Unmarshal(fields["spec"], &spec)
			delete(spec, "naming_version")
			delete(spec, "policy_version")
			fields["spec"] = mustPolicyJSON(spec)
			fields["resources"] = mustPolicyJSON(manifest.Resources)
			fields["future"] = json.RawMessage(`{"keep":true}`)
			d.SetPolicyManifest(mustPolicyJSON(fields))
			f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: patches}})
		case "sources":
			var value []string
			value, err = SmartROSPolicySources(step.Rows, step.Gateway)
			if err == nil {
				appendResult(value)
			}
		case "inspect":
			var value *ROSPolicyDesired
			value, err = InspectROSPolicyDesired(ctx, step.Config, step.Router, step.Desired, func(item RuleSet, _ *LibraryRule) (string, error) { return step.Contents[item.ID], nil })
			if err == nil {
				appendResult(value)
			}
		case "view":
			var value []ROSPolicyObject
			value, err = ReadPolicyObjects(ctx, d, d.policyManifest)
			if err == nil {
				appendResult(value)
			}
		case "undo":
			err = d.RestoreROSPolicy(ctx, *undo)
			if err == nil {
				appendResult(Row{})
			}
		default:
			var plan *ROSPolicyPlan
			plan, err = r08Plan(ctx, d, step.Desired)
			if err != nil {
				break
			}
			appendResult(plan)
			if step.Action == "plan" || len(plan.Blockers) > 0 || len(plan.Ops) == 0 {
				continue
			}
			raw, _ := json.Marshal(plan.Ops[0])
			var write policyWrite
			_ = json.Unmarshal(raw, &write)
			if len(step.Prepare) > 0 {
				if err = json.Unmarshal(step.Prepare, &write); err != nil {
					t.Fatal(err)
				}
			}
			undo, err = d.applyPolicy(ctx, write)
			if err != nil {
				break
			}
			appendResult(undo)
			if step.Action == "publish" {
				err = VerifyROSPolicy(ctx, d, plan.Check)
			}
			if step.Action == "rollback" {
				err = d.RestoreROSPolicy(ctx, *undo)
				if err == nil {
					appendResult(Row{})
				}
			}
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		class := "RouterError"
		if e, ok := errors.AsType[*RouterError](err); ok {
			switch e.Kind {
			case KindRollback:
				class = "RouterRollbackError"
			case KindUncertainWrite:
				class = "UncertainWrite"
			}
		}
		out.Error = &r08Error{class, err.Error()}
	}
	out.Manifest = slices.Clone(d.policyManifest)
	if len(out.Manifest) == 0 {
		out.Manifest = json.RawMessage(`null`)
	}
	return out
}

func r08Comparable(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func r08Python(t *testing.T, endpoint string, tc r08Case) r08Output {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	if _, err = os.Stat(python); err != nil {
		t.Fatal("required Python reference unavailable: ", err)
	}
	wd, _ := os.Getwd()
	root := filepath.Clean(filepath.Join(wd, "../../.."))
	data, _ := json.Marshal(tc)
	cmd := exec.Command(python, filepath.Join(wd, "testdata/r08_reference.py"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "R08_ENDPOINT="+endpoint)
	cmd.Stdin = bytes.NewReader(data)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("reference: %v %s", err, raw)
	}
	var out r08Output
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("reference output: %v %s", err, raw)
	}
	return out
}

// r08Writes compares actual requests structurally. Read-only console scripts
// are excluded; address/add scripts and remove requests stay in the comparison.
func r08Writes(t *testing.T, calls []routerfake.Call) any {
	t.Helper()
	result := []routerfake.Call{}
	for _, call := range calls {
		if call.Method == "GET" {
			continue
		}
		if call.Path == "/execute" {
			body, _ := call.Body.(map[string]any)
			script, _ := body["script"].(string)
			if !strings.Contains(script, "cf-done:") && !strings.Contains(script, "cf-id:") {
				continue
			}
		}
		result = append(result, call)
	}
	return r08Comparable(t, result)
}

func TestR08PythonBoundary(t *testing.T) {
	f := routerfake.New(nil)
	server := httptest.NewServer(f)
	defer server.Close()
	cases := r08Cases()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			py := r08Python(t, server.URL, tc)
			pyState := f.Snapshot()
			got := r08Run(t, f, server.URL, tc)
			gotState := f.Snapshot()
			a, b := r08Comparable(t, py), r08Comparable(t, got)
			r08TextEvidence(a)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("result differs: %s", r08Difference(a, b, "$"))
			}
			if !reflect.DeepEqual(r08Comparable(t, pyState.Tables), r08Comparable(t, gotState.Tables)) {
				t.Errorf("device tables differ: %s", r08Difference(r08Comparable(t, pyState.Tables), r08Comparable(t, gotState.Tables), "$"))
			}
			if !reflect.DeepEqual(r08Writes(t, pyState.Calls), r08Writes(t, gotState.Calls)) {
				t.Errorf("write sequence differs: %s", r08Difference(r08Writes(t, pyState.Calls), r08Writes(t, gotState.Calls), "$"))
			}
		})
	}
}

// r08TextEvidence rewrites Python's device evidence rows (every "evidence"
// member) the way Row holds them (O17 1.8): non-text values as their JSON
// text, null members absent.
func r08TextEvidence(v any) {
	switch node := v.(type) {
	case []any:
		for _, item := range node {
			r08TextEvidence(item)
		}
	case map[string]any:
		for key, value := range node {
			if evidence, ok := value.(map[string]any); ok && key == "evidence" {
				for _, rows := range evidence {
					list, _ := rows.([]any)
					for _, row := range list {
						fields, _ := row.(map[string]any)
						for name, field := range fields {
							switch field.(type) {
							case nil:
								delete(fields, name)
							case string:
							default:
								data, _ := json.Marshal(field)
								fields[name] = string(data)
							}
						}
					}
				}
				continue
			}
			r08TextEvidence(value)
		}
	}
}

func r08Difference(a, b any, path string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	if am, ok := a.(map[string]any); ok {
		if bm, ok := b.(map[string]any); ok {
			keys := []string{}
			for k := range am {
				keys = append(keys, k)
			}
			for k := range bm {
				if _, ok := am[k]; !ok {
					keys = append(keys, k)
				}
			}
			slices.Sort(keys)
			for _, k := range keys {
				if !reflect.DeepEqual(am[k], bm[k]) {
					return r08Difference(am[k], bm[k], path+"."+k)
				}
			}
		}
	}
	if aa, ok := a.([]any); ok {
		if ba, ok := b.([]any); ok {
			if len(aa) != len(ba) {
				return fmt.Sprintf("%s lengths Python=%d Go=%d", path, len(aa), len(ba))
			}
			for i := range aa {
				if !reflect.DeepEqual(aa[i], ba[i]) {
					return r08Difference(aa[i], ba[i], fmt.Sprintf("%s[%d]", path, i))
				}
			}
		}
	}
	return fmt.Sprintf("%s Python=%v Go=%v", path, a, b)
}

func r08Cases() []r08Case {
	cases := []r08Case{
		{"publish_noop_view", r08State(), []r08Step{{Action: "publish", Desired: r08Desired(true)}, {Action: "plan", Desired: r08Desired(true)}, {Action: "view"}}},
		{"rollback", r08State(), []r08Step{{Action: "rollback", Desired: r08Desired(true)}}},
	}
	reportState := r08State()
	delete(reportState.Tables[rosNAT][0], "out-interface")
	cases = append(cases, r08Case{"report_policy_nat_retirement", reportState, []r08Step{{Action: "report", Desired: r08Desired(true)}, {Action: "publish", Desired: r08Desired(true)}, {Action: "report", Desired: r08Desired(true)}, {Action: "report", Desired: r08Desired(false)}}})
	nestedState := r08State()
	delete(nestedState.Tables[rosNAT][0], "out-interface")
	sources := []string{}
	for _, network := range ClientSourceNetworks(netip.MustParseAddr(r08Gateway)) {
		sources = append(sources, network.String())
	}
	nested := NATListSync{OpHead: OpHead{Kind: "nat_list_sync"}, Comment: NATListComment, SrcList: NATSrcList, DstList: NATDstList, OutInterface: "bridge", Sources: sources, Destinations: []string{"198.18.0.0/16", r08Target}, SrcAdd: sources, DstAdd: []string{"198.18.0.0/16", r08Target}, SrcRemove: []ROSResource{}, DstRemove: []ROSResource{}, Legacy: []ROSResource{}, Stale: []ROSResource{}}
	nestedPreparation := mustPolicyJSON(map[string]any{"prepare_static": []any{nested, Row{"kind": "route_add", "cidr": r08Target, "gateway": r08Gateway, "comment": "cf"}}, "static_checks": []CIDRGateway{{r08Target, r08Gateway}}, "static_nat_checks": NATChecks{r08Gateway, []string{"198.18.0.0/16", r08Target}, []CIDRGateway{}}})
	cases = append(cases, r08Case{"static_nat_list_nested_rollback", nestedState, []r08Step{{Action: "publish", Desired: r08Desired(true)}, {Action: "rollback", Desired: r08Desired(false), Prepare: nestedPreparation}, {Action: "plan", Desired: r08Desired(true)}, {Action: "view"}}})
	for _, action := range []string{"publish", "rollback"} {
		cases = append(cases, r08Case{"legacy_" + action, r08State(), []r08Step{{Action: "publish", Desired: r08Desired(true)}, {Action: "legacy"}, {Action: "view"}, {Action: action, Desired: r08Desired(true)}}})
	}
	state := r08State()
	for i := range 5 {
		state.Tables[rosRoutes] = append(state.Tables[rosRoutes], routerfake.Row{".id": fmt.Sprintf("*%X", 200+i), "dst-address": fmt.Sprintf("198.51.100.%d/32", i), "gateway": r08Gateway, "comment": "cf-old", "routing-table": "main", "disabled": "false", "active": "true", "immediate-gw": r08Gateway + "%bridge"})
	}
	cases = append(cases, r08Case{"batched_route_migration_rollback", state, []r08Step{{Action: "rollback", Desired: r08Desired(true)}}})
	for _, nat := range []bool{false, true} {
		state := r08State()
		if nat {
			delete(state.Tables[rosNAT][0], "out-interface")
		}
		cases = append(cases, r08Case{fmt.Sprintf("replace_withdraw_%t", nat), state, []r08Step{
			{Action: "publish", Desired: r08Desired(true)},
			{Action: "publish", Desired: func() *Desired {
				d := r08Desired(true)
				d.ROSPolicy.CIDRs = append(d.ROSPolicy.CIDRs, ProxyCIDR{"203.0.113.0/24", []string{"Second"}})
				return d
			}()},
			{Action: "view"},
			{Action: "publish", Desired: func() *Desired { d := r08Desired(false); d.ProxyCIDRs = d.ProxyCIDRs[:1]; return d }()},
		}})
		for _, path := range []string{rosTables, rosRoutes, rosMangle, rosNAT} {
			if path == rosNAT && !nat {
				continue
			}
			for _, lost := range []bool{false, true} {
				s := r08State()
				if nat {
					delete(s.Tables[rosNAT][0], "out-interface")
				}
				fault := routerfake.Fault{Method: "PUT", Path: path, Count: 1, Status: 400}
				if lost {
					fault.Status = 0
					fault.AfterCommit = true
					fault.LoseReply = true
				}
				s.Faults = []routerfake.Fault{fault}
				cases = append(cases, r08Case{fmt.Sprintf("create_%s_nat_%t_lost_%t", path, nat, lost), s, []r08Step{{Action: "publish", Desired: r08Desired(true)}}})
			}
		}
	}
	for _, order := range []any{nil, "main,mangle", "user,mangle,main", "mangle,mangle,main", "mangle,unknown,main", []string{"mangle", "main"}, []any{"mangle", true, "main"}} {
		state := r08State()
		state.Tables[rosSettings] = []routerfake.Row{{"policy-rules": order}}
		cases = append(cases, r08Case{fmt.Sprintf("order_%v", order), state, []r08Step{{Action: "plan", Desired: r08Desired(true)}}})
	}
	for _, missing := range []string{"7.21.2", "7.24.5", ""} {
		state := r08State()
		state.Tables[rosSettings] = []routerfake.Row{{}}
		if missing != "" {
			state.Faults = []routerfake.Fault{{Method: "GET", Path: "/system/resource", Status: 200, Body: routerfake.Row{"version": missing}}}
		} else {
			state.Faults = []routerfake.Fault{{Method: "GET", Path: "/system/resource", Status: 400}}
		}
		cases = append(cases, r08Case{"missing_policy_rules_" + missing, state, []r08Step{{Action: "plan", Desired: r08Desired(true)}}})
	}
	for _, kind := range []string{"fasttrack", "prerouting", "vrf", "foreign_route", "static_overlap", "route_migration", "nat_migration", "list_nat", "inverse_nat", "occupied"} {
		state := r08State()
		desired := r08Desired(true)
		switch kind {
		case "fasttrack":
			state.Tables[rosFilter] = []routerfake.Row{{".id": "*F", "chain": "forward", "action": "fasttrack-connection"}}
		case "prerouting":
			state.Tables[rosMangle] = []routerfake.Row{{".id": "*F", "chain": "prerouting", "action": "jump", "jump-target": "foreign"}}
		case "vrf":
			state.Tables["/ip/vrf"] = []routerfake.Row{{".id": "*F", "name": "foreign"}}
		case "foreign_route", "route_migration":
			comment := "handmade"
			if kind == "route_migration" {
				comment = "cf-old"
			}
			state.Tables[rosRoutes] = append(state.Tables[rosRoutes], routerfake.Row{".id": "*F", "dst-address": r08Target, "gateway": r08Gateway, "comment": comment, "active": "true", "disabled": "false", "routing-table": "main", "immediate-gw": r08Gateway + "%bridge"})
		case "static_overlap":
			desired.ProxyCIDRs = append(desired.ProxyCIDRs, desired.ROSPolicy.CIDRs...)
		case "nat_migration", "list_nat", "inverse_nat":
			row := routerfake.Row{".id": "*F", "chain": "srcnat", "action": "accept", "dst-address": "198.51.100.0/24", "src-address": "192.0.2.0/24", "protocol": "tcp", "comment": "cf-old", "disabled": "false"}
			if kind == "list_nat" {
				delete(row, "dst-address")
				row["dst-address-list"] = "legacy"
				state.Tables[rosAddresses] = []routerfake.Row{{".id": "*E", "list": "legacy", "address": "198.51.100.0/24"}}
			}
			if kind == "inverse_nat" {
				row["dst-address"] = "!198.51.100.0/24"
			}
			state.Tables[rosNAT] = append([]routerfake.Row{row}, state.Tables[rosNAT]...)
		case "occupied":
			state.Tables[rosFilter] = []routerfake.Row{{".id": "*F", "src-address-list": "CF_Source_Proxy_IP_01", "chain": "input", "action": "drop"}}
		}
		action := "plan"
		if kind == "foreign_route" || kind == "nat_migration" || kind == "occupied" {
			action = "publish"
		}
		cases = append(cases, r08Case{kind, state, []r08Step{{Action: action, Desired: desired}}})
	}
	for _, path := range []string{rosAddresses + "/*65", rosTables + "/*68", rosRoutes + "/*69", rosMangle + "/*6C"} {
		desired := r08Desired(true)
		desired.ROSPolicy.CIDRs = []ProxyCIDR{{"203.0.113.0/24", []string{"Replacement"}}}
		mutation := &routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "DELETE", Path: path, Count: 1, Status: 400}}}
		cases = append(cases, r08Case{"old_delete_failure_" + path, r08State(), []r08Step{{Action: "publish", Desired: r08Desired(true)}, {Action: "publish", Desired: desired, Mutation: mutation}}})
	}
	for _, nat := range []bool{false, true} {
		for _, condition := range []string{"complete", "inactive", "wrong-nat-source", "protocol-nat", "late-nat"} {
			state := r08State()
			if nat {
				delete(state.Tables[rosNAT][0], "out-interface")
			}
			prepares := []map[string]any{}
			mutation := &routerfake.ExternalWrite{}
			if nat {
				source := "192.0.2.0/24"
				if condition == "wrong-nat-source" {
					source = "192.0.2.102/32"
				}
				// As in R12, the NAT exemption is prepared before the static route.
				prepares = append(prepares, map[string]any{"kind": "nat_add", "src": []string{source}, "dst": []string{r08Target}, "comment": "cf-static", "out_interface": "bridge"})
				if condition == "protocol-nat" || condition == "late-nat" {
					// A pre-existing narrow/late accept cannot replace full source protection.
					prepares = nil
					row := routerfake.Row{".id": "*FA", "chain": "srcnat", "action": "accept", "src-address": "192.0.2.0/24", "dst-address": r08Target, "out-interface": "bridge", "comment": "cf-existing"}
					if condition == "protocol-nat" {
						row["protocol"] = "tcp"
					}
					mutation.ExtraRows = map[string][]routerfake.Row{rosNAT: {row}}
				}
			}
			prepares = append(prepares, map[string]any{"kind": "route_add", "cidr": r08Target, "gateway": r08Gateway, "comment": "cf"})
			if condition == "inactive" {
				mutation.Faults = []routerfake.Fault{{Method: "PUT", Path: rosRoutes, Count: 1, AfterCommit: true, PatchRows: map[string]map[string]routerfake.Row{rosRoutes: {"*6D": {"active": "false"}, "*6F": {"active": "false"}}}}}
			}
			bypass, notRequired := []string{}, []CIDRGateway{}
			if nat {
				bypass = append(bypass, r08Target)
			} else {
				notRequired = append(notRequired, CIDRGateway{r08Target, r08Gateway})
			}
			prepare := mustPolicyJSON(map[string]any{"prepare_static": prepares, "static_checks": []CIDRGateway{{r08Target, r08Gateway}}, "static_nat_checks": NATChecks{r08Gateway, bypass, notRequired}})
			cases = append(cases, r08Case{fmt.Sprintf("static_replacement_nat_%t_%s", nat, condition), state, []r08Step{{Action: "publish", Desired: r08Desired(true)}, {Action: "publish", Desired: r08Desired(false), Mutation: mutation, Prepare: prepare}}})
		}
	}
	for _, drift := range []string{"comment", "unknown-field", "order", "invalid", "next-hop", "fib", "fasttrack", "counter", "moved-list", "foreign-reference"} {
		mutation := &routerfake.ExternalWrite{}
		switch drift {
		case "comment", "unknown-field":
			mutation.PatchRows = map[string]map[string]routerfake.Row{rosMangle: {"*6C": {drift: "external"}}}
		case "order":
			mutation.RemoveIDs = map[string][]string{rosMangle: {"*6A"}}
			mutation.ExtraRows = map[string][]routerfake.Row{rosMangle: {{".id": "*6A", "chain": "CF_Proxy_Routing_01", "action": "return", "src-address-list": "CF_Bypass_IP_01", "comment": "cf-ros-policy-test", "disabled": "false"}}}
		case "invalid":
			mutation.PatchRows = map[string]map[string]routerfake.Row{rosMangle: {"*6C": {"invalid": "true"}}}
		case "next-hop":
			mutation.PatchRows = map[string]map[string]routerfake.Row{rosRoutes: {"*69": {"immediate-gw": r08Gateway + "%wan"}}}
		case "fib":
			mutation.PatchRows = map[string]map[string]routerfake.Row{rosTables: {"*68": {"fib": "false"}}}
		case "fasttrack":
			mutation.ExtraRows = map[string][]routerfake.Row{rosFilter: {{".id": "*F", "chain": "forward", "action": "fasttrack-connection"}}}
		case "counter":
			mutation.PatchRows = map[string]map[string]routerfake.Row{rosMangle: {"*6C": {"bytes": "99999", "packets": "777"}}}
		case "moved-list":
			mutation.PatchRows = map[string]map[string]routerfake.Row{rosAddresses: {"*65": {"list": "moved", "address": "192.0.2.99"}}}
		case "foreign-reference":
			mutation.ExtraRows = map[string][]routerfake.Row{rosFilter: {{".id": "*F", "chain": "input", "action": "drop", "src-address-list": "CF_Source_Proxy_IP_01"}}}
		}
		cases = append(cases, r08Case{"live_drift_" + drift, r08State(), []r08Step{{Action: "publish", Desired: r08Desired(true)}, {Action: "plan", Desired: r08Desired(true), Mutation: mutation}, {Action: "view"}}})
	}
	for _, count := range []int{200, 201, 4096} {
		desired := r08Desired(true)
		desired.ROSPolicy.CIDRs = []ProxyCIDR{}
		for i := range count {
			desired.ROSPolicy.CIDRs = append(desired.ROSPolicy.CIDRs, ProxyCIDR{fmt.Sprintf("198.18.%d.%d/32", i/256, i%256), []string{"Synthetic"}})
		}
		cases = append(cases, r08Case{fmt.Sprintf("large_bundle_%d", count), r08State(), []r08Step{{Action: "rollback", Desired: desired}}})
	}
	cases = append(cases, r08Case{"longest_prefix_sources", r08State(), []r08Step{{Action: "sources", Gateway: r08Gateway, Rows: json.RawMessage(`[{"ip":"192.0.2.0/24","mode":"smart"},{"ip":"192.0.2.0/25","mode":"direct"},{"ip":"192.0.2.101","mode":"smart"},{"ip":"192.0.2.151","mode":"direct"},{"ip":"2001:db8::/32","mode":"smart"}]`)}}})
	for _, policy := range []string{"Proxy", "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "Only direct"} {
		for _, problem := range []string{"none", "missing", "disabled", "deleted"} {
			config := map[string]any{"rule_configs": []map[string]any{{"id": "selected", "itemType": "ruleset", "name": "已停用，没有本地缓存，未计入", "policy": policy, "enabled": true}}, "oxidns": map[string]any{"device_policies": []Row{{"ip": "192.0.2.101", "mode": "smart"}}}, "proxy_groups": []map[string]any{{"name": "Only direct", "manual_nodes": []string{"DIRECT", "REJECT"}}}}
			contents := map[string]string{"selected": "IP-CIDR,198.51.100.0/24,no-resolve\n"}
			if problem == "missing" {
				contents["selected"] = ""
			}
			if problem == "disabled" {
				config["rule_configs"].([]map[string]any)[0]["enabled"] = false
			}
			if problem == "deleted" {
				config["rule_configs"] = []Row{}
			}
			router := &Router{RouterSettings: RouterSettings{ID: "synthetic-router", RosPolicyRuleSets: []string{"selected"}}}
			cases = append(cases, r08Case{fmt.Sprintf("inspect_%s_%s", policy, problem), r08State(), []r08Step{{Action: "inspect", Config: mustPolicyJSON(config), Router: router, Desired: r08Desired(true), Contents: contents}}})
		}
	}
	return cases
}
