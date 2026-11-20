package server

import (
	"context"
	"reflect"
	"testing"

	"configflow/internal/gateway"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestNewAppWiresRuleContentForGatewayProviders(t *testing.T) {
	t.Cleanup(gateway.SetRuleSourceReader(nil))
	NewApp()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(repo.WithScope(repo.WithRepository(context.Background(), r)))
	cancel()
	raw := []byte(`{"profile_id":"default","mihomo":{"linux_gateway":{"enabled":true,"rule_set_ids":["selected"]}},"rule_configs":[{"id":"selected","itemType":"ruleset","library_rule_id":"nets","behavior":"ipcidr","enabled":true,"policy":"DIRECT"}],"rule_library":[{"id":"nets","name":"Nets","source_type":"content","behavior":"ipcidr","format":"text","content":"192.0.2.0/24\n","enabled":true}]}`)
	native, err := model.DecodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := gateway.NewConfig(native).ProviderPayload(ctx, "selected", nil)
	if err != nil || !reflect.DeepEqual(payload, []string{"192.0.2.0/24"}) {
		t.Fatalf("gateway C32 loader: %v %v", payload, err)
	}
}
