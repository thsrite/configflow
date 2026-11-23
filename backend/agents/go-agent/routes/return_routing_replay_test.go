package routes

import (
	"strings"
	"testing"
)

func TestReturnRoutingBootReplaySkipsCompleteCurrentResources(t *testing.T) {
	host := &fakeHost{}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
		t.Fatal(err)
	}
	beforeState := jsonText(readStateJSON(t, cfg))
	beforeHost := jsonText([]any{host.routes, host.rules, host.nft})
	host.mutations = nil

	reapplyReturnRouting(cfg) // Agent restart with everything still in place

	assertNoWrites(t, host)
	if beforeState != jsonText(readStateJSON(t, cfg)) || beforeHost != jsonText([]any{host.routes, host.rules, host.nft}) {
		t.Fatal("skipped replay must leave resources and ownership untouched")
	}
	if status := managedReturnStatus(cfg); status["table_present"] != true {
		t.Fatalf("resources must stay healthy: %v", status)
	}
}

func TestReturnRoutingBootReplayStillRunsWhenRouterMACChanged(t *testing.T) {
	host := &fakeHost{}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
		t.Fatal(err)
	}
	// Router replaced: same IP, new MAC. The old rule would mark nothing.
	host.readReplies = map[string]string{"ip neigh show 192.168.1.1 dev eth0": "192.168.1.1 lladdr 00:11:22:33:44:55 REACHABLE\n"}
	host.mutations = nil

	reapplyReturnRouting(cfg)

	if len(host.mutations) == 0 {
		t.Fatal("a changed router MAC must trigger a real replay")
	}
	if text := jsonText(host.nft); !strings.Contains(text, "00:11:22:33:44:55") || strings.Contains(text, "00:e0:b4:66:d5:f9") {
		t.Fatalf("replayed rule must match the new router MAC: %s", text)
	}
	if status := managedReturnStatus(cfg); status["table_present"] != true {
		t.Fatalf("replayed resources must pass readback: %v", status)
	}
}
