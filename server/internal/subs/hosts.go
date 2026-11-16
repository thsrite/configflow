package subs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

var hostsLogger = logx.Get("backend.utils.subscription_hosts")

// Limits of the hosts section kept per subscription.
const (
	MaxEntries        = 1000
	MaxAddresses      = 8
	MaxSkippedSamples = 20
	MaxReportItems    = 200
)

// Reasons maps a reason code to the text shown in the preview.
var Reasons = map[string]string{
	"parse_error":        "hosts 段无法解析",
	"not_mapping":        "hosts 不是「域名: 地址」的映射",
	"wildcard":           "通配符条目不合并，只合并节点自己的域名",
	"invalid_domain":     "域名无效",
	"invalid_value":      "取值不是合法的 IP、IP 列表或域名",
	"too_many_addresses": fmt.Sprintf("IP 超过 %d 个", MaxAddresses),
	"over_limit":         fmt.Sprintf("超过每个订阅 %d 条的上限", MaxEntries),
	"not_node_domain":    "不是这个订阅里被使用节点的 server 域名",
	"custom_hosts":       "Mihomo 基础配置已手写这个域名，保留手写值",
	"mosdns_hosts":       "MosDNS 自定义 Hosts 已写这个域名，保留同步值",
	"conflict":           "另一个订阅给了不同的值，按订阅列表顺序采用排在前面的订阅",
	"cycle":              "会和已有 hosts 形成域名映射环",
}

func validLabel(l string) bool {
	if l == "" || len(l) > 63 {
		return false
	}
	for i := 0; i < len(l); i++ {
		c := l[i]
		isAlnum := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
		if isAlnum {
			continue
		}
		if c == '-' && i != 0 && i != len(l)-1 {
			continue
		}
		return false
	}
	return true
}

// domainOf is _domain: a lowercase exact domain with at least two labels, or "".
func domainOf(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	name := strings.TrimRight(pyLower(pyStrip(s)), ".")
	if name == "" || len([]rune(name)) > 253 {
		return ""
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return ""
	}
	allDigits := true
	for _, l := range labels {
		if !validLabel(l) {
			return ""
		}
		if !pyIsDigit(l) {
			allDigits = false
		}
	}
	if allDigits {
		return ""
	}
	return name
}

// addressOf is _address: the canonical text of a usable IP, or "".
func addressOf(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	info, ok := pyIPAddress(pyStrip(s))
	if !ok || info.unspecified || info.multicast {
		return ""
	}
	return info.text
}

// normalizeValue is _normalize_value: (stored value, reason).
func normalizeValue(key string, value any) (any, string) {
	var list []any
	switch v := value.(type) {
	case string:
		list = []any{v}
	case []any:
		list = v
	default:
		return nil, "invalid_value"
	}
	if len(list) == 0 {
		return nil, "invalid_value"
	}
	for _, item := range list {
		if _, ok := item.(string); !ok {
			return nil, "invalid_value"
		}
	}
	if len(list) > MaxAddresses {
		return nil, "too_many_addresses"
	}
	addresses := make([]string, len(list))
	all := true
	for i, item := range list {
		addresses[i] = addressOf(item)
		if addresses[i] == "" {
			all = false
		}
	}
	if all {
		seen := map[string]bool{}
		out := []any{}
		for _, a := range addresses {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
		return out, ""
	}
	if len(list) == 1 {
		if target := domainOf(list[0]); target != "" && target != key {
			return target, ""
		}
	}
	return nil, "invalid_value"
}

// pySplitLines is str.splitlines().
func pySplitLines(s string) []string {
	var lines []string
	start := 0
	rs := []rune(s)
	startR := 0
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch r {
		case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		case '\r':
			lines = append(lines, string(rs[startR:i]))
			if i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			startR = i + 1
			continue
		default:
			continue
		}
		lines = append(lines, string(rs[startR:i]))
		startR = i + 1
	}
	_ = start
	if startR < len(rs) {
		lines = append(lines, string(rs[startR:]))
	}
	return lines
}

