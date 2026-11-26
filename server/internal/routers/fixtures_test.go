package routers

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/routers/device"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"configflow/internal/routers/routeedit"
	"configflow/internal/routers/routerfake"
	"configflow/internal/routers/spec"
)

const r08Gateway = "192.0.2.3"
const r08Target = "198.51.100.0/25"

type r06Case struct {
	DuplicateName  bool                   `json:"-"`
	Timeout        bool                   `json:"timeout,omitempty"`
	Name           string                 `json:"-"`
	Vendor         string                 `json:"vendor"`
	Action         string                 `json:"action"`
	Values         *routeedit.RouteValues `json:"values,omitempty"`
	Payload        json.RawMessage        `json:"payload,omitempty"`
	Router         spec.Router            `json:"router"`
	ExpectedChange map[string]any         `json:"expected_change,omitempty"`
	ROS            routerfake.State       `json:"-"`
	IK             routerfake.IkuaiState  `json:"-"`
}
type r06Error struct {
	Class   string `json:"class"`
	Message string `json:"message"`
}
type r06Outcome struct {
	Result  any               `json:"result"`
	Error   *r06Error         `json:"error"`
	Records []json.RawMessage `json:"records"`
}

func r06JSON(v any) json.RawMessage { data, _ := json.Marshal(v); return data }
func r06Normalize(v any) any        { var out any; _ = json.Unmarshal(r06JSON(v), &out); return out }
func r06Equivalent(a, b any) bool   { return reflect.DeepEqual(r06Normalize(a), r06Normalize(b)) }
func r06StripHashes(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			if (k == "revision" && x["cidr"] != nil && x["snapshot"] == nil) || (k == "fingerprint" && x["summary"] != nil && x["action"] != nil) {
				if s, ok := item.(string); ok && len(s) == 64 {
					x[k] = "<opaque-sha256>"
				}
			} else if k != "snapshot" {
				r06StripHashes(item)
			}
		}
	case []any:
		for _, item := range x {
			r06StripHashes(item)
		}
	}
}
func r06Base(vendor, action string) r06Case {
	tc := r06Case{Vendor: vendor, Action: action, Values: &routeedit.RouteValues{CIDR: "198.51.100.0/24", Gateway: "192.0.2.3", Enabled: true, Comment: "用户 & backup\n备注"}, Router: spec.Router{RouterSettings: spec.RouterSettings{ID: "r1", Type: vendor, Endpoint: "http://example.com", Username: "synthetic-admin", VerifyTLS: true}}}
	tc.ROS.Tables = map[string][]routerfake.Row{"/ip/route": {{".id": "*10", "dst-address": "203.0.113.0/24", "gateway": "192.0.2.1", "routing-table": "main", "disabled": "false", "comment": "office", "distance": "17", "scope": "20", "target-scope": "12", "check-gateway": "ping", "pref-src": "192.0.2.2", "suppress-hw-offload": "true", "dynamic": "false", "static": "true", "active": "true", "blackhole": "false", "immediate-gw": "192.0.2.1%ether1", "hw-offloaded": "false"}}}
	tc.IK.Tables = map[string][]routerfake.Row{"static_rt": {{"id": 1, "enabled": "yes", "tagname": "RT_1", "ip_type": "4", "interface": "lan4", "dst_addr": "203.0.113.0", "gateway": "192.0.2.1", "netmask": "255.255.255.0", "prio": 7, "comment": "office", "status": 1}}}
	return tc
}

func r08State() routerfake.State {
	return routerfake.State{PolicyWire: true, Tables: map[string][]routerfake.Row{
		ros.PathTables: {{".id": "*0", "name": "main", "fib": ""}},
		ros.PathRoutes: {
			{".id": "*1", "dst-address": "192.0.2.0/24", "gateway": "bridge", "routing-table": "main", "immediate-gw": "bridge", "active": "true", "connect": "true", "dynamic": "true"},
			{".id": "*2", "dst-address": "198.18.0.0/16", "gateway": r08Gateway, "routing-table": "main", "active": "true", "immediate-gw": r08Gateway + "%bridge", "disabled": "false", "comment": "cf"},
		},
		ros.PathNAT: {{".id": "*3", "chain": "srcnat", "action": "masquerade", "out-interface": "wan", "disabled": "false"}},
	}}
}

