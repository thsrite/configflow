package gateway_test

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/gateway"
	"configflow/internal/model"
)

func TestNewConfigWithReaderNilFailsClosed(t *testing.T) {
	raw := []byte(`{"profile_id":"default","mihomo":{"linux_gateway":{"enabled":true,"rule_set_ids":["selected"]}},"rule_configs":[{"id":"selected","itemType":"ruleset","library_rule_id":"nets","behavior":"ipcidr","enabled":true,"policy":"DIRECT"}],"rule_library":[{"id":"nets","name":"Nets","source_type":"url","url":"https://example.com/nets.txt","behavior":"ipcidr","format":"text","enabled":true}]}`)
	cfg, err := model.DecodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = gateway.NewConfigWithReader(nil, cfg, nil).ProviderPayload(context.Background(), "selected", nil)
	if !errors.Is(err, gateway.ErrRulesetContentUnavailable) {
		t.Fatalf("NewConfigWithReader(..., nil) error = %v, want %v", err, gateway.ErrRulesetContentUnavailable)
	}
}
