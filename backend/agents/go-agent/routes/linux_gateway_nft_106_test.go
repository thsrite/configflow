package routes

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This fixture is the actual Debian 12 nft 1.0.6 read-back from the failed
// isolated acceptance run: table/chain comments disappeared, rule comments did
// not. Match that output after real transaction submission, not a permissive
// mock which echoes unsupported JSON fields back unchanged.
func useGatewayNFT106(t *testing.T, f *gatewayFixture) {
	t.Helper()
	original := gatewayRunInput
	gatewayRunInput = func(input, name string, args ...string) (string, error) {
		result, err := original(input, name, args...)
		if err == nil && args[0] != "-c" && len(f.nft) != 0 {
			state, loadErr := loadGatewayState(f.cfg)
			if loadErr != nil || state == nil {
				t.Fatalf("missing creation journal: %+v %v", state, loadErr)
			}
			f.nft = gatewayNFTFixture(t, state, "1.0.6")
		}
		return result, err
	}
}

func legacyPendingGateway106(t *testing.T, f *gatewayFixture) *gatewayState {
	t.Helper()
	state := &gatewayState{Version: 1, Token: strings.Repeat("c", 32), Spec: f.spec,
		ProfileID: "test-profile", TunDevice: "Meta", Pending: true, Original: map[string]string{}}
	state.NFT = gatewayDesiredNFT(state)
	legacyGatewayMembership(state.NFT)
	if err := recordGatewaySysctls(state); err != nil {
		t.Fatal(err)
	}
	if err := saveGatewayState(f.cfg, state); err != nil {
		t.Fatal(err)
	}
	f.nft = gatewayNFTFixture(t, state, "1.0.6")
	return state
}

func assertGateway106Pinned(t *testing.T, cfg *Config) {
	t.Helper()
	state, err := loadGatewayState(cfg)
	if err != nil || state == nil || !gatewayNFTObjectCommentsMissing(state.NFT) || !gatewayNFTHandles(state.NFT, true) {
		t.Fatalf("kernel representation and identities not pinned: %+v %v", state, err)
	}
}

func TestLinuxGatewayNFT106LifecycleAndRetry(t *testing.T) {
	f := newGatewayFixture(t)
	useGatewayNFT106(t, f)
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatalf("Debian 12 apply failed: %v", err)
	}
	assertGateway106Pinned(t, f.cfg)
	if status := gatewayManaged(f.cfg); status["healthy"] != true {
		t.Fatalf("nft 1.0.6 gateway unhealthy: %+v", status)
	}
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil || f.mutations != 1 {
		t.Fatalf("repeat apply recreated or rejected the table: %v, mutations %d", err, f.mutations)
	}
	if err := removeLinuxGateway(f.cfg); err != nil {
		t.Fatal(err)
	}
	if state, err := loadGatewayState(f.cfg); err != nil || state != nil || len(f.nft) != 0 {
		t.Fatalf("revoke left managed resources: %+v %v", state, err)
	}
	if value, _ := gatewayReadSysctl("net/ipv4/ip_forward"); value != "0" {
		t.Fatal("revoke did not restore forwarding")
	}
}

func TestLinuxGatewayNFT106RecoversLegacyPendingByRetryOrRevoke(t *testing.T) {
	for _, action := range []string{"retry", "revoke"} {
		t.Run(action, func(t *testing.T) {
			f := newGatewayFixture(t)
			legacyPendingGateway106(t, f)
			if action == "retry" {
				if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil || f.mutations != 0 {
					t.Fatalf("legacy retry rejected or recreated table: %v, mutations %d", err, f.mutations)
				}
				assertGateway106Pinned(t, f.cfg)
			}
			if err := removeLinuxGateway(f.cfg); err != nil || len(f.nft) != 0 {
				t.Fatalf("legacy revoke failed: %v", err)
			}
			if value, _ := gatewayReadSysctl("net/ipv4/ip_forward"); value != "0" {
				t.Fatal("legacy recovery failed to restore forwarding")
			}
		})
	}
}

