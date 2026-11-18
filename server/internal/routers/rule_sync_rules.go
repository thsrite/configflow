package routers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/netipx"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

const syncMaxBytes = 4 * 1024 * 1024
const syncMaxIPs = 20000

// RuleSyncRecord holds classified source versions, never downloaded contents.
type RuleSyncRecord struct {
	Version      string   `json:"version"`
	IPv4         []string `json:"ipv4"`
	IPv6         []string `json:"ipv6"`
	IP6Count     int      `json:"ip6_count"`
	IPCount      int      `json:"ip_count"`
	DomainCount  int      `json:"domain_count"`
	IgnoredCount int      `json:"ignored_count"`
	InvalidCount int      `json:"invalid_count"`
	Name         string   `json:"name"`
	SourceType   string   `json:"source_type"`
	LibraryID    string   `json:"library_id"`
}

var syncDomain = regexp.MustCompile(`^(?:\+\.|\*\.|\.)?(?:[\pL\pN_\-]+\.)+[\pL\pN_\-]+\.?$`)
var syncNumeric = regexp.MustCompile(`^[\pN.]+$`)
var syncPayload = regexp.MustCompile(`(?m)^payload\s*:`)
var syncDomainTypes = strings.Fields("DOMAIN DOMAIN-SUFFIX DOMAIN-KEYWORD DOMAIN-REGEX DOMAIN-WILDCARD")
var syncOtherTypes = strings.Fields("GEOIP GEOSITE IP-ASN SRC-IP-CIDR SRC-IP-CIDR6 SRC-PORT DST-PORT PROCESS-NAME PROCESS-PATH PROCESS-NAME-REGEX PROCESS-PATH-REGEX NETWORK IN-TYPE IN-NAME IN-USER IN-PORT UID DSCP AND OR NOT")

func syncVersion(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// ClassifySyncRules strictly classifies bounded UTF-8 rule text for unattended writes.
func ClassifySyncRules(content, behavior string) (*RuleSyncRecord, error) {
	if len(content) > syncMaxBytes {
		return nil, routerError(messageSyncLarge)
	}
	text := strings.TrimSpace(strings.TrimLeft(content, "\ufeff"))
	if text == "" || strings.ContainsAny(text[:1], "<{[") {
		return nil, routerError(messageSyncFormat)
	}
	if !slices.Contains([]string{"classical", "ipcidr", "domain"}, behavior) {
		return nil, routerError(messageSyncBehavior)
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var entries []*yaml.Node
	if syncPayload.MatchString(text) {
		root, err := yamlx.Parse([]byte(text))
		if err != nil {
			return nil, routerError(messageSyncYAML)
		}
		payload := yamlx.Resolve(yamlx.Get(root, "payload"))
		if payload == nil || payload.Kind != yaml.SequenceNode {
			return nil, routerError(messageSyncPayload)
		}
		entries = payload.Content
	} else {
		for _, line := range lines {
			entries = append(entries, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: line})
		}
	}
	if len(entries) > 100000 {
		return nil, routerError(messageSyncLines)
	}
	record := &RuleSyncRecord{Version: syncVersion(content), IPv4: []string{}, IPv6: []string{}}
	v4, v6 := map[string]bool{}, map[string]bool{}
	invalid, total, examples := 0, 0, []string{}
	bad := func(i int) {
		invalid++
		if len(examples) < 3 {
			examples = append(examples, strconv.Itoa(i+1))
		}
	}
	for i, entry := range entries {
		raw := yamlx.Resolve(entry)
		if raw == nil || raw.Kind != yaml.ScalarNode || raw.ShortTag() != "!!str" {
			bad(i)
			continue
		}
		line := strings.TrimSpace(raw.Value)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		total++
		kind, value := "", line
		if a, b, ok := strings.Cut(line, ","); ok {
			kind = strings.ToUpper(strings.TrimSpace(a))
			value = strings.TrimSpace(b)
			if kind == "IP-CIDR" || kind == "IP-CIDR6" {
				value, _, _ = strings.Cut(value, ",")
				value = strings.TrimSpace(value)
			}
		}
		domain := slices.Contains(syncDomainTypes, kind)
		if kind == "" {
			for _, prefix := range []string{"domain:", "full:", "keyword:", "regexp:"} {
				if strings.HasPrefix(line, prefix) {
					domain = true
					value = strings.TrimSpace(strings.TrimPrefix(line, prefix))
					break
				}
			}
		}
		network, ok := netipx.ParseNetwork(value, false)
		if kind == "" && !domain && !ok {
			domain = syncDomain.MatchString(value) && !syncNumeric.MatchString(value)
		}
		if domain && value != "" && behavior != "ipcidr" {
			record.DomainCount++
			continue
		}
		if kind == "IP-CIDR" && ok && !network.Addr().Is4() || kind == "IP-CIDR6" && ok && network.Addr().Is4() {
			ok = false
		}
		if ok && behavior != "domain" && (kind == "" || kind == "IP-CIDR" || kind == "IP-CIDR6") {
			if network.Bits() > 0 && netipx.IsGlobal(network) {
				target, message := v4, messageSyncIPs
				if network.Addr().Is6() {
					target, message = v6, messageSyncIPs6
				}
				target[network.String()] = true
				if len(target) > syncMaxIPs {
					return nil, routerError(message)
				}
			} else {
				record.IgnoredCount++
			}
			continue
		}
		if slices.Contains(syncOtherTypes, kind) && value != "" && behavior == "classical" {
			record.IgnoredCount++
			continue
		}
		bad(i)
	}
	if total == 0 {
		return nil, routerError(messageSyncEmpty)
	}
	if invalid > 0 {
		detail := messageSyncInvalid
		if behavior == "ipcidr" {
			detail = messageSyncInvalidIP
		}
		return nil, routerError(fmt.Sprintf(messageSyncInvalidLines, detail, invalid, strings.Join(examples, "、")))
	}
	for n := range v4 {
		record.IPv4 = append(record.IPv4, n)
	}
	for n := range v6 {
		record.IPv6 = append(record.IPv6, n)
	}
	slices.Sort(record.IPv4)
	slices.Sort(record.IPv6)
	record.IPCount = len(v4)
	record.IP6Count = len(v6)
	return record, nil
}

type syncSource struct {
	Item    RuleSet
	Library LibraryRule
	Source  struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		SourceType string `json:"source_type"`
		URL        string `json:"url"`
		Behavior   string `json:"behavior"`
		Content    string `json:"content"`
	}
	Behavior string
}

