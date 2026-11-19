package rulefetch

// These narrow adapters are temporary until the API, substore and deploy
// callers own typed records. They never decode unrelated config collections.
import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func decodeLegacy(value, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(data, target)
}
func legacyObject(value any) *jsonx.Object {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	parsed, _ := jsonx.Parse(data)
	object, _ := parsed.(*jsonx.Object)
	return object
}
func sourceConfig(config *jsonx.Object) *SourceConfig {
	if config == nil {
		return nil
	}
	result := &SourceConfig{}
	_ = decodeLegacy(config.GetObject("system_config"), &result.Settings)
	_ = decodeLegacy(config.GetList("rule_library"), &result.Library)
	return result
}

func legacyRule(value *jsonx.Object) model.RuleConfig {
	var result model.RuleConfig
	_ = decodeLegacy(value, &result)
	return result
}
func legacyContent(value any) (Content, error) {
	switch v := value.(type) {
	case string:
		return Content{Data: []byte(v), Text: true}, nil
	case []byte:
		return Content{Data: v}, nil
	}
	return Content{}, errors.New("rule content must be text or bytes")
}

// RuleCacheReadPath adapts the existing library view.
func RuleCacheReadPath(ctx context.Context, rule *jsonx.Object, library []any) (string, error) {
	var entries []model.RuleLibraryItem
	if library != nil {
		_ = decodeLegacy(library, &entries)
		if entries == nil {
			entries = []model.RuleLibraryItem{}
		}
	}
	return CacheReadPath(ctx, legacyRule(rule), entries)
}

// ReadValidatedRuleCache adapts a legacy rule to the typed cache owner.
func ReadValidatedRuleCache(ctx context.Context, rule, config *jsonx.Object) ([]byte, error) {
	return ReadValidatedRuleCacheWithFormat(ctx, rule, config, "")
}

// ReadValidatedRuleCacheWithFormat skips inference for a caller-known format.
// The ordinary stable-file path does not touch library at all. Legacy-name
// fallback projects only the already-decoded IDs/names, never source bodies.
func ReadValidatedRuleCacheWithFormat(ctx context.Context, rule, config *jsonx.Object, format string) ([]byte, error) {
	if format == "" {
		return ReadCacheWithFormat(ctx, legacyRule(rule), sourceConfig(config), "")
	}
	var input *SourceConfig
	if config != nil {
		input = &SourceConfig{cacheLibrary: func() []model.RuleLibraryItem {
			entries := config.GetList("rule_library")
			result := make([]model.RuleLibraryItem, 0, len(entries))
			for _, entry := range entries {
				if object, ok := entry.(*jsonx.Object); ok {
					result = append(result, model.RuleLibraryItem{ID: object.GetString("id"), Name: object.GetString("name")})
				}
			}
			return result
		}}
	}
	return ReadCacheWithFormat(ctx, legacyRule(rule), input, format)
}

// ResolveRemoteRuleSource projects only declarations for the unported deploy owner.
func ResolveRemoteRuleSource(url string, config *jsonx.Object, id string) (*jsonx.Object, error) {
	input := sourceConfig(config)
	if input == nil {
		input = &SourceConfig{}
	}
	if config != nil {
		_ = decodeLegacy(config.GetList("rule_configs"), &input.Rules)
		_ = decodeLegacy(config.GetList("rule_sets"), &input.RuleSets)
	}
	result, err := ResolveSource(url, input, id)
	if result == nil {
		return nil, err
	}
	return legacyObject(result), err
}
