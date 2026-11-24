package rospolicy_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"configflow/internal/routers/routerfake"
	"configflow/internal/routers/spec"
	"configflow/internal/testgolden"
)

const (
	r08Gateway = "192.0.2.3"
	r08Target  = "198.51.100.0/25"
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
	d := &spec.Desired{
		FakeIPRange:  "198.18.0.0/16",
		ProxyGateway: r08Gateway,
		ProxyCIDRs:   []spec.ProxyCIDR{{CIDR: "198.18.0.0/16", Sources: []string{"fake-ip"}}},
		Warnings:     []string{},
	}
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

func readPolicyNat(ctx context.Context, c *ros.Client) (*ros.NatContext, *rospolicy.ROSPolicySnapshot, error) {
	routes, err := c.List(ctx, ros.PathRoutes)
	if err != nil {
		return nil, nil, err
	}
	nats, err := c.List(ctx, ros.PathNAT)
	if err != nil {
		return nil, nil, err
	}
	snap, err := rospolicy.SnapshotROSPolicy(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	nat := &ros.NatContext{
		Routes:       routes,
		Rules:        nats,
		Mangle:       (*snap)[ros.PathMangle],
		RoutingRules: (*snap)["/routing/rule"],
		VRFs:         (*snap)["/ip/vrf"],
	}
	return nat, snap, nil
}

// TestROSPolicyGoldenReplay records and replays deterministic Go-native
// boundary outputs for R08 (RouterOS policy routing plan + apply + withdraw).
func TestROSPolicyGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	t.Run("R08_rospolicy_lifecycle", func(t *testing.T) {
		f := routerfake.New(nil)
		f.Reset(r08State())
		srv := httptest.NewServer(f)
		defer srv.Close()
		c := ros.NewClient(srv.URL, "synthetic-user", "synthetic-pass", false)
		defer c.Close()

		ctx := context.Background()
		readState := func(ctx context.Context) (*device.State, *ros.NatContext, error) {
			nat, _, err := readPolicyNat(ctx, c)
			if err != nil {
				return nil, nil, err
			}
			return &device.State{}, nat, nil
		}

		desired := r08Desired(true)
		desired.ROSPolicy.Previous = slices.Clone(c.PolicyManifest())
		nat, snap, err := readPolicyNat(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		plan := rospolicy.BuildROSPolicy(desired, nat, snap)
		if _, err := rospolicy.ApplyROSPolicy(ctx, c, readState, plan.Ops[0].(*rospolicy.ROSPolicyReplace)); err != nil {
			t.Fatal(err)
		}
		afterApply := comparableValue(t, f.Snapshot().Tables)

		withdrawDesired := r08Desired(false)
		withdrawDesired.ROSPolicy.Previous = slices.Clone(c.PolicyManifest())
		nat, snap, err = readPolicyNat(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		withdrawPlan := rospolicy.BuildROSPolicy(withdrawDesired, nat, snap)
		if _, err := rospolicy.ApplyROSPolicy(ctx, c, readState, withdrawPlan.Ops[0].(*rospolicy.ROSPolicyReplace)); err != nil {
			t.Fatal(err)
		}
		afterWithdraw := comparableValue(t, f.Snapshot().Tables)

		ref.Assert(t, []byte(`{"module":"R08","case":"apply_and_withdraw"}`), goldenMarshal(t, map[string]any{
			"apply_ops":      len(plan.Ops),
			"withdraw_ops":   len(withdrawPlan.Ops),
			"after_apply":    afterApply,
			"after_withdraw": afterWithdraw,
		}))
	})
}
