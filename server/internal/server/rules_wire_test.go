package server

import (
	"context"
	"reflect"
	"testing"

	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func TestNewAppWiresRuleContentForGatewayProviders(t *testing.T) {
	t.Cleanup(gateway.SetRulesetContent(nil))
	NewApp()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(repo.WithScope(repo.WithRepository(context.Background(), r)))
	cancel()
	config := jsonx.Obj("profile_id", "default", "mihomo", jsonx.Obj("linux_gateway", jsonx.Obj("enabled", true, "rule_set_ids", []any{"selected"})), "rule_configs", []any{jsonx.Obj("id", "selected", "itemType", "ruleset", "library_rule_id", "nets", "behavior", "ipcidr", "enabled", true, "policy", "DIRECT")}, "rule_library", []any{jsonx.Obj("id", "nets", "name", "Nets", "source_type", "content", "behavior", "ipcidr", "format", "text", "content", "192.0.2.0/24\n", "enabled", true)})
	payload, err := gateway.NewConfig(config).ProviderPayload(ctx, "selected", nil)
	if err != nil || !reflect.DeepEqual(payload, []string{"192.0.2.0/24"}) {
		t.Fatalf("gateway C32 loader: %v %v", payload, err)
	}
}
