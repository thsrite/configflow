package setup

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/routers/device"
	routers "configflow/internal/routers/spec"
)

// TestGatewayCheckReadsSelectedRuleSetsOffline covers the gateway (not full
// TUN) completion check with a rule set selected for the Linux gateway: its
// IP ranges come from the offline rule-set reader the server wires
// (ReadOfflineRouteContent; before O17 W9 the spec.RulesetContent global).
// Without that owner the check fails closed with device.ErrNotPorted.
func TestGatewayCheckReadsSelectedRuleSetsOffline(t *testing.T) {
	s, ctx := preparedFixture(t, "gateway")
	body := json.RawMessage(`{"domain_rules":{"direct":[],"proxy":[]},"local_dns":"https://192.0.2.53/dns-query"}`)
	if _, err := s.Prepare(ctx, testSessionID, body, "http://localhost"); err != nil {
		t.Fatal(err)
	}
	s.owners.Installation = installedOwner()
	record, err := storeFor(s.repository).read(ctx, testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repository.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		shared.RuleLibrary = append(shared.RuleLibrary, model.RuleLibraryItem{
			ID: "w9-lib", Name: "w9-lib", SourceType: new("content"), Content: new("IP-CIDR,198.51.100.0/24"),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.repository.UpdateProfile(ctx, record.ProfileID, func(_ context.Context, p *model.Profile) error {
		p.RuleConfigs = append(p.RuleConfigs, model.RuleConfig{
			ID: "w9-set", ItemType: "ruleset", Name: "w9-set", LibraryRuleID: "w9-lib", Policy: new("Proxy"),
		})
		settings := map[string]any{}
		if raw := p.Mihomo.LinuxGateway.JSON(); raw != nil {
			if err := json.Unmarshal(raw, &settings); err != nil {
				return err
			}
		}
		settings["rule_set_ids"] = []string{"w9-set"}
		raw, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		return p.Mihomo.SetJSON("linux_gateway", raw)
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Action(ctx, testSessionID, "check", json.RawMessage(`{}`), ""); !errors.Is(err, device.ErrNotPorted) {
		t.Fatalf("check without the offline reader = %v, want ErrNotPorted", err)
	}

	var read []string
	s.owners.ReadOfflineRouteContent = func(_ context.Context, profile json.RawMessage, item routers.RuleSet, _ *routers.LibraryRule) (string, error) {
		if !strings.Contains(string(profile), `"w9-set"`) {
			t.Error("offline reader did not get the published profile")
		}
		read = append(read, item.ID)
		return "IP-CIDR,198.51.100.0/24\n", nil
	}
	if _, err := s.Action(ctx, testSessionID, "check", json.RawMessage(`{}`), ""); err != nil {
		t.Fatal(err)
	}
	if len(read) != 1 || read[0] != "w9-set" {
		t.Fatalf("offline reader calls = %v", read)
	}
	// The synthetic range is a documentation network, which ComputeDesired
	// leaves out of the routes as not global; the read itself is the contract.
}