func TestLinuxGatewayNFT106RecreatesMissingTableAfterRestart(t *testing.T) {
	f := newGatewayFixture(t)
	useGatewayNFT106(t, f)
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	// nft rules are volatile, unlike the durable active journal.
	f.nft = []map[string]any{}
	if err := reapplyLinuxGateway(f.cfg, 1, func() { t.Fatal("unexpected retry") }); err != nil {
		t.Fatalf("boot replay failed to recreate the vanished table: %v", err)
	}
	assertGateway106Pinned(t, f.cfg)
	if managed := gatewayManaged(f.cfg); managed["healthy"] != true || f.mutations != 2 {
		t.Fatalf("recreated gateway is unhealthy: %+v, mutations %d", managed, f.mutations)
	}
	if err := removeLinuxGateway(f.cfg); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxGatewayNFT106RejectsUnprovenOrChangedResources(t *testing.T) {
	for _, phase := range []string{"legacy-pending", "pinned"} {
		for _, test := range []struct {
			name   string
			change func(*gatewayState, *gatewayFixture)
		}{
			{"rule comment removed", func(_ *gatewayState, f *gatewayFixture) { delete(f.nft[3]["rule"].(map[string]any), "comment") }},
			{"rule comment changed", func(_ *gatewayState, f *gatewayFixture) {
				f.nft[4]["rule"].(map[string]any)["comment"] = "foreign"
			}},
			{"foreign table comment", func(_ *gatewayState, f *gatewayFixture) {
				f.nft[0]["table"].(map[string]any)["comment"] = "foreign"
			}},
			{"partial metadata omission", func(s *gatewayState, f *gatewayFixture) {
				f.nft[1]["chain"].(map[string]any)["comment"] = gatewayOwner(s)
			}},
			{"empty table comment", func(_ *gatewayState, f *gatewayFixture) {
				f.nft[0]["table"].(map[string]any)["comment"] = ""
			}},
			{"missing table handle", func(_ *gatewayState, f *gatewayFixture) { delete(f.nft[0]["table"].(map[string]any), "handle") }},
			{"zero chain handle", func(_ *gatewayState, f *gatewayFixture) { f.nft[1]["chain"].(map[string]any)["handle"] = 0 }},
			{"invalid rule handle", func(_ *gatewayState, f *gatewayFixture) { f.nft[3]["rule"].(map[string]any)["handle"] = -1 }},
			{"extra rule", func(_ *gatewayState, f *gatewayFixture) {
				f.nft = append(f.nft, nftObject("rule", map[string]any{"family": "ip", "table": gatewayTable, "chain": "forward", "expr": []any{map[string]any{"drop": nil}}}))
			}},
			{"missing rule", func(_ *gatewayState, f *gatewayFixture) { f.nft = f.nft[:5] }},
			{"chain policy changed", func(_ *gatewayState, f *gatewayFixture) {
				f.nft[1]["chain"].(map[string]any)["policy"] = "drop"
			}},
			{"rule expression changed", func(_ *gatewayState, f *gatewayFixture) {
				f.nft[3]["rule"].(map[string]any)["expr"].([]any)[2] = map[string]any{"drop": nil}
			}},
			{"expression order changed", func(_ *gatewayState, f *gatewayFixture) {
				expr := f.nft[3]["rule"].(map[string]any)["expr"].([]any)
				expr[0], expr[1] = expr[1], expr[0]
			}},
			{"chain rule order changed", func(_ *gatewayState, f *gatewayFixture) { f.nft[3], f.nft[4] = f.nft[4], f.nft[3] }},
		} {
			t.Run(phase+"/"+test.name, func(t *testing.T) {
				f := newGatewayFixture(t)
				var state *gatewayState
				if phase == "legacy-pending" {
					state = legacyPendingGateway106(t, f)
				} else {
					useGatewayNFT106(t, f)
					if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
						t.Fatal(err)
					}
					state, _ = loadGatewayState(f.cfg)
				}
				test.change(state, f)
				mutations := f.mutations
				path, _ := gatewayPath(f.cfg)
				before, _ := os.ReadFile(path)
				for _, action := range []func() error{
					func() error { return applyLinuxGateway(f.cfg, f.spec, "test-profile") },
					func() error { return removeLinuxGateway(f.cfg) },
				} {
					if err := action(); err == nil {
						t.Fatal("accepted externally changed resources")
					}
					current, _ := os.ReadFile(path)
					if f.mutations != mutations || !bytes.Equal(before, current) {
						t.Fatal("rejected action mutated resources or journal")
					}
				}
			})
		}
	}
}

func TestLinuxGatewayNFT106MissingCommentsNeverOverridePinnedIdentity(t *testing.T) {
	for _, changed := range []string{"table", "chain", "rule"} {
		t.Run(changed, func(t *testing.T) {
			f := newGatewayFixture(t)
			useGatewayNFT106(t, f)
			if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
				t.Fatal(err)
			}
			index := map[string]int{"table": 0, "chain": 1, "rule": 3}[changed]
			f.nft[index][changed].(map[string]any)["handle"] = 99999
			if err := removeLinuxGateway(f.cfg); err == nil || f.mutations != 1 {
				t.Fatal("deleted replacement resource")
			}
		})
	}
	for _, change := range []func(*gatewayState){
		func(s *gatewayState) { s.Active = true },
		func(s *gatewayState) { s.Pending = false },
		func(s *gatewayState) { s.AppliedAt = "already-applied" },
		func(s *gatewayState) { s.NFT[0]["table"].(map[string]any)["handle"] = 5 },
	} {
		f := newGatewayFixture(t)
		state := legacyPendingGateway106(t, f)
		change(state)
		if err := checkGatewayOwnership(state, f.nft, true); err == nil {
			t.Fatal("noninitial journal ignored missing metadata")
		}
	}
}

