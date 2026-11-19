package rulefetch

// These narrow adapters are temporary until the API, substore and deploy
// callers own typed records. They never decode unrelated config collections.
import (
	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"errors"
	"time"
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
func legacyConfig(config *SourceConfig) *jsonx.Object {
	if config == nil {
		return nil
	}
	return legacyObject(struct {
		Settings model.SystemConfig      `json:"system_config"`
		Library  []model.RuleLibraryItem `json:"rule_library"`
		Rules    []model.RuleConfig      `json:"rule_configs"`
		RuleSets []model.RuleConfig      `json:"rule_sets"`
	}{config.Settings, config.Library, config.Rules, config.RuleSets})
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
func contentBytes(value any) []byte { content, _ := legacyContent(value); return content.Data }

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

// WriteValidatedRuleCache adapts text/binary content without Python value rules.
func WriteValidatedRuleCache(ctx context.Context, rule *jsonx.Object, value any, config *jsonx.Object) (string, error) {
	content, err := legacyContent(value)
	if err != nil {
		return "", err
	}
	return WriteCache(ctx, legacyRule(rule), content, sourceConfig(config))
}

// RefreshOptions is the temporary boundary for callers with injected downloads.
type RefreshOptions struct {
	Timeout    time.Duration
	Fetch      func(string) (any, error)
	AllowStale bool
	Client     *Client
}

// RefreshRemoteRule adapts a legacy caller to RefreshRule.
func RefreshRemoteRule(ctx context.Context, rule, config *jsonx.Object, opt RefreshOptions) ([]byte, error, error) {
	native := CacheOptions{Timeout: opt.Timeout, AllowStale: opt.AllowStale, Client: opt.Client}
	if opt.Fetch != nil {
		native.Fetch = func(ctx context.Context, url string) (Content, error) {
			if err := ctx.Err(); err != nil {
				return Content{}, err
			}
			value, err := opt.Fetch(url)
			if err != nil {
				return Content{}, err
			}
			if err := ctx.Err(); err != nil {
				return Content{}, err
			}
			return legacyContent(value)
		}
	}
	return RefreshRule(ctx, legacyRule(rule), sourceConfig(config), native)
}

// ReadRuleSourceContent adapts the optional native content result.
func ReadRuleSourceContent(ctx context.Context, rule *jsonx.Object, client *Client) (any, error) {
	content, err := ReadSource(ctx, legacyRule(rule), client)
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, nil
	}
	if content.Text {
		return string(content.Data), nil
	}
	return content.Data, nil
}

// SaveRuleToLocal adapts a legacy rule source.
func SaveRuleToLocal(ctx context.Context, rule *jsonx.Object, client *Client) (string, error) {
	return SaveSource(ctx, legacyRule(rule), client)
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
