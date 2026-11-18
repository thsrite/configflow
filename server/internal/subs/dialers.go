package subs

import (
	"regexp"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

var dialersLogger = logx.Get("backend.utils.subscription_dialers")

// Limits and settings of subscription dialer records.
const (
	MaxTargets  = 50
	MaxMembers  = 100
	MaxReported = 20
)

var dialerBuiltins = map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "PASS-RULE": true, "COMPATIBLE": true}

var groupTypes = []string{"select", "url-test", "fallback", "load-balance"}

// groupKeys are the health-check and selection settings kept from the airport's group.
var groupKeys = []string{"url", "interval", "tolerance", "lazy", "timeout", "expected-status", "strategy", "max-failed-times"}

// GroupName is group_name: `<subscription>/<target>`.
func GroupName(subscription, target string) string { return subscription + "/" + target }

// iterate is Python's iteration over `value or []`: lists, strings (characters)
// and dict keys; ok is false for values that cannot be iterated.
func iterate(v any) (items []any, ok bool) {
	switch x := v.(type) {
	case nil:
		return nil, true
	case []any:
		return x, true
	case string:
		for _, r := range x {
			items = append(items, string(r))
		}
		return items, true
	case *jsonx.Object:
		for _, k := range x.Keys() {
			items = append(items, k)
		}
		return items, true
	}
	if !jsonx.Truthy(v) {
		return nil, true
	}
	return nil, false
}

func isScalarSetting(v any) bool {
	switch v.(type) {
	case string, bool, jsonx.Number:
		return true
	}
	return false
}

func notIterable(v any) error {
	return pyErr("TypeError: '" + pyTypeName(v) + "' object is not iterable")
}

// DialerRecord is dialer_record: the recorded airport groups of sub. cache is
// the loaded subscription cache document (may be nil); downloadedText returns
// the last downloaded body of a URL for caches made before the record existed.
func DialerRecord(sub, cache *jsonx.Object, downloadedText func(url string) (string, bool)) (*jsonx.Object, error) {
	metadata := cache.GetObject("metadata")
	if metadata != nil && metadata.Has("dialers") {
		rec, _ := metadata.Value("dialers").(*jsonx.Object)
		return rec, nil
	}
	if !jsonx.Truthy(sub.Value("url")) {
		return nil, nil
	}
	url, _ := sub.Value("url").(string)
	if downloadedText == nil {
		return nil, nil
	}
	text, ok := downloadedText(url)
	if !ok {
		return nil, nil
	}
	return ExtractSubscriptionDialers(text)
}

func airportGroups(record *jsonx.Object) map[string]bool {
	out := map[string]bool{}
	if record == nil {
		return out
	}
	for _, k := range record.GetObject("targets").Keys() {
		out[k] = true
	}
	for _, g := range record.GetList("unresolved") {
		if s, ok := g.(string); ok {
			out[s] = true
		}
	}
	return out
}

var escapeChars = regexp.MustCompile("[\\\\()\\[\\]{}?*+\\-|^$.&~# \t\n\r\v\f]")

// pyReEscape is re.escape(s).
func pyReEscape(s string) string {
	return escapeChars.ReplaceAllStringFunc(s, func(m string) string { return "\\" + m })
}

func namePattern(name string) string {
	return strings.ReplaceAll(pyReEscape(name), "`", `\x60`)
}

func nodeDialer(node *jsonx.Object) string {
	target, err := rawDialer(node)
	if err != nil {
		return ""
	}
	if s, ok := target.(string); ok && !dialerBuiltins[s] {
		return s
	}
	return ""
}

// DialerDeps are the collaborators of AddDialerGroups that belong to other packages.
type DialerDeps struct {
	// LoadCache is load_subscription_cache(sub_id): {"nodes": [...], "metadata": {...}} or nil.
	LoadCache func(subID string) *jsonx.Object
	// ConvertNode is converters.mihomo.convert_node_to_mihomo; nil result means "cannot convert".
	ConvertNode func(node *jsonx.Object) *jsonx.Object
	// DownloadedText is the last downloaded body of a URL (see DialerRecord); may be nil.
	DownloadedText func(url string) (string, bool)
}

