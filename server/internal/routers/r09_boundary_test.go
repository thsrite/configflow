package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"configflow/internal/routers/routerfake"
)

type r09Step struct {
	Action       string                    `json:"action"`
	Settings     *DNSGuard                 `json:"settings,omitempty"`
	Desired      *Desired                  `json:"desired,omitempty"`
	Mutation     *routerfake.ExternalWrite `json:"mutation,omitempty"`
	AfterPreview *routerfake.ExternalWrite `json:"after_preview,omitempty"`
}
type r09Case struct {
	Name        string           `json:"name"`
	State       routerfake.State `json:"state"`
	Desired     *Desired         `json:"desired"`
	Settings    DNSGuard         `json:"settings"`
	Previous    json.RawMessage  `json:"previous"`
	PolicyCIDRs []ProxyCIDR      `json:"policy_cidrs,omitempty"`
	Steps       []r09Step        `json:"steps"`
}
type r09Outcome struct {
	Steps  []r09StepResult             `json:"steps"`
	Tables map[string][]routerfake.Row `json:"tables"`
	Writes []routerfake.Call           `json:"writes"`
}
type r09StepResult struct {
	Plan     *DNSGuardPlan   `json:"plan"`
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
	}}, Desired: &Desired{FakeIPRange: "198.18.0.0/16", ProxyGateway: "192.0.2.4", ProxyCIDRs: []ProxyCIDR{{CIDR: "198.18.0.0/16", Sources: []string{"fake-ip"}}}, DNSSettings: &DNSSettings{Server: "192.0.2.2"}}, Settings: DNSGuard{Enabled: true, BlockDoT: true, Failover: true, Bypass: []string{"192.0.2.50/32"}}, Steps: []r09Step{{Action: "apply"}, {Action: "plan"}}}
}
func r09Cases() []r09Case {
	cases := []r09Case{r09Base("first_enable_synced")}
	add := func(tc r09Case) { cases = append(cases, tc) }
	tc := r09Base("disable")
	off := tc.Settings
	off.Enabled = false
	tc.Steps = append(tc.Steps, r09Step{Action: "apply", Settings: &off}, r09Step{Action: "plan"})
	add(tc)
	tc = r09Base("optional_off")
	tc.Settings.BlockDoT = false
	tc.Settings.Failover = false
	tc.Settings.Bypass = []string{}
	add(tc)
	tc = r09Base("incremental")
	changed := r09Copy(tc.Settings)
	changed.Bypass = []string{"192.0.2.60/32"}
	tc.Steps = append(tc.Steps, r09Step{Action: "apply", Settings: &changed}, r09Step{Action: "plan"})
	add(tc)
	tc = r09Base("same_spec_roles_change")
	changed = r09Copy(tc.Settings)
	changed.BlockDoT = false
	tc.Steps = append(tc.Steps, r09Step{Action: "apply", Settings: &changed}, r09Step{Action: "plan"})
	add(tc)
	tc = r09Base("roundtrip")
	tc.Steps = []r09Step{{Action: "roundtrip"}, {Action: "plan"}}
	add(tc)
	for _, path := range dnsguard.Paths() {
		tc = r09Base("first_reject" + path)
		tc.State.Faults = []routerfake.Fault{{Method: "PUT", Path: path, Count: 1, Status: 400, Body: routerfake.Row{"message": "injected refusal"}}}
		tc.Steps = []r09Step{{Action: "apply"}}
		add(tc)
	}
	tc = r09Base("incremental_reject_watch")
	changed = r09Copy(tc.Settings)
	changed.Bypass = []string{"192.0.2.60/32"}
	tc.Steps = append(tc.Steps, r09Step{Action: "apply", Settings: &changed, Mutation: &routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "PUT", Path: ros.PathNetwatch, Count: 1, Status: 400, Body: routerfake.Row{"message": "injected refusal"}}}}})
	add(tc)
	tc = r09Base("disable_reject_address")
	off = tc.Settings
	off.Enabled = false
	tc.Steps = append(tc.Steps, r09Step{Action: "apply", Settings: &off, Mutation: &routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "DELETE", Path: ros.PathAddresses + "/*66", Status: 400, Body: routerfake.Row{"message": "injected refusal"}}}}})
	add(tc)
	for _, variant := range []string{"foreign_list", "foreign_watch", "no_netwatch", "dns_error", "disconnected", "duplicate_route", "interface_mismatch", "not_main", "inactive", "proxy_overlap", "policy_overlap", "no_routes"} {
		tc = r09Base(variant)
		tc.Steps = []r09Step{{Action: "plan"}}
		switch variant {
		case "foreign_list":
			tc.State.Tables[ros.PathAddresses] = []routerfake.Row{{".id": "*F1", "list": DNSGuardBypassList, "address": "192.0.2.9"}}
		case "foreign_watch":
			tc.State.Tables[ros.PathNetwatch] = []routerfake.Row{{".id": "*F1", "name": dnsguard.WatchName, "host": "192.0.2.9"}}
		case "no_netwatch":
			tc.State.Faults = []routerfake.Fault{{Method: "GET", Path: ros.PathNetwatch, Status: 404}}
		case "dns_error":
			tc.Desired.Error = "synthetic DNS error"
		case "disconnected":
			tc.Desired.Server = "203.0.113.2"
		case "duplicate_route":
			tc.State.Tables[ros.PathRoutes] = append(tc.State.Tables[ros.PathRoutes], r09Copy(tc.State.Tables[ros.PathRoutes][0]))
			tc.State.Tables[ros.PathRoutes][1][".id"] = "*11"
		case "interface_mismatch":
			tc.State.Tables[ros.PathRoutes][0]["immediate-gw"] = "other"
		case "not_main":
			tc.State.Tables[ros.PathRoutes][0]["routing-table"] = "guest"
		case "inactive":
			tc.State.Tables[ros.PathRoutes][0]["active"] = "false"
		case "proxy_overlap":
			tc.Desired.ProxyCIDRs = append(tc.Desired.ProxyCIDRs, ProxyCIDR{CIDR: "192.0.2.2/32"})
		case "policy_overlap":
			tc.PolicyCIDRs = []ProxyCIDR{{CIDR: "192.0.2.2/32"}}
		case "no_routes":
			tc.State.Tables[ros.PathRoutes] = []routerfake.Row{}
		}
		add(tc)
	}
	for _, variant := range []string{"paused", "paused_delta", "manual_disabled", "outside_edit", "moved_address", "missing_address", "gone_all", "changed_after_preview", "watch_unreadable_owned"} {
		tc = r09Base(variant)
		mutation := &routerfake.ExternalWrite{}
		switch variant {
		case "paused", "paused_delta", "manual_disabled":
			mutation.PatchRows = map[string]map[string]routerfake.Row{ros.PathNAT: {"*6A": {"disabled": "true"}, "*6B": {"disabled": "true"}}}
			if variant == "manual_disabled" {
				tc.Settings.Failover = false
			}
		case "outside_edit", "changed_after_preview":
			mutation.PatchRows = map[string]map[string]routerfake.Row{ros.PathNAT: {"*68": {"src-address": "192.0.2.0/25"}}}
		case "moved_address":
			mutation.PatchRows = map[string]map[string]routerfake.Row{ros.PathAddresses: {"*65": {"list": "foreign"}}}
		case "missing_address":
			mutation.RemoveIDs = map[string][]string{ros.PathAddresses: {"*65"}}
		case "gone_all":
			mutation.RemoveIDs = map[string][]string{ros.PathAddresses: {"*65", "*66", "*67"}, ros.PathNAT: {"*68", "*69", "*6A", "*6B"}, ros.PathFilter: {"*6C", "*6D"}, ros.PathNetwatch: {"*6E"}}
			off = r09Copy(tc.Settings)
			off.Enabled = false
		case "watch_unreadable_owned":
			mutation.Faults = []routerfake.Fault{{Method: "GET", Path: ros.PathNetwatch, Status: 404}}
		}
		step := r09Step{Action: "plan", Mutation: mutation}
		if variant == "paused_delta" {
			changed = r09Copy(tc.Settings)
			changed.Bypass = append(changed.Bypass, "192.0.2.60/32")
			step.Settings = &changed
			step.Action = "apply"
		}
		if variant == "gone_all" {
			step.Settings = &off
			step.Action = "apply"
		}
		if variant == "changed_after_preview" {
			changed = r09Copy(tc.Settings)
			changed.Bypass = []string{"192.0.2.60/32"}
			step.Settings = &changed
			step.Action = "apply"
			step.AfterPreview = mutation
			step.Mutation = nil
		}
		tc.Steps = append(tc.Steps, step)
		add(tc)
	}
	for _, variant := range []string{"ha", "ha_host_outside", "ha_vip_outside", "ha_host_proxy", "ha_vip_proxy", "ha_transition", "ha_empty_hosts", "ha_validation_order"} {
		tc = r09Base(variant)
		ha := r09Copy(tc.Desired)
		ha.Server = "192.0.2.10"
		ha.HA = &DNSHA{VIP: "192.0.2.10", Hosts: []string{"192.0.2.8", "192.0.2.9"}, PeerID: "peer"}
		ha.ProxyGateway = "192.0.2.11"
		switch variant {
		case "ha_host_outside":
			ha.HA.Hosts[1] = "203.0.113.9"
		case "ha_vip_outside":
			ha.HA.VIP = "203.0.113.10"
		case "ha_host_proxy":
			ha.ProxyCIDRs = append(ha.ProxyCIDRs, ProxyCIDR{CIDR: "192.0.2.9/32"})
		case "ha_empty_hosts":
			ha.HA.Hosts = []string{}
		case "ha_validation_order":
			ha.HA.Hosts[1] = "203.0.113.9"
			ha.ProxyCIDRs = append(ha.ProxyCIDRs, ProxyCIDR{CIDR: "192.0.2.10/32"})
		case "ha_vip_proxy":
			ha.ProxyCIDRs = append(ha.ProxyCIDRs, ProxyCIDR{CIDR: "192.0.2.10/32"})
		}
		tc.Settings.Bypass = []string{"192.0.2.12/30", "192.0.2.64/26"}
		if variant == "ha_transition" {
			tc.Steps = append(tc.Steps, r09Step{Action: "apply", Desired: ha}, r09Step{Action: "plan"})
		} else {
			tc.Desired = ha
		}
		add(tc)
	}
	return cases
}

