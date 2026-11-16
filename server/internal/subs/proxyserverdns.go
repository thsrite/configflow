package subs

import (
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/weburl"
)

// Constants of proxy_server_dns.
const (
	PolicyKey    = "proxy-server-nameserver-policy"
	MinCoreText  = "v1.19.20"
	minCoreMajor = 1
	minCoreMinor = 19
	minCorePatch = 20
)

// ConnectivityFilter is Windows' connectivity check (NCSI) fake-ip-filter default.
var ConnectivityFilter = []string{"dns.msftncsi.com", "www.msftncsi.com", "www.msftconnecttest.com"}

var (
	simplePattern = regexp.MustCompile(`^(?:\+\.|\.|\*\.)?[a-z0-9_*.-]+$`)
	pySpace       = `[\s\x{1c}-\x{1f}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]`
	versionRE     = regexp.MustCompile(`(?i)^` + pySpace + `*(?:mihomo` + pySpace + `+)?(?:meta` + pySpace + `+)?v?(\p{Nd}+)\.(\p{Nd}+)\.(\p{Nd}+)`)
)

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

// ListenerAddress is listener_address: Mihomo's own DNS listener as a
// nameserver URL, or ("", reason).
func ListenerAddress(listen any) (string, string) {
	s, ok := listen.(string)
	if !ok || pyStrip(s) == "" {
		return "", "no_listen"
	}
	t := pyStrip(s)
	i := strings.LastIndex(t, ":")
	if i < 0 {
		return "", "listen_unparsed"
	}
	host, port := t[:i], t[i+1:]
	if !pyIsDigit(port) {
		return "", "listen_unparsed"
	}
	n, err := pyIntStr(port)
	if err != nil {
		return "", "listen_unparsed"
	}
	pn, ok := n.Int64()
	if !ok || pn <= 0 || pn >= 65536 {
		return "", "listen_unparsed"
	}
	host = pyStrip(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return fmt.Sprintf("udp://127.0.0.1:%d", pn), ""
	}
	info, ok := pyIPAddress(host)
	if !ok || info.unspecified || info.multicast {
		return "", "listen_unparsed"
	}
	if info.v6 {
		return fmt.Sprintf("udp://[%s]:%d", info.text, pn), ""
	}
	return fmt.Sprintf("udp://%s:%d", info.text, pn), ""
}

// nameserverAddr is the scheme, host name and port (0 = none) of a DNS
// server URL; ok is false when it does not parse or the port is invalid.
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
	t := pyStrip(listen)
	i := strings.LastIndex(t, ":")
	h := ""
	if i >= 0 {
		h = t[:i]
	}
	h = strings.Trim(pyStrip(h), "[]")
	return h == "" || h == "0.0.0.0" || h == "::"
}

// pointsToListener is _points_to_listener: true when every given nameserver
// is this Mihomo's own DNS listener.
func pointsToListener(values []any, listen string) bool {
	address, _ := ListenerAddress(listen)
	if address == "" || len(values) == 0 {
		return false
	}
	_, expectedHost, expectedPort, _ := nameserverAddr(address)
	unspecified := isUnspecifiedListen(listen)
	for _, v := range values {
		s, ok := v.(string)
		if !ok || pyStrip(s) == "" {
			return false
		}
		text := pyStrip(s)
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
		if unspecified {
			if host == "localhost" {
				continue
			}
			if info, ok := pyIPAddress(host); ok && info.loopback {
				continue
			}
		}
		return false
	}
	return true
}

type hostsMap struct {
	keys []string
	m    map[string]any
}

func lookupHost(h *hostsMap, domain string) any {
	if v, ok := h.m[domain]; ok {
		return v
	}
	best, bestLen := "", -1
	var found any
	for _, key := range h.keys {
		if key != domain && matchesText(key, domain) && len(key) > bestLen {
			best, bestLen, found = key, len(key), h.m[key]
		}
	}
	_ = best
	if bestLen < 0 {
		return nil
	}
	return found
}

// FinalTarget is final_target: the domain a hosts chain from domain ends on,
// or "" when it ends in IPs.
func FinalTarget(hosts *jsonx.Object, domain string) string {
	h := &hostsMap{m: map[string]any{}}
	for key, v := range hosts.All() {
		k := targetText(key)
		if _, ok := h.m[k]; !ok {
			h.keys = append(h.keys, k)
		}
		h.m[k] = v
	}
	value := lookupHost(h, domain)
	seen := map[string]bool{}
	for value != nil {
		if list, ok := value.([]any); ok {
			if len(list) == 1 {
				if s, ok := list[0].(string); ok && addressOf(s) == "" {
					value = s
					goto next
				}
			}
			return ""
		}
	next:
		s, ok := value.(string)
		if !ok || addressOf(s) != "" {
			return ""
		}
		target := targetText(s)
		if seen[target] {
			return ""
		}
		seen[target] = true
		following := lookupHost(h, target)
		if following == nil {
			return target
		}
		value = following
	}
	return ""
}

