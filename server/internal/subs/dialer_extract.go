package subs

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"configflow/internal/model"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// SubscriptionShapeError identifies a consumed external subscription field.
// It deliberately does not echo the entire source (which may contain secrets).
type SubscriptionShapeError struct{ Field string }

func (e *SubscriptionShapeError) Error() string { return fmt.Sprintf(msgSubscriptionList, e.Field) }
func subscriptionList(document *yamlx.Node, field string) ([]*yamlx.Node, error) {
	value := yamlx.Resolve(yamlx.Get(document, field))
	if dnsNull(value) {
		return nil, nil
	}
	if value.Kind != yaml.SequenceNode {
		return nil, &SubscriptionShapeError{Field: field}
	}
	return value.Content, nil
}
func readProxyName(proxy *yamlx.Node) (string, bool) {
	value := yamlx.Get(proxy, "name")
	if dnsNull(value) {
		return "", false
	}
	data, err := yamlx.ToJSON(value)
	if err != nil {
		return "", false
	}
	var text model.String
	if json.Unmarshal(data, &text) != nil {
		return "", false
	}
	return string(text), true
}
func proxyName(proxy *yamlx.Node) string { text, _ := readProxyName(proxy); return text }

// ExtractDialers records only groups referenced by this source's own proxies.
// Traversal is bounded by the parsed graph and each group is visited once per
// target; repeated subgroup references cannot cause exponential expansion.
func ExtractDialers(text string) (*DialersData, error) {
	if !strings.Contains(text, "dialer-proxy") {
		return nil, nil
	}
	document, err := yamlx.Parse([]byte(text))
	if err != nil {
		if errors.Is(err, yamlx.ErrAliasBudget) || errors.Is(err, yamlx.ErrAliasRecursion) {
			return nil, err
		}
		return nil, nil
	}
	if err := yamlx.ProjectJSONKeys(document, yamlx.DiscardNonStringKeys); err != nil {
		return nil, err
	}
	proxies, err := subscriptionList(document, "proxies")
	if err != nil {
		return nil, err
	}
	groupsList, err := subscriptionList(document, "proxy-groups")
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	groups := map[string]*yamlx.Node{}
	for _, proxy := range proxies {
		if name, ok := readProxyName(proxy); ok {
			names[name] = true
		}
	}
	for _, group := range groupsList {
		if name, ok := dnsText(yamlx.Get(group, "name")); ok && !names[name] {
			groups[name] = group
		}
	}
	wantedSet := map[string]bool{}
	for _, proxy := range proxies {
		if target, ok := dnsText(yamlx.Get(proxy, "dialer-proxy")); ok && groups[target] != nil {
			wantedSet[target] = true
		}
	}
	if len(wantedSet) == 0 {
		return nil, nil
	}
	wanted := make([]string, 0, len(wantedSet))
	for name := range wantedSet {
		wanted = append(wanted, name)
	}
	sort.Strings(wanted)
	targets := yamlx.Map()
	unresolved := yamlx.Seq()
	if len(wanted) > MaxTargets {
		for _, name := range wanted[MaxTargets:] {
			unresolved.Content = append(unresolved.Content, yamlx.Str(name))
		}
		wanted = wanted[:MaxTargets]
	}
	for _, target := range wanted {
		members := yamlx.Seq()
		seen, expanded := map[string]bool{}, map[string]bool{}
		pending := []string{target}
		for len(pending) > 0 {
			name := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if names[name] {
				if !seen[name] {
					seen[name] = true
					if len(members.Content) < MaxMembers {
						members.Content = append(members.Content, yamlx.Str(name))
					}
				}
				continue
			}
			group := groups[name]
			if group == nil || expanded[name] {
				continue
			}
			expanded[name] = true
			children, err := subscriptionList(group, "proxies")
			if shape := new(SubscriptionShapeError); errors.As(err, &shape) {
				shape.Field = "proxy-groups[" + strconv.Quote(name) + "].proxies"
			}
			if err != nil {
				return nil, err
			}
			for i := len(children) - 1; i >= 0; i-- {
				data, err := yamlx.ToJSON(children[i])
				if err != nil {
					return nil, err
				}
				var member model.String
				if json.Unmarshal(data, &member) == nil {
					pending = append(pending, string(member))
				}
			}
		}
		if len(members.Content) == 0 {
			unresolved.Content = append(unresolved.Content, yamlx.Str(target))
			continue
		}
		source := groups[target]
		typ := airportGroupType(yamlx.Get(source, "type"))
		entry := yamlx.Map(yamlx.Str("type"), yamlx.Str(typ), yamlx.Str("members"), members)
		for _, field := range groupKeys {
			value := yamlx.Resolve(yamlx.Get(source, field))
			if value != nil && value.Kind == yaml.ScalarNode && value.ShortTag() != "!!null" {
				entry.Content = append(entry.Content, yamlx.Str(field), value)
			}
		}

		targets.Content = append(targets.Content, yamlx.Str(target), entry)
	}
	output := yamlx.Map(yamlx.Str("targets"), targets, yamlx.Str("unresolved"), unresolved)
	data, err := yamlx.ToJSON(output)
	if err != nil {
		return nil, err
	}
	var record DialersData
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

// airportGroupType accepts select, url-test, fallback and load-balance. Any
// other external airport value defaults to select: source configurations often
// contain unsupported group kinds, and usable relays must remain selectable.
// This compatibility rule does not relax stored model.ProxyGroup.Type.
func airportGroupType(value *yamlx.Node) string {
	text, ok := dnsText(value)
	if !ok {
		return "select"
	}
	for _, supported := range groupTypes {
		if text == supported {
			return text
		}
	}
	return "select"
}

func airportTargets(record *DialersData) map[string]bool {
	out := map[string]bool{}
	if record == nil {
		return out
	}
	for name := range record.Targets.All() {
		out[name] = true
	}
	for _, name := range record.Unresolved {
		out[name] = true
	}
	return out
}

// AdaptProxies projects chained source proxies onto groups emitted in main.
// It returns original graph nodes unless it owns a dialer-proxy replacement.
func AdaptProxies(subscription string, proxies []*yamlx.Node, main *yamlx.Node, record *DialersData, chains bool) ([]*yamlx.Node, error) {
	airport := airportTargets(record)
	if len(airport) == 0 {
		return proxies, nil
	}
	names := map[string]bool{}
	for _, field := range []string{"proxies", "proxy-groups"} {
		list := yamlx.Resolve(yamlx.Get(main, field))
		if list == nil || list.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range list.Content {
			if name, ok := dnsText(yamlx.Get(item, "name")); ok {
				names[name] = true
			}
		}
	}
	adapted := make([]*yamlx.Node, 0, len(proxies))
	var dropped []string
	for _, proxy := range proxies {
		target, ok := dnsText(yamlx.Get(proxy, "dialer-proxy"))
		switch {
		case !ok || !airport[target]:
			adapted = append(adapted, proxy)
		case !chains:
			dropped = append(dropped, proxyName(proxy))
		case names[GroupName(subscription, target)]:
			// The individual record is validated before Clone, never the whole config.
			validated, err := yamlx.Encode(proxy)
			if err != nil {
				return nil, err
			}
			owned := yamlx.Clone(validated)
			yamlx.Set(owned, "dialer-proxy", yamlx.Str(GroupName(subscription, target)))
			adapted = append(adapted, owned)
		case names[target]:
			adapted = append(adapted, proxy)
		default:
			dropped = append(dropped, proxyName(proxy))
		}
	}
	if len(dropped) > 0 {
		shown := dropped
		if len(shown) > 5 {
			shown = shown[:5]
		}
		dialersLogger.Infof(msgDialerSkipped, subscription, len(dropped), strings.Join(shown, "、"))
	}
	return adapted, nil
}