// hostsBlock is _hosts_block: the top-level `hosts:` block of a YAML text.
func hostsBlock(text string) (string, bool) {
	// ^(?:hosts|"hosts"|'hosts')[ \t]*:  with re.M
	pos := 0
	for pos <= len(text) {
		line := text[pos:]
		end := strings.IndexByte(line, '\n')
		if end >= 0 {
			line = line[:end]
		}
		if keyLine(line) {
			lines := pySplitLines(text[pos:])
			block := []string{lines[0]}
			for _, l := range lines[1:] {
				if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") || pyStrip(l) == "" || strings.HasPrefix(l, "#") {
					block = append(block, l)
					continue
				}
				break
			}
			return strings.Join(block, "\n"), true
		}
		if end < 0 {
			break
		}
		pos += end + 1
	}
	return "", false
}

func keyLine(line string) bool {
	for _, prefix := range []string{"hosts", `"hosts"`, `'hosts'`} {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			rest = strings.TrimLeft(rest, " \t")
			if strings.HasPrefix(rest, ":") {
				return true
			}
		}
	}
	return false
}

func hostsFailure(reason string) *jsonx.Object {
	return jsonx.Obj("entries", jsonx.NewObject(), "total", jsonx.Int(0), "skipped_count", jsonx.Int(1),
		"skipped", []any{jsonx.Obj("domain", "", "reason", reason)})
}

// ExtractSubscriptionHosts is extract_subscription_hosts: the validated
// `hosts` of a subscription body, or nil when it has none. Keys that are not
// strings are judged by str(key) (ints and floats, bools as "True"/"False")
// or as "" (None, dates, bytes), like Python.
func ExtractSubscriptionHosts(text string) *jsonx.Object {
	if !strings.Contains(text, "hosts") {
		return nil
	}
	block, ok := hostsBlock(text)
	if !ok {
		return nil
	}
	document, err := yamlx.LoadWith(block, yamlx.LoadOptions{KeepNonStringKeys: true})
	if err != nil {
		return hostsFailure("parse_error")
	}
	raw, _ := yamlx.Lookup(document, "hosts")
	if raw == nil {
		return nil
	}
	var pairs []yamlx.Pair
	switch hosts := raw.(type) {
	case *jsonx.Object:
		for key, value := range hosts.All() {
			pairs = append(pairs, yamlx.Pair{Key: key, Value: value})
		}
	case *yamlx.Mapping:
		pairs = hosts.Pairs
	default:
		return hostsFailure("not_mapping")
	}
	entries := jsonx.NewObject()
	skipped := []any{}
	skippedCount := 0
	index := 0
	for _, pair := range pairs {
		key, value := pair.Key, pair.Value
		reason := ""
		name := ""
		switch key.(type) {
		case string, jsonx.Number, bool:
			name = pyLower(pyStrip(jsonx.Str(key)))
		}
		switch {
		case index >= MaxEntries:
			reason = "over_limit"
		case strings.Contains(name, "*") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "+."):
			reason = "wildcard"
		case domainOf(name) == "" || addressOf(name) != "":
			reason = "invalid_domain"
		default:
			name = domainOf(name)
			var normalized any
			normalized, reason = normalizeValue(name, value)
			if reason == "" {
				entries.Set(name, normalized)
			}
		}
		if reason != "" {
			skippedCount++
			if len(skipped) < MaxSkippedSamples {
				sample := name
				if r := []rune(sample); len(r) > 253 {
					sample = string(r[:253])
				}
				skipped = append(skipped, jsonx.Obj("domain", sample, "reason", reason))
			}
		}
		index++
	}
	return jsonx.Obj("entries", entries, "total", jsonx.Int(len(pairs)),
		"skipped_count", jsonx.Int(skippedCount), "skipped", skipped)
}

