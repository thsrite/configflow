package onboarding

import (
	"slices"

	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// editMapping detaches only the mapping container being edited. Its keys and
// values remain shared; callers replace fields or detach the next container
// before editing it. Never expand a user document's aliases into a value tree.
func editMapping(n *yamlx.Node) *yamlx.Node {
	n = yamlx.Resolve(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := *n
	out.Anchor = ""
	out.Content = slices.Clone(n.Content)
	return &out
}

func text(n *yamlx.Node) string {
	n = yamlx.Resolve(n)
	if n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
		return n.Value
	}
	return ""
}
func boolean(n *yamlx.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!bool" && n.Value == "true"
}
func field(n *yamlx.Node, keys ...string) *yamlx.Node {
	for _, key := range keys {
		n = yamlx.Get(n, key)
	}
	return n
}
func nonempty(n *yamlx.Node) bool {
	return n != nil && n.ShortTag() != "!!null" && (n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode) && len(n.Content) > 0 || text(n) != "" || boolean(n)
}
func pluginIndex(document *yamlx.Node) map[string]*yamlx.Node {
	out := map[string]*yamlx.Node{}
	if plugins := field(document, "plugins"); plugins != nil {
		for _, item := range plugins.Content {
			out[text(field(item, "tag"))] = item
		}
	}
	return out
}
func step(exec string) *yamlx.Node { return yamlx.Map(yamlx.Str("exec"), yamlx.Str(exec)) }
func secondarySteps() *yamlx.Node {
	return yamlx.Seq(step("$forward_fallback"), yamlx.Map(yamlx.Str("matches"), yamlx.Seq(yamlx.Str("!has_resp")), yamlx.Str("exec"), yamlx.Str("reject 3")))
}
func proxySteps() *yamlx.Node          { return yamlx.Seq(step("$fallback"), step("accept")) }
func nativeError(message string) error { return &oxidns.SettingsError{Msg: message} }

// CheckNativeDNSPath verifies that the native listeners reach the canonical
// remote/fallback chains before onboarding edits their upstreams.
func CheckNativeDNSPath(settings *yamlx.Node, allowDisabledFallback bool) error {
	document, err := oxidns.NativeDocument(settings, false, nil)
	if err != nil {
		return err
	}
	plugins := pluginIndex(document)
	for _, tag := range []string{"server_udp", "server_tcp"} {
		if item := plugins[tag]; item != nil && text(field(item, "args", "entry")) != "sequence_main" {
			return nativeError(msgNativeListener)
		}
	}
	proxy := plugins["proxy_dns_seq"]
	enabled := yamlx.Equal(field(proxy, "args"), proxySteps())
	disabled := yamlx.Equal(field(proxy, "args"), yamlx.Seq(step("goto remote_dns"))) || yamlx.Equal(field(proxy, "args"), yamlx.Seq(step("$remote_dns"), step("accept")))
	if text(field(proxy, "type")) != "sequence" || !enabled && !(allowDisabledFallback && disabled) {
		return nativeError(msgNativeProxy)
	}
	if enabled {
		fallback := plugins["fallback"]
		if text(field(fallback, "type")) != "fallback" || text(field(fallback, "args", "primary")) != "remote_dns" || text(field(fallback, "args", "secondary")) != "fallback_dns" {
			return nativeError(msgNativeFallback)
		}
		secondary := plugins["fallback_dns"]
		if text(field(secondary, "type")) != "sequence" || !yamlx.Equal(field(secondary, "args"), secondarySteps()) {
			return nativeError(msgNativeSecondary)
		}
	}
	remote := plugins["remote_dns"]
	steps := field(remote, "args")
	expected := secondarySteps()
	yamlx.Set(expected.Content[0], "exec", yamlx.Str("$forward_remote"))
	if text(field(remote, "type")) != "sequence" || steps == nil || steps.Kind != yaml.SequenceNode || len(steps.Content) < 2 || !yamlx.Equal(yamlx.Seq(steps.Content[:2]...), expected) {
		return nativeError(msgNativeRemote)
	}
	for _, item := range steps.Content[2:] {
		if item.Kind != yaml.MappingNode || len(item.Content) != 2 || len(text(field(item, "exec"))) < 4 || text(field(item, "exec"))[:4] != "ttl " {
			return nativeError(msgNativeRemote)
		}
	}
	return nil
}

// PrepareNativeFallback restores only the recognized disabled fallback graph.
// Foreign literal references and unresolved expressions prevent projection.
func PrepareNativeFallback(settings *yamlx.Node) (*yamlx.Node, error) {
	document, err := oxidns.LoadNativeYAML(text(field(settings, "native_config")), "config.yaml")
	if err != nil {
		return nil, err
	}
	if nonempty(field(document, "include")) {
		return nil, nativeError(msgNativeInclude)
	}
	plugins := pluginIndex(document)
	kinds := map[string]string{"forward_fallback": "forward", "fallback_dns": "sequence", "fallback": "fallback"}
	defaults := map[string]*yamlx.Node{
		"forward_fallback": yamlx.Map(yamlx.Str("upstreams"), yamlx.Seq(yamlx.Map(yamlx.Str("addr"), yamlx.Str(DNSTarget))), yamlx.Str("concurrent"), yamlx.Int(1)),
		"fallback_dns":     secondarySteps(),
		"fallback":         yamlx.Map(yamlx.Str("primary"), yamlx.Str("remote_dns"), yamlx.Str("secondary"), yamlx.Str("fallback_dns"), yamlx.Str("threshold"), yamlx.Int(500), yamlx.Str("always_standby"), yamlx.Bool(false)),
	}
	tags := []string{"forward_fallback", "fallback_dns", "fallback"}
	restored := map[string]*yamlx.Node{}
	for _, tag := range tags {
		item := plugins[tag]
		if item == nil {
			item = field(settings, "_common_disabled", tag)
		}
		if item == nil {
			item = yamlx.Map(yamlx.Str("tag"), yamlx.Str(tag), yamlx.Str("type"), yamlx.Str(kinds[tag]), yamlx.Str("args"), defaults[tag])
		}
		args := field(item, "args")
		if text(field(item, "tag")) != tag || text(field(item, "type")) != kinds[tag] || tag == "fallback_dns" && !yamlx.Equal(args, secondarySteps()) || tag != "fallback_dns" && (args == nil || args.Kind != yaml.MappingNode) {
			return nil, nativeError(msgNativePlugin)
		}
		if tag == "fallback" && (text(field(args, "primary")) != "remote_dns" || text(field(args, "secondary")) != "fallback_dns") {
			return nil, nativeError(msgNativePlugin)
		}
		if tag == "forward_fallback" {
			if upstreams := field(args, "upstreams"); upstreams != nil && upstreams.Kind != yaml.SequenceNode {
				return nil, nativeError(msgNativePlugin)
			}
		}
		restored[tag] = item
	}
	remaining := editMapping(document)
	if nodes := field(document, "plugins"); nodes != nil {
		masked := yamlx.Seq()
		yamlx.Set(remaining, "plugins", masked)
		for _, original := range nodes.Content {
			item := editMapping(original)
			if item == nil {
				masked.Content = append(masked.Content, original)
				continue
			}
			masked.Content = append(masked.Content, item)
			tag := text(field(item, "tag"))
			yamlx.Delete(item, "tag")
			yamlx.Delete(item, "type")
			switch tag {
			case "proxy_dns_seq", "fallback_dns":
				yamlx.Delete(item, "args")
			case "fallback":
				args := editMapping(field(item, "args"))
				yamlx.Set(item, "args", args)
				yamlx.Delete(args, "primary")
				yamlx.Delete(args, "secondary")
			}
		}
	}
	if len(oxidns.ReferenceLocationsWithOptions(document, nil, oxidns.ReferenceOptions{Dynamic: true, AdditionalFields: []string{"tag"}})) > 0 || len(oxidns.ReferenceLocations(remaining, tags, false)) > 0 {
		return nil, nativeError(msgNativeReferences)
	}
	proxy := plugins["proxy_dns_seq"]
	if yamlx.Equal(field(proxy, "args"), proxySteps()) {
		return settings, nil
	}
	if proxy == nil {
		return nil, nativeError(msgNativeProxy)
	}
	for _, tag := range tags {
		if plugins[tag] == nil {
			nodes := field(document, "plugins")
			nodes.Content = append(nodes.Content, restored[tag])
		}
	}
	yamlx.Set(proxy, "args", proxySteps())
	encoded, err := yamlx.Marshal(document)
	if err != nil {
		return nil, err
	}
	result := editMapping(settings)
	yamlx.Set(result, "native_config", yamlx.Str(string(encoded)))
	return result, nil
}
