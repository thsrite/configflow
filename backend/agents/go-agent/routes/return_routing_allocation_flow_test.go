package routes

import (
	"os"
	"path/filepath"
	"testing"
)

func statusAllocation(t *testing.T, cfg *Config) returnAllocation {
	t.Helper()
	status := returnSafetyStatus(cfg)
	if status["ok"] != true {
		t.Fatalf("expected safe allocation: %v", status)
	}
	allocation, ok := status["allocation"].(returnAllocation)
	if !ok || allocation.Version != 1 {
		t.Fatalf("allocation missing: %v", status)
	}
	return allocation
}

func TestAllocatedReturnRoutingLifecyclePreservesForeignResources(t *testing.T) {
	host := &fakeHost{
		routes: []map[string]any{{"dst": "default", "table": 104, "gateway": "10.0.0.1", "dev": "foreign0", "protocol": 99}},
		rules:  []map[string]any{{"priority": 104, "src": "all", "table": 200, "fwmark": "0x400000", "fwmask": "0x400000"}},
		nft:    []map[string]any{{"table": map[string]any{"family": "inet", "name": "foreign", "handle": 900}}},
	}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	foreignRoutes, foreignRules, foreignNFT := jsonMaps(host.routes), jsonMaps(host.rules), jsonMaps(host.nft)
	allocation := statusAllocation(t, cfg)
	if allocation.Table != 105 || allocation.Priority != 105 || allocation.Mark != "0x800000" || allocation.Reused {
		t.Fatalf("expected automatic avoidance: %+v", allocation)
	}
	assertNoWrites(t, host)
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.AgentConfigFile), "return-routing.json")); !os.IsNotExist(err) {
		t.Fatalf("preview must not persist/reserve allocation: %v", err)
	}
	spec := sampleSpec()
	spec.Table, spec.Priority, spec.Mark = allocation.Table, allocation.Priority, allocation.Mark
	if _, err := ApplyReturnRouting(cfg, spec); err != nil {
		t.Fatal(err)
	}
	state, err := loadReturnState(cfg)
	if err != nil || state.Spec.Table != 105 || state.Spec.Priority != 105 || state.Spec.Mark != "0x800000" || !returnResourcesComplete(state) {
		t.Fatalf("allocation not persisted completely: %+v %v", state, err)
	}
	allocation = statusAllocation(t, cfg)
	if !allocation.Reused || allocation.Table != 105 || allocation.Priority != 105 || allocation.Mark != "0x800000" {
		t.Fatalf("allocation must be reused: %+v", allocation)
	}
	reapplyReturnRouting(cfg)
	state, _ = loadReturnState(cfg)
	if !returnResourcesComplete(state) {
		t.Fatal("restart did not reuse complete owned resources")
	}
	if _, err = RemoveReturnRouting(cfg, false); err != nil {
		t.Fatal(err)
	}
	if !sameJSON(host.routes, foreignRoutes) || !sameJSON(host.rules, foreignRules) {
		t.Fatalf("foreign IP resources changed: %v %v", host.routes, host.rules)
	}
	for _, row := range foreignNFT {
		found, err := matchingResource(row, host.nft)
		if err != nil || found == nil {
			t.Fatalf("foreign nft object changed: %v", host.nft)
		}
	}
	allocation = statusAllocation(t, cfg)
	if !allocation.Reused || allocation.Table != 105 || allocation.Priority != 105 || allocation.Mark != "0x800000" {
		t.Fatalf("inactive owned allocation must remain stable: %+v", allocation)
	}
}

func TestAllocationPreviewBecomesStaleBeforeApply(t *testing.T) {
	host := &fakeHost{}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	first := statusAllocation(t, cfg)
	host.routes = append(host.routes, map[string]any{"dst": "default", "table": 104, "gateway": "10.0.0.1", "dev": "foreign0"})
	before := jsonText(host.routes)
	if _, err := ApplyReturnRouting(cfg, sampleSpec()); err == nil {
		t.Fatal("stale allocation must be refused")
	}
	assertNoWrites(t, host)
	if jsonText(host.routes) != before {
		t.Fatal("stale apply changed foreign route")
	}
	next := statusAllocation(t, cfg)
	if first.Table != 104 || next.Table != 105 {
		t.Fatalf("new preview must select a free table: %+v -> %+v", first, next)
	}
}

func TestAllocationCannotBeChangedBehindThePreview(t *testing.T) {
	host := &fakeHost{}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	spec := sampleSpec()
	spec.Table = 106
	if _, err := ApplyReturnRouting(cfg, spec); err == nil {
		t.Fatal("unoffered allocation must require a new preview")
	}
	assertNoWrites(t, host)
}