// HostsDigest is hosts_digest.
func HostsDigest(record *jsonx.Object) string {
	entries := record.GetObject("entries")
	var v any = jsonx.NewObject()
	if entries != nil && entries.Len() > 0 {
		v = entries
	}
	data, _ := jsonx.Encode(v, jsonx.Options{EnsureASCII: true, SortKeys: true})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// DescribeChange is describe_change.
func DescribeChange(previous, current *jsonx.Object) *jsonx.Object {
	oldE, newE := previous.GetObject("entries"), current.GetObject("entries")
	if oldE == nil {
		oldE = jsonx.NewObject()
	}
	if newE == nil {
		newE = jsonx.NewObject()
	}
	added, removed, changed := 0, 0, 0
	for k, v := range newE.All() {
		if o, ok := oldE.Get(k); !ok {
			added++
		} else if !jsonx.Equal(v, o) {
			changed++
		}
	}
	for k := range oldE.All() {
		if !newE.Has(k) {
			removed++
		}
	}
	return jsonx.Obj("added", jsonx.Int(added), "removed", jsonx.Int(removed), "changed", jsonx.Int(changed))
}

// ---------------------------------------------------------------- cache index

// HostsIndex is the stored hosts of a subscription plus the names of nodes
// per hosts domain (subscription_hosts_index without the file-stamp memo,
// which belongs to the cache package).
type HostsIndex struct {
	URL          any
	Entries      *jsonx.Object
	Nodes        map[string][]string
	Total        int
	SkippedCount int
	Skipped      []any
}

func normalizeServer(value any) string {
	if _, ok := value.(string); ok && addressOf(value) == "" {
		return domainOf(value)
	}
	return ""
}

func intOrZero(v any) int {
	if !jsonx.Truthy(v) {
		return 0
	}
	n, err := pyInt(v)
	if err != nil {
		return 0
	}
	i, _ := n.Int64()
	return int(i)
}

// NewHostsIndex builds the index from a loaded subscription cache document
// ({"nodes": [...], "metadata": {...}}); nil when the cache has no hosts.
func NewHostsIndex(cache *jsonx.Object) *HostsIndex {
	metadata := cache.GetObject("metadata")
	record := metadata.GetObject("hosts")
	if record == nil {
		return nil
	}
	entries := record.GetObject("entries")
	if entries == nil {
		return nil
	}
	nodes := map[string][]string{}
	for _, n := range cache.GetList("nodes") {
		node, ok := n.(*jsonx.Object)
		if !ok {
			continue
		}
		server := normalizeServer(node.Value("server"))
		if server != "" && entries.Has(server) {
			nodes[server] = append(nodes[server], jsonx.Str(getOr(node, "name", "")))
		}
	}
	var url any
	if metadata != nil {
		url = metadata.Value("url")
	}
	idx := &HostsIndex{URL: url, Entries: entries, Nodes: nodes,
		Total: intOrZero(record.Value("total")), SkippedCount: intOrZero(record.Value("skipped_count"))}
	if l, ok := record.Value("skipped").([]any); ok {
		idx.Skipped = append([]any(nil), l...)
	}
	return idx
}

// ForURL drops an index recorded for another URL (value['url'] == sub.get('url')).
func (h *HostsIndex) ForURL(sub *jsonx.Object) *HostsIndex {
	if h == nil || jsonx.Equal(h.URL, sub.Value("url")) {
		return h
	}
	return nil
}

// HostsIndexOf returns the index of a subscription (already URL-checked) or nil.
type HostsIndexOf func(sub *jsonx.Object) *HostsIndex

// --------------------------------------------------------------------- merge

// Matches is _matches: Mihomo domain-trie matching of one hosts key.
func Matches(pattern any, domain string) bool {
	p, ok := pattern.(string)
	if !ok {
		return false
	}
	return matchesText(p, domain)
}

func matchesText(pattern, domain string) bool {
	pattern = strings.TrimRight(pyLower(pyStrip(pattern)), ".")
	if strings.HasPrefix(pattern, "+.") {
		base := pattern[2:]
		return domain == base || strings.HasSuffix(domain, "."+base)
	}
	if strings.HasPrefix(pattern, ".") {
		return strings.HasSuffix(domain, pattern)
	}
	if !strings.Contains(pattern, "*") {
		return pattern == domain
	}
	left, right := strings.Split(pattern, "."), strings.Split(domain, ".")
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != "*" && left[i] != right[i] {
			return false
		}
	}
	return true
}

type providerSource struct {
	kind, id string
	query    map[string][]string
}

