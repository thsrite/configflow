package subs

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// PolicyKey is the Mihomo policy that routes proxy hostname lookups.
const PolicyKey = "proxy-server-nameserver-policy"

// MinCoreText is the first Mihomo version that supports PolicyKey.
const MinCoreText = "v1.19.20"
const (
	minCoreMajor = 1
	minCoreMinor = 19
	minCorePatch = 20
)

// ConnectivityFilter is the Windows connectivity check default fake-IP exemption.
var ConnectivityFilter = []string{"dns.msftncsi.com", "www.msftncsi.com", "www.msftconnecttest.com"}
var simplePattern = regexp.MustCompile(`^(?:\+\.|\.|\*\.)?[a-z0-9_*.-]+$`)
var versionRE = regexp.MustCompile(`(?i)^v?([0-9]+)\.([0-9]+)\.([0-9]+)`)

// registrable is _registrable: "<domain>.<suffix>" per tldextract 5.1.2's
// bundled public suffix list with private suffixes counted as public (one
// airport's names under github.io must not become +.github.io), or "" for an
// unlisted suffix or a name that is itself a suffix.
func registrable(domain string) string {
	_, label, suffix := ExtractDomain(domain, true)
	if label != "" && suffix != "" {
		return label + "." + suffix
	}
	return ""
}

func reversedLabels(s string) []string {
	labels := strings.Split(strings.TrimPrefix(s, "+."), ".")
	for i, j := 0, len(labels)-1; i < j; i, j = i+1, j-1 {
		labels[i], labels[j] = labels[j], labels[i]
	}
	return labels
}

// Compress is compress: names under one registrable domain merge into
// `+.<deepest common suffix>`; unlisted names stay exact.
func Compress(domains []string) []string {
	groups := map[string][]string{}
	var bases []string
	result := map[string]bool{}
	seen := map[string]bool{}
	for _, d := range domains {
		if seen[d] {
			continue
		}
		seen[d] = true
		base := registrable(d)
		if base == "" {
			result[d] = true
			continue
		}
		if _, ok := groups[base]; !ok {
			bases = append(bases, base)
		}
		groups[base] = append(groups[base], d)
	}
	for _, base := range bases {
		names := groups[base]
		if len(names) == 1 {
			result[names[0]] = true
			continue
		}
		var common []string
		rev := make([][]string, len(names))
		minLen := -1
		for i, n := range names {
			l := strings.Split(n, ".")
			for a, b := 0, len(l)-1; a < b; a, b = a+1, b-1 {
				l[a], l[b] = l[b], l[a]
			}
			rev[i] = l
			if minLen < 0 || len(l) < minLen {
				minLen = len(l)
			}
		}
		for col := 0; col < minLen; col++ {
			same := true
			for _, l := range rev {
				if l[col] != rev[0][col] {
					same = false
					break
				}
			}
			if !same {
				break
			}
			common = append(common, rev[0][col])
		}
		for a, b := 0, len(common)-1; a < b; a, b = a+1, b-1 {
			common[a], common[b] = common[b], common[a]
		}
		suffix := strings.Join(common, ".")
		if strings.HasSuffix(suffix, base) {
			result["+."+suffix] = true
		} else {
			result["+."+base] = true
		}
	}
	out := make([]string, 0, len(result))
	for k := range result {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := reversedLabels(out[i]), reversedLabels(out[j])
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		return out[i] < out[j]
	})
	return out
}

