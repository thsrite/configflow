package gateway

// The boundary to packages that still speak jsonx (repo's configuration
// view, convert's generated Mihomo document, routersplit, rulefetch) and to
// the yamlx facade. Nothing else in this package touches jsonx. N8 replaces
// NewConfig's input by the typed model; the rest of the package stays as it is.

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"

	"configflow/internal/jsonx"
	"configflow/internal/routersplit"
	"configflow/internal/rulefetch"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// NewConfig reads the gateway's view of a profile configuration (repo's
// compat view: profile, shared rule library, system settings, nodes). raw
// is kept for the router split check and the rule content hook and must not
// be modified while the Config is in use.
func NewConfig(raw *jsonx.Object) *Config {
	if raw == nil {
		raw = jsonx.NewObject()
	}
	c := &Config{raw: raw, profileID: memberOf(raw, "profile_id")}
	system, _ := raw.Value("system_config").(*jsonx.Object)
	c.serverDomain, _ = system.Value("server_domain").(string)
	c.configToken, _ = system.Value("config_token").(string)
	mihomo, _ := raw.Value("mihomo").(*jsonx.Object)
	c.customConfig, _ = mihomo.Value("custom_config").(string)
	c.settings, c.settingsErr = settingsOf(mihomo)
	for _, entry := range raw.GetList("rule_configs") {
		if obj, ok := entry.(*jsonx.Object); ok {
			c.rules = append(c.rules, ruleOf(obj))
		}
	}
	for _, entry := range raw.GetList("rule_library") {
		if obj, ok := entry.(*jsonx.Object); ok {
			c.library = append(c.library, ruleOf(obj))
		}
	}
	return c
}

// SettingsOf is the gateway setting of a profile configuration
// (gateway_settings) without reading the rest of it.
func SettingsOf(config *jsonx.Object) (Settings, error) {
	mihomo, _ := config.Value("mihomo").(*jsonx.Object)
	return settingsOf(mihomo)
}

func settingsOf(mihomo *jsonx.Object) (Settings, error) {
	value, present := mihomo.Get("linux_gateway")
	if !present || value == nil {
		return NormalizeSettings(nil)
	}
	raw, err := jsonx.Compact(value)
	if err != nil {
		return Settings{}, gatewayError(msgSettingsNotObject)
	}
	return NormalizeSettings(raw)
}

func memberOf(obj *jsonx.Object, key string) member {
	value, present := obj.Get(key)
	text, isString := value.(string)
	return member{present: present, isString: isString, text: text, truthy: present && jsonx.Truthy(value)}
}

func flagOf(obj *jsonx.Object, key string) flag {
	value, present := obj.Get(key)
	return flag{present: present, on: jsonx.Truthy(value)}
}

// strOf is Python's str() of the member, "" when it is absent.
func strOf(obj *jsonx.Object, key string) string {
	value, present := obj.Get(key)
	if !present {
		return ""
	}
	return jsonx.Str(value)
}

func ruleOf(obj *jsonx.Object) *Rule {
	return &Rule{
		id: memberOf(obj, "id"), itemType: memberOf(obj, "itemType"), name: memberOf(obj, "name"),
		libraryRuleID: memberOf(obj, "library_rule_id"), behavior: memberOf(obj, "behavior"),
		format: memberOf(obj, "format"), url: memberOf(obj, "url"), sourceType: memberOf(obj, "source_type"),
		content: memberOf(obj, "content"), policy: memberOf(obj, "policy"),
		enabled: flagOf(obj, "enabled"), libraryEnabled: flagOf(obj, "library_enabled"),
		ruleType: strOf(obj, "rule_type"), value: strOf(obj, "value"),
		raw: obj,
	}
}

// JSONX is the stored entry (read only).
func (r *Rule) JSONX() *jsonx.Object { return r.raw }

// withSettings is the configuration with another gateway setting (a copy).
func (c *Config) withSettings(s Settings) *Config {
	raw := c.raw.DeepCopy()
	mihomo, ok := raw.Value("mihomo").(*jsonx.Object)
	if !ok {
		mihomo = jsonx.NewObject()
		raw.Set("mihomo", mihomo)
	}
	mihomo.Set("linux_gateway", jsonx.Obj("enabled", s.Enabled, "rule_set_ids", jsonx.ListOf(s.RuleSetIDs)))
	return NewConfig(raw)
}

// checkGeneration is the router split check that precedes every gateway
// patch of a generated document.
func (c *Config) checkGeneration() error { return routersplit.CheckGeneration(c.raw) }

// nodes are the shared nodes (config['nodes']) for the routing-mark check.
func (c *Config) nodes() *yaml.Node {
	value, _ := c.raw.Get("nodes")
	return (&codec{}).node(value)
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

// RulesetContentFunc reads a rule set's text like
// routes.rules.get_ruleset_content(item, library, config_data=config): the
// validated local cache, inline content, else a download (cached); "" when
// it cannot be read. library is nil when the rule set has no library entry.
type RulesetContentFunc func(ctx context.Context, item, library, config *jsonx.Object) (string, error)

// ErrRulesetContentUnavailable is returned while no RulesetContentFunc is
// set (config C32 sets it); reading rule contents then fails closed.
var ErrRulesetContentUnavailable = errors.New("gateway: rule set content reader is not available (config C32)")

var (
	contentMu      sync.RWMutex
	rulesetContent RulesetContentFunc
)

// SetRulesetContent installs the default rule content reader and returns a
// function that restores the previous one.
func SetRulesetContent(fn RulesetContentFunc) (restore func()) {
	contentMu.Lock()
	previous := rulesetContent
	rulesetContent = fn
	contentMu.Unlock()
	return func() {
		contentMu.Lock()
		rulesetContent = previous
		contentMu.Unlock()
	}
}

// storedContent reads a rule set through the default reader. With
// requireCache an empty result is only accepted when a cache file exists
// (an explicitly empty source); a failed download is an *Error, so Mihomo
// keeps its previous provider.
func (c *Config) storedContent(ctx context.Context, item, library *Rule, requireCache bool) (string, error) {
	contentMu.RLock()
	read := rulesetContent
	contentMu.RUnlock()
	if read == nil {
		return "", ErrRulesetContentUnavailable
	}
	var libraryRaw *jsonx.Object
	if library != nil {
		libraryRaw = library.raw
	}
	content, err := read(ctx, item.raw, libraryRaw, c.raw)
	if err != nil || content != "" || !requireCache {
		return content, err
	}
	var entries []any
	if value, present := c.raw.Get("rule_library"); !present {
		entries = []any{}
	} else {
		entries, _ = value.([]any)
	}
	path, err := rulefetch.RuleCacheReadPath(ctx, source(item, library).raw, entries)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", gatewayError(msgUnreadable)
	}
	return "", nil
}

