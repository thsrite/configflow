package routers

import (
	"bytes"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/rospolicy"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"configflow/internal/routers/routerfake"
)

func TestR09ReportPreservesPolicyAndDNSGuard(t *testing.T) {
	ctx := t.Context()
	tc := r09Base("combined_report")
	for _, path := range rospolicy.Paths() {
		if _, ok := tc.State.Tables[path]; !ok {
			tc.State.Tables[path] = []routerfake.Row{}
		}
	}
	f := routerfake.New(tc.State.Tables)
	srv := httptest.NewServer(f)
	defer srv.Close()
	d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	state, err := d.ReadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state.ROSPolicy, err = rospolicy.SnapshotROSPolicy(ctx, d.Client)
	if err != nil {
		t.Fatal(err)
	}
	state.DNSGuard, err = dnsguard.Read(ctx, d.Client)
	if err != nil {
		t.Fatal(err)
	}
	tc.Desired.DNSGuard = &DesiredDNSGuard{Settings: tc.Settings}
	tc.Desired.ROSPolicy = &ROSPolicyDesired{Selected: []string{"synthetic-policy"}, Sources: []string{"192.0.2.0/24"}, CIDRs: []ProxyCIDR{{CIDR: "203.0.113.0/24"}}}
	report, err := BuildReport(tc.Desired, state)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, part := range report.Parts {
		counts[part.Key]++
	}
	if counts["dns_guard"] != 1 || counts["policy_routing"] != 1 || state.ROSPolicyPlan == nil || state.DNSGuardPlan == nil || len(state.DNSGuardPlan.Blockers) != 0 || len(state.DNSGuardPlan.Ops) != 1 {
		t.Fatalf("combined planning lost a feature: %s", r09Raw(report))
	}
	if len(state.ROSPolicyPlan.Blockers) != 0 || len(state.ROSPolicyPlan.Ops) != 1 {
		t.Fatalf("combined planning blocked policy: %s", r09Raw(state.ROSPolicyPlan))
	}
	// The real combined desired model must also protect DNS from policy routing.
	tc.Desired.ROSPolicy.CIDRs = []ProxyCIDR{{CIDR: "192.0.2.2/32"}}
	if plan := dnsguard.Build(tc.Desired, state.DNSGuard, state.NatContext); len(plan.Blockers) != 1 || plan.Blockers[0] != dnsguard.MessageProxy || len(plan.Ops) != 0 {
		t.Fatalf("policy destination did not protect DNS: %s", r09Raw(plan))
	}
}

func TestR09ManifestHolderKeepsPolicyPublication(t *testing.T) {
	ctx := t.Context()
	tc := r09Base("manifest_holder")
	f := routerfake.New(tc.State.Tables)
	srv := httptest.NewServer(f)
	defer srv.Close()
	d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	if holder := d.Manifests(); holder.Policy.Set || holder.DNSGuard.Set {
		t.Fatal("new driver reports an assigned manifest")
	}
	undo := r09ApplyPlan(t, d, r09Plan(t, d, tc.Settings, nil))
	holder := d.Manifests()
	if !holder.DNSGuard.Set || !json.Valid(holder.DNSGuard.Value) || holder.Policy.Set {
		t.Fatalf("DNS publication is missing or claims policy: %+v", holder)
	}
	proof := bytes.Clone(holder.DNSGuard.Value)
	holder.DNSGuard.Value[0] = 'x'
	if !bytes.Equal(d.Manifests().DNSGuard.Value, proof) {
		t.Fatal("manifest holder aliases driver ownership proof")
	}
	policy := json.RawMessage(`{"resources":[]}`)
	d.SetPolicyManifest(policy)
	if err := dnsguard.Restore(ctx, d.Client, *undo); err != nil {
		t.Fatal(err)
	}
	holder = d.Manifests()
	if !holder.DNSGuard.Set || len(holder.DNSGuard.Value) != 0 || !holder.Policy.Set || !bytes.Equal(holder.Policy.Value, policy) {
		t.Fatalf("DNS withdrawal clobbered policy or lost explicit null: %+v", holder)
	}
}
