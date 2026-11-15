package rulefetch

// Port of backend/utils/remote_rule_sources.py: resolve declared rule
// sources without guessing a type from a consumer part.

import (
	"strings"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
)

func sameURL(left, right any, config *jsonx.Object) bool {
	base := strings.TrimRight(jsonx.Str(config.GetObject("system_config").GetOr("server_domain", "")), "/")
	absolute := func(value any) string {
		text, _ := value.(string)
		if !jsonx.Truthy(value) {
			text = ""
		}
		if strings.HasPrefix(text, "/") && base != "" {
			return base + text
		}
		return text
	}
	return absolute(left) == absolute(right)
}

// ResolveRemoteRuleSource is resolve_remote_rule_source(url, config,
// source_id): the declaration behind a remote URL, nil when no declaration
// matches, or a *RemoteRuleValidationError. sourceID "" is None.
func ResolveRemoteRuleSource(url string, config *jsonx.Object, sourceID string) (*jsonx.Object, error) {
	var library []*jsonx.Object
	byID := map[string]*jsonx.Object{}
	for _, value := range config.GetList("rule_library") {
		item, ok := value.(*jsonx.Object)
		if !ok {
			continue
		}
		library = append(library, item)
		if id := item.Value("id"); id != nil {
			byID[jsonx.Str(id)] = item
		}
	}
	var items []*jsonx.Object
	for _, value := range config.GetList("rule_configs") {
		if item, ok := value.(*jsonx.Object); ok && item.Value("itemType") == "ruleset" {
			items = append(items, item)
		}
	}
	for _, value := range config.GetList("rule_sets") {
		if item, ok := value.(*jsonx.Object); ok {
			items = append(items, item)
		}
	}
	if sourceID != "" {
		source := byID[sourceID]
		if source == nil {
			var item *jsonx.Object
			for _, candidate := range items {
				if jsonx.Str(candidate.Value("id")) == sourceID {
					item = candidate
					break
				}
			}
			source = byID[jsonx.Str(item.Value("library_rule_id"))]
			if source == nil {
				source = item
			}
		}
		if source == nil {
			return nil, NewRemoteRuleMessage("规则来源已不存在，请重新生成并发布配置。")
		}
		if source.GetOr("source_type", "url") != "url" {
			return nil, NewRemoteRuleMessage("规则来源已改为手写内容，请重新生成并发布配置。")
		}
		if !sameURL(source.Value("url"), url, config) {
			return nil, NewRemoteRuleMessage("规则订阅地址已变更，请重新生成并发布配置。")
		}
		return source, nil
	}
	var candidates []*jsonx.Object
	for _, item := range library {
		if sameURL(item.Value("url"), url, config) {
			candidates = append(candidates, item)
		}
	}
	for _, item := range items {
		if sameURL(item.Value("url"), url, config) {
			if declared := byID[jsonx.Str(item.Value("library_rule_id"))]; declared != nil {
				candidates = append(candidates, declared)
			} else {
				candidates = append(candidates, item)
			}
		}
	}
	var remote []*jsonx.Object
	for _, item := range candidates {
		if item.GetOr("source_type", "url") == "url" {
			remote = append(remote, item)
		}
	}
	if len(remote) == 0 {
		return nil, nil
	}
	declarations := map[string]bool{}
	for _, item := range remote {
		format, err := convert.RulesetFormat(item, config, "")
		if err != nil {
			return nil, err
		}
		declarations[jsonx.Repr(item.GetOr("behavior", "domain"))+"\x00"+format] = true
	}
	if len(declarations) != 1 {
		return nil, NewRemoteRuleMessage("同一订阅地址对应多个不同规则类型或格式，无法确定当前来源；请重新生成并发布配置。")
	}
	return remote[0], nil
}
