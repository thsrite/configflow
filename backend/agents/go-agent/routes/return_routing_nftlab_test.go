//go:build nftlab

package routes

// Real-kernel return-routing lifecycle for lab L5-D1. Runs as root inside a
// disposable container (integration/return_routing_nft_lab.sh); never on a
// host whose network matters.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var labGateway = regexp.MustCompile(`default via (\d+\.\d+\.\d+\.\d+)`)

func nftLabSetup(t *testing.T) (*Config, string) {
	t.Helper()
	if os.Getenv("CONFIGFLOW_NFT_LAB") != "1" {
		t.Skip("set CONFIGFLOW_NFT_LAB=1 inside a disposable container")
	}
	old := sysctlRoot
	sysctlRoot = t.TempDir()
	t.Cleanup(func() { sysctlRoot = old })
	out, err := runCommand("ip", "-4", "route", "show", "default")
	if err != nil {
		t.Fatal(err)
	}
	m := labGateway.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no default gateway: %q", out)
	}
	// Each case starts from a host without return-routing resources.
	_, _ = runCommand("nft", "delete", "table", "inet", managedReturnTable)
	for _, table := range []string{"104", "105"} {
		_, _ = runCommand("ip", "-4", "route", "flush", "table", table)
	}
	for _, priority := range []string{"104", "105"} {
		for {
			if _, err := runCommand("ip", "-4", "rule", "del", "priority", priority); err != nil {
				break
			}
		}
	}
	return testConfig(t, "mode: rule\n"), m[1]
}

func labSpec(router string, protocol int) ReturnRoutingSpec {
	spec := ReturnRoutingSpec{SafetyVersion: protocol, RouterIP: router, Mark: "0x400000", Table: 104, Priority: 104}
	if protocol == returnProtocolByDestination {
		spec.ProxyDst = []string{"198.18.0.0/16", "203.0.113.7/32"}
	}
	return spec
}

// assertLabClean checks that only the empty retained table is left.
func assertLabClean(t *testing.T) {
	t.Helper()
	routes, _ := runCommand("ip", "-4", "route", "show", "table", "104")
	rules, _ := runCommand("ip", "-4", "rule", "show")
	nft, _ := runCommand("nft", "list", "table", "inet", managedReturnTable)
	if strings.TrimSpace(routes) != "" || strings.Contains(rules, "lookup 104") || strings.Contains(nft, "chain") {
		t.Fatalf("resources left behind:\nroutes=%s\nrules=%s\nnft=%s", routes, rules, nft)
	}
}

func assertLabHealthy(t *testing.T, cfg *Config) {
	t.Helper()
	if status := managedReturnStatus(cfg); status["table_present"] != true {
		t.Fatalf("readback not healthy: %v", status)
	}
	if status := returnSafetyStatus(cfg); status["ok"] != true {
		t.Fatalf("plan blocked after apply: %v", status)
	}
}

func TestNFTLabReturnRoutingLifecycle(t *testing.T) {
	version, _ := runCommand("nft", "--version")
	t.Logf("%s", strings.TrimSpace(version))
	for _, protocol := range []int{returnProtocolByRouter, returnProtocolByDestination} {
		t.Run(map[int]string{1: "by-destination", 2: "by-router"}[protocol], func(t *testing.T) {
			cfg, router := nftLabSetup(t)
			for round := 1; round <= 2; round++ {
				if _, err := ApplyReturnRouting(cfg, labSpec(router, protocol)); err != nil {
					t.Fatalf("apply round %d: %v", round, err)
				}
				assertLabHealthy(t, cfg)
				reapplyReturnRouting(cfg) // Agent restart: must keep resources
				assertLabHealthy(t, cfg)
				if _, err := RemoveReturnRouting(cfg, false); err != nil {
					t.Fatalf("remove round %d: %v", round, err)
				}
				assertLabClean(t)
			}
		})
	}
}

// writePre191Residue reproduces what Agents before 1.9.1 left on nft 1.0.6:
// journaled routes and rule, nft objects with table/chain comments written
// but never recorded, and a pending transaction.
func writePre191Residue(t *testing.T, cfg *Config, spec ReturnRoutingSpec) {
	t.Helper()
	resolved, err := prepareSpec(cfg, spec)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := newReturnOwner()
	if err != nil {
		t.Fatal(err)
	}
	state := &returnRoutingState{Spec: spec, Ownership: owner}
	for _, dst := range []string{"default", resolved.lanCIDR} {
		expected := ownedRouteExpectation(resolved, owner, dst)
		args, _ := routeArguments("add", expected)
		if _, err = runCommand("ip", args...); err != nil {
			t.Fatal(err)
		}
		resources, _ := readReturnResources()
		match, _ := matchingResource(expected, resources.Routes)
		if match == nil {
			t.Fatal("route readback")
		}
		owner.Routes = append(owner.Routes, match)
	}
	rule := ownedRuleExpectation(resolved)
	args, _ := ruleArguments("add", rule)
	if _, err = runCommand("ip", args...); err != nil {
		t.Fatal(err)
	}
	resources, _ := readReturnResources()
	match, _ := matchingResource(rule, resources.Rules)
	if match == nil {
		t.Fatal("rule readback")
	}
	owner.Rules = append(owner.Rules, match)
	commands := []map[string]interface{}{}
	for _, object := range desiredReturnNFT(resolved, owner) {
		verb := "create"
		for kind, body := range object {
			if kind == "rule" {
				verb = "add"
			} else {
				body.(map[string]interface{})["comment"] = ownerTag(owner)
			}
		}
		commands = append(commands, map[string]interface{}{verb: object})
	}
	if err = runNFT(commands); err != nil {
		t.Fatal(err)
	}
	if err = saveReturnState(cfg, state); err != nil {
		t.Fatal(err)
	}
	// nft 1.0.6 drops table/chain comments from JSON input altogether; newer
	// releases store and list them, so the residue there carries the old tags.
	version, _ := runCommand("nft", "--version")
	listing, _ := runCommand("nft", "list", "table", "inet", managedReturnTable)
	if !strings.Contains(version, "v1.0.6") && !strings.Contains(listing, "comment \""+ownerTag(owner)+"\"") {
		t.Fatalf("residue must carry the old table/chain comments:\n%s", listing)
	}
}

func TestNFTLabReturnRoutingRecoversPre191Residue(t *testing.T) {
	for _, first := range []string{"remove", "apply"} {
		t.Run(first, func(t *testing.T) {
			cfg, router := nftLabSetup(t)
			spec := labSpec(router, returnProtocolByRouter)
			writePre191Residue(t, cfg, spec)
			if status := returnSafetyStatus(cfg); status["ok"] != true {
				t.Fatalf("plan blocked by recoverable residue: %v", status)
			}
			if first == "apply" {
				if _, err := ApplyReturnRouting(cfg, spec); err != nil {
					t.Fatalf("apply over residue: %v", err)
				}
				assertLabHealthy(t, cfg)
			}
			if _, err := RemoveReturnRouting(cfg, false); err != nil {
				t.Fatalf("remove residue: %v", err)
			}
			assertLabClean(t)
			if _, err := ApplyReturnRouting(cfg, spec); err != nil {
				t.Fatalf("re-enable after recovery: %v", err)
			}
			assertLabHealthy(t, cfg)
			if _, err := RemoveReturnRouting(cfg, false); err != nil {
				t.Fatal(err)
			}
			assertLabClean(t)
		})
	}
}