// DNSListener returns a usable local DNS URL, or a stable warning category.
func DNSListener(listen string) (string, string) {
	t := strings.TrimSpace(listen)
	if t == "" {
		return "", "no_listen"
	}
	i := strings.LastIndex(t, ":")
	if i < 0 {
		return "", "listen_unparsed"
	}
	host, port := strings.TrimSpace(t[:i]), t[i+1:]
	if port == "" {
		return "", "listen_unparsed"
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return "", "listen_unparsed"
		}
	}
	pn, err := strconv.ParseUint(port, 10, 16)
	if err != nil || pn == 0 {
		return "", "listen_unparsed"
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return fmt.Sprintf("udp://127.0.0.1:%d", pn), ""
	}
	address, ok := parseHostIP(host)
	if !ok || address.WithZone("").Unmap().IsUnspecified() || address.WithZone("").Unmap().IsMulticast() {
		return "", "listen_unparsed"
	}
	if address.Is6() {
		return fmt.Sprintf("udp://[%s]:%d", address, pn), ""
	}
	return fmt.Sprintf("udp://%s:%d", address, pn), ""
}
func nameserverAddr(text string) (scheme, host string, port int, ok bool) {
	u, err := weburl.Parse(text)
	if err != nil {
		return "", "", 0, false
	}
	port, _, err = weburl.Port(u)
	if err != nil {
		return "", "", 0, false
	}
	return strings.ToLower(u.Scheme), weburl.Hostname(u), port, true
}
func isUnspecifiedListen(listen string) bool {
	t := strings.TrimSpace(listen)
	i := strings.LastIndex(t, ":")
	h := ""
	if i >= 0 {
		h = t[:i]
	}
	h = strings.Trim(strings.TrimSpace(h), "[]")
	return h == "" || h == "0.0.0.0" || h == "::"
}
func listenerTargets(values *yamlx.Node, listen string) bool {
	address, _ := DNSListener(listen)
	if address == "" {
		return false
	}
	values = yamlx.Resolve(values)
	if values == nil {
		return false
	}
	items := []*yamlx.Node{values}
	if values.Kind == yaml.SequenceNode {
		items = values.Content
	}
	if len(items) == 0 {
		return false
	}
	_, expectedHost, expectedPort, _ := nameserverAddr(address)
	for _, value := range items {
		s, ok := dnsText(value)
		if !ok || strings.TrimSpace(s) == "" {
			return false
		}
		text := strings.TrimSpace(s)
		if !strings.Contains(s, "://") {
			text = "udp://" + text
		}
		scheme, host, port, ok := nameserverAddr(text)
		if !ok {
			return false
		}
		if port == 0 {
			port = 53
		}
		if (scheme != "udp" && scheme != "tcp") || port != expectedPort {
			return false
		}
		if host == expectedHost || (expectedHost == "127.0.0.1" && host == "localhost") {
			continue
		}
		if isUnspecifiedListen(listen) {
			if host == "localhost" {
				continue
			}
			if addr, ok := parseHostIP(host); ok && addr.WithZone("").Unmap().IsLoopback() {
				continue
			}
		}
		return false
	}
	return true
}

// CoreVersion parses an ASCII semantic version, accepting the usual Mihomo prefixes.
// Version components are unbounded; build and prerelease suffixes are immaterial.
func CoreVersion(text string) (version [3]*big.Int, ok bool) {
	parts := strings.Fields(text)
	if len(parts) > 0 && strings.EqualFold(parts[0], "mihomo") {
		parts = parts[1:]
	}
	if len(parts) > 0 && strings.EqualFold(parts[0], "meta") {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return version, false
	}
	match := versionRE.FindStringSubmatch(parts[0])
	if match == nil {
		return version, false
	}
	for i := range version {
		version[i], ok = new(big.Int).SetString(match[i+1], 10)
		if !ok {
			return version, false
		}
	}
	return version, true
}

// IsCoreTooOld reports whether a recognized core predates proxy DNS policies.
func IsCoreTooOld(text string) bool {
	v, ok := CoreVersion(text)
	if !ok {
		return false
	}
	minimum := [3]int64{minCoreMajor, minCoreMinor, minCorePatch}
	for i := range v {
		if cmp := v[i].Cmp(big.NewInt(minimum[i])); cmp != 0 {
			return cmp < 0
		}
	}
	return false
}

// Warnings are the fixed warning texts of ApplyNodeDNS.
var Warnings = map[string]string{
	"dns_disabled":       "DNS 没有开启（dns.enable），没有本机 DNS 监听可用",
	"use_hosts_off":      "dns.use-hosts 为 false，DNS 监听不使用 hosts",
	"no_listen":          "没有配置 dns.listen，没有本机 DNS 监听可用",
	"listen_unparsed":    "dns.listen 不是「IP:端口」形式，无法推导本机 DNS 监听地址",
	"no_nameserver":      "没有写 proxy-server-nameserver：Mihomo 要求先有它才接受 proxy-server-nameserver-policy，否则拒绝加载配置；ConfigFlow 不替你填写，在基础配置的 dns 里写上（例如公网 DoH）后重新生成",
	"policy_not_mapping": "手写的 proxy-server-nameserver-policy 不是「域名: DNS」的映射，ConfigFlow 无法追加",
	"filter_not_list":    "dns.fake-ip-filter 不是列表，ConfigFlow 无法加入映射目标，映射目标会拿到假地址，所以没有改动",
}

