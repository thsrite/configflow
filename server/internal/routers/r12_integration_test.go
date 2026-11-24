package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"configflow/internal/model"
	"configflow/internal/routers/routerfake"
)

type r12FullSource struct {
	records map[string]*GatewayAgent
	client  ReturnAgent
}

func (s r12FullSource) Agent(_ context.Context, id string) (*GatewayAgent, error) {
	return s.records[id], nil
}
func (s r12FullSource) ReturnAgent(*GatewayAgent) ReturnAgent { return s.client }

type r12FullCase struct {
	r12BoundaryCase
	Config json.RawMessage         `json:"config"`
	Agents map[string]*model.Agent `json:"agents"`
	Host   ipv6.IPv6Host           `json:"host"`
}

func TestR12FullInspectionStagesPython(t *testing.T) {
	hostCalls := 0
	ipv6Hosts := func(context.Context, *GatewayAgent) (ipv6.IPv6Host, error) { hostCalls++; return r10Host(), nil }
	for _, mode := range []string{"all", "fake_fail", "dhcp_fail", "dns6_fail", "guard_fail", "pd_fail", "pd_skip", "dns6_skip", "ipv4_only"} {
		t.Run(mode, func(t *testing.T) {
			s := r11State()
			base := r08State()
			s.Tables[ros.PathRoutes] = base.Tables[ros.PathRoutes][:1]
			s.Tables[ros.PathNAT] = base.Tables[ros.PathNAT]
			s.Tables[ros.PathDHCPNetworks][0]["dns-server"] = "192.0.2.99"
			if mode == "pd_skip" {
				s.Tables[pdClients] = []routerfake.Row{}
			}
			if mode == "dns6_skip" {
				s.Tables[ros.PathIPv6ND] = []routerfake.Row{{".id": "*E0", "interface": "all", "advertise-dns": "true", "disabled": "false"}}
				s.Tables[rosIPv4Addresses] = append(s.Tables[rosIPv4Addresses], routerfake.Row{".id": "*B2", "address": "198.51.100.1/24", "interface": "other"})
			}
			switch mode {
			case "fake_fail":
				s.Faults = []routerfake.Fault{{Method: "PUT", Path: ros.PathIPv6Routes, Count: 1, Status: 403}}
			case "dhcp_fail":
				s.Faults = []routerfake.Fault{{Method: "PATCH", Path: ros.PathDHCPNetworks + "/*D0", Count: 1, Status: 403}}
			case "dns6_fail":
				s.Faults = []routerfake.Fault{{Method: "PATCH", Path: ros.PathIPv6ND + "/*E0", Count: 1, Status: 403}}
			case "guard_fail":
				s.Faults = []routerfake.Fault{{Method: "PUT", Path: ros.PathFilter, Count: 1, Status: 403}}
			case "pd_fail":
				s.Faults = []routerfake.Fault{{Method: "PATCH", Path: pdClients + "/*C0", Count: 1, Status: 403}}
			}
			r := r10Router()
			r.Endpoint = "http://192.0.2.1"
			r.ProxyGatewayAgentID = "gateway"
			r.DNSAgentID = "dns"
			r.DNSGuard = &spec.DNSGuard{Enabled: true, BlockDoT: true, Failover: true, Bypass: []string{}}
			r.IPv6PDGuard = &spec.PDGuard{Enabled: true}
			yaml := r10YAML
			if mode == "ipv4_only" {
				yaml = "{}"
				r.IPv6PDGuard = nil
				r.DNSAgentID = ""
				r.DNSGuard = nil
			}
			cfg := device.JSON(map[string]any{"profile_id": "default", "mihomo": map[string]string{"custom_config": yaml}, "rule_configs": []any{}})
			gateway := r12Gateway()
			dns := &model.Agent{ID: "dns", Host: "192.0.2.2", ServiceType: "oxidns", Status: "online", ProfileID: "default"}
			records := map[string]*model.Agent{"gateway": gateway.Agent, "dns": dns}
			source := r12FullSource{records: map[string]*GatewayAgent{"gateway": gateway, "dns": {Agent: dns, DNS: &device.DNSAgent{ID: dns.ID, Host: dns.Host, ServiceType: dns.ServiceType, Status: dns.Status}}}}
			tc := r12FullCase{r12BoundaryCase: r12BoundaryCase{Router: r, State: s, Desired: r12Desired(), Return: r12SafeStatus(), Execute: true}, Config: cfg, Agents: records, Host: r10Host()}
			f := routerfake.New(nil)
			server := httptest.NewServer(f)
			defer server.Close()
			python := r12Python(t, server.URL, tc)
			pythonState := f.Snapshot()
			f.Reset(s)
			d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			hostCalls = 0
			inspection, e := Inspect(t.Context(), InspectRequest{ProfileID: "default", Config: cfg, Router: r, Driver: d, IPv6Hosts: ipv6Hosts, Agents: source})
			if e != nil {
				t.Fatal(e)
			}
			if (mode == "ipv4_only" && hostCalls != 0) || (mode != "ipv4_only" && hostCalls != 1) {
				t.Fatalf("host requests %d", hostCalls)
			}
			plan, e := BuildPlan(t.Context(), r, inspection, d)
			if e != nil {
				t.Fatal(e)
			}
			r12Compare(t, "report", python.Report, inspection.Report)
			r12Compare(t, "plan", r12PlanComparable(t, python.Plan), r12PlanComparable(t, plan))
			result := Execute(t.Context(), plan, d, nil)
			r12Compare(t, "result", python.Result, result)
			r12Compare(t, "device tables", pythonState.Tables, f.Snapshot().Tables)
			r12Compare(t, "device writes", r10Writes(t, pythonState.Calls), r10Writes(t, f.Calls()))
		})
	}
}
