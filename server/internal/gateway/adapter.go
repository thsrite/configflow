package gateway

// Native typed boundary between gateway and model, routersplit, rulefetch,
// and yamlx.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"

	"configflow/internal/model"
	"configflow/internal/routersplit"
	"configflow/internal/rulefetch"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// NewConfig reads the gateway's view of a resolved profile configuration
// (profile, shared rule library, system settings, nodes). The caller's
// *model.Config is never modified.
func NewConfig(config *model.Config) *Config {
	c := &Config{}
	if config == nil {
		c.settings, c.settingsErr = NormalizeSettings(nil)
		return c
	}
	if config.ProfileID != "" {
		c.profileID = member{present: true, isString: true, text: config.ProfileID, truthy: true}
	}
	c.system = config.SystemConfig
	if sysFields := marshalFields(config.SystemConfig); sysFields != nil {
		c.serverDomain = rawString(sysFields, "server_domain")
		c.configToken = rawString(sysFields, "config_token")
	}
	c.profile = config.Profile
	if config.Profile != nil {
		if mihomoFields := marshalFields(config.Profile.Mihomo); mihomoFields != nil {
			c.customConfig = rawString(mihomoFields, "custom_config")
		}
		for _, entry := range config.Profile.RuleConfigs {
			c.rules = append(c.rules, ruleFromConfig(entry))
		}
	}
	c.settings, c.settingsErr = SettingsOf(config)
	if config.RuleLibrary != nil {
		c.hasLibrary = true
		c.rawLibrary = append([]model.RuleLibraryItem{}, config.RuleLibrary...)
		for i := range c.rawLibrary {
			c.library = append(c.library, ruleFromLibrary(&c.rawLibrary[i]))
		}
	}
	if len(config.Nodes) > 0 {
		c.nodesSlice = append([]model.Node{}, config.Nodes...)
	}
	return c
}

// SettingsOf is the gateway setting of a profile configuration
// (gateway_settings) without reading the rest of it.
func SettingsOf(config *model.Config) (Settings, error) {
	if config == nil || config.Profile == nil {
		return NormalizeSettings(nil)
	}
	raw, present := config.Profile.Mihomo.Fields.Get("linux_gateway")
	if !present {
		return NormalizeSettings(nil)
	}
	return NormalizeSettings(raw)
}

func marshalFields(v any) map[string]json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	return fields
}

func rawString(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return ""
	}
	var text string
	if json.Unmarshal(trimmed, &text) != nil {
		return ""
	}
	return text
}

func jsonTruthy(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	switch string(trimmed) {
	case "null", "false", `""`, "[]", "{}":
		return false
	case "true":
		return true
	}
	switch trimmed[0] {
	case '"':
		var s string
		return json.Unmarshal(trimmed, &s) == nil && s != ""
	case '[':
		var list []json.RawMessage
		return json.Unmarshal(trimmed, &list) == nil && len(list) > 0
	case '{':
		var obj map[string]json.RawMessage
		return json.Unmarshal(trimmed, &obj) == nil && len(obj) > 0
	default:
		n, err := strconv.ParseFloat(string(trimmed), 64)
		return err == nil && n != 0
	}
}

func memberOfFields(fields map[string]json.RawMessage, key string) member {
	raw, present := fields[key]
	if !present {
		return member{}
	}
	trimmed := bytes.TrimSpace(raw)
	var text string
	isString := len(trimmed) > 0 && trimmed[0] == '"' && json.Unmarshal(trimmed, &text) == nil
	return member{present: true, isString: isString, text: text, truthy: jsonTruthy(trimmed)}
}

func flagOfFields(fields map[string]json.RawMessage, key string) flag {
	raw, present := fields[key]
	if !present {
		return flag{}
	}
	return flag{present: true, on: jsonTruthy(raw)}
}

func displayOfFields(fields map[string]json.RawMessage, key string) string {
	raw, present := fields[key]
	if !present {
		return ""
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if json.Unmarshal(trimmed, &text) == nil {
			return text
		}
	}
	return string(trimmed)
}

func ruleFromFields(fields map[string]json.RawMessage) *Rule {
	return &Rule{
		id: memberOfFields(fields, "id"), itemType: memberOfFields(fields, "itemType"), name: memberOfFields(fields, "name"),
		libraryRuleID: memberOfFields(fields, "library_rule_id"), behavior: memberOfFields(fields, "behavior"),
		format: memberOfFields(fields, "format"), url: memberOfFields(fields, "url"), sourceType: memberOfFields(fields, "source_type"),
		content: memberOfFields(fields, "content"), policy: memberOfFields(fields, "policy"),
		enabled: flagOfFields(fields, "enabled"), libraryEnabled: flagOfFields(fields, "library_enabled"),
		ruleType: displayOfFields(fields, "rule_type"), value: displayOfFields(fields, "value"),
	}
}

func ruleFromConfig(entry model.RuleConfig) *Rule {
	r := ruleFromFields(marshalFields(entry))
	r.ruleRecord = entry
	return r
}

func ruleFromLibrary(entry *model.RuleLibraryItem) *Rule {
	r := ruleFromFields(marshalFields(entry))
	r.libraryRecord = entry
	return r
}

