package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/routers/routerfake"
)

const r10YAML = "ipv6: true\ndns:\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\n  fake-ip-range6: fd00:face::1/64\ntun:\n  enable: true\n"

func r10Config(raw string) *model.Profile {
	return &model.Profile{Mihomo: model.MihomoSettings{CustomConfig: raw}}
}
func r10Agent() *model.Agent {
	return &model.Agent{ID: "gateway", Host: "192.0.2.4", ServiceType: "mihomo", Enabled: new(true)}
}
func r10Host() ipv6.IPv6Host {
	return ipv6.IPv6Host{Interface: "eth0", Addresses: []ipv6.IPv6HostAddress{{Address: "fd00:1::4", Usable: true}, {Address: "fe80::4"}}}
}
func r10Router() *spec.Router {
	return &spec.Router{RouterSettings: spec.RouterSettings{ID: "synthetic", Type: "routeros"}, Extra: map[string]json.RawMessage{}}
}
func r10State() routerfake.State {
	return routerfake.State{IPv6Wire: true, Tables: map[string][]routerfake.Row{
		ros.PathIPv6Routes: {}, rosIPv6Addresses: {{".id": "*A1", "address": "fd00:1::1/64", "interface": "bridge"}}, rosIPv4Addresses: {{".id": "*B1", "address": "192.0.2.1/24", "interface": "bridge", "actual-interface": "bridge"}}, "/ipv6/firewall/filter": {}, "/ipv6/firewall/address-list": {}, "/interface": {{".id": "*C1", "name": "bridge"}}, "/interface/list": {}, "/interface/list/member": {},
	}}
}
func r10Wanted(n int) *spec.Desired {
	d := &spec.Desired{ProxyCIDRs: []spec.ProxyCIDR{}, Warnings: []string{}, IPv6CIDRs: []spec.ProxyCIDR{}, BlockingWarnings: []string{}}
	for i := 0; i < n; i++ {
		d.IPv6CIDRs = append(d.IPv6CIDRs, spec.ProxyCIDR{CIDR: fmt.Sprintf("2001:db8:%x::/48", i+10), Sources: []string{"Synthetic"}})
	}
	return d
}
func r10Route(id, cidr, gateway string) routerfake.Row {
	return routerfake.Row{".id": id, "dst-address": cidr, "gateway": gateway, "routing-table": "main", "distance": "1", "disabled": "false", "active": "true", "dynamic": "false", "comment": "foreign"}
}

type r10Step struct {
	Action   string                    `json:"action"`
	Config   *model.Profile            `json:"config"`
	Desired  *spec.Desired             `json:"desired"`
	Agent    *model.Agent              `json:"agent"`
	Host     ipv6.IPv6Host             `json:"host"`
	Mutation *routerfake.ExternalWrite `json:"mutation,omitempty"`
}
type r10Case struct {
	Name    string           `json:"name"`
	Feature string           `json:"feature"`
	State   routerfake.State `json:"state"`
	Router  *spec.Router     `json:"router"`
	Steps   []r10Step        `json:"steps"`
}

