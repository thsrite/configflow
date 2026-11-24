package ipv6_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/routerfake"
	"configflow/internal/routers/spec"
	"configflow/internal/testgolden"
)

const r10YAML = "ipv6: true\ndns:\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\n  fake-ip-range6: fd00:face::1/64\ntun:\n  enable: true\n"

func goldenMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func comparableValue(t *testing.T, v any) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(goldenMarshal(t, v), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func writeCalls(t *testing.T, calls []routerfake.Call) any {
	t.Helper()
	out := []routerfake.Call{}
	for _, call := range calls {
		if call.Method == "GET" || strings.HasSuffix(call.Path, "/print") {
			continue
		}
		if call.Path == "/execute" {
			body, _ := call.Body.(map[string]any)
			script, _ := body["script"].(string)
			if !strings.Contains(script, "cf-done:") && !strings.Contains(script, "cf-id:") {
				continue
			}
		}
		out = append(out, call)
	}
	return comparableValue(t, out)
}

func r10State() routerfake.State {
	return routerfake.State{IPv6Wire: true, Tables: map[string][]routerfake.Row{
		ros.PathIPv6Routes:       {},
		ipv6.PathIPv6Addresses:   {{".id": "*A1", "address": "fd00:1::1/64", "interface": "bridge"}},
		ipv6.PathIPv4Addresses:   {{".id": "*B1", "address": "192.0.2.1/24", "interface": "bridge", "actual-interface": "bridge"}},
		"/ipv6/firewall/filter":  {},
		ros.PathIPv6AddressLists: {},
		"/interface":             {{".id": "*C1", "name": "bridge"}},
		"/interface/list":        {},
		"/interface/list/member": {},
	}}
}

func r11State() routerfake.State {
	s := r10State()
	s.Tables["/ip/dns"] = []routerfake.Row{{"servers": "192.0.2.2", "dynamic-servers": ""}}
	for _, path := range []string{ipv6.PathPDScripts, ipv6.PathPDSchedulers, ipv6.PathPDClients, ros.PathIPv6AddressLists, ipv6.PathPDPrefixes, ros.PathIPv6ND, "/ipv6/firewall/nat", "/ipv6/firewall/raw", "/ipv6/firewall/mangle"} {
		s.Tables[path] = []routerfake.Row{}
	}
	s.Tables[ipv6.PathPDClients] = []routerfake.Row{
		{".id": "*C0", "interface": "wan", "pool-name": "pool-example", "request": "prefix", "disabled": "false", "status": "bound", "prefix": "2001:db8:1::/60, 1d"},
		{".id": "*C1", "interface": "wan", "request": "prefix", "disabled": "true"},
	}
	s.Tables[ipv6.PathIPv6Addresses] = append(s.Tables[ipv6.PathIPv6Addresses], routerfake.Row{".id": "*A2", "address": "2001:db8:1::1/64", "interface": "bridge", "from-pool": "pool-example", "advertise": "true", "disabled": "false"})
	s.Tables[ros.PathDHCPNetworks] = []routerfake.Row{{".id": "*D0", "address": "192.0.2.0/24", "gateway": "192.0.2.1", "dns-server": "192.0.2.2", "dns-none": "false"}}
	s.Tables[ros.PathDHCPServers] = []routerfake.Row{{".id": "*D1", "name": "lan", "interface": "bridge", "disabled": "false"}}
	for _, p := range []string{ros.PathDHCPLeases, ros.PathDHCPOptions, ros.PathDHCPOptionSets, ros.PathDHCPMatchers} {
		s.Tables[p] = []routerfake.Row{}
	}
	s.Tables[ros.PathIPv6ND] = []routerfake.Row{{".id": "*E0", "interface": "bridge", "advertise-dns": "yes", "dns-servers": "2001:db8::53", "disabled": "false", "dynamic": "false", "invalid": "false", "ra-lifetime": "30m", "mtu": "1500", "comment": "synthetic LAN"}}
	return s
}

func r10Wanted(n int) *spec.Desired {
	d := &spec.Desired{ProxyCIDRs: []spec.ProxyCIDR{}, Warnings: []string{}, IPv6CIDRs: []spec.ProxyCIDR{}, BlockingWarnings: []string{}}
	for i := 0; i < n; i++ {
		d.IPv6CIDRs = append(d.IPv6CIDRs, spec.ProxyCIDR{CIDR: fmt.Sprintf("2001:db8:%x::/48", i+10), Sources: []string{"Synthetic"}})
	}
	return d
}

// TestIPv6GoldenReplay records and replays deterministic Go-native boundary
// outputs for R10 (IPv6 Fake-IP & IPv6 routes) and R11 (IPv6 PD guard, IPv6
// DNS, and watch script template).
func TestIPv6GoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	// R10: IPv6 Fake-IP & IPv6 routes publish/idempotent/withdraw/rollback.
	t.Run("R10_ipv6_lifecycle", func(t *testing.T) {
		f := routerfake.New(nil)
		srv := httptest.NewServer(f)
		defer srv.Close()
		ctx := context.Background()

		for _, feature := range []string{"fake", "routes"} {
			for _, variant := range []string{"publish_idempotent", "withdraw", "rollback"} {
				f.Reset(r10State())
				c := ros.NewClient(srv.URL, "synthetic-user", "synthetic-pass", false)

				type stepSpec struct {
					action string
					cfg    *model.Profile
					agent  *model.Agent
					host   ipv6.IPv6Host
				}
				normal := func(action string) stepSpec {
					return stepSpec{
						action: action,
						cfg:    &model.Profile{Mihomo: model.MihomoSettings{CustomConfig: r10YAML}},
						agent:  &model.Agent{ID: "gateway", Host: "192.0.2.4", ServiceType: "mihomo", Enabled: new(true)},
						host:   ipv6.IPv6Host{Interface: "eth0", Addresses: []ipv6.IPv6HostAddress{{Address: "fd00:1::4", Usable: true}, {Address: "fe80::4"}}},
					}
				}
				var steps []stepSpec
				switch variant {
				case "publish_idempotent":
					steps = []stepSpec{normal("publish"), normal("publish")}
				case "withdraw":
					off := normal("publish")
					off.cfg = &model.Profile{Mihomo: model.MihomoSettings{CustomConfig: "ipv6: false\n"}}
					off.agent = nil
					off.host = ipv6.IPv6Host{Error: "offline"}
					steps = []stepSpec{normal("publish"), off}
				case "rollback":
					steps = []stepSpec{normal("rollback"), normal("plan")}
				}

				results := []json.RawMessage{}
				for _, s := range steps {
					r := &spec.Router{RouterSettings: spec.RouterSettings{ID: "synthetic", Type: "routeros"}, Extra: map[string]json.RawMessage{}}
					if feature == "fake" {
						if c.IPv6FakeIPManifestSet() {
							r.Extra["ipv6_fakeip_state"] = ros.GuardRaw(c.IPv6FakeIPManifest())
						}
						plan := ipv6.InspectIPv6FakeIP(ctx, s.cfg, r, s.agent, s.host, c)
						results = append(results, device.JSON(plan))
						if s.action == "plan" || len(plan.Blockers) > 0 {
							continue
						}
						var undo *ipv6.IPv6FakeIPUndo
						if len(plan.Ops) > 0 {
							var err error
							undo, err = ipv6.ApplyIPv6FakeIP(ctx, c, *plan.Ops[0].(*ipv6.IPv6FakeIPReplace))
							if err != nil {
								t.Fatal(err)
							}
							results = append(results, device.JSON(undo))
						}
						if err := ipv6.VerifyIPv6FakeIP(ctx, c, &plan.Check); err != nil {
							t.Fatal(err)
						}
						if s.action == "rollback" && undo != nil {
							if err := ipv6.RestoreIPv6FakeIP(ctx, c, *undo); err != nil {
								t.Fatal(err)
							}
							results = append(results, json.RawMessage(`{}`))
						}
					} else {
						if c.IPv6RoutesManifestSet() {
							r.Extra["ipv6_routes_state"] = ros.GuardRaw(c.IPv6RoutesManifest())
						}
						plan := ipv6.InspectIPv6Routes(ctx, s.cfg, r, r10Wanted(5), s.agent, s.host, c)
						results = append(results, device.JSON(plan))
						if s.action == "plan" || len(plan.Skip) > 0 {
							continue
						}
						var undo *ipv6.IPv6RoutesUndo
						if len(plan.Ops) > 0 {
							var err error
							undo, err = ipv6.ApplyIPv6Routes(ctx, c, *plan.Ops[0].(*ipv6.IPv6RoutesReplace))
							if err != nil {
								t.Fatal(err)
							}
							results = append(results, device.JSON(undo))
						}
						if err := ipv6.VerifyIPv6Routes(ctx, c, plan.Check); err != nil {
							t.Fatal(err)
						}
						if s.action == "rollback" && undo != nil {
							if err := ipv6.RestoreIPv6Routes(ctx, c, *undo); err != nil {
								t.Fatal(err)
							}
							results = append(results, json.RawMessage(`{}`))
						}
					}
				}
				var manifest json.RawMessage
				if feature == "fake" {
					manifest = ros.GuardRaw(c.IPv6FakeIPManifest())
				} else {
					manifest = ros.GuardRaw(c.IPv6RoutesManifest())
				}
				c.Close()
				snap := f.Snapshot()
				ref.Assert(t, goldenMarshal(t, map[string]string{"module": "R10", "case": feature + "/" + variant}), goldenMarshal(t, map[string]any{
					"output": comparableValue(t, map[string]any{"results": results, "manifest": manifest}),
					"tables": comparableValue(t, snap.Tables),
					"writes": writeCalls(t, snap.Calls),
				}))
			}
		}
	})

	// R11: RouterOS IPv6 PD guard, IPv6 DNS, and watch script template.
	t.Run("R11_ipv6_pd_and_dns", func(t *testing.T) {
		f := routerfake.New(nil)
		f.Reset(r11State())
		srv := httptest.NewServer(f)
		defer srv.Close()
		c := ros.NewClient(srv.URL, "synthetic-user", "synthetic-pass", false)
		defer c.Close()
		ctx := context.Background()

		r := &spec.Router{RouterSettings: spec.RouterSettings{ID: "synthetic", Type: "routeros", IPv6PDGuard: &spec.PDGuard{Enabled: true}}, Extra: map[string]json.RawMessage{}}
		pdPlan, err := ipv6.InspectIPv6PDGuard(ctx, r, "192.0.2.4", c)
		if err != nil {
			t.Fatal(err)
		}
		undo, err := ipv6.ApplyIPv6PDGuard(ctx, c, *pdPlan.Ops[0].(*ipv6.IPv6PDGuardSync))
		if err != nil {
			t.Fatal(err)
		}
		afterPD := comparableValue(t, f.Snapshot().Tables)
		if err := ipv6.RestoreIPv6PDGuard(ctx, c, *undo); err != nil {
			t.Fatal(err)
		}
		afterRestore := comparableValue(t, f.Snapshot().Tables)

		desired := &spec.Desired{ProxyCIDRs: []spec.ProxyCIDR{}, IPv6CIDRs: []spec.ProxyCIDR{}, Warnings: []string{}, BlockingWarnings: []string{}, ProxyGateway: "192.0.2.4", DNSSettings: &device.DNSSettings{AgentID: "dns", Server: "192.0.2.2"}}
		scope := device.DNSCheck{ID: "*D0", Network: "192.0.2.0/24", Gateway: "192.0.2.4", DNS: []string{"192.0.2.2"}}
		dnsPlan, err := ipv6.InspectIPv6DNS(ctx, desired, scope, c)
		if err != nil {
			t.Fatal(err)
		}
		script, err := ipv6.IPv6PDWatchSource("bridge", "pool-example")
		if err != nil {
			t.Fatal(err)
		}
		ref.Assert(t, []byte(`{"module":"R11","case":"pd_dns_template"}`), goldenMarshal(t, map[string]any{
			"pd_status":     pdPlan.Status,
			"dns_status":    dnsPlan.Status,
			"after_pd":      afterPD,
			"after_restore": afterRestore,
			"script":        script,
		}))
	})
}
