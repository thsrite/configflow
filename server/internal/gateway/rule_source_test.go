package gateway_test

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/gateway"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

func TestNewConfigWithReaderNilFailsClosed(t *testing.T) {
	t.Cleanup(gateway.SetRuleSourceReader(func(context.Context, model.RuleConfig, *model.RuleLibraryItem, *rulefetch.SourceConfig) (string, error) {
		return "192.0.2.0/24\n", nil
	}))
	raw := []byte(`{"profile_id":"default","mihomo":{"linux_gateway":{"enabled":true,"rule_set_ids":["selected"]}},"rule_configs":[{"id":"selected","itemType":"ruleset","library_rule_id":"nets","behavior":"ipcidr","enabled":true,"policy":"DIRECT"}],"rule_library":[{"id":"nets","name":"Nets","source_type":"url","url":"https://example.com/nets.txt","behavior":"ipcidr","format":"text","enabled":true}]}`)
	cfg, err := model.DecodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = gateway.NewConfigWithReader(cfg, nil).ProviderPayload(context.Background(), "selected", nil)
	if !errors.Is(err, gateway.ErrRulesetContentUnavailable) {
		t.Fatalf("NewConfigWithReader(..., nil) error = %v, want %v", err, gateway.ErrRulesetContentUnavailable)
	}
}