// ParseCoreVersion is parse_core_version: (major, minor, patch) of a Mihomo
// version such as `v1.19.33`, or ok=false. The parts are unbounded like
// Python ints.
func ParseCoreVersion(text any) (version [3]*big.Int, ok bool) {
	s, isStr := text.(string)
	if !isStr {
		return version, false
	}
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return version, false
	}
	for i := 0; i < 3; i++ {
		n, err := pyIntStr(m[i+1])
		if err != nil {
			return version, false
		}
		version[i], _ = n.BigInt()
	}
	return version, true
}

// CoreTooOld is core_too_old: true only for a recognised older version.
func CoreTooOld(version any) bool {
	v, ok := ParseCoreVersion(version)
	if !ok {
		return false
	}
	min := [3]int64{minCoreMajor, minCoreMinor, minCorePatch}
	for i := 0; i < 3; i++ {
		if c := v[i].Cmp(big.NewInt(min[i])); c != 0 {
			return c < 0
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
	return fmt.Sprintf("Mihomo 内核 %s 早于 %s，不认识 proxy-server-nameserver-policy，所以没有加入；升级内核后重新推送，映射才对节点生效", version, MinCoreText)
}

func simplePatterns(entries []any) []string {
	var out []string
	for _, e := range entries {
		if s, ok := e.(string); ok && simplePattern.MatchString(pyLower(pyStrip(s))) {
			out = append(out, targetText(s))
		}
	}
	return out
}

func uncheckedEntries(entries []any) bool {
	for _, e := range entries {
		if s, ok := e.(string); ok && !simplePattern.MatchString(pyLower(pyStrip(s))) {
			return true
		}
	}
	return false
}

func covered(patterns []string, domain string) bool {
	for _, p := range patterns {
		if matchesText(p, domain) {
			return true
		}
	}
	return false
}

type policyKeyEntry struct {
	pattern string
	value   any
}

func policyKeys(policy *jsonx.Object) (plain []policyKeyEntry, matchers []string) {
	for key, value := range policy.All() {
		text := pyStrip(key)
		lower := pyLower(text)
		if strings.HasPrefix(lower, "geosite:") || strings.HasPrefix(lower, "rule-set:") {
			matchers = append(matchers, text)
			continue
		}
		for _, part := range strings.Split(text, ",") {
			if pyStrip(part) != "" {
				plain = append(plain, policyKeyEntry{targetText(part), value})
			}
		}
	}
	return
}

func userPolicyFor(plain []policyKeyEntry, domain string) *policyKeyEntry {
	var matches []policyKeyEntry
	for _, p := range plain {
		if matchesText(p.pattern, domain) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	for i := range matches {
		if matches[i].pattern == domain {
			return &matches[i]
		}
	}
	best := 0
	for i := range matches {
		if len(matches[i].pattern) > len(matches[best].pattern) {
			best = i
		}
	}
	return &matches[best]
}

// ApplyNodeDNS is apply_node_dns: it resolves mapped node domains through
// the DNS listener when a merged mapping needs it. merged holds the
// subscription entries merged into main["hosts"]. It returns nil (and leaves
// main untouched) when no merged mapping ends on a domain, otherwise the
// preview report. coreVersion is "" when unknown.
func ApplyNodeDNS(main, merged *jsonx.Object, coreVersion string) *jsonx.Object {
	hosts := main.GetObject("hosts")
	if hosts == nil {
		hosts = jsonx.NewObject()
	}
	type mapping struct{ domain, target string }
	var mapped []mapping
	for domain, value := range merged.All() {
		if _, ok := value.(string); ok {
			if t := FinalTarget(hosts, domain); t != "" {
				mapped = append(mapped, mapping{domain, t})
			}
		}
	}
	if len(mapped) == 0 {
		return nil
	}
	report := jsonx.Obj("mapped", jsonx.Int(len(mapped)), "applied", false, "listener", nil, "policy", []any{},
		"filter", []any{}, "targets_filtered", jsonx.Int(0), "targets_covered", jsonx.Int(0), "fake_ip", nil,
		"skipped", []any{}, "warnings", []any{}, "notes", []any{}, "min_version", MinCoreText)
	warn := func(s string) { report.Set("warnings", append(report.GetList("warnings"), s)) }
	note := func(s string) { report.Set("notes", append(report.GetList("notes"), s)) }
	skip := func(o *jsonx.Object) { report.Set("skipped", append(report.GetList("skipped"), o)) }

	dns := main.GetObject("dns")
	if dns == nil || dns.Value("enable") != true {
		warn(Warnings["dns_disabled"])
		return report
	}
	if dns.Value("use-hosts") == false {
		warn(Warnings["use_hosts_off"])
		return report
	}
	address, reason := ListenerAddress(dns.Value("listen"))
	if address == "" {
		warn(Warnings[reason])
		return report
	}
	report.Set("listener", address)
	if coreVersion != "" && CoreTooOld(coreVersion) {
		warn(oldCoreWarning(coreVersion))
		return report
	}
	if !jsonx.Truthy(dns.Value("proxy-server-nameserver")) {
		warn(Warnings["no_nameserver"])
		return report
	}
	var written *jsonx.Object
	if w := dns.Value(PolicyKey); w != nil {
		o, ok := w.(*jsonx.Object)
		if !ok {
			warn(Warnings["policy_not_mapping"])
			return report
		}
		written = o
	}
	writtenOrEmpty := written
	if writtenOrEmpty == nil {
		writtenOrEmpty = jsonx.NewObject()
	}
	plain, matchers := policyKeys(writtenOrEmpty)

	type chosenItem struct {
		obj    *jsonx.Object
		source string
	}
	chosen := jsonx.NewObject()
	for _, m := range mapped {
		found := userPolicyFor(plain, m.domain)
		if found == nil {
			chosen.Set(m.domain, jsonx.Obj("domain", m.domain, "target", m.target, "source", "configflow"))
			continue
		}
		values, isList := found.value.([]any)
		if !isList {
			values = []any{found.value}
		}
		if pointsToListener(values, jsonx.Str(dns.Value("listen"))) {
			chosen.Set(m.domain, jsonx.Obj("domain", m.domain, "target", m.target, "source", "user"))
		} else {
			skip(jsonx.Obj("domain", m.domain, "target", m.target, "reason", "policy_conflict"))
			var writtenFor string
			if found.pattern == m.domain {
				writtenFor = "你已为 " + m.domain + " 写了 proxy-server-nameserver-policy"
			} else {
				writtenFor = "你写的 proxy-server-nameserver-policy 条目 " + found.pattern + " 覆盖了 " + m.domain
			}
			warn(writtenFor + "，保留你的设置；这条映射对该节点不生效")
		}
	}
	if len(matchers) > 0 {
		note("你写的 proxy-server-nameserver-policy 里有 geosite / rule-set 条目，ConfigFlow 无法检查；Mihomo 按顺序先匹配它们，匹配到的节点域名仍按你的设置解析")
	}

	var additions []string
	var filterValue []any
	hasFilterValue := false
	if dns.Value("enhanced-mode") == "fake-ip" {
		var mode any = "blacklist"
		if jsonx.Truthy(dns.Value("fake-ip-filter-mode")) {
			mode = dns.Value("fake-ip-filter-mode")
		}
		var current any
		if mode == "rule" {
			current = dns.Value("fake-ip-filter")
			if !jsonx.Truthy(current) {
				current = []any{}
			}
		} else if dns.Has("fake-ip-filter") {
			current = dns.Value("fake-ip-filter")
			if current == nil {
				current = []any{}
			}
		} else {
			current = jsonx.ListOf(ConnectivityFilter)
		}
		currentList, ok := current.([]any)
		if !ok {
			warn(Warnings["filter_not_list"])
			return report
		}
		report.Set("fake_ip", mode)
		if mode == "whitelist" {
			patterns := simplePatterns(currentList)
			for _, domain := range chosen.Keys() {
				item := chosen.GetObject(domain)
				if covered(patterns, item.GetString("target")) {
					chosen.Delete(domain)
					skip(jsonx.Obj("domain", domain, "target", item.Value("target"), "reason", "whitelist_covers"))
					warn("fake-ip-filter-mode 为 whitelist，名单覆盖了映射目标 " + item.GetString("target") + "，改走本机 DNS 监听会让 " + domain + " 拿到假地址，所以没有改动这条")
				}
			}
			if chosen.Len() > 0 {
				text := "fake-ip-filter-mode 为 whitelist：只有名单内的域名返回假地址，映射目标不在名单内，无需改动 fake-ip-filter"
				if uncheckedEntries(currentList) {
					text += "；名单里的 geosite / rule-set 条目无法检查"
				}
				note(text)
			}
		} else {
			var targets []string
			seen := map[string]bool{}
			for _, item := range chosen.Values() {
				t := item.(*jsonx.Object).GetString("target")
				if !seen[t] {
					seen[t] = true
					targets = append(targets, t)
				}
			}
			var patterns []string
			if mode != "rule" {
				patterns = simplePatterns(currentList)
			}
			var needed []string
			for _, t := range targets {
				if !covered(patterns, t) {
					needed = append(needed, t)
				}
			}
			report.Set("targets_covered", jsonx.Int(len(targets)-len(needed)))
			additions = Compress(needed)
			report.Set("targets_filtered", jsonx.Int(len(needed)))
			hasFilterValue = true
			if mode == "rule" {
				var rules []any
				for _, entry := range additions {
					if strings.HasPrefix(entry, "+.") {
						rules = append(rules, "DOMAIN-SUFFIX,"+entry[2:]+",real-ip")
					} else {
						rules = append(rules, "DOMAIN,"+entry+",real-ip")
					}
				}
				filterValue = append([]any{}, rules...)
				for _, rule := range currentList {
					if !containsValue(rules, rule) {
						filterValue = append(filterValue, rule)
					}
				}
			} else {
				filterValue = append([]any{}, currentList...)
				for _, entry := range additions {
					if !containsValue(currentList, entry) {
						filterValue = append(filterValue, entry)
					}
				}
			}
		}
	} else {
		report.Set("fake_ip", "not_needed")
	}
	if chosen.Len() == 0 {
		return report
	}
	policy := writtenOrEmpty.Clone()
	for domain, v := range chosen.All() {
		if v.(*jsonx.Object).GetString("source") == "configflow" {
			policy.Set(domain, address)
		}
	}
	if !jsonx.Equal(policy, writtenOrEmpty) {
		dns.Set(PolicyKey, policy)
	}
	if len(additions) > 0 && hasFilterValue {
		dns.Set("fake-ip-filter", filterValue)
	}
	report.Set("policy", append([]any{}, chosen.Values()...))
	report.Set("filter", jsonx.ListOf(additions))
	report.Set("applied", true)
	if _, ok := ParseCoreVersion(coreVersion); !ok {
		note("需要 Mihomo " + MinCoreText + " 或更新：更旧的内核会忽略 proxy-server-nameserver-policy，这些节点照旧连接左侧域名自己的地址")
	}
	return report
}

func containsValue(list []any, v any) bool {
	for _, x := range list {
		if jsonx.Equal(x, v) {
			return true
		}
	}
	return false
}

// StatusLoader loads an Agent's stored status document (the traffic store's
// load_status); nil when there is none or it cannot be read.
type StatusLoader func(agentID string) *jsonx.Object

// ReportedCoreVersion is reported_core_version: the Mihomo version an Agent
// last reported, "" when ConfigFlow does not know it. Only a Mihomo Agent
// with domain discovery enabled counts (a report left from before it was
// turned off may be stale); statusOf is called only for those.
func ReportedCoreVersion(agent *jsonx.Object, statusOf StatusLoader) string {
	if agent == nil {
		return ""
	}
	if st := getOr(agent, "service_type", "mihomo"); st != "mihomo" || !jsonx.Truthy(agent.Value("domain_discovery_enabled")) ||
		!isString(agent.Value("id")) {
		return ""
	}
	if statusOf == nil {
		return ""
	}
	v, _ := statusOf(agent.GetString("id")).Value("mihomo_version").(string)
	return pyStrip(v)
}

// OutdatedAgents is outdated_agents over an agent list: Mihomo Agents of
// profileID (an Agent without profile_id belongs to defaultID) whose
// reported core ignores the policy. statusOf loads an Agent's stored status.
func OutdatedAgents(agents []*jsonx.Object, profileID, defaultID string, statusOf StatusLoader) []any {
	found := []any{}
	for _, agent := range agents {
		pid := defaultID
		if jsonx.Truthy(agent.Value("profile_id")) {
			pid = jsonx.Str(agent.Value("profile_id"))
		}
		if pid != profileID {
			continue
		}
		version := ReportedCoreVersion(agent, statusOf)
		if version != "" && CoreTooOld(version) {
			name := agent.Value("name")
			if !jsonx.Truthy(name) {
				name = agent.Value("id")
			}
			found = append(found, jsonx.Obj("name", jsonx.Str(name), "version", version))
		}
	}
	return found
}