// withSettings is the configuration with another gateway setting (a copy).
func (c *Config) withSettings(s Settings) *Config {
	next := *c
	next.settings = Settings{Enabled: s.Enabled, RuleSetIDs: append([]string{}, s.RuleSetIDs...)}
	next.settingsErr = nil
	var cloned model.Profile
	if c.profile != nil {
		if raw, err := model.Marshal(c.profile); err == nil {
			_ = json.Unmarshal(raw, &cloned)
		}
	}
	payload, _ := json.Marshal(struct {
		Enabled    bool     `json:"enabled"`
		RuleSetIDs []string `json:"rule_set_ids"`
	}{Enabled: s.Enabled, RuleSetIDs: append([]string{}, s.RuleSetIDs...)})
	_ = cloned.Mihomo.Fields.Set("linux_gateway", payload)
	next.profile = &cloned
	return &next
}

// checkGeneration is the router split check that precedes every gateway
// patch of a generated document.
func (c *Config) checkGeneration() error { return routersplit.CheckProfile(c.profile) }

// nodes are the shared nodes (config.Nodes) for the routing-mark check.
func (c *Config) nodes() *yaml.Node {
	if len(c.nodesSlice) == 0 {
		return nil
	}
	raw, err := json.Marshal(c.nodesSlice)
	if err != nil {
		return nil
	}
	n, err := yamlx.FromJSON(raw)
	if err != nil {
		return nil
	}
	return n
}

// BaseDocument is the profile's Mihomo base configuration
// (mihomo.custom_config) as a mapping; empty text is an empty mapping.
// Errors are *Error.
func (c *Config) BaseDocument() (*yaml.Node, error) {
	if strings.TrimSpace(c.customConfig) == "" {
		return yamlx.Map(), nil
	}
	node, err := yamlx.Parse([]byte(c.customConfig))
	if err != nil {
		return nil, &Error{Message: msgBaseYAMLInvalid, Cause: err}
	}
	if node.Kind != yaml.MappingNode {
		return nil, gatewayError(msgBaseNotMapping)
	}
	return node, nil
}

// yamlPayloadStrings reads a YAML rule set: the payload list of a mapping,
// or the document itself, which must be a list of strings.
func yamlPayloadStrings(text string) ([]string, error) {
	node, err := yamlx.Parse([]byte(text))
	if err != nil {
		return nil, &Error{Message: msgYAMLSyntax, Cause: err}
	}
	if node.Kind == yaml.MappingNode {
		node = yamlx.Get(node, "payload")
	}
	node = yamlx.Resolve(node)
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil, gatewayError(msgYAMLPayload)
	}
	out := make([]string, len(node.Content))
	for i, item := range node.Content {
		item = yamlx.Resolve(item)
		if item == nil || item.Kind != yaml.ScalarNode || item.ShortTag() != "!!str" {
			return nil, gatewayError(msgYAMLPayload)
		}
		out[i] = item.Value
	}
	return out, nil
}

// dumpPayload writes a rule provider file ({payload: [...]}).
func dumpPayload(payload []string) (string, error) {
	data, err := yamlx.Marshal(struct {
		Payload []string `yaml:"payload"`
	}{Payload: payload})
	return string(data), err
}

// ErrRulesetContentUnavailable is returned while no RuleSourceReader is
// set (config C32 sets it); reading rule contents then fails closed.
var ErrRulesetContentUnavailable = errors.New("gateway: rule set content reader is not available (config C32)")

var (
	contentMu        sync.RWMutex
	ruleSourceReader RuleSourceReader
)

// storedContent reads a rule set through the installed RuleSourceReader. With
// requireCache an empty result is only accepted when a cache file exists
// (an explicitly empty source); a failed download is an *Error, so Mihomo
// keeps its previous provider.
func (c *Config) storedContent(ctx context.Context, item, library *Rule, requireCache bool) (string, error) {
	contentMu.RLock()
	sourceReader := ruleSourceReader
	contentMu.RUnlock()
	if sourceReader == nil {
		return "", ErrRulesetContentUnavailable
	}
	var declaration *model.RuleLibraryItem
	if library != nil {
		declaration = library.libraryRecord
	}
	// Only fetch settings and cache/format metadata cross this reader boundary.
	// Never serialize node parameters, secrets or every library's source content per rule.
	config := &rulefetch.SourceConfig{
		Settings: model.SystemConfig{
			ServerDomain:      c.system.ServerDomain,
			GitHubProxyDomain: c.system.GitHubProxyDomain,
			RuleFetchProxy:    c.system.RuleFetchProxy,
		},
	}
	if c.hasLibrary {
		config.Library = make([]model.RuleLibraryItem, 0, len(c.rawLibrary))
		for _, entry := range c.rawLibrary {
			config.Library = append(config.Library, model.RuleLibraryItem{
				ID:     entry.ID,
				Name:   entry.Name,
				Format: entry.Format,
				URL:    entry.URL,
			})
		}
	}
	content, err := sourceReader(ctx, item.ruleRecord, declaration, config)
	if err != nil || content != "" || !requireCache {
		return content, err
	}
	targetRule := item.ruleRecord
	if library != nil && library.libraryRecord != nil {
		raw, encodeErr := model.Marshal(library.libraryRecord)
		if encodeErr != nil {
			return "", encodeErr
		}
		var libAsRule model.RuleConfig
		if err := json.Unmarshal(raw, &libAsRule); err != nil {
			return "", err
		}
		targetRule = libAsRule
	}
	entries := c.rawLibrary
	if entries == nil {
		entries = []model.RuleLibraryItem{}
	}
	path, err := rulefetch.CacheReadPath(ctx, targetRule, entries)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", gatewayError(msgUnreadable)
	}
	return "", nil
}
