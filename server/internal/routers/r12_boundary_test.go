package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/routers/routerfake"
)

type r12BoundaryCase struct {
	Router  *Router       `json:"router"`
	State   any           `json:"state"`
	Desired *Desired      `json:"desired"`
	Return  *ReturnStatus `json:"return_status"`
	Execute bool          `json:"execute"`
	DHCP    bool          `json:"dhcp"`
}
type r12BoundaryOutput struct {
	Report     json.RawMessage   `json:"report"`
	Plan       json.RawMessage   `json:"plan"`
	Result     *Result           `json:"result"`
	AgentCalls []json.RawMessage `json:"agent_calls"`
}

func r12Python(t *testing.T, endpoint string, tc any) r12BoundaryOutput {
	t.Helper()
	home, e := os.UserHomeDir()
	if e != nil {
		t.Fatal(e)
	}
	wd, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(tc)
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(t.Context(), filepath.Join(home, "cf-test/.venv/bin/python"), filepath.Join(wd, "testdata/r12_reference.py"))
	cmd.Dir = filepath.Clean(filepath.Join(wd, "../../.."))
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "R12_ENDPOINT="+endpoint)
	cmd.Stdin = bytes.NewReader(b)
	raw, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("Python: %v %s", e, raw)
	}
	var out r12BoundaryOutput
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatalf("Python: %v %s", e, raw)
	}
	return out
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
	var spec ReturnSpec
	if e := json.Unmarshal(raw, &spec); e != nil {
		return e
	}
	a.s.Managed = &ReturnManaged{Active: true, TablePresent: true, Spec: raw}
	a.s.FWMarkRules = []json.RawMessage{device.JSON(map[string]string{"priority": "104", "table": "104", "mark": spec.Mark + "/" + spec.Mark})}
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
func r12Desired() *Desired {
	return &Desired{FakeIPRange: "198.18.0.0/16", ProxyGateway: "192.0.2.4", ProxyCIDRs: []ProxyCIDR{{"198.18.0.0/16", []string{"fake-ip"}}, {"198.51.100.0/25", []string{"Synthetic"}}}, IPv6CIDRs: []ProxyCIDR{}, Warnings: []string{}, BlockingWarnings: []string{}, DNSSettings: &DNSSettings{}}
}
func TestR12PythonWholePlanAndExecution(t *testing.T) {
	for _, vendor := range []string{"routeros", "ikuai"} {
		for _, mode := range []string{"missing", "adopt", "conflict", "return1", "return2", "nat_failure", "route_failure", "return_nat_failure", "empty", "retire", "dhcp"} {
			t.Run(vendor+"/"+mode, func(t *testing.T) {
				desired := r12Desired()
				r := &Router{RouterSettings: RouterSettings{ID: "r1", Type: vendor, Endpoint: "http://192.0.2.1"}}
				ret := r12SafeStatus()
				if strings.HasPrefix(mode, "return") {
					r.ManageReturnRouting = true
				}
				if mode == "return2" {
					ret.Safety = bytes.Replace(ret.Safety, []byte(`"version":1`), []byte(`"protocols":[1,2],"version":1`), 1)
				}
				var d Driver
				var state any
				var reset func()
				var tables func() any
				var writes func() any
				if vendor == "routeros" {
					s := r08State()
					s.Tables[rosRoutes] = s.Tables[rosRoutes][:1]
					if mode == "adopt" || mode == "empty" || mode == "conflict" {
						gw := "192.0.2.4"
						comment := "manual"
						if mode == "empty" {
							comment = "cf"
						}
						if mode == "conflict" {
							gw = "192.0.2.9"
						}
						for i, c := range []string{"198.18.0.0/16", "198.51.100.0/25"} {
							s.Tables[rosRoutes] = append(s.Tables[rosRoutes], routerfake.Row{".id": []string{"*A", "*B"}[i], "dst-address": c, "gateway": gw, "routing-table": "main", "disabled": "false", "active": "true", "comment": comment})
						}
					}
					if mode == "dhcp" {
						r.DNSAgentID = "dns"
						desired.DNSSettings = &DNSSettings{AgentID: "dns", Server: "192.0.2.2"}
						s.Tables[rosDHCPNetworks] = []routerfake.Row{{".id": "*D0", "address": "192.0.2.0/24", "gateway": "192.0.2.1", "dns-server": "192.0.2.99", "dns-none": "false"}}
						s.Tables[rosDHCPServers] = []routerfake.Row{{".id": "*D1", "interface": "bridge", "disabled": "false"}}
					}
					if mode == "nat_failure" || mode == "return_nat_failure" {
						s.Faults = []routerfake.Fault{{Method: "PUT", Path: rosNAT, Count: 1, Status: 400}}
					}
					if mode == "route_failure" {
						s.Faults = []routerfake.Fault{{Method: "PUT", Path: rosRoutes, Count: 1, Status: 400}}
					}
					f := routerfake.New(nil)
					f.Reset(s)
					server := httptest.NewServer(f)
					defer server.Close()
					state = s
					reset = func() { f.Reset(s) }
					tables = func() any { return f.Snapshot().Tables }
					writes = func() any { return r08Writes(t, f.Calls()) }
					d = NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
					tc := r12BoundaryCase{r, state, desired, ret, true, mode == "dhcp"}
					python := r12Python(t, server.URL, tc)
					pt, pw := tables(), writes()
					reset()
					defer d.Close()
					r12RunCompare(t, tc, d, python, pt, pw, tables, writes)
				} else {
					s := routerfake.IkuaiState{Tables: map[string][]routerfake.Row{}}
					if mode == "adopt" || mode == "empty" || mode == "conflict" {
						comment := "manual"
						gw := "192.0.2.4"
						if mode == "empty" {
							comment = "cf"
						}
						if mode == "conflict" {
							gw = "192.0.2.9"
						}
						s.Tables["static_rt"] = []routerfake.Row{{"id": 1, "tagname": "RT_1", "dst_addr": "198.18.0.0", "netmask": "255.255.0.0", "gateway": gw, "ip_type": "4", "interface": "auto", "prio": 1, "comment": comment, "enabled": "yes"}}
					}
					if mode == "retire" {
						s.Tables["stream_ipport"] = []routerfake.Row{{"id": 1, "tagname": "cf_pr_1", "comment": PolicyManualComment, "enabled": "yes", "nexthop": "192.0.2.4", "protocol": "any", "prio": 0, "type": 1, "src_addr": map[string]any{"custom": []string{"192.0.2.4"}, "object": []any{}}, "dst_addr": map[string]any{"custom": []string{"203.0.113.0/24"}, "object": []any{}}, "src_addr_inv": 1, "dst_addr_inv": 0}}
					}
					if mode == "nat_failure" || mode == "return_nat_failure" {
						s.Faults = []routerfake.IkuaiFault{{Func: "nat_rule", Action: "add", Count: 1, Code: pointer(2), Message: "synthetic refusal"}}
					}
					if mode == "route_failure" {
						s.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "add", Count: 1, Code: pointer(2), Message: "synthetic refusal"}}
					}
					f := routerfake.NewIkuai(s)
					server := httptest.NewServer(f)
					defer server.Close()
					state = s
					reset = func() { f.Reset(s) }
					tables = func() any { return f.Snapshot().Tables }
					writes = func() any {
						out := []routerfake.IkuaiCall{}
						for _, c := range f.Snapshot().Calls {
							if m, ok := c.Body.(map[string]any); ok && m["action"] != "show" && c.Path == "/Action/call" {
								out = append(out, c)
							}
						}
						return out
					}
					d = NewIkuai(server.URL, routerfake.IkuaiUser, routerfake.IkuaiPassword, false)
					tc := r12BoundaryCase{r, state, desired, ret, true, false}
					python := r12Python(t, server.URL, tc)
					pt, pw := tables(), writes()
					reset()
					defer d.Close()
					r12RunCompare(t, tc, d, python, pt, pw, tables, writes)
				}
			})
		}
	}
}
func r12RunCompare(t *testing.T, tc r12BoundaryCase, d Driver, python r12BoundaryOutput, pt, pw any, tables, writes func() any) {
	t.Helper()
	s, e := d.ReadState(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if !tc.Router.ManageReturnRouting {
		RetirePolicyRoutes(s)
	}
	if tc.DHCP {
		s.DhcpDNS, e = d.ReadDHCPDNS(t.Context())
		if e != nil {
			t.Fatal(e)
		}
	}
	report, e := BuildReport(tc.Desired, s)
	if e != nil {
		t.Fatal(e)
	}
	report.ReturnRouting = tc.Return
	plan, e := BuildPlan(t.Context(), tc.Router, &Inspection{Desired: tc.Desired, State: s, Report: report}, d)
	if e != nil {
		t.Fatal(e)
	}
	r12Compare(t, "whole plan", r12PlanComparable(t, python.Plan), r12PlanComparable(t, plan))
	var a *r12Agent
	var agent ReturnAgent
	if tc.Router.ManageReturnRouting {
		copy, _ := json.Marshal(tc.Return)
		var status ReturnStatus
		_ = json.Unmarshal(copy, &status)
		a = &r12Agent{s: &status}
		agent = a
	}
	result := Execute(t.Context(), plan, d, agent)
	r12Compare(t, "result", python.Result, result)
	r12Compare(t, "tables", pt, tables())
	r12Compare(t, "writes", pw, writes())
	calls := []json.RawMessage{}
	if a != nil {
		calls = a.calls
	}
	r12Compare(t, "Agent calls", python.AgentCalls, calls)
}