func selectedSyncSources(config json.RawMessage, r *Router) ([]syncSource, error) {
	var doc struct {
		Rules   []json.RawMessage `json:"rule_configs"`
		Library []json.RawMessage `json:"rule_library"`
	}
	if err := json.Unmarshal(config, &doc); err != nil {
		return nil, err
	}
	rules, library := map[string]RuleSet{}, map[string]LibraryRule{}
	for _, raw := range doc.Rules {
		var v RuleSet
		if json.Unmarshal(raw, &v) == nil && v.ItemType == "ruleset" {
			v.Raw = raw
			rules[v.ID] = v
		}
	}
	for _, raw := range doc.Library {
		var v LibraryRule
		if json.Unmarshal(raw, &v) == nil {
			v.Raw = raw
			library[v.ID] = v
		}
	}
	ids := append(r.StaticRuleSets(), r.RosPolicyRuleSets...)
	seen := map[string]bool{}
	result := []syncSource{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if len(seen) > 128 {
			return nil, routerError(messageSyncSelectionCap)
		}
		item, exists := rules[id]
		source, found := library[item.LibraryRuleID]
		var ip *RuleSet
		var sp *LibraryRule
		if exists {
			ip = &item
		}
		if found {
			sp = &source
		}
		if issue := RuleSourceIssue(ip, sp, true); issue != "" {
			return nil, routerError(fmt.Sprintf(messageSyncSourceIssue, issue))
		}
		v := syncSource{Item: item, Library: source}
		if err := json.Unmarshal(source.Raw, &v.Source); err != nil {
			return nil, err
		}
		if v.Source.SourceType == "" {
			v.Source.SourceType = "url"
		}
		var b struct {
			Behavior string `json:"behavior"`
		}
		_ = json.Unmarshal(item.Raw, &b)
		v.Behavior = orText(v.Source.Behavior, orText(b.Behavior, "domain"))
		result = append(result, v)
	}
	if len(result) == 0 {
		return nil, routerError(messageSyncSelect)
	}
	return result, nil
}

func syncIPLoader(records map[string]*RuleSyncRecord) ContentLoader {
	return func(item RuleSet, _ *LibraryRule) (string, error) {
		record := records[item.ID]
		if record == nil {
			return "# no routable IP", nil
		}
		values := append(slices.Clone(record.IPv4), record.IPv6...)
		if len(values) == 0 {
			return "# no routable IP", nil
		}
		return strings.Join(values, "\n"), nil
	}
}
func checkSyncUpdates(previous, current map[string]*RuleSyncRecord) error {
	for _, id := range sortedSyncKeys(current) {
		record := current[id]
		old := previous[id]
		if old == nil {
			return routerError(messageSyncSelectionChanged)
		}
		if record.SourceType != "url" && record.Version != old.Version {
			return routerError(fmt.Sprintf(messageSyncLocalChanged, record.Name))
		}
		for _, item := range []struct{ version, old, next int }{{4, old.IPCount, record.IPCount}, {6, old.IP6Count, record.IP6Count}} {
			if item.old > 0 && item.next == 0 {
				return routerError(fmt.Sprintf(messageSyncLostIPs, record.Name, item.version, item.old))
			}
		}
	}
	return nil
}
func sortedSyncKeys[T any](items map[string]T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func syncCIDRsEqual(a, b []ProxyCIDR) bool { return jsonEqual(a, b) }
func syncCIDRSet(items []ProxyCIDR) map[string]bool {
	result := map[string]bool{}
	for _, item := range items {
		result[item.CIDR] = true
	}
	return result
}
func syncNetworkKey(value string) string {
	if p, ok := netipx.ParseNetwork(value, false); ok {
		return p.String()
	}
	return value
}