// AddDialerGroups is add_dialer_groups: it adds the groups the emitted
// subscriptions' chained nodes need to main and returns a report or nil.
// It returns *DialerReferenceError when a name it must create is taken.
func AddDialerGroups(main, config *jsonx.Object, deps DialerDeps) (*jsonx.Object, error) {
	subscriptions, _ := objectsByID(listField(config, "subscriptions"), false)
	aggregations, _ := objectsByID(listField(config, "subscription_aggregations"), false)
	direct := map[string]bool{}
	var emitted []string
	has := func(id string) bool {
		for _, e := range emitted {
			if e == id {
				return true
			}
		}
		return false
	}
	for _, src := range providerSources(main, config) {
		var members []any
		if src.kind == "subscription" {
			members = []any{src.id}
			direct[src.id] = true
		} else {
			members = listField(aggregations[src.id], "subscriptions")
		}
		for _, m := range members {
			if id, ok := m.(string); ok && subscriptions[id] != nil && !has(id) {
				emitted = append(emitted, id)
			}
		}
	}
	caches := map[string]*jsonx.Object{}
	var cacheOrder []string
	for _, id := range emitted {
		if jsonx.Truthy(getOr(subscriptions[id], "enabled", true)) {
			cache := deps.LoadCache(id)
			if cache == nil {
				cache = jsonx.NewObject()
			}
			caches[id] = cache
			cacheOrder = append(cacheOrder, id)
		}
	}
	if len(caches) == 0 {
		return nil, nil
	}
	nodeNames := map[string]bool{}
	for _, id := range cacheOrder {
		for _, n := range caches[id].GetList("nodes") {
			if node, ok := n.(*jsonx.Object); ok {
				if name, ok := node.Value("name").(string); ok {
					nodeNames[name] = true
				}
			}
		}
	}

	providers := main.GetObject("proxy-providers")
	if providers == nil {
		providers = jsonx.NewObject()
	}
	proxies, _ := main.SetDefault("proxies", []any{}).([]any)
	groups, _ := main.SetDefault("proxy-groups", []any{}).([]any)
	taken := map[string]bool{}
	for _, list := range [][]any{proxies, groups} {
		for _, item := range list {
			if o, ok := item.(*jsonx.Object); ok {
				taken[jsonx.Str(o.Value("name"))] = true
			}
		}
	}
	own := map[string]bool{}
	created := []any{}
	var skipped []*jsonx.Object

	for _, subID := range cacheOrder {
		cache := caches[subID]
		sub := subscriptions[subID]
		var nodes []any
		for _, n := range cache.GetList("nodes") {
			if node, ok := n.(*jsonx.Object); ok {
				if name, ok := node.Value("name").(string); ok && name != "" {
					nodes = append(nodes, node)
				}
			}
		}
		visibleNodes := FilterSubscriptionItems(sub, nodes)
		type chainedNode struct {
			node   *jsonx.Object
			target string
		}
		var chained []chainedNode
		for _, n := range visibleNodes {
			node := n.(*jsonx.Object)
			if !strings.Contains(jsonx.Str(getOr(node, "proxy_string", ""))+jsonx.Str(getOr(node, "params", "")), "dialer-proxy") {
				continue
			}
			if target := nodeDialer(node); target != "" && !taken[target] {
				chained = append(chained, chainedNode{node, target})
			}
		}
		if len(chained) == 0 {
			continue
		}
		record, err := DialerRecord(sub, cache, deps.DownloadedText)
		if err != nil {
			main.Set("proxies", proxies)
			main.Set("proxy-groups", groups)
			return nil, err
		}
		known := record.GetObject("targets")
		airport := airportGroups(record)
		byName := map[string]*jsonx.Object{}
		for _, n := range nodes {
			node := n.(*jsonx.Object)
			byName[node.GetString("name")] = node
		}
		visible := map[string]bool{}
		for _, n := range visibleNodes {
			visible[n.(*jsonx.Object).GetString("name")] = true
		}
		var name any = sub.Value("name")
		nameText := jsonx.Str(name)
		resolved := map[string]*string{}

		claim := func(value string) error {
			if taken[value] {
				return &DialerReferenceError{Message: "名称「" + value + "」已被使用，与订阅「" + nameText + "」自动生成的中转分组冲突，请改名后重新生成"}
			}
			taken[value] = true
			own[value] = true
			return nil
		}

		var resolve func(target string, path []string) (*string, error)
		resolve = func(target string, path []string) (*string, error) {
			if v, ok := resolved[target]; ok {
				return v, nil
			}
			resolved[target] = nil
			entry := known.GetObject(target)
			if entry == nil || entry.Len() == 0 {
				return nil, nil
			}
			for _, p := range path {
				if p == target {
					return nil, nil
				}
			}
			members := entry.GetList("members")
			for _, m := range members {
				if byName[jsonx.Str(m)] == nil {
					return nil, nil
				}
			}
			group := jsonx.Obj("name", GroupName(nameText, target), "type", entry.Value("type"), "hidden", true)
			for _, key := range groupKeys {
				if entry.Has(key) {
					group.Set(key, entry.Value(key))
				}
			}
			allVisible := true
			for _, m := range members {
				if !visible[jsonx.Str(m)] {
					allVisible = false
				}
			}
			copied := false
			if direct[subID] && providers.Has(nameText) && allVisible {
				patterns := make([]string, len(members))
				for i, m := range members {
					patterns[i] = namePattern(jsonx.Str(m))
				}
				group.Set("use", []any{name})
				group.Set("filter", "^(?:"+strings.Join(patterns, "|")+")$")
			} else {
				var pending []*jsonx.Object
				for _, m := range members {
					member := jsonx.Str(m)
					copyName := GroupName(nameText, member)
					if own[copyName] {
						continue
					}
					base := deps.ConvertNode(byName[member])
					if base == nil || base.Len() == 0 {
						return nil, nil
					}
					proxy := base.Clone()
					proxy.Set("name", copyName)
					if nested, ok := proxy.Value("dialer-proxy").(string); ok && airport[nested] {
						emittedNested, err := resolve(nested, append(append([]string(nil), path...), target))
						if err != nil {
							return nil, err
						}
						if emittedNested == nil {
							return nil, nil
						}
						proxy.Set("dialer-proxy", *emittedNested)
					}
					pending = append(pending, proxy)
				}
				for _, proxy := range pending {
					if err := claim(proxy.GetString("name")); err != nil {
						return nil, err
					}
					proxies = append(proxies, proxy)
				}
				list := make([]any, len(members))
				for i, m := range members {
					list[i] = GroupName(nameText, jsonx.Str(m))
				}
				group.Set("proxies", list)
				copied = true
			}
			if err := claim(group.GetString("name")); err != nil {
				return nil, err
			}
			groups = append(groups, group)
			created = append(created, jsonx.Obj("subscription", name, "target", target, "group", group.Value("name"),
				"members", members, "copied", copied))
			groupName := group.GetString("name")
			resolved[target] = &groupName
			return &groupName, nil
		}

		for _, c := range chained {
			if airport[c.target] {
				r, err := resolve(c.target, nil)
				if err != nil {
					main.Set("proxies", proxies)
					main.Set("proxy-groups", groups)
					return nil, err
				}
				if r == nil {
					skipped = append(skipped, jsonx.Obj("subscription", name, "node", c.node.Value("name"), "target", c.target, "reason", "missing"))
				}
			} else if record == nil && !nodeNames[c.target] {
				skipped = append(skipped, jsonx.Obj("subscription", name, "node", c.node.Value("name"), "target", c.target, "reason", "refresh"))
			}
		}
	}
	main.Set("proxies", proxies)
	main.Set("proxy-groups", groups)

	if len(created) == 0 && len(skipped) == 0 {
		return nil, nil
	}
	shown := skipped
	if len(shown) > MaxReported {
		shown = shown[:MaxReported]
	}
	shownList := make([]any, len(shown))
	for i, s := range shown {
		shownList[i] = s
	}
	report := jsonx.Obj("groups", created, "skipped", shownList, "skipped_count", jsonx.Int(len(skipped)))
	if len(skipped) > 0 {
		var parts []string
		for i, s := range skipped {
			if i >= 5 {
				break
			}
			parts = append(parts, jsonx.Str(s.Value("subscription"))+"「"+jsonx.Str(s.Value("node"))+"」→ "+jsonx.Str(s.Value("target")))
		}
		dialersLogger.Warningf("订阅链式节点：%d 个节点的中转分组未能补上（%s）", len(skipped), strings.Join(parts, "；"))
	}
	return report, nil
}
