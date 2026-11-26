package setup

import (
	"configflow/internal/jsondecode"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

var identitySeparators = regexp.MustCompile(`[^a-z0-9]+`)

var legacyAliases = map[string]string{
	"preset:direct": "builtin:chinamax", "preset:proxy": "builtin:proxy",
	"library:setup_dns_cn": "builtin:chinamax", "library:setup_cn_domains": "builtin:chinamax",
	"library:setup_dns_proxy": "builtin:proxy", "library:setup_proxy_domains": "builtin:proxy",
}

type builtin struct {
	Identity string
	Item     model.RuleLibraryItem
	Route    string
}

type template struct {
	ProxyGroups   []model.ProxyGroup      `json:"proxy_groups"`
	RuleConfigs   []model.RuleConfig      `json:"rule_configs"`
	RuleLibrary   []model.RuleLibraryItem `json:"rule_library"`
	Subscriptions []model.Subscription    `json:"subscriptions"`
	MosDNS        struct {
		Direct []string `json:"direct_rulesets"`
		Proxy  []string `json:"proxy_rulesets"`
	} `json:"mosdns"`
	Mihomo struct {
		CustomConfig string `json:"custom_config"`
	} `json:"mihomo"`
}

type catalog struct {
	Template template
	Items    []builtin
}

func loadCatalog() (*catalog, error) {
	raw, err := json.Marshal(repo.InitialConfig())
	if err != nil {
		return nil, err
	}
	c := &catalog{}
	if err := jsondecode.Unmarshal(raw, &c.Template); err != nil {
		return nil, fmt.Errorf("read setup template: %w", err)
	}
	routes := map[string]string{}
	for _, id := range c.Template.MosDNS.Direct {
		routes[id] = "direct"
	}
	for _, id := range c.Template.MosDNS.Proxy {
		routes[id] = "proxy"
	}
	order, defaults := map[string]int{}, map[string]string{}
	for _, rule := range c.Template.RuleConfigs {
		if route := routes[rule.ID]; route != "" {
			defaults[rule.LibraryRuleID] = route
		}
		if rule.ItemType == "ruleset" {
			order[rule.LibraryRuleID] = len(order)
		}
	}
	items := slices.Clone(c.Template.RuleLibrary)
	sort.SliceStable(items, func(i, j int) bool {
		left, ok := order[items[i].ID]
		if !ok {
			left = len(order)
		}
		right, ok := order[items[j].ID]
		if !ok {
			right = len(order)
		}
		return left < right
	})
	for _, item := range items {
		source := text(item.SourceType)
		if source == "" {
			source = "url"
			if item.Content != nil {
				source = "content"
			}
		}
		entry := model.RuleLibraryItem{Name: item.Name, SourceType: &source, Behavior: pointer(orText(item.Behavior, "domain")),
			Format: pointer(orText(item.Format, "text")), Enabled: pointer(resourceEnabled(item.Enabled))}
		if source == "content" {
			entry.Content = pointer(text(item.Content))
		} else {
			entry.URL = pointer(text(item.URL))
		}
		c.Items = append(c.Items, builtin{"builtin:" + identitySeparators.ReplaceAllString(strings.ToLower(item.Name), "_"), entry, defaults[item.ID]})
	}
	return c, nil
}

func orText(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

func supported(item model.RuleLibraryItem) bool {
	u, err := url.Parse(text(item.URL))
	return (resourceEnabled(item.Enabled)) && slices.Contains([]string{"domain", "classical"}, orText(item.Behavior, "domain")) &&
		slices.Contains([]string{"text", "yaml"}, orText(item.Format, "text")) && err == nil && !strings.HasSuffix(strings.ToLower(u.Path), ".mrs")
}

func sourceKey(item model.RuleLibraryItem) string {
	source := ""
	if text(item.SourceType) == "content" {
		digest := sha256.Sum256([]byte(text(item.Content)))
		source = "content:" + hex.EncodeToString(digest[:])
	} else if value := text(item.URL); value != "" {
		source = "url:" + strings.ReplaceAll(value, "/refs/heads/", "/")
	}
	if source == "" {
		return ""
	}
	return source + "\x00" + orText(item.Behavior, "domain") + "\x00" + orText(item.Format, "text")
}

// contentIdentity uses shared delivery/validation before comparing handwritten
// sets. Ambiguous YAML, warnings and invalid syntax never become equality proof.
func contentIdentity(item model.RuleLibraryItem) string {
	behavior, format := orText(item.Behavior, "domain"), orText(item.Format, "text")
	if text(item.SourceType) != "content" || behavior != "domain" && behavior != "classical" {
		return ""
	}
	content := text(item.Content)
	report, err := rulefetch.Inspect(content, behavior, format)
	if err != nil || len(report.Errors) != 0 {
		return ""
	}
	content, warnings, err := rulefetch.NormalizeRuleset(content, behavior, format)
	if err != nil || len(warnings) != 0 {
		return ""
	}
	var lines []string
	if format == "yaml" {
		node, err := yamlx.Parse([]byte(content))
		if err != nil {
			return ""
		}
		payload := yamlx.Get(node, "payload")
		if payload == nil || payload.Kind != yaml.SequenceNode {
			return ""
		}
		for _, entry := range payload.Content {
			if entry.Kind != yaml.ScalarNode || entry.ShortTag() != "!!str" || strings.TrimSpace(entry.Value) != entry.Value {
				return ""
			}
			lines = append(lines, entry.Value)
		}
	} else {
		lines = rulefetch.RuleLines(strings.TrimPrefix(content, "\ufeff"))
	}
	set := map[string]bool{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if behavior == "domain" {
			value := strings.ToLower(line)
			kind := "DOMAIN"
			if strings.HasPrefix(value, "+.") {
				kind, value = "DOMAIN-SUFFIX", value[2:]
			} else if strings.HasPrefix(value, ".") || strings.Contains(value, "*") {
				kind = "domain"
			}
			set[kind+"\x00"+value] = true
			continue
		}
		kind, payload, parameters := rules.SplitRulePayload(line)
		if slices.Contains([]string{"DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-WILDCARD"}, kind) {
			payload = strings.ToLower(payload)
		} else if kind == "IP-CIDR" || kind == "IP-CIDR6" {
			prefix, err := netip.ParsePrefix(payload)
			if err != nil {
				return ""
			}
			payload = prefix.Masked().String()
		}
		slices.Sort(parameters)
		parts := append([]string{kind, payload}, parameters...)
		set[strings.Join(parts, "\x00")] = true
	}
	if len(set) == 0 {
		return ""
	}
	entries := make([]string, 0, len(set))
	for key := range set {
		entries = append(entries, key)
	}
	slices.Sort(entries)
	raw, _ := json.Marshal(entries)
	return string(raw)
}

type ruleMatcher struct{ content map[string]string }

func (m *ruleMatcher) same(a, b model.RuleLibraryItem) bool {
	if key := sourceKey(a); key != "" && key == sourceKey(b) {
		return true
	}
	identity := func(item model.RuleLibraryItem) string {
		key := sourceKey(item)
		if key == "" {
			return ""
		}
		if m.content == nil {
			m.content = map[string]string{}
		}
		value, ok := m.content[key]
		if !ok {
			value = contentIdentity(item)
			m.content[key] = value
		}
		return value
	}
	return identity(a) != "" && identity(a) == identity(b)
}

func (c *catalog) identity(item model.RuleLibraryItem, matcher *ruleMatcher) string {
	for _, builtin := range c.Items {
		if matcher.same(item, builtin.Item) {
			return builtin.Identity
		}
	}
	return ""
}

// DomainOption is a safe rule identity offered by the setup page.
type DomainOption struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Source   string        `json:"source"`
	Behavior *string       `json:"behavior,omitempty"`
	Aliases  *[]string     `json:"aliases,omitempty"`
	Conflict *NameConflict `json:"conflict,omitempty"`
}

// NameConflict identifies a different shared rule set occupying a template name.
type NameConflict struct {
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
}

func (c *catalog) options(shared model.Shared) []DomainOption {
	matcher := &ruleMatcher{}
	identities := map[string]string{}
	own := map[string]bool{}
	for _, item := range shared.RuleLibrary {
		identities[item.ID] = c.identity(item, matcher)
		_, alias := legacyAliases["library:"+item.ID]
		own[item.ID] = supported(item) && identities[item.ID] == "" && !alias
	}
	options := []DomainOption{}
	for _, builtin := range c.Items {
		matches := []model.RuleLibraryItem{}
		usable := false
		for i := range shared.RuleLibrary {
			item := &shared.RuleLibrary[i]
			if identities[item.ID] == builtin.Identity {
				matches = append(matches, *item)
				usable = usable || resourceEnabled(item.Enabled)
			}
		}
		if !supported(builtin.Item) || len(matches) > 0 && !usable {
			continue
		}
		aliases := []string{}
		// The Python literal's order is part of the list response.
		for _, alias := range []string{"preset:direct", "preset:proxy", "library:setup_dns_cn", "library:setup_cn_domains", "library:setup_dns_proxy", "library:setup_proxy_domains"} {
			if legacyAliases[alias] == builtin.Identity {
				aliases = append(aliases, alias)
			}
		}
		for _, item := range matches {
			aliases = append(aliases, "library:"+item.ID)
		}
		option := DomainOption{ID: builtin.Identity, Name: builtin.Item.Name, Source: "builtin", Behavior: builtin.Item.Behavior, Aliases: &aliases}
		if len(matches) == 0 {
			var namesake *model.RuleLibraryItem
			for i := range shared.RuleLibrary {
				entry := &shared.RuleLibrary[i]
				if entry.Name == builtin.Item.Name && (namesake == nil || !own[namesake.ID] && own[entry.ID]) {
					namesake = entry
				}
			}
			if namesake != nil {
				option.Conflict = &NameConflict{Name: namesake.Name}
				if own[namesake.ID] {
					option.Conflict.ID = "library:" + namesake.ID
				}
			}
		}
		options = append(options, option)
	}
	for _, item := range shared.RuleLibrary {
		if own[item.ID] {
			options = append(options, DomainOption{ID: "library:" + item.ID, Name: item.Name, Source: "library"})
		}
	}
	return options
}

func (c *catalog) recommended(options []DomainOption) Selections {
	available := map[string]bool{}
	for _, option := range options {
		available[option.ID] = true
	}
	result := Selections{Direct: []string{}, Proxy: []string{}}
	for _, builtin := range c.Items {
		if !available[builtin.Identity] || !supported(builtin.Item) {
			continue
		}
		if builtin.Route == "direct" {
			result.Direct = append(result.Direct, builtin.Identity)
		} else if builtin.Route == "proxy" {
			result.Proxy = append(result.Proxy, builtin.Identity)
		}
	}
	return result
}

func (c *catalog) canonical(shared model.Shared, selections Selections) Selections {
	aliases := make(map[string]string, len(legacyAliases)+len(shared.RuleLibrary))
	for alias, target := range legacyAliases {
		aliases[alias] = target
	}
	matcher := &ruleMatcher{}
	for _, item := range shared.RuleLibrary {
		if identity := c.identity(item, matcher); identity != "" {
			aliases["library:"+item.ID] = identity
		}
	}
	canonical := func(values []string) []string {
		out := []string{}
		seen := map[string]bool{}
		for _, value := range values {
			if alias := aliases[value]; alias != "" {
				value = alias
			}
			if !seen[value] {
				seen[value], out = true, append(out, value)
			}
		}
		return out
	}
	return Selections{Direct: canonical(selections.Direct), Proxy: canonical(selections.Proxy)}
}

func (c *catalog) ensureLibrary(ctx context.Context, r *repo.Repository, selected map[string]bool) (map[string]model.RuleLibraryItem, error) {
	find := func(shared *model.Shared, wanted model.RuleLibraryItem, matcher *ruleMatcher) *model.RuleLibraryItem {
		var found *model.RuleLibraryItem
		for i := range shared.RuleLibrary {
			item := &shared.RuleLibrary[i]
			if matcher.same(*item, wanted) {
				if resourceEnabled(item.Enabled) {
					return item
				}
				if found == nil {
					found = item
				}
			}
		}
		return found
	}
	missing := func(shared *model.Shared, matcher *ruleMatcher) map[string]bool {
		names := map[string]bool{}
		for _, item := range shared.RuleLibrary {
			names[item.Name] = true
		}
		out := map[string]bool{}
		for _, builtin := range c.Items {
			if find(shared, builtin.Item, matcher) == nil && (!names[builtin.Item.Name] || selected[builtin.Identity]) {
				out[builtin.Identity] = true
			}
		}
		return out
	}
	shared, err := sharedSnapshot(ctx, r)
	if err != nil {
		return nil, err
	}
	matched := map[string]model.RuleLibraryItem{}
	matcher := &ruleMatcher{}
	if len(missing(&shared, matcher)) == 0 {
		for _, builtin := range c.Items {
			if item := find(&shared, builtin.Item, matcher); item != nil {
				matched[builtin.Identity] = *item
			}
		}
		return matched, nil
	}
	err = updateShared(ctx, r, func(shared *model.Shared) error {
		matcher := &ruleMatcher{}
		added := missing(shared, matcher)
		names, ids := map[string]bool{}, map[string]bool{}
		for _, item := range shared.RuleLibrary {
			names[item.Name], ids[item.ID] = true, true
		}
		for _, builtin := range c.Items {
			item := find(shared, builtin.Item, matcher)
			if item == nil && added[builtin.Identity] {
				copy := builtin.Item
				baseID := strings.ReplaceAll(builtin.Identity, ":", "_")
				copy.ID = baseID
				for suffix := 2; ids[copy.ID]; suffix++ {
					copy.ID = fmt.Sprintf("%s_%d", baseID, suffix)
				}
				for suffix := 1; names[copy.Name]; suffix++ {
					if suffix == 1 {
						copy.Name = builtin.Item.Name + msgBuiltinNameSuffix
					} else {
						copy.Name = fmt.Sprintf(msgBuiltinNumberedName, builtin.Item.Name, suffix)
					}
				}
				ids[copy.ID], names[copy.Name] = true, true
				shared.RuleLibrary = append(shared.RuleLibrary, copy)
				item = &shared.RuleLibrary[len(shared.RuleLibrary)-1]
			}
			if item != nil {
				matched[builtin.Identity] = *item
			}
		}
		return nil
	})
	return matched, err
}
