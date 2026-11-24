package dnsguard_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"configflow/internal/routers/device"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/routerfake"
	"configflow/internal/routers/spec"
	"configflow/internal/testgolden"
)

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

func r09SeedState() routerfake.State {
	return routerfake.State{Tables: map[string][]routerfake.Row{
		ros.PathRoutes: {{".id": "*10", "dst-address": "192.0.2.0/24", "gateway": "bridge", "immediate-gw": "bridge", "connect": "true", "active": "true", "dynamic": "true", "distance": "0", "routing-table": "main"}},
		ros.PathNAT: {
			{".id": "*20", "chain": "dstnat", "action": "dst-nat", "protocol": "udp", "dst-port": "53", "to-addresses": "192.0.2.2", "comment": "old-dns", "disabled": "false"},
			{".id": "*21", "chain": "srcnat", "action": "masquerade", "out-interface": "ether1", "disabled": "false"},
		},
	}}
}

func r09Desired(settings spec.DNSGuard, previous json.RawMessage) *spec.Desired {
	return &spec.Desired{
		FakeIPRange:  "198.18.0.0/16",
		ProxyGateway: "192.0.2.4",
		ProxyCIDRs:   []spec.ProxyCIDR{{CIDR: "198.18.0.0/16", Sources: []string{"fake-ip"}}},
		DNSSettings:  &device.DNSSettings{Server: "192.0.2.2"},
		DNSGuard:     &spec.DesiredDNSGuard{Settings: settings, Previous: previous},
	}
}

func runDNSGuardSequence(t *testing.T, f *routerfake.Fake, endpoint string, actions []string) map[string]any {
	t.Helper()
	f.Reset(r09SeedState())
	c := ros.NewClient(endpoint, "synthetic-user", "synthetic-pass", false)
	defer c.Close()
	ctx := context.Background()

	settings := spec.DNSGuard{Enabled: true, BlockDoT: true, Failover: true, Bypass: []string{"192.0.2.50/32"}}
	var previous json.RawMessage
	steps := []map[string]any{}

	for _, action := range actions {
		c.SetDNSGuardManifest(previous)
		routes, err := c.List(ctx, ros.PathRoutes)
		if err != nil {
			t.Fatal(err)
		}
		nats, err := c.List(ctx, ros.PathNAT)
		if err != nil {
			t.Fatal(err)
		}
		snap, err := dnsguard.Read(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		nat := &ros.NatContext{Routes: routes, Rules: nats}
		plan := dnsguard.Build(r09Desired(settings, ros.GuardRaw(previous)), snap, nat)

		stepOut := map[string]any{"action": action, "plan": plan, "manifest": ros.GuardRaw(previous)}
		if action != "plan" && len(plan.Blockers) == 0 && len(plan.Ops) > 0 {
			undo, err := dnsguard.Apply(ctx, c, *plan.Ops[0].(*dnsguard.Replace))
			if err != nil {
				t.Fatal(err)
			}
			stepOut["undo"] = undo
			if action == "roundtrip" {
				if err := dnsguard.Restore(ctx, c, *undo); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := dnsguard.Verify(ctx, c, &dnsguard.Check{Roles: plan.Roles}); err != nil {
					t.Fatal(err)
				}
			}
			previous = ros.GuardRaw(c.DNSGuardManifest())
			stepOut["manifest"] = previous
		}
		steps = append(steps, stepOut)
	}

	snap := f.Snapshot()
	writes := []routerfake.Call{}
	for _, call := range snap.Calls {
		if call.Method != "GET" {
			writes = append(writes, call)
		}
	}
	return map[string]any{
		"steps":  steps,
		"tables": snap.Tables,
		"writes": writes,
	}
}

// TestDNSGuardGoldenReplay records and replays deterministic Go-native boundary
// outputs for R09 (RouterOS DNS guard plan/publish/withdraw/rollback).
func TestDNSGuardGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	cases := []struct {
		name    string
		actions []string
	}{
		{"publish_idempotent", []string{"publish", "publish"}},
		{"publish_and_withdraw", []string{"publish", "plan"}},
		{"roundtrip_rollback", []string{"roundtrip"}},
	}
	for _, tc := range cases {
		t.Run("R09/"+tc.name, func(t *testing.T) {
			f := routerfake.New(nil)
			srv := httptest.NewServer(f)
			defer srv.Close()
			out := runDNSGuardSequence(t, f, srv.URL, tc.actions)
			ref.Assert(t, goldenMarshal(t, map[string]string{"module": "R09", "case": tc.name}), goldenMarshal(t, comparableValue(t, out)))
		})
	}
}
