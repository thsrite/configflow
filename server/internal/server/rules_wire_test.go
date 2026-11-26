package server

import (
	"context"
	"reflect"
	"testing"

	"configflow/internal/gateway"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestNewAppWiresRuleContentForGatewayProviders(t *testing.T) {
	r, err := repo.Open(t.TempDir(), RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	app, err := New(context.Background(), Options{Repo: r, Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Agents.Close)
	ctx, cancel := context.WithCancel(repo.WithScope(context.Background()))
	cancel()
	raw := []byte(`{"profile_id":"default","mihomo":{"linux_gateway":{"enabled":true,"rule_set_ids":["selected"]}},"rule_configs":[{"id":"selected","itemType":"ruleset","library_rule_id":"nets","behavior":"ipcidr","enabled":true,"policy":"DIRECT"}],"rule_library":[{"id":"nets","name":"Nets","source_type":"content","behavior":"ipcidr","format":"text","content":"192.0.2.0/24\n","enabled":true}]}`)
	native, err := model.DecodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := gateway.NewConfigWithReader(r, native, app.RuleSource.ReadGateway).ProviderPayload(ctx, "selected", nil)
	if err != nil || !reflect.DeepEqual(payload, []string{"192.0.2.0/24"}) {
		t.Fatalf("gateway C32 loader: %v %v", payload, err)
	}
}