func r08Desired(selected bool) *spec.Desired {
	d := &spec.Desired{FakeIPRange: "198.18.0.0/16", ProxyGateway: r08Gateway, ProxyCIDRs: []spec.ProxyCIDR{{CIDR: "198.18.0.0/16", Sources: []string{"fake-ip"}}}, Warnings: []string{}}
	p := &spec.ROSPolicyDesired{Selected: []string{}, Sources: []string{}, CIDRs: []spec.ProxyCIDR{}, Owner: "cf-ros-policy-test"}
	if selected {
		p.Selected = []string{"synthetic"}
		p.Sources = []string{"192.0.2.101/32"}
		p.CIDRs = []spec.ProxyCIDR{{CIDR: r08Target, Sources: []string{"Synthetic"}}}
	} else {
		d.ProxyCIDRs = append(d.ProxyCIDRs, spec.ProxyCIDR{CIDR: r08Target, Sources: []string{"Synthetic"}})
	}
	d.ROSPolicy = p
	return d
}

func r08Plan(ctx context.Context, d *RouterOS, desired *spec.Desired) (*rospolicy.ROSPolicyPlan, error) {
	desired.ROSPolicy.Previous = slices.Clone(d.PolicyManifest())
	state, err := r08InspectState(ctx, d)
	if err != nil {
		return nil, err
	}
	return rospolicy.BuildROSPolicy(desired, state.NatContext, state.ROSPolicy), nil
}

func r08InspectState(ctx context.Context, d *RouterOS) (*RouterState, error) {
	state, err := d.ReadState(ctx)
	if err != nil {
		return nil, err
	}
	state.ROSPolicy, err = rospolicy.SnapshotROSPolicy(ctx, d.Client)
	if err != nil {
		return nil, err
	}
	state.NatContext.RoutingRules = (*state.ROSPolicy)["/routing/rule"]
	state.NatContext.VRFs = (*state.ROSPolicy)["/ip/vrf"]
	if state.NatContext.Mangle == nil {
		state.NatContext.Mangle = (*state.ROSPolicy)[ros.PathMangle]
	}
	return state, nil
}

