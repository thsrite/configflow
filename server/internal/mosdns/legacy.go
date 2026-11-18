package mosdns

// Legacy entry points are transcoders for callers owned by later N8 groups.
// Generation, selection and parsing use the typed implementations exclusively.
import (
	"configflow/internal/convert"
	"configflow/internal/refresh"
	"configflow/internal/rulefetch"
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
		var projection model.Fields
		if err = projection.Set("system_config", raw); err != nil {
			return nil, err
		}
		data, err := projection.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var system model.System
		if err = json.Unmarshal(data, &system); err != nil {
			return nil, err
		}
		config.SystemConfig = system.SystemConfig
	}
	if raw, ok := fields.Get("profile_id"); ok {
		var projection model.Fields
		if err = projection.Set("profile_id", raw); err != nil {
			return nil, err
		}
		data, err := projection.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var identity profileIdentity
		if err = json.Unmarshal(data, &identity); err != nil {
			return nil, err
		}
		config.ProfileID = identity.ID
	}
	if raw, ok := fields.Get("rule_library"); ok {
		var projection model.Fields
		if err = projection.Set("rule_library", raw); err != nil {
			return nil, err
		}
		data, err := projection.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var shared model.Shared
		if err = json.Unmarshal(data, &shared); err != nil {
			return nil, err
		}
		config.RuleLibrary = shared.RuleLibrary
	}
	return config, nil
}
func legacyConfig(config *jsonx.Object) (*model.Config, error) {
	projection := jsonx.NewObject()
	if config != nil {
		for _, key := range []string{"mosdns", "rule_configs", "rules", "rule_sets", "system_config", "profile_id", "rule_library"} {
			if value, ok := config.Get(key); ok {
				projection.Set(key, value)
			}
		}
	}
	data, err := jsonx.Compact(projection)
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
	r, err := legacyRuleList(rules)
	if err != nil {
		return nil, nil, err
	}
	s, err := legacyRuleList(sets)
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
		return nil, ErrUpstreamType
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

// DecodeList converts a converter result list (downloads, custom files).
func DecodeList[T any](list []any) ([]T, error) {
	var out []T
	return out, viaJSON(list, &out)
}

func viaJSON(value any, target any) error {
	data, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	if snap, ok := target.(*snapshot); ok {
		native, err := ConfigFromJSON(data)
		if err != nil {
			return err
		}
		*snap = snapshotOf(native)
		return nil
	}
	return json.Unmarshal(data, target)
}

// BuildMosDNSZip transcodes the old captured snapshot into the typed ZIP owner.
func BuildMosDNSZip(ctx context.Context, config *jsonx.Object, content string, downloads []Download, files []File, options ArchiveOptions) ([]byte, error) {
	native, err := legacyConfig(config)
	if err != nil {
		return nil, err
	}
	return BuildZIP(ctx, native, content, downloads, files, options)
}

func legacySourceConfig(config *model.Config) (*jsonx.Object, error) {
	if config == nil {
		return nil, nil
	}
	// Only collaborator metadata is encoded. Large unrelated profile settings,
	// nodes and Agent deployment history never enter the rule-fetch chain.
	type systemDTO model.SystemConfig
	system := systemDTO(config.SystemConfig)
	system.Fields = model.Fields{}
	object := jsonx.Obj("profile_id", config.ProfileID)
	raw, err := json.Marshal(system)
	if err != nil {
		return nil, err
	}
	sys, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	object.Set("system_config", sys)
	library := []any{}
	for _, entry := range config.RuleLibrary {
		type libraryDTO model.RuleLibraryItem
		value := libraryDTO(entry)
		value.Fields = model.Fields{}
		item, err := legacyConsumedRecord(entry, value)
		if err != nil {
			return nil, err
		}
		library = append(library, item)
	}
	object.Set("rule_library", library)
	rules := []any{}
	for _, entry := range config.Profile.RuleConfigs {
		type ruleDTO model.RuleConfig
		value := ruleDTO(entry)
		value.Fields = model.Fields{}
		item, err := legacyConsumedRecord(entry, value)
		if err != nil {
			return nil, err
		}
		rules = append(rules, item)
	}
	object.Set("rule_configs", rules)
	return object, nil
}
func resolveLegacySource(url string, config *model.Config, id string) (*model.RuleLibraryItem, error) {
	value, err := legacySourceConfig(config)
	if err != nil {
		return nil, err
	}
	item, err := rulefetch.ResolveRemoteRuleSource(url, value, id)
	if err != nil || item == nil {
		return nil, err
	}
	data, err := jsonx.Compact(item)
	if err != nil {
		return nil, err
	}
	var result model.RuleLibraryItem
	err = json.Unmarshal(data, &result)
	return &result, err
}
func fetchLegacySource(ctx context.Context, fetch nativeFetchFunc, config *model.Config, src source, name string) (string, error) {
	legacy, err := legacySourceConfig(config)
	if err != nil {
		return "", err
	}
	declared, err := rulefetch.ResolveRemoteRuleSource(src.url, legacy, src.identity)
	if err != nil {
		return "", err
	}
	if declared == nil {
		content, err := fetch(ctx, src.url, config, "")
		return strings.ToValidUTF8(string(content), "�"), err
	}
	if format, err := convert.RulesetFormat(declared, legacy, ""); err != nil {
		return "", err
	} else if format == "mrs" {
		return "", rulefetch.NewRemoteRuleMessage(mrsMessage)
	}
	content, stale, err := refresh.ReadRuleSource(ctx, declared, refresh.RuleOptions{Config: legacy, Fetch: func(url string) (any, error) { return fetch(ctx, url, config, "") }})
	if err != nil {
		return "", err
	}
	if stale != nil {
		archiveLogger.Warningf("MosDNS ZIP 规则“%s”更新失败，使用上一次有效缓存：%s", name, refresh.DescribeFailure(stale))
	}
	return decodeText(content)
}

var fetchRemoteFn fetchFunc = fetchRemote

type fetchFunc func(context.Context, string, *jsonx.Object, string) ([]byte, error)

func fetchRemote(ctx context.Context, url string, config *jsonx.Object, host string) ([]byte, error) {
	var native *model.Config
	if config != nil {
		var err error
		native, err = legacyConfig(config)
		if err != nil {
			return nil, err
		}
	}
	return fetchNative(ctx, url, native, host)
}
func fetchCaptured(ctx context.Context, url string, config *model.Config, host string) ([]byte, error) {
	value, err := legacySourceConfig(config)
	if err != nil {
		return nil, err
	}
	return fetchRemoteFn(ctx, url, value, host)
}
func requestCaptured(ctx context.Context, url string, config *model.Config, request rulefetch.Request) (*rulefetch.Response, error) {
	value, err := legacySourceConfig(config)
	if err != nil {
		return nil, err
	}
	request.Config = value
	return RequestRule(ctx, url, request)
}
func proxySourceURL(url string, config *model.Config) (string, error) {
	value, err := legacySourceConfig(config)
	if err != nil {
		return "", err
	}
	return convert.ApplyGithubProxyDomain(url, value)
}
func readCapturedSource(ctx context.Context, item model.RuleLibraryItem, config *model.Config, fetch func(string) (any, error)) ([]byte, error, error) {
	value, err := legacySourceConfig(config)
	if err != nil {
		return nil, nil, err
	}
	raw, err := model.Marshal(item)
	if err != nil {
		return nil, nil, err
	}
	decoded, err := jsonx.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	object, _ := decoded.(*jsonx.Object)
	return refresh.ReadRuleSource(ctx, object, refresh.RuleOptions{Config: value, Fetch: fetch})
}

// RulesetParts transcodes the former polymorphic part selector.
func RulesetParts(value any) []string { behavior, _ := value.(string); return Parts(behavior) }

func legacyConsumedRecord(record, consumed any) (*jsonx.Object, error) {
	originalData, err := model.Marshal(record)
	if err != nil {
		return nil, err
	}
	var original model.Fields
	if err = json.Unmarshal(originalData, &original); err != nil {
		return nil, err
	}
	data, err := json.Marshal(consumed)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	object, _ := value.(*jsonx.Object)
	for key, value := range object.All() {
		if key == "Fields" || value == nil {
			object.Delete(key)
			continue
		}
		if text, ok := value.(string); ok && text == "" {
			raw, present := original.Get(key)
			if !present || string(raw) == "null" {
				object.Delete(key)
			}
		}
	}
	return object, nil
}

type profileIdentity struct {
	ID     string `json:"profile_id"`
	Fields model.Fields
	raw    model.Raw
}

func (v *profileIdentity) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, v, &v.raw)
}

func legacyRuleList(rules []model.RuleConfig) ([]any, error) {
	result := []any{}
	for _, rule := range rules {
		raw, err := model.Marshal(rule)
		if err != nil {
			return nil, err
		}
		original, err := jsonx.Parse(raw)
		if err != nil {
			return nil, err
		}
		object := original.(*jsonx.Object)
		type ruleDTO model.RuleConfig
		value := ruleDTO(rule)
		value.Fields = model.Fields{}
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		known, err := jsonx.Parse(data)
		if err != nil {
			return nil, err
		}
		for key, value := range known.(*jsonx.Object).All() {
			if key == "Fields" || !object.Has(key) {
				continue
			}
			if value == nil {
				object.Delete(key)
			} else {
				object.Set(key, value)
			}
		}
		result = append(result, object)
	}
	return result, nil
}
