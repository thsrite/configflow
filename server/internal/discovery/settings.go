package discovery

import (
	"configflow/internal/lifecycle"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"unicode/utf8"

	"configflow/internal/repo"
	"configflow/internal/rules"
	"github.com/google/uuid"
)

// SettingsPayload returns the selected rulesets and valid candidates.
func SettingsPayload(ctx context.Context, id string) (*SettingsView, error) {
	c, err := loadConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	s := c.Settings
	candidates := []map[string]string{}
	for _, item := range c.Library {
		if problem(&item) == nil {
			candidates = append(candidates, map[string]string{"id": item.ID, "name": item.Name, "behavior": item.Behavior})
		}
	}
	return &SettingsView{Direct: info(c, s.DirectRuleset), Proxy: info(c, s.ProxyRuleset), Ignored: s.Ignored, AutoProbe: s.AutoProbe, AutoApply: s.AutoApply, Confidence: s.Confidence, ConfidenceChoices: []int{80, 90, 95}, ProbePath: proxyPath(c), RegionEnabled: s.RegionEnabled, RegionPort: s.RegionPort, Candidates: candidates, Policies: c.policies()}, nil
}
func invalid(msg string) error { return repo.NewValidationError(msg) }

// UpdateSettings validates and saves a partial settings object.
func UpdateSettings(ctx context.Context, id string, payload json.RawMessage) (*SettingsView, error) {
	ctx = lifecycle.Detach(ctx)
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil || fields == nil {
		return nil, invalid(msgObject)
	}
	c, err := loadConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	s := c.Settings
	for _, key := range []string{"direct_ruleset", "proxy_ruleset"} {
		v, ok := fields[key]
		if !ok {
			continue
		}
		var value *string
		if JSONOptionEnabled(v) {
			if err := json.Unmarshal(v, &value); err != nil {
				return nil, invalid(msgRuleset)
			}
		}
		if value != nil && *value == "" {
			value = nil
		}
		if value != nil && problem(c.library(*value)) != nil {
			return nil, invalid(msgRuleset)
		}
		if key == "direct_ruleset" {
			s.DirectRuleset = value
		} else {
			s.ProxyRuleset = value
		}
	}
	for _, key := range []string{"auto_probe", "auto_apply", "region_check_enabled"} {
		v, ok := fields[key]
		if !ok {
			continue
		}
		var value bool
		if string(v) == "null" || json.Unmarshal(v, &value) != nil {
			return nil, invalid(fmt.Sprintf(msgBoolean, key))
		}
		switch key {
		case "auto_probe":
			s.AutoProbe = value
		case "auto_apply":
			s.AutoApply = value
		default:
			s.RegionEnabled = value
		}
	}
	if v, ok := fields["auto_apply_min_confidence"]; ok {
		var n float64
		if json.Unmarshal(v, &n) != nil || n != 80 && n != 90 && n != 95 {
			return nil, invalid(msgConfidence)
		}
		s.Confidence = int(n)
	}
	if v, ok := fields["region_probe_port"]; ok {
		var n int
		if json.Unmarshal(v, &n) != nil || n < 1024 || n > 65535 {
			return nil, invalid(msgPort)
		}
		used, err := customPorts(c.Mihomo.Custom)
		if err != nil {
			return nil, err
		}
		if used[n] {
			return nil, invalid(fmt.Sprintf(msgPortUsed, n))
		}
		s.RegionPort = n
	}
	err = editSettings(ctx, id, func(current *Settings) error {
		for key := range fields {
			switch key {
			case "direct_ruleset":
				current.DirectRuleset = s.DirectRuleset
			case "proxy_ruleset":
				current.ProxyRuleset = s.ProxyRuleset
			case "auto_probe":
				current.AutoProbe = s.AutoProbe
			case "auto_apply":
				current.AutoApply = s.AutoApply
			case "auto_apply_min_confidence":
				current.Confidence = s.Confidence
			case "region_check_enabled":
				current.RegionEnabled = s.RegionEnabled
			case "region_probe_port":
				current.RegionPort = s.RegionPort
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return SettingsPayload(ctx, id)
}
func uniqueID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + fmt.Sprintf("%x", now().UnixNano())
	}
	return prefix + hex.EncodeToString(b[:])
}
func uniqueName(items []libraryItem, base string) string {
	name := base
	for n := 2; ; n++ {
		exists := false
		for _, item := range items {
			exists = exists || item.Name == name
		}
		if !exists {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, n)
	}
}
func newLibrary(items []libraryItem, name string) libraryItem {
	return libraryItem{ID: uniqueID("lib_"), Name: uniqueName(items, name), SourceType: "content", Behavior: "classical", Extra: map[string]json.RawMessage{"enabled": json.RawMessage("true")}}
}
func newReference(id, policy string) (ruleConfig, error) {
	identity, err := uuid.NewRandom()
	if err != nil {
		return ruleConfig{}, fmt.Errorf("create ruleset ID: %w", err)
	}
	return ruleConfig{ID: "ruleset_" + identity.String(), ItemType: "ruleset", LibraryID: id, Policy: policy, Enabled: true, Extra: map[string]json.RawMessage{"no_resolve": json.RawMessage("false")}}, nil
}
func insertRules(rules []ruleConfig, index int, added ...ruleConfig) []ruleConfig {
	out := make([]ruleConfig, 0, len(rules)+len(added))
	out = append(out, rules[:index]...)
	out = append(out, added...)
	return append(out, rules[index:]...)
}

// InitRulesets creates new discovery libraries and references before MATCH.
func InitRulesets(ctx context.Context, id string, targets []string, proxyPolicy string) (*SettingsView, error) {
	ctx = lifecycle.Detach(ctx)
	if len(targets) == 0 {
		targets = []string{"direct", "proxy"}
	}
	selected := make([]string, 0, 2)
	seen := make(map[string]bool, 2)
	for _, t := range targets {
		if t != "direct" && t != "proxy" {
			return nil, invalid(msgTargets)
		}
		if !seen[t] {
			seen[t] = true
			selected = append(selected, t)
		}
	}
	targets = selected
	c, err := loadConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	if contains(targets, "proxy") && !contains(c.policies(), proxyPolicy) {
		return nil, invalid(msgProxyPolicy)
	}
	created := map[string]libraryItem{}
	err = editLibrary(ctx, func(items *[]libraryItem) error {
		for _, t := range targets {
			name := msgDirectName
			if t == "proxy" {
				name = msgProxyName
			}
			item := newLibrary(*items, name)
			*items = append(*items, item)
			created[t] = item
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, t := range targets {
		if err = saveRule(ctx, created[t]); err != nil {
			return nil, err
		}
	}
	err = editProfile(ctx, id, func(c *config) error {
		pos := len(c.Rules)
		for i, r := range c.Rules {
			if r.ItemType == "rule" && r.RuleType == "MATCH" {
				pos = i
				break
			}
		}
		refs := []ruleConfig{}
		for _, t := range targets {
			item := created[t]
			policy := "DIRECT"
			if t == "proxy" {
				policy = proxyPolicy
				c.Settings.ProxyRuleset = new(item.ID)
			} else {
				c.Settings.DirectRuleset = new(item.ID)
			}
			ref, err := newReference(item.ID, policy)
			if err != nil {
				return err
			}
			refs = append(refs, ref)
		}
		c.Rules = insertRules(c.Rules, pos, refs...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return SettingsPayload(ctx, id)
}

// CleanDomain normalizes a domain and rejects IP addresses and single labels.
func CleanDomain(value string) string {
	value = strings.TrimRight(strings.ToLower(strings.TrimSpace(value)), ".")
	if _, err := netip.ParseAddr(value); err == nil {
		return ""
	}
	if !strings.Contains(value, ".") || !rules.IsValidDomain(value, true) {
		return ""
	}
	return value
}

// UpdateIgnored adds or removes normalized domains in the bounded ignore list.
func UpdateIgnored(ctx context.Context, id string, values []string, add bool) ([]string, error) {
	ctx = lifecycle.Detach(ctx)
	if len(values) == 0 {
		return nil, invalid(msgIgnored)
	}
	for i, v := range values {
		values[i] = CleanDomain(v)
		if values[i] == "" {
			return nil, invalid(msgIgnored)
		}
	}
	out := []string{}
	err := editSettings(ctx, id, func(s *Settings) error {
		if add {
			for _, v := range values {
				if !contains(s.Ignored, v) {
					s.Ignored = append(s.Ignored, v)
				}
			}
			if len(s.Ignored) > 2000 {
				return invalid(msgIgnoredLimit)
			}
		} else {
			next := []string{}
			for _, v := range s.Ignored {
				if !contains(values, v) {
					next = append(next, v)
				}
			}
			s.Ignored = next
		}
		out = s.Ignored
		return nil
	})
	return out, err
}

// SaveWebsiteNote saves or deletes the bounded annotation for a domain.
func SaveWebsiteNote(ctx context.Context, id string, payload json.RawMessage) (map[string]string, error) {
	ctx = lifecycle.Detach(ctx)
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil || fields == nil {
		return nil, invalid(msgObject)
	}
	var domain string
	var noteValue *string
	if json.Unmarshal(fields["domain"], &domain) != nil || json.Unmarshal(fields["note"], &noteValue) != nil || noteValue == nil {
		return nil, invalid(msgNote)
	}
	note := *noteValue
	domain = CleanDomain(domain)
	if domain == "" || utf8.RuneCountInString(note) > 200 {
		return nil, invalid(msgNote)
	}
	note = strings.TrimSpace(note)
	err := editSettings(ctx, id, func(s *Settings) error {
		if note == "" {
			delete(s.WebsiteNotes, domain)
		} else {
			if _, exists := s.WebsiteNotes[domain]; !exists && len(s.WebsiteNotes) >= 2000 {
				return invalid(msgNoteLimit)
			}
			s.WebsiteNotes[domain] = note
		}
		return nil
	})
	return map[string]string{"domain": domain, "note": note}, err
}