func oldCoreWarning(version string) string {
	return fmt.Sprintf(msgDNSOldCore, version, MinCoreText)
}

// DNSChoice records ownership of one selected proxy hostname lookup policy.
type DNSChoice struct {
	Domain string `json:"domain"`
	Target string `json:"target"`
	Source string `json:"source"`
}

// DNSSkip explains a mapping deliberately left unchanged.
type DNSSkip struct {
	Domain string `json:"domain"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// DNSReport is the stable generated-preview DTO for subscription hostname DNS.
type DNSReport struct {
	Mapped          int         `json:"mapped"`
	Applied         bool        `json:"applied"`
	Listener        *string     `json:"listener"`
	Policy          []DNSChoice `json:"policy"`
	Filter          []string    `json:"filter"`
	TargetsFiltered int         `json:"targets_filtered"`
	TargetsCovered  int         `json:"targets_covered"`
	FakeIP          *string     `json:"fake_ip"`
	Skipped         []DNSSkip   `json:"skipped"`
	Warnings        []string    `json:"warnings"`
	Notes           []string    `json:"notes"`
	MinVersion      string      `json:"min_version"`
}

type dnsSettings struct {
	Enable       *bool  `json:"enable"`
	UseHosts     *bool  `json:"use-hosts"`
	Listen       string `json:"listen"`
	EnhancedMode string `json:"enhanced-mode"`
	FilterMode   string `json:"fake-ip-filter-mode"`
	Fields       model.Fields
	Raw          model.Raw
}

func (d *dnsSettings) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, d, &d.Raw) }

// readDNSSettings decodes only the consumed fixed scalar members, once. The
// potentially large and open hosts, policy and filter graphs remain Nodes.
func readDNSSettings(dns *yamlx.Node) (dnsSettings, error) {
	fixed := struct {
		Enable       json.RawMessage `json:"enable,omitempty"`
		UseHosts     json.RawMessage `json:"use-hosts,omitempty"`
		Listen       json.RawMessage `json:"listen,omitempty"`
		EnhancedMode json.RawMessage `json:"enhanced-mode,omitempty"`
		FilterMode   json.RawMessage `json:"fake-ip-filter-mode,omitempty"`
	}{}
	fields := []struct {
		name string
		out  *json.RawMessage
	}{{"enable", &fixed.Enable}, {"use-hosts", &fixed.UseHosts}, {"listen", &fixed.Listen}, {"enhanced-mode", &fixed.EnhancedMode}, {"fake-ip-filter-mode", &fixed.FilterMode}}
	for _, field := range fields {
		if value := yamlx.Get(dns, field.name); value != nil {
			data, err := yamlx.ToJSON(value)
			if err != nil {
				return dnsSettings{}, err
			}
			*field.out = data
		}
	}
	data, err := jsonresp.Encode(fixed)
	var out dnsSettings
	if err == nil {
		err = json.Unmarshal(data, &out)
	}
	return out, err
}
func dnsText(n *yamlx.Node) (string, bool) {
	n = yamlx.Resolve(n)
	if n == nil || n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
		return "", false
	}
	return n.Value, true
}
func dnsNull(n *yamlx.Node) bool {
	n = yamlx.Resolve(n)
	return n == nil || (n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null")
}
func simplePatterns(entries []*yamlx.Node) []string {
	var out []string
	for _, entry := range entries {
		if text, ok := dnsText(entry); ok && simplePattern.MatchString(hostPatternText(text)) {
			out = append(out, hostPatternText(text))
		}
	}
	return out
}
func uncheckedEntries(entries []*yamlx.Node) bool {
	for _, entry := range entries {
		if text, ok := dnsText(entry); ok && !simplePattern.MatchString(hostPatternText(text)) {
			return true
		}
	}
	return false
}
func covered(patterns []string, domain string) bool {
	for _, pattern := range patterns {
		if hostPatternMatches(pattern, domain) {
			return true
		}
	}
	return false
}

type policyKeyEntry struct {
	pattern string
	value   *yamlx.Node
}

func policyKeys(policy *yamlx.Node) (plain []policyKeyEntry, matchers []string) {
	for key, value := range yamlx.AllPairs(policy) {
		keyText, ok := dnsText(key)
		if !ok {
			continue
		}
		text := strings.TrimSpace(keyText)
		lower := strings.ToLower(text)
		if strings.HasPrefix(lower, "geosite:") || strings.HasPrefix(lower, "rule-set:") {
			matchers = append(matchers, text)
			continue
		}
		for _, part := range strings.Split(text, ",") {
			if strings.TrimSpace(part) != "" {
				plain = append(plain, policyKeyEntry{hostPatternText(part), value})
			}
		}
	}
	return
}
func userPolicyFor(plain []policyKeyEntry, domain string) *policyKeyEntry {
	best := -1
	for i := range plain {
		if hostPatternMatches(plain[i].pattern, domain) {
			if plain[i].pattern == domain {
				return &plain[i]
			}
			if best < 0 || len(plain[i].pattern) > len(plain[best].pattern) {
				best = i
			}
		}
	}
	if best < 0 {
		return nil
	}
	return &plain[best]
}
func containsDNSNode(items []*yamlx.Node, value *yamlx.Node) bool {
	for _, item := range items {
		if yamlx.Equal(item, value) {
			return true
		}
	}
	return false
}

// ApplyDNS plans and applies subscription hostname policies on an open Mihomo
// graph. It indexes hosts exactly once and clones only the owned DNS subtree.
// All validation precedes writes, so warnings and errors leave the graph intact.
func ApplyDNS(main, merged *yamlx.Node, coreVersion string) (*DNSReport, error) {
	resolver := NewHostResolver(yamlx.Get(main, "hosts"))
	var mapped []DNSChoice
	for key, value := range yamlx.AllPairs(merged) {
		domain, ok := dnsText(key)
		if !ok {
			continue
		}
		if _, ok := dnsText(value); ok {
			if target := resolver.Target(domain); target != "" {
				mapped = append(mapped, DNSChoice{Domain: domain, Target: target})
			}
		}
	}
	if len(mapped) == 0 {
		return nil, nil
	}
	report := &DNSReport{Mapped: len(mapped), Policy: []DNSChoice{}, Filter: []string{}, Skipped: []DNSSkip{}, Warnings: []string{}, Notes: []string{}, MinVersion: MinCoreText}
	warn := func(text string) { report.Warnings = append(report.Warnings, text) }
	dns := yamlx.Resolve(yamlx.Get(main, "dns"))
	settings, err := readDNSSettings(dns)
	if err != nil {
		return nil, err
	}
	if settings.Enable == nil || !*settings.Enable {
		warn(Warnings["dns_disabled"])
		return report, nil
	}
	if settings.UseHosts != nil && !*settings.UseHosts {
		warn(Warnings["use_hosts_off"])
		return report, nil
	}
	address, reason := DNSListener(settings.Listen)
	if address == "" {
		warn(Warnings[reason])
		return report, nil
	}
	report.Listener = &address
	if coreVersion != "" && IsCoreTooOld(coreVersion) {
		warn(oldCoreWarning(coreVersion))
		return report, nil
	}
	nameservers := yamlx.Resolve(yamlx.Get(dns, "proxy-server-nameserver"))
	if nameservers == nil || !((nameservers.Kind == yaml.SequenceNode && len(nameservers.Content) > 0) || (nameservers.Kind == yaml.ScalarNode && nameservers.ShortTag() == "!!str" && nameservers.Value != "")) {
		warn(Warnings["no_nameserver"])
		return report, nil
	}
	written := yamlx.Resolve(yamlx.Get(dns, PolicyKey))
	if !dnsNull(written) && written.Kind != yaml.MappingNode {
		warn(Warnings["policy_not_mapping"])
		return report, nil
	}
	if dnsNull(written) {
		written = yamlx.Map()
	}
	plain, matchers := policyKeys(written)
	chosen := make([]DNSChoice, 0, len(mapped))
	for _, item := range mapped {
		found := userPolicyFor(plain, item.Domain)
		if found == nil {
			item.Source = "configflow"
			chosen = append(chosen, item)
			continue
		}
		if listenerTargets(found.value, settings.Listen) {
			item.Source = "user"
			chosen = append(chosen, item)
			continue
		}
		report.Skipped = append(report.Skipped, DNSSkip{item.Domain, item.Target, "policy_conflict"})
		if found.pattern == item.Domain {
			warn(fmt.Sprintf(msgDNSPolicyConflict, item.Domain))
		} else {
			warn(fmt.Sprintf(msgDNSPolicyCovers, found.pattern, item.Domain))
		}
	}
	if len(matchers) > 0 {
		report.Notes = append(report.Notes, msgDNSMatchers)
	}
	var additions []string
	var filterValue []*yamlx.Node
	if settings.EnhancedMode == "fake-ip" {
		mode := settings.FilterMode
		if mode == "" {
			mode = "blacklist"
		}
		report.FakeIP = &mode
		current := yamlx.Resolve(yamlx.Get(dns, "fake-ip-filter"))
		if current == nil && mode != "rule" {
			current = yamlx.Seq()
			for _, text := range ConnectivityFilter {
				current.Content = append(current.Content, yamlx.Str(text))
			}
		}
		if dnsNull(current) {
			current = yamlx.Seq()
		}
		if current.Kind != yaml.SequenceNode {
			warn(Warnings["filter_not_list"])
			report.FakeIP = nil
			return report, nil
		}
		if mode == "whitelist" {
			patterns := simplePatterns(current.Content)
			kept := chosen[:0]
			for _, item := range chosen {
				if covered(patterns, item.Target) {
					report.Skipped = append(report.Skipped, DNSSkip{item.Domain, item.Target, "whitelist_covers"})
					warn(fmt.Sprintf(msgDNSWhitelistCovers, item.Target, item.Domain))
				} else {
					kept = append(kept, item)
				}
			}
			chosen = kept
			if len(chosen) > 0 {
				text := msgDNSWhitelist
				if uncheckedEntries(current.Content) {
					text += msgDNSUnchecked
				}
				report.Notes = append(report.Notes, text)
			}
		} else {
			var targets, needed, patterns []string
			seen := map[string]bool{}
			for _, item := range chosen {
				if !seen[item.Target] {
					seen[item.Target] = true
					targets = append(targets, item.Target)
				}
			}
			if mode != "rule" {
				patterns = simplePatterns(current.Content)
			}
			for _, target := range targets {
				if !covered(patterns, target) {
					needed = append(needed, target)
				}
			}
			report.TargetsCovered = len(targets) - len(needed)
			report.TargetsFiltered = len(needed)
			additions = Compress(needed)
			if mode == "rule" {
				for _, entry := range additions {
					text := "DOMAIN," + entry + ",real-ip"
					if strings.HasPrefix(entry, "+.") {
						text = "DOMAIN-SUFFIX," + entry[2:] + ",real-ip"
					}
					filterValue = append(filterValue, yamlx.Str(text))
				}
				rules := append([]*yamlx.Node(nil), filterValue...)
				for _, entry := range current.Content {
					if !containsDNSNode(rules, entry) {
						filterValue = append(filterValue, entry)
					}
				}
			} else {
				filterValue = append(filterValue, current.Content...)
				for _, entry := range additions {
					n := yamlx.Str(entry)
					if !containsDNSNode(current.Content, n) {
						filterValue = append(filterValue, n)
					}
				}
			}
		}
	} else {
		mode := "not_needed"
		report.FakeIP = &mode
	}
	if len(chosen) == 0 {
		return report, nil
	}
	// Validate before Clone: Clone deliberately returns null for invalid graphs.
	if _, err := yamlx.Encode(dns); err != nil {
		return nil, err
	}
	policy := yamlx.Clone(written)
	for _, item := range chosen {
		if item.Source == "configflow" {
			yamlx.Set(policy, item.Domain, yamlx.Str(address))
		}
	}
	policyChanged := !yamlx.Equal(policy, written)
	if policyChanged || len(additions) > 0 {
		owned := yamlx.Clone(dns)
		if policyChanged {
			yamlx.Set(owned, PolicyKey, policy)
		}
		if len(additions) > 0 {
			yamlx.Set(owned, "fake-ip-filter", yamlx.Clone(yamlx.Seq(filterValue...)))
		}
		yamlx.Set(main, "dns", owned)
		yamlx.MaterializeAliases(main)
	}
	report.Policy = chosen
	report.Filter = append(report.Filter, additions...)
	report.Applied = true
	if _, ok := CoreVersion(coreVersion); !ok {
		report.Notes = append(report.Notes, fmt.Sprintf(msgDNSUnknownVersion, MinCoreText))
	}
	return report, nil
}
