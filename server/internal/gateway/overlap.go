package gateway

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"configflow/internal/netipx"
)

// ipRange is a network as its first and last address.
type ipRange struct {
	first, last netip.Addr
	network     netip.Prefix
	rule        int // index of the rule it belongs to (direct ranges)
}

func rangeOf(network netip.Prefix, rule int) ipRange {
	return ipRange{first: network.Masked().Addr(), last: netipx.Broadcast(network), network: network.Masked(), rule: rule}
}

// supernetOf reports whether a covers b.
func supernetOf(a, b netip.Prefix) bool {
	return a.Bits() <= b.Bits() && a.Contains(b.Addr())
}

// DirectOverlapWarnings warns when a taken-over rule set captures addresses
// that a later DIRECT rule routes: taken-over connections reach Mihomo by IP
// only and the first matching rule wins, so the direct rule never gets them.
// Rule sets whose content cannot be read are named in a last warning. With
// the gateway off the list is empty. load nil reads the rule library (which
// may download a remote set once and cache it).
func (c *Config) DirectOverlapWarnings(ctx context.Context, load ContentLoader) ([]string, error) {
	settings, err := c.Settings()
	if err != nil || !settings.Enabled {
		return []string{}, err
	}
	var active []*Rule
	for _, item := range c.rules {
		if item.enabled.or(true) && item.libraryEnabled.or(true) {
			active = append(active, item)
		}
	}
	type loaded struct {
		networks []netip.Prefix
	}
	cache := map[string]loaded{}
	var unread []*Rule
	networksOf := func(item *Rule) ([]netip.Prefix, error) {
		if item.itemType.or("") == "rule" {
			if item.ruleType != "IP-CIDR" {
				return nil, nil
			}
			network, ok := parseNetwork(strings.TrimSpace(item.value))
			if !ok || !network.Addr().Is4() {
				return nil, nil
			}
			return []netip.Prefix{network}, nil
		}
		key := item.id.text
		if hit, ok := cache[key]; ok {
			return hit.networks, nil
		}
		networks, read, err := c.ruleSetNetworks(ctx, item, load)
		if err != nil {
			return nil, err
		}
		cache[key] = loaded{networks: networks}
		if !read {
			unread = append(unread, item)
		}
		return networks, nil
	}
	warnings := []string{}
	for index, item := range active {
		if item.itemType.or("") != "ruleset" || !item.id.isString || !slices.Contains(settings.RuleSetIDs, item.id.text) {
			continue
		}
		takenNetworks, err := networksOf(item)
		if err != nil {
			return nil, err
		}
		collapsed, err := netipx.Collapse(takenNetworks)
		if err != nil {
			return nil, err
		}
		taken := make([]ipRange, len(collapsed))
		for i, network := range collapsed {
			taken[i] = rangeOf(network, 0)
		}
		var direct []ipRange
		for order := index + 1; order < len(active); order++ {
			later := active[order]
			if later.policy.or("") != "DIRECT" {
				continue
			}
			networks, err := networksOf(later)
			if err != nil {
				return nil, err
			}
			for _, network := range networks {
				direct = append(direct, rangeOf(network, order))
			}
		}
		slices.SortStableFunc(direct, func(a, b ipRange) int {
			return cmp.Or(a.first.Compare(b.first), a.last.Compare(b.last), cmp.Compare(a.rule, b.rule))
		})
		// Both lists are sorted by start and the taken ranges are disjoint,
		// so one forward sweep finds every overlapping direct range.
		type hit struct {
			outer, inner netip.Prefix
			rule         int
		}
		var hits []hit
		position := 0
		for _, d := range direct {
			for position < len(taken) && taken[position].last.Less(d.first) {
				position++
			}
			if position < len(taken) && taken[position].first.Compare(d.last) <= 0 {
				hits = append(hits, hit{outer: taken[position].network, inner: d.network, rule: d.rule})
			}
		}
		if len(hits) == 0 {
			continue
		}
		var rules []int
		for _, h := range hits {
			rules = append(rules, h.rule)
		}
		slices.Sort(rules)
		rules = slices.Compact(rules)
		names := make([]string, len(rules))
		for i, rule := range rules {
			names[i] = c.ruleName(active[rule])
		}
		outer, inner := hits[0].outer, hits[0].inner
		var example string
		switch {
		case outer == inner:
			example = fmt.Sprintf(fmtOverlapSame, outer)
		case supernetOf(outer, inner):
			example = fmt.Sprintf(fmtOverlapCovers, outer, inner)
		default:
			example = fmt.Sprintf(fmtOverlapPartial, outer, inner)
		}
		name := c.ruleName(item)
		warnings = append(warnings, fmt.Sprintf(fmtOverlap, name, strings.Join(names, overlapNameJoiner), len(hits), example, name))
	}
	if len(unread) > 0 {
		names := make([]string, len(unread))
		for i, item := range unread {
			names[i] = c.ruleName(item)
		}
		warnings = append(warnings, fmt.Sprintf(fmtOverlapUnread, strings.Join(names, overlapNameJoiner)))
	}
	return warnings, nil
}

// ruleSetNetworks are the IPv4 networks of a rule set for the overlap check
// (any prefix, malformed lines skipped); read is false when its content
// cannot be read. An unusable rule set has none.
func (c *Config) ruleSetNetworks(ctx context.Context, item *Rule, load ContentLoader) (networks []netip.Prefix, read bool, err error) {
	library := c.Library(item)
	src := source(item, library)
	if c.Reason(item) != "" {
		return nil, true, nil
	}
	inline := src.sourceType.or("") == "content"
	var content string
	switch {
	case inline:
		if src.content.present && !src.content.isString {
			// Not text: unreadable unless it is empty (null).
			return nil, !src.content.truthy, nil
		}
		content = src.content.or("")
	case load != nil:
		if content, err = load(ctx, item, library); err != nil {
			return nil, false, err
		}
	default:
		if content, err = c.storedContent(ctx, item, library, false); err != nil {
			return nil, false, err
		}
	}
	if content == "" && !inline {
		return nil, false, nil
	}
	lines, err := contentLines(content)
	if err != nil {
		return nil, false, nil
	}
	return ruleNetworks(lines, behavior(item, library)), true, nil
}