// ApplyJSONX is Apply on convert's generated document (the
// convert.MihomoHooks.ApplyLinuxGateway hook): mihomo is changed in place
// and only when the patch succeeds. With the gateway off nothing is
// converted.
func ApplyJSONX(mihomo, config *jsonx.Object, baseURL string) error {
	c := NewConfig(config)
	if settings, err := c.Settings(); err != nil || !settings.Enabled {
		if checkErr := c.checkGeneration(); checkErr != nil {
			return checkErr
		}
		return err
	}
	k := &codec{leaves: map[*yaml.Node]any{}}
	doc := k.node(mihomo)
	if err := c.Apply(doc, baseURL); err != nil {
		return err
	}
	patched := k.value(doc).(*jsonx.Object)
	for _, key := range mihomo.Keys() {
		if !patched.Has(key) {
			mihomo.Delete(key)
		}
	}
	for key, value := range patched.All() {
		mihomo.Set(key, value)
	}
	return nil
}

// RoutingMarkPathJSONX is RoutingMarkPath for a jsonx value (a loaded
// provider file, for the deployment bundle).
func RoutingMarkPathJSONX(value any, root string) string {
	return RoutingMarkPath((&codec{}).node(value), root)
}

// codec converts jsonx values to nodes and back. A container the input
// shares (a YAML alias the loader kept as one value) becomes one shared node
// and comes back as one shared value, as Python's dicts and lists stay shared:
// the conversion is linear in the loaded document, never in its expansion.
// With leaves set, each scalar node remembers its original value, so values
// the patch did not replace come back exactly as they were (number forms,
// timestamps, binary).
type codec struct {
	leaves map[*yaml.Node]any
	nodes  map[any]*yaml.Node // shared input container -> its node
	values map[*yaml.Node]any // container node -> its rebuilt value
}

// listID identifies a list: its first element and its length (two lists
// that only share a prefix of one backing array are different lists).
type listID struct {
	first *any
	n     int
}

// containerID identifies a jsonx container (aliases share it); ok is false
// for scalars and empty lists.
func containerID(v any) (id any, ok bool) {
	switch value := v.(type) {
	case *jsonx.Object:
		return value, value != nil
	case []any:
		if len(value) > 0 {
			return listID{&value[0], len(value)}, true
		}
	}
	return nil, false
}

func (k *codec) node(v any) *yaml.Node {
	id, container := containerID(v)
	if container {
		if n, ok := k.nodes[id]; ok {
			return n
		}
		if k.nodes == nil {
			k.nodes = map[any]*yaml.Node{}
		}
	}
	switch value := v.(type) {
	case *jsonx.Object:
		m := yamlx.Map()
		if container {
			k.nodes[id] = m
		}
		for key, child := range value.All() {
			m.Content = append(m.Content, yamlx.Str(key), k.node(child))
		}
		return m
	case []any:
		s := yamlx.Seq()
		if container {
			k.nodes[id] = s
		}
		for _, child := range value {
			s.Content = append(s.Content, k.node(child))
		}
		return s
	}
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: tagOpaque}
	switch value := v.(type) {
	case string:
		n.Tag, n.Value = "!!str", value
	case bool:
		n.Tag, n.Value = "!!bool", strconv.FormatBool(value)
	case nil:
		n.Tag, n.Value = "!!null", "null"
	case jsonx.Number:
		n.Tag, n.Value = "!!float", value.String()
		if value.IsInt() {
			n.Tag = "!!int"
		}
	}
	if k.leaves != nil {
		k.leaves[n] = v
	}
	return n
}

func (k *codec) value(n *yaml.Node) any {
	if n = yamlx.Resolve(n); n == nil {
		return nil
	}
	if original, ok := k.leaves[n]; ok {
		return original
	}
	if done, ok := k.values[n]; ok {
		return done
	}
	if k.values == nil {
		k.values = map[*yaml.Node]any{}
	}
	switch n.Kind {
	case yaml.MappingNode:
		obj := jsonx.NewObject()
		k.values[n] = obj
		for i := 0; i+1 < len(n.Content); i += 2 {
			obj.Set(keyText(n.Content[i]), k.value(n.Content[i+1]))
		}
		return obj
	case yaml.SequenceNode:
		list := make([]any, len(n.Content))
		k.values[n] = list
		for i, child := range n.Content {
			list[i] = k.value(child)
		}
		return list
	}
	switch n.ShortTag() {
	case "!!bool":
		return n.Value == "true"
	case "!!null":
		return nil
	case "!!int", "!!float":
		if number, err := jsonx.ParseNumber(n.Value); err == nil {
			return number
		}
	}
	return n.Value
}
