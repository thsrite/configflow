package routes

import (
	"strings"
	"testing"
)

// tagContainers adds the table/chain comments that Agents before 1.9.1 wrote.
func tagContainers(rows []map[string]any, tag string) {
	for _, row := range rows {
		for _, kind := range []string{"table", "chain"} {
			if body, ok := row[kind].(map[string]any); ok {
				body["comment"] = tag
			}
		}
	}
}

func containerComments(rows []map[string]any) int {
	count := 0
	for _, row := range rows {
		for _, kind := range []string{"table", "chain"} {
			if body, ok := row[kind].(map[string]any); ok && body["comment"] != nil {
				count++
			}
		}
	}
	return count
}

func TestReturnRoutingWorksWhenNFTOmitsContainerComments(t *testing.T) {
	host := &fakeHost{nft106: true}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	for round := 0; round < 2; round++ {
		if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
			t.Fatalf("round %d apply: %v", round, err)
		}
		if status, ok := managedReturnStatus(cfg).(*ReturnRoutingManagedDetail); !ok || !status.TablePresent {
			t.Fatalf("round %d readback not healthy: %v", round, status)
		}
		if _, err := RemoveReturnRouting(cfg, false); err != nil {
			t.Fatalf("round %d remove: %v", round, err)
		}
		if len(host.routes) != 0 || len(host.rules) != 0 || len(host.nft) != 1 {
			t.Fatalf("round %d remove left resources: %v %v %v", round, host.routes, host.rules, host.nft)
		}
	}
	if n := containerComments(host.nft); n != 0 {
		t.Fatalf("tables and chains must not carry version-dependent comments: %v", host.nft)
	}
}

// pre191Residue reproduces lab L5-D1: routes and the rule are journaled, the
// nft objects were written with table/chain comments but never recorded, and
// the transaction is still pending.
func pre191Residue(t *testing.T, host *fakeHost, cfg *Config, listsComments bool) *returnRoutingState {
	t.Helper()
	if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
		t.Fatal(err)
	}
	state, err := loadReturnState(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tagContainers(host.nft, ownerTag(state.Ownership))
	state.Ownership.NFT = []map[string]any{}
	state.Ownership.Pending, state.Active, state.AppliedAt = true, false, ""
	if err = saveReturnState(cfg, state); err != nil {
		t.Fatal(err)
	}
	host.nft106 = !listsComments
	host.mutations = nil
	return state
}

func TestReturnRoutingRecoversPre191PendingResidue(t *testing.T) {
	for _, listsComments := range []bool{false, true} {
		for _, first := range []string{"remove", "apply"} {
			name := first + "/nft-1.0.6"
			if listsComments {
				name = first + "/newer-nft"
			}
			t.Run(name, func(t *testing.T) {
				host := &fakeHost{}
				installFakeHost(t, host)
				cfg := testConfig(t, tunConfig)
				pre191Residue(t, host, cfg, listsComments)
				if status := returnSafetyStatus(cfg); !status.OK {
					t.Fatalf("plan must not be blocked by a recoverable residue: %v", status)
				}
				if first == "apply" {
					if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
						t.Fatalf("apply must replay the recovered transaction: %v", err)
					}
					if status, ok := managedReturnStatus(cfg).(*ReturnRoutingManagedDetail); !ok || !status.TablePresent {
						t.Fatalf("replayed resources not healthy: %v", status)
					}
				}
				if _, err := RemoveReturnRouting(cfg, false); err != nil {
					t.Fatalf("remove must undo the recovered transaction: %v", err)
				}
				if len(host.routes) != 0 || len(host.rules) != 0 || len(host.nft) != 1 {
					t.Fatalf("recovered removal left resources: %v %v %v", host.routes, host.rules, host.nft)
				}
				if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
					t.Fatalf("re-enable after recovery: %v", err)
				}
			})
		}
	}
}

func TestReturnRoutingPendingRecoveryRefusesUnprovableObjects(t *testing.T) {
	foreignToken := strings.Repeat("f", 32)
	cases := map[string]func(host *fakeHost, state *returnRoutingState){
		"foreign chain": func(host *fakeHost, _ *returnRoutingState) {
			host.nft = append(host.nft, map[string]any{"chain": map[string]any{"family": "inet", "table": managedReturnTable, "name": "cf_" + foreignToken + "_pre", "handle": 900.0}})
		},
		"untagged rule in owned chain": func(host *fakeHost, state *returnRoutingState) {
			host.nft = append(host.nft, map[string]any{"rule": map[string]any{"family": "inet", "table": managedReturnTable, "chain": "cf_" + state.Ownership.Token + "_pre", "handle": 901.0, "expr": []any{}}})
		},
		"table tagged by another journal": func(host *fakeHost, _ *returnRoutingState) {
			host.nft[0]["table"].(map[string]any)["comment"] = "configflow-return:" + foreignToken
		},
		"empty untagged table": func(host *fakeHost, _ *returnRoutingState) {
			host.nft = host.nft[:1]
			delete(host.nft[0]["table"].(map[string]any), "comment")
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			host := &fakeHost{}
			installFakeHost(t, host)
			cfg := testConfig(t, tunConfig)
			state := pre191Residue(t, host, cfg, true)
			change(host, state)
			before := jsonText(readStateJSON(t, cfg))
			if status := returnSafetyStatus(cfg); status.OK {
				t.Fatalf("plan must stay blocked: %v", status)
			}
			if _, err := ApplyReturnRouting(cfg, sampleSpec()); err == nil {
				t.Fatal("apply must refuse an unprovable residue")
			}
			if _, err := RemoveReturnRouting(cfg, false); err == nil {
				t.Fatal("remove must refuse an unprovable residue")
			}
			assertNoWrites(t, host)
			if before != jsonText(readStateJSON(t, cfg)) {
				t.Fatal("refused recovery changed the journal")
			}
		})
	}
}

// Agents before 1.9.1 recorded table/chain comments; upgrading must neither
// replay (a brief return-routing gap) nor refuse to remove them.
func TestReturnRoutingAcceptsPre191RecordedComments(t *testing.T) {
	host := &fakeHost{}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
		t.Fatal(err)
	}
	state, err := loadReturnState(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tag := ownerTag(state.Ownership)
	tagContainers(host.nft, tag)
	tagContainers(state.Ownership.NFT, tag)
	if err = saveReturnState(cfg, state); err != nil {
		t.Fatal(err)
	}
	host.mutations = nil
	reapplyReturnRouting(cfg)
	assertNoWrites(t, host)
	if _, err = RemoveReturnRouting(cfg, false); err != nil {
		t.Fatal(err)
	}
	if len(host.routes) != 0 || len(host.rules) != 0 || len(host.nft) != 1 {
		t.Fatalf("removal left resources: %v %v %v", host.routes, host.rules, host.nft)
	}
}
