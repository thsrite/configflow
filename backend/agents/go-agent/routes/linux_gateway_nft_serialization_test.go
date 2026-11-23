package routes

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Captured from nft 1.1.3 inside the isolated Linux VM, after nft accepted our
// transaction. In particular, nft serializes ct state {established,related}
// with op == even when its JSON input used op in.
func realGatewayNFT(t *testing.T, state *gatewayState) []map[string]any {
	return gatewayNFTFixture(t, state, "1.1.3")
}

func gatewayNFTFixture(t *testing.T, state *gatewayState, version string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/linux_gateway_nft_" + version + ".json")
	if err != nil {
		t.Fatal(err)
	}
	content := strings.NewReplacer(
		"21b2e3d7a26ad459ff4d7c413642c76f", state.Token,
		"192.0.2.0", strings.Split(state.Spec.LANCIDR, "/")[0],
		"192.0.2.2", state.Spec.Address,
		"lan0", state.Spec.Interface,
	).Replace(string(raw))
	var document struct {
		NFT []map[string]any `json:"nftables"`
	}
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		t.Fatal(err)
	}
	return gatewayNFTObjects(document.NFT)
}

func legacyGatewayMembership(objects []map[string]any) {
	for _, object := range objects {
		rule, ok := object["rule"].(map[string]any)
		if !ok || !strings.HasSuffix(rule["comment"].(string), ":reply") {
			continue
		}
		rule["expr"].([]any)[2].(map[string]any)["match"].(map[string]any)["op"] = "in"
	}
}

func TestLinuxGatewayRealNFTSerializationLifecycle(t *testing.T) {
	f := newGatewayFixture(t)
	originalRunInput := gatewayRunInput
	gatewayRunInput = func(input, name string, args ...string) (string, error) {
		output, err := originalRunInput(input, name, args...)
		if err == nil && args[0] != "-c" && len(f.nft) != 0 {
			tag := f.nft[0]["table"].(map[string]any)["comment"].(string)
			f.nft = realGatewayNFT(t, &gatewayState{Token: strings.TrimPrefix(tag, "configflow-gateway:"), Spec: f.spec})
		}
		return output, err
	}
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	if managed := gatewayManaged(f.cfg); !managed.Healthy {
		t.Fatalf("real nft response not healthy: %+v", managed)
	}
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	if f.mutations != 1 {
		t.Fatal("repeated apply changed the existing table")
	}
	if err := removeLinuxGateway(f.cfg); err != nil {
		t.Fatal(err)
	}
	if len(f.nft) != 0 {
		t.Fatal("real nft table was not removed")
	}
}

func TestLinuxGatewayLegacyPendingJournalRecoversKernelHandles(t *testing.T) {
	f := newGatewayFixture(t)
	state := &gatewayState{Version: 1, Token: strings.Repeat("a", 32), Spec: f.spec, ProfileID: "test-profile", TunDevice: "Meta", Pending: true, Original: map[string]string{}}
	state.NFT = gatewayDesiredNFT(state)
	legacyGatewayMembership(state.NFT)
	if err := recordGatewaySysctls(state); err != nil {
		t.Fatal(err)
	}
	if err := saveGatewayState(f.cfg, state); err != nil {
		t.Fatal(err)
	}
	f.nft = realGatewayNFT(t, state)
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatalf("retry rejected old pending journal: %v", err)
	}
	if f.mutations != 0 {
		t.Fatal("retry recreated existing table")
	}
	recovered, err := loadGatewayState(f.cfg)
	if err != nil || recovered == nil || !recovered.Active || recovered.Pending {
		t.Fatalf("recovery did not finish: %+v %v", recovered, err)
	}
	for _, object := range recovered.NFT {
		for _, value := range object {
			if value.(map[string]any)["handle"] == nil {
				t.Fatal("recovery failed to persist an existing kernel handle")
			}
		}
	}
	f.nft[0]["table"].(map[string]any)["handle"] = 99999
	if err := removeLinuxGateway(f.cfg); err == nil {
		t.Fatal("recovered ownership accepted a replacement table")
	}
}

func TestLinuxGatewayNFTCompatibilityStillRejectsExternalChanges(t *testing.T) {
	state := &gatewayState{Token: strings.Repeat("b", 32), Spec: LinuxGatewaySpec{Interface: "lan0", Address: "192.0.2.2", LANCIDR: "192.0.2.0/24", UpstreamGateway: "192.0.2.1"}}
	state.NFT = realGatewayNFT(t, state)
	legacyGatewayMembership(state.NFT)
	if err := checkGatewayOwnership(state, realGatewayNFT(t, state), true); err != nil {
		t.Fatalf("legacy spelling should be equivalent: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func([]map[string]any)
	}{
		{"table handle", func(actual []map[string]any) { actual[0]["table"].(map[string]any)["handle"] = 4000 }},
		{"ownership comment", func(actual []map[string]any) {
			actual[4]["rule"].(map[string]any)["comment"] = "foreign"
		}},
		{"different states", func(actual []map[string]any) {
			match := actual[4]["rule"].(map[string]any)["expr"].([]any)[2].(map[string]any)["match"].(map[string]any)
			match["right"] = map[string]any{"set": []any{"new", "related"}}
		}},
		{"different operator", func(actual []map[string]any) {
			actual[4]["rule"].(map[string]any)["expr"].([]any)[2].(map[string]any)["match"].(map[string]any)["op"] = "!="
		}},
		{"verdict", func(actual []map[string]any) {
			actual[4]["rule"].(map[string]any)["expr"].([]any)[3] = map[string]any{"drop": nil}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual := realGatewayNFT(t, state)
			test.mutate(actual)
			if err := checkGatewayOwnership(state, actual, true); err == nil {
				t.Fatal("accepted external change")
			}
		})
	}
	match := state.NFT[4]["rule"].(map[string]any)["expr"].([]any)[2].(map[string]any)["match"].(map[string]any)
	if match["op"] != "in" {
		t.Fatal("comparison mutated the loaded journal")
	}
}
