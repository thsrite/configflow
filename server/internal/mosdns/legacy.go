package mosdns

// Legacy entry points are transcoders for callers owned by later N8 groups.
// Generation, selection and parsing use the typed implementations exclusively.
import (
	"encoding/json"
	"fmt"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// ConfigFromJSON decodes the MosDNS projection without decoding unrelated resources.
func ConfigFromJSON(data []byte) (*model.Config, error) {
	var fields model.Fields
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	var profileFields model.Fields
	for _, key := range []string{"mosdns", "rule_configs", "rules", "rule_sets"} {
		if raw, ok := fields.Get(key); ok {
			if err := profileFields.Set(key, raw); err != nil {
				return nil, err
			}
		}
	}
	raw, err := profileFields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	profile := new(model.Profile)
	if err = json.Unmarshal(raw, profile); err != nil {
		return nil, err
	}
	config := &model.Config{Profile: profile}
	if raw, ok := fields.Get("system_config"); ok {
		if err = json.Unmarshal(raw, &config.SystemConfig); err != nil {
			return nil, err
		}
	}
	if raw, ok := fields.Get("profile_id"); ok {
		var id model.String
		if err = json.Unmarshal(raw, &id); err != nil {
			return nil, err
		}
		config.ProfileID = string(id)
	}
	return config, nil
}
func legacyConfig(config *jsonx.Object) (*model.Config, error) {
	data, err := jsonx.Compact(config)
	if err != nil {
		return nil, err
	}
	return ConfigFromJSON(data)
}
func legacyValue(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}
func legacyList(value any) ([]any, error) {
	out, err := legacyValue(value)
	if err != nil {
		return nil, err
	}
	list, ok := out.([]any)
	if !ok {
		return nil, fmt.Errorf("mosdns: expected array")
	}
	return list, nil
}
func legacyNode(node *yamlx.Node) (any, error) {
	data, err := yamlx.ToJSON(node)
	if err != nil {
		if hasNonStringKey(node) {
			return node, nil
		}
		return nil, err
	}
	return jsonx.Parse(data)
}

// GenerateMosDNSConfig transcodes an old configuration into Generate.
func GenerateMosDNSConfig(config *jsonx.Object, baseURL string) (string, error) {
	native, err := legacyConfig(config)
	if err != nil {
		return "", err
	}
	return Generate(native, baseURL)
}

// GetMosDNSRulesetDownloads transcodes Downloads for existing generator owners.
func GetMosDNSRulesetDownloads(config *jsonx.Object, baseURL string) ([]any, error) {
	native, err := legacyConfig(config)
	if err != nil {
		return nil, err
	}
	out, err := Downloads(native, baseURL)
	if err != nil {
		return nil, err
	}
	return legacyList(out)
}

// GetMosDNSCustomFiles transcodes CustomFiles for existing generator owners.
func GetMosDNSCustomFiles(config *jsonx.Object) ([]any, error) {
	native, err := legacyConfig(config)
	if err != nil {
		return nil, err
	}
	out, err := CustomFiles(native)
	if err != nil {
		return nil, err
	}
	return legacyList(out)
}

// SplitRulesAndRulesets transcodes SplitRules for remaining rule consumers.
func SplitRulesAndRulesets(config *jsonx.Object) (any, any, error) {
	native, err := legacyConfig(config)
	if err != nil {
		return nil, nil, err
	}
	rules, sets, err := SplitRules(native)
	if err != nil {
		return nil, nil, err
	}
	r, err := legacyValue(rules)
	if err != nil {
		return nil, nil, err
	}
	s, err := legacyValue(sets)
	return r, s, err
}

// ParseDNSUpstreams transcodes native YAML upstreams for old callers.
func ParseDNSUpstreams(value any) ([]any, error) {
	text, ok := value.(string)
	if value == nil {
		text = ""
		ok = true
	}
	if !ok {
		return nil, fmt.Errorf("mosdns: upstreams must be text")
	}
	nodes, err := Upstreams(text)
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, node := range nodes {
		v, err := legacyNode(node)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// ParseCustomMatches transcodes shared model match definitions.
func ParseCustomMatches(value any) ([]any, error) {
	data, err := jsonx.Compact(value)
	if err != nil {
		return nil, err
	}
	var settings model.MosDNSSettings
	var fields model.Fields
	if err = fields.Set("custom_matches", data); err != nil {
		return nil, err
	}
	data, err = fields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	out := []any{}
	for _, node := range MatchEntries(settings.CustomMatches) {
		v, err := legacyNode(node)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// RulesetFile is the old file DTO. Native users use SourceFile.
type RulesetFile struct {
	Part      string
	URL       any
	LocalPath string
}

// RulesetFiles transcodes a declaration into typed source files.
func RulesetFiles(config *jsonx.Object, baseURL string, value any, stem string) ([]RulesetFile, error) {
	native, err := legacyConfig(config)
	if err != nil {
		return nil, err
	}
	data, err := jsonx.Compact(value)
	if err != nil {
		return nil, err
	}
	var item model.RuleConfig
	if err = json.Unmarshal(data, &item); err != nil {
		return nil, err
	}
	files, err := RuleFiles(native, baseURL, item, stem)
	if err != nil {
		return nil, err
	}
	out := make([]RulesetFile, 0, len(files))
	for _, f := range files {
		var u any
		if f.URL != nil {
			u = *f.URL
		}
		out = append(out, RulesetFile{Part: f.Part, URL: u, LocalPath: f.LocalPath})
	}
	return out, nil
}

// BuildRuleProxyURL transcodes the old optional string arguments.
func BuildRuleProxyURL(config *jsonx.Object, baseURL, source string, part, sourceID any) (string, error) {
	native, err := legacyConfig(config)
	if err != nil {
		return "", err
	}
	p, _ := part.(string)
	id, _ := sourceID.(string)
	return RuleProxyURL(native, baseURL, source, p, id)
}