func r10StepFor(action string, n int) r10Step {
	return r10Step{Action: action, Config: r10Config(r10YAML), Desired: r10Wanted(n), Agent: r10Agent(), Host: r10Host()}
}
func r10Run(t *testing.T, f *routerfake.Fake, endpoint string, tc r10Case) r08Output {
	t.Helper()
	f.Reset(tc.State)
	d := NewRouterOS(endpoint, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	ctx := context.Background()
	out := r08Output{Results: []json.RawMessage{}}
	var undo json.RawMessage
	var fakePlan *ipv6.IPv6FakeIPPlan
	var routesPlan *ipv6.IPv6RoutesPlan
	var err error
	add := func(value any) {
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		out.Results = append(out.Results, raw)
	}
	for _, step := range tc.Steps {
		if step.Mutation != nil {
			f.ApplyExternal(*step.Mutation)
		}
		if step.Action == "undo" {
			_, err = d.Apply(ctx, undo)
			if err == nil {
				add(ros.Row{})
			}
		} else if step.Action == "apply" {
			var raw json.RawMessage
			if tc.Feature == "fake" {
				raw = device.JSON(fakePlan.Ops[0])
			} else {
				raw = device.JSON(routesPlan.Ops[0])
			}
			undo, err = d.Apply(ctx, raw)
			if err == nil {
				add(undo)
			}
		} else {
			r := r09Copy(*tc.Router)
			if r.Extra == nil {
				r.Extra = map[string]json.RawMessage{}
			}
			blocked := false
			var ops []device.Op
			if tc.Feature == "fake" {
				if d.IPv6FakeIPManifestSet() {
					r.Extra["ipv6_fakeip_state"] = ros.GuardRaw(d.IPv6FakeIPManifest())
				}
				fakePlan = ipv6.InspectIPv6FakeIP(ctx, step.Config, &r, step.Agent, step.Host, d.Client)
				add(fakePlan)
				ops = fakePlan.Ops
				blocked = len(fakePlan.Blockers) > 0
			} else {
				if d.IPv6RoutesManifestSet() {
					r.Extra["ipv6_routes_state"] = ros.GuardRaw(d.IPv6RoutesManifest())
				}
				routesPlan = ipv6.InspectIPv6Routes(ctx, step.Config, &r, step.Desired, step.Agent, step.Host, d.Client)
				add(routesPlan)
				ops = routesPlan.Ops
				blocked = len(routesPlan.Skip) > 0
			}
			if step.Action == "report" {
				state, e := d.ReadState(ctx)
				if e != nil {
					err = e
					break
				}
				if tc.Feature == "fake" {
					state.IPv6FakeIPPlan = fakePlan
				} else {
					state.IPv6RoutesPlan = routesPlan
				}
				report, e := BuildReport(step.Desired, state)
				if e != nil {
					err = e
					break
				}
				add(report)
				continue
			}
			if step.Action == "plan" || blocked {
				continue
			}
			if len(ops) > 0 {
				undo, err = d.Apply(ctx, device.JSON(ops[0]))
				if err == nil {
					add(undo)
				}
			}
			if err == nil {
				if tc.Feature == "fake" {
					err = ipv6.VerifyIPv6FakeIP(ctx, d.Client, &fakePlan.Check)
				} else {
					err = ipv6.VerifyIPv6Routes(ctx, d.Client, routesPlan.Check)
				}
			}
			if err == nil && step.Action == "rollback" && len(ops) > 0 {
				_, err = d.Apply(ctx, undo)
				if err == nil {
					add(ros.Row{})
				}
			}
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		class := "RouterError"
		if e, ok := errors.AsType[*device.RouterError](err); ok && e.Kind == device.KindRollback {
			class = "RouterRollbackError"
		}
		out.Error = &r08Error{class, err.Error()}
	}
	if tc.Feature == "fake" {
		out.Manifest = ros.GuardRaw(d.IPv6FakeIPManifest())
	} else {
		out.Manifest = ros.GuardRaw(d.IPv6RoutesManifest())
	}
	return out
}
func r10Python(t *testing.T, endpoint string, tc r10Case) r08Output {
	t.Helper()
	raw := r09Python(t, "r10_reference.py", tc, endpoint)
	var out r08Output
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Python output: %v %s", err, raw)
	}
	return out
}
func TestR10PythonBoundary(t *testing.T) {
	f := routerfake.New(nil)
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, tc := range r10Cases() {
		t.Run(tc.Name, func(t *testing.T) {
			py := r10Python(t, srv.URL, tc)
			pyState := f.Snapshot()
			got := r10Run(t, f, srv.URL, tc)
			gotState := f.Snapshot()
			a, b := r08Comparable(t, py), r08Comparable(t, got)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("R10 result differs: %s", r08Difference(a, b, "$"))
			}
			a, b = r08Comparable(t, pyState.Tables), r08Comparable(t, gotState.Tables)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("R10 device tables differ: %s", r08Difference(a, b, "$"))
			}
			a, b = r10Writes(t, pyState.Calls), r10Writes(t, gotState.Calls)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("R10 write sequence differs: %s", r08Difference(a, b, "$"))
			}
		})
	}
}
func r10Cases() []r10Case {
	out := []r10Case{}
	for _, feature := range []string{"fake", "routes"} {
		base := func(name string, steps ...r10Step) r10Case {
			return r10Case{feature + "/" + name, feature, r10State(), r10Router(), steps}
		}
		out = append(out, base("publish_idempotent", r10StepFor("publish", 5), r10StepFor("publish", 5)), base("rollback", r10StepFor("rollback", 5)), base("report_missing_synced", r10StepFor("report", 5), r10StepFor("publish", 5), r10StepFor("report", 5)))
		off := r10StepFor("publish", 5)
		off.Config = r10Config("ipv6: false\n")
		off.Agent = nil
		off.Host = ipv6.IPv6Host{Error: "offline"}
		out = append(out, base("withdraw", r10StepFor("publish", 5), off))
		replace := r10StepFor("publish", 5)
		replace.Host.Addresses = []ipv6.IPv6HostAddress{{Address: "fe80::5"}}
		out = append(out, base("replace", r10StepFor("publish", 5), replace, r10StepFor("plan", 5)), base("replace_rollback", r10StepFor("publish", 5), func() r10Step { s := replace; s.Action = "rollback"; return s }()))
		for _, variant := range []string{"foreign_good", "foreign_wrong", "foreign_disabled", "foreign_inactive", "foreign_duplicate", "local_overlap", "unavailable_host", "ambiguous_host", "ula_only", "preserve_ula", "no_tun", "nodes_only", "invalid_bool", "incomplete_input", "same_port_drop", "unread_filter"} {
			step := r10StepFor("plan", 5)
			tc := base(variant, step)
			cidr := "fd00:face::/64"
			if feature == "routes" {
				cidr = step.Desired.IPv6CIDRs[0].CIDR
			}
			switch variant {
			case "foreign_good", "foreign_wrong", "foreign_disabled", "foreign_inactive", "foreign_duplicate", "preserve_ula":
				row := r10Route("*50", cidr, "fe80::4%bridge")
				if variant == "foreign_wrong" {
					row["gateway"] = "fe80::9%bridge"
				}
				if variant == "foreign_disabled" {
					row["disabled"] = "true"
				}
				if variant == "foreign_inactive" {
					row["active"] = "false"
				}
				if variant == "preserve_ula" {
					row["gateway"] = "fd00:1::4%bridge"
				}
				tc.State.Tables[ros.PathIPv6Routes] = []routerfake.Row{row}
				if variant == "foreign_duplicate" {
					second := r09Copy(row)
					second[".id"] = "*51"
					tc.State.Tables[ros.PathIPv6Routes] = append(tc.State.Tables[ros.PathIPv6Routes], second)
				}
			case "local_overlap":
				tc.State.Tables[rosIPv6Addresses] = append(tc.State.Tables[rosIPv6Addresses], routerfake.Row{".id": "*99", "address": stringsReplaceCIDRHost(cidr), "interface": "other"})
			case "unavailable_host":
				tc.Steps[0].Host = ipv6.IPv6Host{Error: "offline"}
			case "ambiguous_host":
				tc.Steps[0].Host.Addresses = append(tc.Steps[0].Host.Addresses, ipv6.IPv6HostAddress{Address: "fe80::5"})
			case "ula_only":
				tc.Steps[0].Host.Addresses = tc.Steps[0].Host.Addresses[:1]
			case "no_tun":
				tc.Steps[0].Config = r10Config("ipv6: true\ndns: {enable: true, ipv6: true, enhanced-mode: fake-ip, fake-ip-range6: 'fd00:face::/64'}\n")
			case "nodes_only":
				tc.Steps[0].Config = r10Config("ipv6: true\ndns: {enable: true, ipv6: false, enhanced-mode: fake-ip, fake-ip-range6: 'fd00:face::/64'}\n")
			case "invalid_bool":
				tc.Steps[0].Config = r10Config("ipv6: 'true'\n")
			case "incomplete_input":
				tc.Steps = []r10Step{r10StepFor("publish", 5), r10StepFor("publish", 2)}
				tc.Steps[1].Desired.BlockingWarnings = []string{"synthetic input unavailable"}
			case "same_port_drop":
				tc.State.Tables["/ipv6/firewall/filter"] = []routerfake.Row{{".id": "*10", "chain": "forward", "action": "drop", "connection-state": "invalid"}}
			case "unread_filter":
				tc.State.Faults = []routerfake.Fault{{Method: "GET", Path: "/ipv6/firewall/filter", Status: 403}}
			}
			out = append(out, tc)
		}
		for _, field := range []string{"address", "actual-interface", "lifetime", "route_order"} {
			tc := base("preview_"+field, r10StepFor("plan", 5), r10StepFor("apply", 5))
			patch := routerfake.Row{}
			path, id := rosIPv4Addresses, "*B1"
			switch field {
			case "address":
				path, id = rosIPv6Addresses, "*A1"
				patch["address"] = "fd00:1::2/64"
			case "actual-interface":
				patch[field] = "other"
			case "lifetime":
				path, id = rosIPv6Addresses, "*A1"
				patch["valid"] = "2h"
				patch["preferred"] = "1h"
			case "route_order":
				patch["disabled"] = "false"
			}
			tc.Steps[1].Mutation = &routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{path: {id: patch}}}}
			out = append(out, tc)
		}
	}

	for _, feature := range []string{"fake", "routes"} {
		for _, variant := range []string{"inactive_new", "refused_put", "lost_reply", "withdraw_rollback"} {
			tc := r10Case{Name: feature + "/" + variant, Feature: feature, State: r10State(), Router: r10Router(), Steps: []r10Step{r10StepFor("publish", 5)}}
			switch variant {
			case "inactive_new":
				if feature == "fake" {
					tc.State.Faults = []routerfake.Fault{{Method: "PUT", Path: ros.PathIPv6Routes, Count: 1, AfterCommit: true, PatchRows: map[string]map[string]routerfake.Row{ros.PathIPv6Routes: {"*65": {"active": "false"}}}}}
				} else {
					tc.State.Faults = []routerfake.Fault{{Method: "POST", Path: "/execute", Count: 1, AfterCommit: true, PatchCreated: map[int]routerfake.Row{0: {"active": "false"}}}}
				}
			case "refused_put":
				tc.State.Faults = []routerfake.Fault{{Method: "PUT", Path: ros.PathIPv6Routes, Count: 1, Status: 403}, {Method: "POST", Path: "/execute", Count: 1, Status: 403}}
			case "lost_reply":
				tc.State.Faults = []routerfake.Fault{{Method: "PUT", Path: ros.PathIPv6Routes, Count: 1, AfterCommit: true, LoseReply: true}, {Method: "POST", Path: "/execute", Count: 1, AfterCommit: true, LoseReply: true}}
			case "withdraw_rollback":
				step := r10StepFor("rollback", 5)
				step.Config = r10Config("ipv6: false")
				step.Agent = nil
				step.Host = ipv6.IPv6Host{}
				tc.Steps = append(tc.Steps, step, r10StepFor("plan", 5))
			}
			out = append(out, tc)
		}
	}
	for _, n := range []int{1, 3, 4, 100, 101, 201, 501, 4096} {
		tc := r10Case{fmt.Sprintf("routes/batch_%d", n), "routes", r10State(), r10Router(), []r10Step{r10StepFor("publish", n)}}
		if n == 501 {
			step := r10StepFor("publish", 0)
			step.Config = r10Config("ipv6: false")
			step.Agent = nil
			step.Host = ipv6.IPv6Host{}
			tc.Steps = append(tc.Steps, step)
		}
		out = append(out, tc)
	}
	// Preserve all bytes of synthetic YAML entering the settings owner. The module
	// consumes YAML; it does not generate or rewrite any YAML document.
	return out
}
func stringsReplaceCIDRHost(cidr string) string {
	for i := 0; i < len(cidr)-1; i++ {
		if cidr[i:i+2] == "::" {
			return cidr[:i] + "::1" + cidr[i+2:]
		}
	}
	return cidr
}

func TestR10ManifestCopies(t *testing.T) {
	d := NewRouterOSWithHTTP("", nil)
	raw := json.RawMessage(`{"future":{"keep":true}}`)
	d.SetIPv6FakeIPManifest(raw)
	d.SetIPv6RoutesManifest(raw)
	raw[2] = 'X'
	a := d.Manifests()
	if bytes.Equal(a.IPv6FakeIP.Value, raw) {
		t.Fatal("aliased input")
	}
	a.IPv6Routes.Value[2] = 'Y'
	if bytes.Equal(a.IPv6Routes.Value, d.Manifests().IPv6Routes.Value) {
		t.Fatal("aliased output")
	}
	if !a.IPv6FakeIP.Set || !a.IPv6Routes.Set {
		t.Fatal("presence lost")
	}
	d.SetIPv6FakeIPManifest(nil)
	if !bytes.Equal(d.Manifests().IPv6FakeIP.Value, []byte("null")) {
		t.Fatal("withdrawal not explicit null")
	}
}

// /print is a read-only RouterOS query; R10 adds one proof read before each
// destructive batch. Every actual PUT/PATCH/DELETE/remove/execute stays compared.
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
