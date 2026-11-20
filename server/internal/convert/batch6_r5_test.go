package convert

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestBatch6R5GenerationRejectsRuleIdentityTypes(t *testing.T) {
	generators := []struct {
		name     string
		generate func(context.Context, *model.Config) (string, error)
	}{
		{"Mihomo", func(ctx context.Context, config *model.Config) (string, error) {
			return MihomoConfig(ctx, config, MihomoOptions{SkipProviderPreflight: true})
		}},
		{"Surge", func(ctx context.Context, config *model.Config) (string, error) {
			return SurgeConfig(ctx, config, "")
		}},
		{"Loon", func(ctx context.Context, config *model.Config) (string, error) {
			return LoonConfig(ctx, config, "")
		}},
	}
	for _, generator := range generators {
		for _, field := range []string{"value", "rule_type"} {
			for _, value := range []string{`5`, `5.0`, `null`, `false`, `[]`, `{}`} {
				t.Run(generator.name+"/"+field+"/"+value, func(t *testing.T) {
					rule := mustJSON(t, `{"itemType":"rule","rule_type":"MATCH","value":"","policy":"DIRECT"}`)
					raw, err := ParseJString(value)
					if err != nil {
						t.Fatal(err)
					}
					rule.Set(field, raw)
					config, err := DecodeConfigJ(JObjKV("rule_configs", []any{rule}))
					if err != nil {
						t.Fatal(err)
					}
					output, err := generator.generate(t.Context(), config)
					want := "规则 " + field + " 必须是字符串，当前值：" + value
					if err == nil || err.Error() != want || output != "" {
						t.Fatalf("want %q and no output; got %q, %v", want, output, err)
					}
				})
			}
		}
	}
}

func TestBatch6R5RuleIdentityValidationPreservesStorage(t *testing.T) {
	const source = `{"rule_configs":[{"itemType":"rule","rule_type":"DST-PORT","value":"5","policy":"DIRECT","future":1e3}]}`
	config, err := DecodeRulesConfigJ(mustJSON(t, source))
	if err != nil {
		t.Fatal(err)
	}
	before, err := model.Marshal(config.Profile.RuleConfigs[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRuleIdentities(config.Profile.RuleConfigs); err != nil {
		t.Fatal(err)
	}
	after, err := model.Marshal(config.Profile.RuleConfigs[0])
	if err != nil || string(before) != string(after) {
		t.Fatalf("validation changed storage: %s %v", after, err)
	}
	// Model decoding remains lossless; rejection belongs to generation only.
	var rule model.RuleConfig
	if err := json.Unmarshal([]byte(`{"value":5}`), &rule); err != nil || rule.Value != "5" {
		t.Fatalf("shared scalar decode: %+v %v", rule, err)
	}
}