// splitURL returns path and query of urllib.parse.urlsplit(url).
func splitURL(url string) (path, query string) {
	url = strings.TrimLeft(url, whatwgTrim)
	url = strings.NewReplacer("\t", "", "\r", "", "\n", "").Replace(url)
	if i := strings.IndexByte(url, ':'); i > 0 {
		ok := isASCIILetter(url[0])
		for j := 0; j < i && ok; j++ {
			c := url[j]
			ok = isASCIILetter(c) || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'
		}
		if ok {
			url = url[i+1:]
		}
	}
	if strings.HasPrefix(url, "//") {
		rest := url[2:]
		if j := strings.IndexAny(rest, "/?#"); j >= 0 {
			url = rest[j:]
		} else {
			url = ""
		}
	}
	if i := strings.IndexByte(url, '#'); i >= 0 {
		url = url[:i]
	}
	if i := strings.IndexByte(url, '?'); i >= 0 {
		return url[:i], url[i+1:]
	}
	return url, ""
}

const whatwgTrim = "\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\x0c\r\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f "

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// listField is `config.get(key, [])` for a list of items.
func listField(o *jsonx.Object, key string) []any {
	l, _ := o.Value(key).([]any)
	return l
}

// providerSources is _provider_sources: every provider ConfigFlow generated in main.
func providerSources(main, config *jsonx.Object) []providerSource {
	subscriptions, aggregations := map[string]bool{}, map[string]bool{}
	for _, item := range listField(config, "subscriptions") {
		if o, ok := item.(*jsonx.Object); ok {
			if id, ok := o.Value("id").(string); ok {
				subscriptions[id] = true
			}
		}
	}
	for _, item := range listField(config, "subscription_aggregations") {
		if o, ok := item.(*jsonx.Object); ok {
			if id, ok := o.Value("id").(string); ok {
				aggregations[id] = true
			}
		}
	}
	var out []providerSource
	providers := main.GetObject("proxy-providers")
	if providers == nil {
		return nil
	}
	for _, definition := range providers.Values() {
		def, ok := definition.(*jsonx.Object)
		if !ok {
			continue
		}
		url, ok := def.Value("url").(string)
		if !ok {
			continue
		}
		rawPath, rawQuery := splitURL(url)
		path := weburl.Unquote(rawPath)
		_, query := ParseQS(rawQuery)
		if strings.Contains(path, "/subscriptions/") {
			ident := path[strings.LastIndex(path, "/subscriptions/")+len("/subscriptions/"):]
			ident, _, _ = strings.Cut(ident, "/")
			if subscriptions[ident] {
				out = append(out, providerSource{"subscription", ident, query})
			}
		} else if strings.Contains(path, "/aggregations/") {
			ident := path[strings.LastIndex(path, "/aggregations/")+len("/aggregations/"):]
			ident, _, _ = strings.Cut(ident, "/")
			if aggregations[ident] {
				out = append(out, providerSource{"aggregation", ident, query})
			}
		}
	}
	return out
}

type nameCheck func(name string) bool