type r08Step struct {
	Action   string                    `json:"action"`
	Desired  *spec.Desired             `json:"desired,omitempty"`
	Mutation *routerfake.ExternalWrite `json:"mutation,omitempty"`
	Prepare  json.RawMessage           `json:"prepare,omitempty"`
	Rows     json.RawMessage           `json:"rows,omitempty"`
	Gateway  string                    `json:"gateway,omitempty"`
	Config   json.RawMessage           `json:"config,omitempty"`
	Router   *spec.Router              `json:"router,omitempty"`
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

type r09Step struct {
	Action       string                    `json:"action"`
	Settings     *spec.DNSGuard            `json:"settings,omitempty"`
	Desired      *spec.Desired             `json:"desired,omitempty"`
	Mutation     *routerfake.ExternalWrite `json:"mutation,omitempty"`
	AfterPreview *routerfake.ExternalWrite `json:"after_preview,omitempty"`
}
type r09Case struct {
	Name        string           `json:"name"`
	State       routerfake.State `json:"state"`
	Desired     *spec.Desired    `json:"desired"`
	Settings    spec.DNSGuard    `json:"settings"`
	Previous    json.RawMessage  `json:"previous"`
	PolicyCIDRs []spec.ProxyCIDR `json:"policy_cidrs,omitempty"`
	Steps       []r09Step        `json:"steps"`
}
type r09Outcome struct {
	Steps  []r09StepResult             `json:"steps"`
	Tables map[string][]routerfake.Row `json:"tables"`
	Writes []routerfake.Call           `json:"writes"`
}
type r09StepResult struct {
	Plan     *dnsguard.Plan  `json:"plan"`
	Undo     json.RawMessage `json:"undo"`
	Error    *r09Error       `json:"error"`
	Manifest json.RawMessage `json:"manifest"`
}
type r09Error struct {
	Class   string `json:"class"`
	Message string `json:"message"`
}

func r09Raw(v any) json.RawMessage { data, _ := json.Marshal(v); return data }
func r09Copy[T any](v T) T         { var out T; _ = json.Unmarshal(r09Raw(v), &out); return out }
func r09Base(name string) r09Case {
	return r09Case{Name: name, State: routerfake.State{Tables: map[string][]routerfake.Row{
		ros.PathRoutes: {{".id": "*10", "dst-address": "192.0.2.0/24", "gateway": "bridge", "immediate-gw": "bridge", "connect": "true", "active": "true", "dynamic": "true", "distance": "0", "routing-table": "main"}},
		ros.PathNAT:    {{".id": "*20", "chain": "dstnat", "action": "dst-nat", "protocol": "udp", "dst-port": "53", "to-addresses": "192.0.2.2", "comment": "old-dns", "disabled": "false"}, {".id": "*21", "chain": "srcnat", "action": "masquerade", "out-interface": "ether1", "disabled": "false"}},
	}}, Desired: &spec.Desired{FakeIPRange: "198.18.0.0/16", ProxyGateway: "192.0.2.4", ProxyCIDRs: []spec.ProxyCIDR{{CIDR: "198.18.0.0/16", Sources: []string{"fake-ip"}}}, DNSSettings: &device.DNSSettings{Server: "192.0.2.2"}}, Settings: spec.DNSGuard{Enabled: true, BlockDoT: true, Failover: true, Bypass: []string{"192.0.2.50/32"}}, Steps: []r09Step{{Action: "apply"}, {Action: "plan"}}}
}

func r09Go(t *testing.T, tc r09Case, f *routerfake.Fake, endpoint string) r09Outcome {
	t.Helper()
	f.Reset(tc.State)
	d := NewRouterOS(endpoint, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	previous := ros.GuardRaw(tc.Previous)
	settings := tc.Settings
	desired := tc.Desired
	out := r09Outcome{Steps: []r09StepResult{}, Writes: []routerfake.Call{}}
	for _, step := range tc.Steps {
		if step.Mutation != nil {
			f.ApplyExternal(*step.Mutation)
		}
		if step.Settings != nil {
			settings = *step.Settings
		}
		if step.Desired != nil {
			desired = step.Desired
		}
		d.SetDNSGuardManifest(previous)
		state, err := d.ReadState(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		state.DNSGuard, err = dnsguard.Read(context.Background(), d.Client)
		if err != nil {
			t.Fatal(err)
		}
		request := r09Copy(*desired)
		request.DNSGuard = &spec.DesiredDNSGuard{Settings: settings, Previous: previous}
		if len(tc.PolicyCIDRs) > 0 {
			request.ROSPolicy = &spec.ROSPolicyDesired{CIDRs: tc.PolicyCIDRs}
		}
		plan := dnsguard.Build(&request, state.DNSGuard, state.NatContext)
		row := r09StepResult{Plan: plan, Manifest: ros.GuardRaw(previous), Undo: json.RawMessage("null")}
		if step.AfterPreview != nil {
			f.ApplyExternal(*step.AfterPreview)
		}
		if step.Action != "plan" && len(plan.Blockers) == 0 {
			if len(plan.Ops) > 0 {
				row.Undo, err = d.Apply(context.Background(), r09Raw(plan.Ops[0]))
			}
			if err == nil && step.Action == "roundtrip" && len(row.Undo) > 0 && string(row.Undo) != "null" {
				_, err = d.Apply(context.Background(), row.Undo)
			} else if err == nil {
				err = dnsguard.Verify(context.Background(), d.Client, &dnsguard.Check{Roles: plan.Roles})
			}
			if err != nil {
				class := "RouterError"
				if device.RouterErrorKind(err) == device.KindRollback {
					class = "RouterRollbackError"
				}
				row.Error = &r09Error{Class: class, Message: err.Error()}
			}
			previous = ros.GuardRaw(d.DNSGuardManifest())
			row.Manifest = previous
		}
		out.Steps = append(out.Steps, row)
	}
	state := f.Snapshot()
	out.Tables = state.Tables
	for _, call := range state.Calls {
		if call.Method != "GET" {
			out.Writes = append(out.Writes, call)
		}
	}
	return out
}
func r09JSON(t *testing.T, v any) any {
	t.Helper()
	var out any
	data := r09Raw(v)
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

type r11Step struct {
	Action        string                          `json:"action"`
	Enabled       bool                            `json:"enabled"`
	Desired       *spec.Desired                   `json:"desired"`
	Scope         device.DNSCheck                 `json:"scope"`
	Mutation      *routerfake.ExternalWrite       `json:"mutation,omitempty"`
	Agent         *model.Agent                    `json:"agent,omitempty"`
	Host          ipv6.IPv6Host                   `json:"host"`
	Configuration ipv6.IPv6ReadinessConfiguration `json:"configuration"`
	DNSTarget     string                          `json:"dns_target"`
	PolicyContext ipv6.PolicyContext              `json:"policy_context"`
}
type r11Case struct {
	Name    string           `json:"name"`
	Feature string           `json:"feature"`
	State   routerfake.State `json:"state"`
	Router  *spec.Router     `json:"router"`
	Steps   []r11Step        `json:"steps"`
}

func r11State() routerfake.State {
	s := r10State()
	s.Tables["/ip/dns"] = []routerfake.Row{{"servers": "192.0.2.2", "dynamic-servers": ""}}
	for _, path := range []string{pdScripts, pdSchedulers, pdClients, ros.PathIPv6AddressLists, pdPrefixes, ros.PathIPv6ND, "/ipv6/firewall/nat", "/ipv6/firewall/raw", "/ipv6/firewall/mangle"} {
		s.Tables[path] = []routerfake.Row{}
	}
	s.Tables[pdClients] = []routerfake.Row{{".id": "*C0", "interface": "wan", "pool-name": "pool-example", "request": "prefix", "disabled": "false", "status": "bound", "prefix": "2001:db8:1::/60, 1d"}, {".id": "*C1", "interface": "wan", "request": "prefix", "disabled": "true"}}
	s.Tables[rosIPv6Addresses] = append(s.Tables[rosIPv6Addresses], routerfake.Row{".id": "*A2", "address": "2001:db8:1::1/64", "interface": "bridge", "from-pool": "pool-example", "advertise": "true", "disabled": "false"})
	s.Tables[ros.PathDHCPNetworks] = []routerfake.Row{{".id": "*D0", "address": "192.0.2.0/24", "gateway": "192.0.2.1", "dns-server": "192.0.2.2", "dns-none": "false"}}
	s.Tables[ros.PathDHCPServers] = []routerfake.Row{{".id": "*D1", "name": "lan", "interface": "bridge", "disabled": "false"}}
	for _, p := range []string{ros.PathDHCPLeases, ros.PathDHCPOptions, ros.PathDHCPOptionSets, ros.PathDHCPMatchers} {
		s.Tables[p] = []routerfake.Row{}
	}
	s.Tables[ros.PathIPv6ND] = []routerfake.Row{{".id": "*E0", "interface": "bridge", "advertise-dns": "yes", "dns-servers": "2001:db8::53", "disabled": "false", "dynamic": "false", "invalid": "false", "ra-lifetime": "30m", "mtu": "1500", "comment": "synthetic LAN"}}
	return s
}
func r11StepFor(action string) r11Step {
	return r11Step{Action: action, Enabled: true, Desired: &spec.Desired{ProxyCIDRs: []spec.ProxyCIDR{}, IPv6CIDRs: []spec.ProxyCIDR{}, Warnings: []string{}, BlockingWarnings: []string{}, ProxyGateway: "192.0.2.4", DNSSettings: &device.DNSSettings{AgentID: "dns", Server: "192.0.2.2"}}, Scope: device.DNSCheck{ID: "*D0", Network: "192.0.2.0/24", Gateway: "192.0.2.4", DNS: []string{"192.0.2.2"}}, Agent: r10Agent(), Host: r10Host(), Configuration: ipv6.IPv6ReadinessConfiguration{PolicyConfiguration: ipv6.PolicyConfiguration{FakeIPv6Enabled: true, FakeIPv6Range: "fd00:face::/64"}, IPv6: true, FakeIPv6Route: new(true), TUNEnabled: true}}
}

func r12SafeStatus() *ReturnStatus {
	var s ReturnStatus
	_ = json.Unmarshal([]byte(`{"status":"ok","managed":null,"return_tables":[],"fwmark_rules":[],"safety":{"version":1,"ok":true,"blockers":[],"allocation":{"version":1,"reused":false,"table":104,"priority":104,"mark":"0x400000"}}}`), &s)
	return &s
}

type r12Agent struct {
	s                                            *ReturnStatus
	calls                                        []json.RawMessage
	failApply, errorAfterApply, errorAfterRemove bool
}

func (a *r12Agent) Status(context.Context) (*ReturnStatus, error) {
	b, e := json.Marshal(a.s)
	if e != nil {
		return nil, e
	}
	var s ReturnStatus
	e = json.Unmarshal(b, &s)
	return &s, e
}
func (a *r12Agent) Apply(_ context.Context, raw json.RawMessage) error {
	a.calls = append(a.calls, device.JSON([]any{"apply", raw}))
	var sp ReturnSpec
	if e := json.Unmarshal(raw, &sp); e != nil {
		return e
	}
	a.s.Managed = &ReturnManaged{Active: true, TablePresent: true, Spec: raw}
	a.s.FWMarkRules = []json.RawMessage{device.JSON(map[string]string{"priority": "104", "table": "104", "mark": sp.Mark + "/" + sp.Mark})}
	if a.errorAfterApply {
		return &agentsTransportFailure{}
	}
	return nil
}
func (a *r12Agent) Remove(_ context.Context, restore bool) error {
	a.calls = append(a.calls, device.JSON([]any{"remove", restore}))
	a.s.Managed = nil
	a.s.FWMarkRules = []json.RawMessage{}
	if a.errorAfterRemove {
		return &agentsTransportFailure{}
	}
	return nil
}

type agentsTransportFailure struct{}

func (*agentsTransportFailure) Error() string { return "synthetic lost reply" }
func r12Compare(t *testing.T, label string, a, b any) {
	t.Helper()
	left, right := r08Comparable(t, a), r08Comparable(t, b)
	if !reflect.DeepEqual(left, right) {
		t.Errorf("%s differ: %s", label, r08Difference(left, right, "$"))
	}
}
func r12PlanComparable(t *testing.T, p any) any {
	t.Helper()
	m := r08Comparable(t, p).(map[string]any)
	delete(m, "fingerprint")
	return m
}
func r12Desired() *spec.Desired {
	return &spec.Desired{FakeIPRange: "198.18.0.0/16", ProxyGateway: "192.0.2.4", ProxyCIDRs: []spec.ProxyCIDR{{CIDR: "198.18.0.0/16", Sources: []string{"fake-ip"}}, {CIDR: "198.51.100.0/25", Sources: []string{"Synthetic"}}}, IPv6CIDRs: []spec.ProxyCIDR{}, Warnings: []string{}, BlockingWarnings: []string{}, DNSSettings: &device.DNSSettings{}}
}

func r14Plan(t *testing.T, ctx context.Context, d *RouterOS, r *spec.Router, desired *spec.Desired) *Plan {
	t.Helper()
	desired.ROSPolicy.Previous = slices.Clone(d.PolicyManifest())
	state, err := r08InspectState(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := rospolicy.OwnedROSPolicyRows(d.PolicyManifest(), state.ROSPolicy)
	if err != nil {
		owned = nil
	}
	ids := map[string]bool{}
	for _, v := range owned {
		if v.Path == ros.PathMangle {
			ids[v.ID] = true
		}
	}
	state.NatContext.Mangle = []ros.Row{}
	for _, row := range (*state.ROSPolicy)[ros.PathMangle] {
		if !ids[row.ID()] {
			cp := ros.CloneRow(row)
			delete(cp, "bytes")
			delete(cp, "packets")
			state.NatContext.Mangle = append(state.NatContext.Mangle, cp)
		}
	}
	RetirePolicyRoutes(state)
	report, err := BuildReport(desired, state)
	if err != nil {
		t.Fatal(err)
	}
	report.ReturnRouting = &ReturnStatus{Status: "disabled"}
	plan, err := BuildPlan(ctx, r, &Inspection{Desired: desired, State: state, Report: report}, d)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func r12SourceAgent(source AgentSource) *GatewayAgent { return source.(r12Source).gateway }

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

func r10Writes(t *testing.T, calls []routerfake.Call) any {
	t.Helper()
	filtered := []routerfake.Call{}
	for _, call := range calls {
		if !strings.HasSuffix(call.Path, "/print") {
			filtered = append(filtered, call)
		}
	}
	return r08Writes(t, filtered)
}
