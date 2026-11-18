package convert

// TODO(N8-8b/c/d, N8-9): delete these transcoding entry points as callers
// adopt the typed APIs. Business decisions belong to the typed implementation.
import (
	"encoding/json"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func decodeLegacy[T any](value any) (T, error) {
	var result T
	data, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

func encodeLegacy(value any) (any, error) {
	data, err := model.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}

func configFromLegacy(value *jsonx.Object) (*model.Config, error) {
	profile, err := decodeLegacy[model.Profile](value)
	if err != nil {
		return nil, err
	}
	shared, err := decodeLegacy[model.Shared](value)
	if err != nil {
		return nil, err
	}
	wire, err := decodeLegacy[model.System](value)
	if err != nil {
		return nil, err
	}
	return &model.Config{Profile: &profile, ProfileID: value.GetString("profile_id"), SystemConfig: wire.SystemConfig, Backup: wire.Backup, Agents: wire.Agents, HasAgents: value.Has("agents"), Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations, RuleLibrary: shared.RuleLibrary}, nil
}

// NTPDirectRule adapts the built-in typed rule for remaining legacy callers.
func NTPDirectRule() *jsonx.Object {
	value, _ := encodeLegacy(BuiltinNTPRule())
	result, _ := value.(*jsonx.Object)
	return result
}

// UDPFallback adapts the resolved view to FallbackPolicy.
func UDPFallback(value *jsonx.Object) (string, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return "", err
	}
	return FallbackPolicy(config), nil
}

// NTPDirect adapts the resolved view to DirectNTP.
func NTPDirect(value *jsonx.Object) (bool, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return false, err
	}
	return DirectNTP(config), nil
}

// EmittedRules adapts typed emitted rules to the legacy generator boundary.
func EmittedRules(value *jsonx.Object) ([]any, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeLegacy(RulesForOutput(config))
	if err != nil {
		return nil, err
	}
	result, _ := encoded.([]any)
	return result, nil
}

// AfterMatchComment adapts a stored rule to RuleComment.
func AfterMatchComment(value *jsonx.Object) string {
	rule, _ := decodeLegacy[model.RuleConfig](value)
	return RuleComment(rule)
}

// RulesetFormat adapts stored rule records to the shared typed implementation.
func RulesetFormat(rule, value *jsonx.Object, sourceURL string) (string, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return "", err
	}
	item, err := decodeLegacy[model.RuleConfig](rule)
	if err != nil {
		return "", err
	}
	return RuleFormat(item, config, sourceURL)
}