func firstValue(query map[string][]string, key string) string {
	if v := query[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// queryFilter is _query_filter: nil when the query carries no usable filter.
func queryFilter(query map[string][]string) nameCheck {
	include, exclude := firstValue(query, "regex"), firstValue(query, "exclude_regex")
	if include == "" && exclude == "" {
		return nil
	}
	// A pattern only Python can run (isUnsupported) is not applied (domain
	// decision); a pattern Python rejects drops the whole filter, as there.
	var keep, drop Matcher
	if pyStrip(include) != "" {
		re, err := compileRegex(include)
		if err != nil && !isUnsupported(err) {
			return nil
		}
		if err == nil {
			keep = re
		}
	}
	if pyStrip(exclude) != "" {
		re, err := compileRegex(exclude)
		if err != nil && !isUnsupported(err) {
			return nil
		}
		if err == nil {
			drop = re
		}
	}
	return func(name string) bool {
		return (keep == nil || keep.Search(name)) && (drop == nil || !drop.Search(name))
	}
}

// InlineProxy pairs a delivered node with its converted proxy (which may be nil).
type InlineProxy struct {
	Node  *jsonx.Object
	Proxy *jsonx.Object
}

type usedDomains map[string]map[string]bool

func objectsByID(items []any, enabledOnly bool) (map[string]*jsonx.Object, []string) {
	m := map[string]*jsonx.Object{}
	var order []string
	for _, item := range items {
		o, ok := item.(*jsonx.Object)
		if !ok {
			continue
		}
		id, ok := o.Value("id").(string)
		if !ok || id == "" {
			continue
		}
		if enabledOnly && !jsonx.Truthy(getOr(o, "enabled", true)) {
			continue
		}
		if _, dup := m[id]; !dup {
			order = append(order, id)
		}
		m[id] = o
	}
	return m, order
}

// computeUsedDomains is _used_domains.
func computeUsedDomains(main, config *jsonx.Object, inline []InlineProxy, indexOf HostsIndexOf) (usedDomains, map[string]*jsonx.Object, func(string) *HostsIndex) {
	subscriptions, _ := objectsByID(listField(config, "subscriptions"), true)
	used := usedDomains{}
	indexes := map[string]*HostsIndex{}
	loaded := map[string]bool{}
	index := func(subID string) *HostsIndex {
		if !loaded[subID] {
			loaded[subID] = true
			if sub, ok := subscriptions[subID]; ok {
				indexes[subID] = indexOf(sub)
			}
		}
		return indexes[subID]
	}
	add := func(subID string, domains []string) {
		if len(domains) == 0 {
			return
		}
		if used[subID] == nil {
			used[subID] = map[string]bool{}
		}
		for _, d := range domains {
			used[subID][d] = true
		}
	}
	fromSubscription := func(subID string, nameFilters ...nameCheck) {
		idx := index(subID)
		if idx == nil {
			return
		}
		var checks []nameCheck
		if re := CompileSubscriptionFilter(subscriptions[subID]); re != nil {
			checks = append(checks, re.Search)
		}
		checks = append(checks, nameFilters...)
		if used[subID] == nil {
			used[subID] = map[string]bool{}
		}
		var domains []string
		for domain, names := range idx.Nodes {
			for _, name := range names {
				pass := true
				for _, c := range checks {
					if !c(name) {
						pass = false
						break
					}
				}
				if pass {
					domains = append(domains, domain)
					break
				}
			}
		}
		add(subID, domains)
	}
	nodes := map[string]*jsonx.Object{}
	for _, item := range listField(config, "nodes") {
		if o, ok := item.(*jsonx.Object); ok {
			if id, ok := o.Value("id").(string); ok {
				nodes[id] = o
			}
		}
	}
	fromNode := func(node, proxy *jsonx.Object) {
		subID, _ := node.Value("subscription_id").(string)
		var serverValue any
		if proxy != nil && proxy.Has("server") {
			serverValue = proxy.Value("server")
		} else {
			serverValue = node.Value("server")
		}
		server := normalizeServer(serverValue)
		if _, ok := subscriptions[subID]; ok && server != "" {
			if used[subID] == nil {
				used[subID] = map[string]bool{}
			}
			used[subID][server] = true
		}
	}
	aggregations := listField(config, "subscription_aggregations")
	for _, src := range providerSources(main, config) {
		queryCheck := queryFilter(src.query)
		var extra []nameCheck
		if queryCheck != nil {
			extra = append(extra, queryCheck)
		}
		if src.kind == "subscription" {
			if _, ok := subscriptions[src.id]; ok {
				fromSubscription(src.id, extra...)
			}
			continue
		}
		var aggregation *jsonx.Object
		for _, item := range aggregations {
			if o, ok := item.(*jsonx.Object); ok && o.Value("id") == any(src.id) {
				aggregation = o
				break
			}
		}
		if aggregation == nil || !jsonx.Truthy(getOr(aggregation, "enabled", true)) {
			continue
		}
		pattern := ""
		if p := aggregation.Value("regex_filter"); jsonx.Truthy(p) {
			s, _ := p.(string)
			pattern = pyStrip(s)
		}
		var aggRegex Matcher
		if pattern != "" {
			re, err := compileRegex(pattern)
			if err != nil && !isUnsupported(err) {
				continue // Python: re.error skips the aggregation
			}
			if err == nil {
				aggRegex = re // an unsupported pattern is not applied
			}
		}
		var checks []nameCheck
		if aggRegex != nil {
			checks = append(checks, aggRegex.Search)
		}
		if queryCheck != nil {
			checks = append(checks, queryCheck)
		}
		for _, s := range listField(aggregation, "subscriptions") {
			if id, ok := s.(string); ok {
				if _, ok := subscriptions[id]; ok {
					fromSubscription(id, checks...)
				}
			}
		}
		for _, n := range listField(aggregation, "nodes") {
			id, _ := n.(string)
			node := nodes[id]
			if node == nil || !jsonx.Truthy(getOr(node, "enabled", true)) {
				continue
			}
			pass := true
			for _, c := range checks {
				if !c(jsonx.Str(getOr(node, "name", ""))) {
					pass = false
					break
				}
			}
			if pass {
				fromNode(node, nil)
			}
		}
	}
	for _, ip := range inline {
		fromNode(ip.Node, ip.Proxy)
	}
	return used, subscriptions, index
}

func targetText(s string) string { return strings.TrimRight(pyLower(pyStrip(s)), ".") }

// createsCycle is _creates_cycle: Mihomo refuses a configuration whose
// domain mappings loop; wildcard keys count as well.
func createsCycle(hosts *jsonx.Object, key string, value any) bool {
	v, ok := value.(string)
	if !ok {
		return false
	}
	pending := []string{v}
	seen := map[string]bool{}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current == key {
			return true
		}
		if seen[current] || len(seen) > 256 {
			continue
		}
		seen[current] = true
		for pattern, target := range hosts.All() {
			t, isStr := target.(string)
			if matchesText(pattern, current) && isStr && addressOf(t) == "" {
				t = targetText(t)
				if t == key {
					return true
				}
				pending = append(pending, t)
			}
		}
	}
	return false
}

func emitValue(value any) any {
	if list, ok := value.([]any); ok {
		if len(list) == 1 {
			return list[0]
		}
		return append([]any(nil), list...)
	}
	return value
}

// MergeSubscriptionHosts is merge_subscription_hosts: it merges permitted
// subscription hosts into mihomoConfig['hosts'] and returns the report.
// mergedOut, when non-nil, receives the merged {domain: value}.
func MergeSubscriptionHosts(mihomoConfig, config *jsonx.Object, userKeys, mosdnsKeys []string,
	inline []InlineProxy, mergedOut *jsonx.Object, indexOf HostsIndexOf) *jsonx.Object {
	used, _, index := computeUsedDomains(mihomoConfig, config, inline, indexOf)
	hosts := jsonx.NewObject()
	if h, ok := mihomoConfig.Value("hosts").(*jsonx.Object); ok {
		hosts = h.Clone()
	}
	items := []any{}
	merged := jsonx.NewObject()
	ignored := 0
	perSubscription := []any{}
	owners := map[string]string{}

	note := func(entry *jsonx.Object) {
		if len(items) < MaxReportItems {
			items = append(items, entry)
		}
	}
	extend := func(base *jsonx.Object, pairs ...any) *jsonx.Object {
		out := base.Clone()
		for i := 0; i < len(pairs); i += 2 {
			out.Set(pairs[i].(string), pairs[i+1])
		}
		return out
	}

	for _, s := range listField(config, "subscriptions") {
		sub, ok := s.(*jsonx.Object)
		if !ok {
			continue
		}
		subID, _ := sub.Value("id").(string)
		domains, inUsed := used[subID]
		if !inUsed {
			continue
		}
		idx := index(subID)
		if idx == nil {
			continue
		}
		var name any = sub.Value("name")
		if !jsonx.Truthy(name) {
			name = subID
		}
		nameText := jsonx.Str(name)
		accepted := 0
		for domain, value := range idx.Entries.All() {
			reason := ""
			_, owned := owners[domain]
			switch {
			case !domains[domain]:
				reason = "not_node_domain"
			case anyMatches(userKeys, domain):
				reason = "custom_hosts"
			case anyMatches(mosdnsKeys, domain):
				reason = "mosdns_hosts"
			case owned:
				if m, _ := merged.Get(domain); !jsonx.Equal(m, value) {
					reason = "conflict"
				}
			case createsCycle(hosts, domain, value):
				reason = "cycle"
			}
			entry := jsonx.Obj("subscription", name, "domain", domain, "value", emitValue(value))
			if reason != "" {
				ignored++
				detail := Reasons[reason]
				if reason == "conflict" {
					detail = "订阅「" + owners[domain] + "」给了不同的值，按订阅列表顺序采用「" + owners[domain] + "」"
				}
				note(extend(entry, "status", "ignored", "reason", reason, "detail", detail))
				continue
			}
			accepted++
			if !owned {
				owners[domain] = nameText
				merged.Set(domain, value)
				hosts.Set(domain, emitValue(value))
				kind := "address"
				if _, isStr := value.(string); isStr {
					kind = "domain"
				}
				note(extend(entry, "status", "merged", "kind", kind))
			}
		}
		for _, sm := range idx.Skipped {
			sample, _ := sm.(*jsonx.Object)
			reasonValue := sample.Value("reason")
			detail := "条目无效"
			if r, ok := reasonValue.(string); ok {
				if d, ok := Reasons[r]; ok {
					detail = d
				}
			}
			note(jsonx.Obj("subscription", name, "domain", getOr(sample, "domain", ""), "status", "ignored",
				"reason", reasonValue, "detail", detail))
		}
		ignored += idx.SkippedCount
		perSubscription = append(perSubscription, jsonx.Obj("id", subID, "name", name,
			"total", jsonx.Int(idx.Total), "merged", jsonx.Int(accepted),
			"ignored", jsonx.Int(idx.Entries.Len()-accepted+idx.SkippedCount)))
	}
	if merged.Len() > 0 {
		mihomoConfig.Set("hosts", hosts)
	}
	if mergedOut != nil {
		mergedOut.Update(merged)
	}
	domainMapped := 0
	for _, v := range merged.Values() {
		if _, ok := v.(string); ok {
			domainMapped++
		}
	}
	return jsonx.Obj("merged", jsonx.Int(merged.Len()), "ignored", jsonx.Int(ignored),
		"domain_mapped", jsonx.Int(domainMapped), "subscriptions", perSubscription, "items", items)
}

func anyMatches(keys []string, domain string) bool {
	for _, k := range keys {
		if matchesText(k, domain) {
			return true
		}
	}
	return false
}

var (
	loggedMu    sync.Mutex
	logged      = map[string]string{}
	loggedOrder []string
)

// LogReport is log_report: each distinct outcome once per profile.
func LogReport(profileID string, report *jsonx.Object) {
	if report == nil {
		return
	}
	merged, _ := report.GetNumber("merged")
	ignored, _ := report.GetNumber("ignored")
	if merged.String() == "0" && ignored.String() == "0" {
		return
	}
	summary := jsonx.Obj("merged", report.Value("merged"), "ignored", report.Value("ignored"),
		"domain_mapped", report.Value("domain_mapped"))
	items := report.GetList("items")
	reasons := jsonx.NewObject()
	var reasonOrder []string
	counts := map[string]int{}
	for _, it := range items {
		item := it.(*jsonx.Object)
		if item.GetString("status") == "ignored" {
			r := jsonx.Str(item.Value("reason"))
			if _, ok := counts[r]; !ok {
				reasonOrder = append(reasonOrder, r)
			}
			counts[r]++
		}
	}
	_ = reasons
	data, _ := jsonx.Encode([]any{summary, items}, jsonx.Options{SortKeys: true})
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	loggedMu.Lock()
	if logged[profileID] == digest {
		loggedMu.Unlock()
		return
	}
	if _, ok := logged[profileID]; !ok {
		loggedOrder = append(loggedOrder, profileID)
	}
	logged[profileID] = digest
	for len(logged) > 64 {
		delete(logged, loggedOrder[0])
		loggedOrder = loggedOrder[1:]
	}
	loggedMu.Unlock()
	var parts []string
	for _, r := range reasonOrder {
		text := r
		if d, ok := Reasons[r]; ok {
			text = d
		}
		parts = append(parts, fmt.Sprintf("%s %d 条", text, counts[r]))
	}
	detail := strings.Join(parts, "；")
	suffix := ""
	if detail != "" {
		suffix = "（" + logx.LogText(detail, 120) + "）"
	}
	hostsLogger.Infof("订阅 hosts：合并 %d 条（其中域名映射 %d 条），忽略 %d 条%s",
		intOrZero(report.Value("merged")), intOrZero(report.Value("domain_mapped")), intOrZero(report.Value("ignored")), suffix)
}

var _ = errors.New