func TestLinuxGatewayNFT106DoesNotAdoptSameNameTableWithoutJournal(t *testing.T) {
	f := newGatewayFixture(t)
	state := &gatewayState{Token: strings.Repeat("d", 32), Spec: f.spec}
	f.nft = gatewayNFTFixture(t, state, "1.0.6")
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err == nil || f.mutations != 0 {
		t.Fatal("adopted a same-name table based on apparently valid rule comments")
	}
}

func TestLinuxGatewayNFT106DoesNotRelaxAlreadyPinnedMetadata(t *testing.T) {
	f := newGatewayFixture(t)
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	state, err := loadGatewayState(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	// This was previously captured with comments, so even the complete 1.0.6
	// omission pattern is now a change rather than a creation-time read-back.
	f.nft = gatewayNFTFixture(t, state, "1.0.6")
	if err := removeLinuxGateway(f.cfg); err == nil || f.mutations != 1 {
		t.Fatal("ignored metadata disappearance from an already-pinned table")
	}
}

func TestLinuxGatewayNFT106RejectsModifiedJournalBeforeMutation(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*gatewayState)
	}{
		{"unpinned missing metadata", func(s *gatewayState) {
			for _, object := range s.NFT {
				for _, value := range object {
					delete(value.(map[string]any), "handle")
				}
			}
		}},
		{"rule owner removed", func(s *gatewayState) { delete(s.NFT[3]["rule"].(map[string]any), "comment") }},
		{"foreign metadata", func(s *gatewayState) { s.NFT[0]["table"].(map[string]any)["comment"] = "foreign" }},
		{"rule order", func(s *gatewayState) { s.NFT[3], s.NFT[4] = s.NFT[4], s.NFT[3] }},
	} {
		t.Run(change.name, func(t *testing.T) {
			f := newGatewayFixture(t)
			useGatewayNFT106(t, f)
			if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
				t.Fatal(err)
			}
			state, _ := loadGatewayState(f.cfg)
			change.mutate(state)
			if err := saveGatewayState(f.cfg, state); err != nil {
				t.Fatal(err)
			}
			if err := removeLinuxGateway(f.cfg); err == nil || f.mutations != 1 {
				t.Fatal("modified journal authorized a network mutation")
			}
		})
	}
}

func TestLinuxGatewayNFT106LostMutationReceiptRemainsRecoverable(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprint("retry-", retry), func(t *testing.T) {
			f := newGatewayFixture(t)
			useGatewayNFT106(t, f)
			original := gatewayRunInput
			loseReceipt := true
			gatewayRunInput = func(input, name string, args ...string) (string, error) {
				result, err := original(input, name, args...)
				if err == nil && args[0] != "-c" && loseReceipt {
					loseReceipt = false
					return "", fmt.Errorf("lost committed transaction receipt")
				}
				return result, err
			}
			if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err == nil || len(f.nft) != 6 {
				t.Fatal("fixture did not retain committed table")
			}
			if value, _ := gatewayReadSysctl("net/ipv4/ip_forward"); value != "0" {
				t.Fatal("uncertain transaction enabled forwarding")
			}
			if retry {
				if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil || f.mutations != 1 {
					t.Fatalf("retry failed or recreated table: %v", err)
				}
			}
			if err := removeLinuxGateway(f.cfg); err != nil || len(f.nft) != 0 {
				t.Fatalf("committed transaction could not be revoked: %v", err)
			}
		})
	}
}

func TestLinuxGatewayNFT106SysctlFailureCanRevokeAndRetry(t *testing.T) {
	f := newGatewayFixture(t)
	useGatewayNFT106(t, f)
	write := gatewayWriteSysctl
	failOnce := true
	gatewayWriteSysctl = func(key, value string) error {
		if failOnce && key == "net/ipv4/conf/eth0/send_redirects" {
			failOnce = false
			return fmt.Errorf("test sysctl failure")
		}
		return write(key, value)
	}
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err == nil {
		t.Fatal("ignored sysctl failure")
	}
	assertGateway106Pinned(t, f.cfg)
	if err := removeLinuxGateway(f.cfg); err != nil {
		t.Fatal(err)
	}
	if value, _ := gatewayReadSysctl("net/ipv4/ip_forward"); value != "0" {
		t.Fatal("partial apply did not roll forwarding back")
	}
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	if err := removeLinuxGateway(f.cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.cfg.AgentConfigFile), "linux-gateway.json")); !os.IsNotExist(err) {
		t.Fatal("successful retry left journal")
	}
}