func r09Python(t *testing.T, script string, input any, endpoint string) []byte {
	t.Helper()
	home, _ := os.UserHomeDir()
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	if _, err := os.Stat(python); err != nil {
		t.Fatal("required Python reference unavailable", err)
	}
	wd, _ := os.Getwd()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, filepath.Join(wd, "testdata", script))
	cmd.Dir = filepath.Clean(filepath.Join(wd, "../../.."))
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "R09_ENDPOINT="+endpoint)
	cmd.Stdin = bytes.NewReader(r09Raw(input))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python: %v %s", err, out)
	}
	return bytes.TrimSpace(out)
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
		request.DNSGuard = &DesiredDNSGuard{Settings: settings, Previous: previous}
		if len(tc.PolicyCIDRs) > 0 {
			request.ROSPolicy = &ROSPolicyDesired{CIDRs: tc.PolicyCIDRs}
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
				err = dnsguard.Verify(context.Background(), d.Client, &DNSGuardCheck{Roles: plan.Roles})
			}
			if err != nil {
				class := "RouterError"
				if device.RouterErrorKind(err) == KindRollback {
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
func TestR09PythonBoundary(t *testing.T) {
	f := routerfake.New(nil)
	srv := httptest.NewServer(f)
	defer srv.Close()
	cases := r09Cases()
	seed := r09Go(t, r09Base("seed_for_replan"), f, srv.URL)
	for _, name := range []string{"overtaken_then_replan", "missing_after_preview", "missing_published_member", "duplicate_live_id"} {
		tc := r09Base(name)
		tc.Previous = seed.Steps[0].Manifest
		tc.State.Tables = r09Copy(seed.Tables)
		switch name {
		case "overtaken_then_replan":
			tc.State.Tables[ros.PathNAT] = append([]routerfake.Row{{".id": "*F1", "chain": "dstnat", "action": "redirect", "dst-port": "53", "comment": "foreign DNS"}}, tc.State.Tables[ros.PathNAT]...)
		case "missing_after_preview":
			tc.Settings.Bypass = []string{"192.0.2.60/32"}
			tc.Steps[0].AfterPreview = &routerfake.ExternalWrite{Fault: routerfake.Fault{RemoveIDs: map[string][]string{ros.PathAddresses: {"*65"}}}}
		case "missing_published_member":
			tc.State.Tables[ros.PathAddresses] = tc.State.Tables[ros.PathAddresses][1:]
		case "duplicate_live_id":
			tc.State.Tables[ros.PathNAT] = append(tc.State.Tables[ros.PathNAT], r09Copy(tc.State.Tables[ros.PathNAT][0]))
			tc.Steps = []r09Step{{Action: "plan"}}
		}
		cases = append(cases, tc)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			reference := r09Python(t, "r09_reference.py", tc, srv.URL)
			var want map[string]any
			if err := json.Unmarshal(reference, &want); err != nil {
				t.Fatalf("decode Python %v %s", err, reference)
			}
			got := r09JSON(t, r09Go(t, tc, f, srv.URL))
			if !reflect.DeepEqual(want, got) {
				a, _ := json.MarshalIndent(want, "", " ")
				b, _ := json.MarshalIndent(got, "", " ")
				t.Fatalf("Python/Go differ\nPython %s\nGo %s", a, b)
			}
		})
	}
}

func TestR09BoundaryComparisonDetectsMutations(t *testing.T) {
	tc := r09Base("mutations")
	f := routerfake.New(nil)
	srv := httptest.NewServer(f)
	defer srv.Close()
	original := r09JSON(t, r09Go(t, tc, f, srv.URL))
	data := r09Raw(original)
	for _, kind := range []string{"result", "journal", "request", "table"} {
		t.Run(kind, func(t *testing.T) {
			var changed map[string]any
			_ = json.Unmarshal(data, &changed)
			switch kind {
			case "result":
				changed["steps"].([]any)[0].(map[string]any)["plan"].(map[string]any)["status"] = "BROKEN"
			case "journal":
				changed["steps"].([]any)[0].(map[string]any)["undo"].(map[string]any)["changes"] = []any{}
			case "request":
				changed["writes"].([]any)[0].(map[string]any)["body"].(map[string]any)["address"] = "203.0.113.99"
			case "table":
				changed["tables"].(map[string]any)[ros.PathNAT].([]any)[0].(map[string]any)["dst-port"] = "54"
			}
			if reflect.DeepEqual(original, changed) {
				t.Fatal("mutation escaped comparison")
			}
			t.Log("DETECTED", kind)
		})
	}
}
