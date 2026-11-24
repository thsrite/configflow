package rules

import (
	"configflow/internal/textx"
	"iter"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/rulesetfmt"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// Rule-line parsing and the indexed matcher (backend/utils/rule_matcher.py)
// that domain discovery and the rule IP presence checks run over rule sets
// with hundreds of thousands of lines.

// Logger is the part of the application logger the matcher writes to
// (*logx.Logger satisfies it; tests may pass their own).
type Logger interface {
	Debugf(format string, args ...any)
	Warningf(format string, args ...any)
	Errorf(format string, args ...any)
}

var matcherLogger Logger = logx.Get("backend.utils.rule_matcher")

var (
	strictHost = regexp.MustCompile(`^(?:[a-zA-Z0-9](?:[a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9](?:[a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`)
	dnsName    = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,63}(?:\.[a-zA-Z0-9_\-]{1,63})*$`)
)

// IsValidDomain reports a valid host name. allowDNSLabels admits
// underscores and leading/trailing hyphens in labels (rule tests); domain
// discovery keeps the strict host name form.
func IsValidDomain(query string, allowDNSLabels bool) bool {
	if query == "" || len(query) > 253 {
		return false
	}
	// A malformed dotted IP must not re-enter discovery as a host name;
	// numeric labels such as 3com stay valid.
	if strings.Contains(query, ".") && !slices.ContainsFunc(strings.Split(query, "."), func(p string) bool { return !textx.ASCIIDigits(p) }) {
		return false
	}
	if allowDNSLabels {
		return dnsName.MatchString(query)
	}
	return strictHost.MatchString(query)
}

// NormalizeMatchQuery ignores one DNS root dot without turning a dotted IP
// input into an address.
func NormalizeMatchQuery(query string) string {
	if trimmed, ok := strings.CutSuffix(query, "."); ok && !IsValidIP(trimmed) {
		return trimmed
	}
	return query
}

// ParseRuleLine reads the rule type and value of one line in the Clash,
// MosDNS or plain list dialects; ok is false for comments and blank lines.
func ParseRuleLine(line string) (ruleType, ruleValue string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "//") {
		return "", "", false
	}
	if rest, isItem := strings.CutPrefix(line, "-"); isItem { // a YAML list item
		if line = strings.TrimSpace(rest); line == "" {
			return "", "", false
		}
	}
	if before, _, comment := strings.Cut(line, "#"); comment {
		if line = strings.TrimSpace(before); line == "" {
			return "", "", false
		}
	}
	line = strings.Trim(line, `'"`)

	// 1. Clash/YAML: TYPE,value[,no-resolve]. no-resolve is a parameter; kept
	// in the value it would break IP matching and duplicate detection.
	if head, value, found := strings.Cut(line, ","); found {
		value = strings.TrimSpace(value)
		if strings.HasSuffix(strings.ToLower(value), ",no-resolve") {
			value = strings.TrimSpace(value[:len(value)-len(",no-resolve")])
		}
		return strings.ToUpper(strings.TrimSpace(head)), value, true
	}
	// 2. MosDNS: domain: / full: / keyword: / regexp: / ip:
	if head, value, found := strings.Cut(line, ":"); found {
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(head)) {
		case "payload": // the header line of a YAML rule set
			if value == "" {
				return "", "", false
			}
		case "domain":
			return "DOMAIN-SUFFIX", value, true
		case "full":
			return "DOMAIN", value, true
		case "keyword":
			return "DOMAIN-KEYWORD", value, true
		case "regexp", "regex":
			return "REGEX", value, true
		case "ip":
			return "IP", value, true
		}
	}
	// 3. Lists: +.example.com / *.example.com / .example.com
	switch {
	case strings.HasPrefix(line, "+."):
		return "DOMAIN-SUFFIX", strings.TrimSpace(line[2:]), true
	case strings.HasPrefix(line, "*."):
		return "DIRECT-SUBDOMAIN", strings.TrimSpace(line[2:]), true
	case strings.HasPrefix(line, ".") && !strings.HasPrefix(line, ".."):
		return "SUBDOMAIN-ONLY", strings.TrimSpace(line[1:]), true
	}
	// 4. A bare IP or CIDR, 5. otherwise an exact domain.
	if IsValidIP(line) {
		return "IP", line, true
	}
	if _, ok := parseNetwork(line); ok {
		return "IP-CIDR", line, true
	}
	return "DOMAIN", line, true
}

// MatchDomain matches a domain against one domain rule. DOMAIN-REGEX ignores
// case like Mihomo; REGEX/REGEXP (MosDNS) run on the lowercased pattern.
func MatchDomain(domain, ruleType, ruleValue string) bool {
	domain = strings.ToLower(domain)
	if ruleType == "DOMAIN-REGEX" {
		re, err := CompilePyRegex(ruleValue, true)
		return err == nil && re.Search(domain)
	}
	ruleValue = strings.ToLower(ruleValue)
	switch ruleType {
	case "DOMAIN":
		return domain == ruleValue
	case "DOMAIN-SUFFIX":
		return domain == ruleValue || strings.HasSuffix(domain, "."+ruleValue)
	case "DOMAIN-KEYWORD":
		return strings.Contains(domain, ruleValue)
	case "SUBDOMAIN-ONLY":
		return ruleValue != "" && strings.HasSuffix(domain, "."+ruleValue)
	case "DIRECT-SUBDOMAIN":
		prefix, found := strings.CutSuffix(domain, "."+ruleValue)
		return ruleValue != "" && found && prefix != "" && !strings.Contains(prefix, ".")
	case "REGEX", "REGEXP":
		re, err := CompilePyRegex(ruleValue, false)
		return err == nil && re.Search(domain)
	}
	return false
}

func matchAddr(ip netip.Addr, ruleType, ruleValue string) bool {
	switch ruleType {
	case "IP-CIDR", "IP-CIDR6":
		network, ok := parseNetwork(ruleValue)
		return ok && network.Contains(ip.WithZone(""))
	case "IP":
		other, ok := parseAddr(ruleValue)
		return ok && other == ip
	}
	return false
}

// MatchQuery classifies the query as IP or domain, then matches one rule.
func MatchQuery(query, ruleType, ruleValue string) bool {
	query = NormalizeMatchQuery(query)
	if addr, ok := parseAddr(query); ok {
		return matchAddr(addr, ruleType, ruleValue)
	}
	return IsValidDomain(query, true) && MatchDomain(query, ruleType, ruleValue)
}

// rulesetLine is one decoded provider entry and its original display line.
type rulesetLine struct{ value, source string }

// rulesetLines yields the entries of a "text" or "yaml" provider body.
func rulesetLines(content, format string) iter.Seq[rulesetLine] {
	return func(yield func(rulesetLine) bool) {
		switch format {
		case "text":
			first := true
			for raw := range lines(content) {
				value := raw
				if first {
					value = strings.TrimPrefix(raw, "\ufeff")
				}
				first = false
				if !yield(rulesetLine{strings.TrimSpace(value), strings.TrimSpace(raw)}) {
					return
				}
			}
		case "yaml":
			entries, err := yamlPayloadLines(content)
			// Lookup refuses the whole source; it never retries it as text.
			if err != nil {
				return
			}
			for _, entry := range entries {
				if !yield(entry) {
					return
				}
			}
		}
	}
}

// yamlPayloadLines reads the single top-level `payload:` sequence of
// one-line string scalars. Anything else (not one mapping document, a
// missing or repeated payload key, other entries) yields nothing. A block
// entry is shown as its source line, a flow entry as its value.
func yamlPayloadLines(content string) ([]rulesetLine, error) {
	root, err := yamlx.Parse([]byte(content))
	if err != nil {
		return nil, err
	}
	if root.Kind != yaml.MappingNode {
		return nil, nil
	}
	var payload *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Kind == yaml.ScalarNode && root.Content[i].Value == "payload" {
			if payload != nil {
				return nil, nil
			}
			payload = root.Content[i+1]
		}
	}
	if payload == nil || payload.Kind != yaml.SequenceNode {
		return nil, nil
	}
	for _, node := range payload.Content {
		if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!str" || strings.ContainsAny(node.Value, "\n\r") {
			return nil, nil
		}
	}
	sourceLines := strings.Split(content, "\n")
	result := make([]rulesetLine, 0, len(payload.Content))
	for _, node := range payload.Content {
		source := node.Value
		if payload.Style&yaml.FlowStyle == 0 && node.Line >= 1 && node.Line <= len(sourceLines) {
			source = strings.TrimSpace(sourceLines[node.Line-1])
		}
		result = append(result, rulesetLine{strings.TrimSpace(node.Value), source})
	}
	return result, nil
}

// parseRulesetLine interprets a provider entry by the rule set's Mihomo
// behavior. ParseRuleLine's cross-dialect fallbacks must not create routing
// hits here.
func parseRulesetLine(value, behavior string) (kind, payload string, ok bool) {
	if value == "" || strings.HasPrefix(value, "#") || strings.HasPrefix(value, "//") || strings.HasPrefix(value, "!") {
		return "", "", false
	}
	switch behavior {
	case "ipcidr":
		return "IP-CIDR", value, CIDRError(value) == ""
	case "domain":
		return domainPattern(value)
	case "classical":
	default:
		return "", "", false
	}
	if !strings.Contains(value, ",") && IsRuleType(strings.ToUpper(value)) {
		return "", "", false
	}
	value, err := ClassicalLine(value)
	if err != nil || ClassicalLineError(value) != "" {
		return "", "", false
	}
	head, rest, found := strings.Cut(value, ",")
	kind = strings.ToUpper(strings.TrimSpace(head))
	switch {
	case !found:
		return "", "", false
	case kind == "DOMAIN-REGEX":
		return kind, strings.TrimSpace(rest), true
	case kind == "DOMAIN", kind == "DOMAIN-SUFFIX", kind == "DOMAIN-KEYWORD", kind == "IP-CIDR", kind == "IP-CIDR6":
		first, _, _ := strings.Cut(rest, ",")
		return kind, strings.TrimSpace(first), true
	}
	return "", "", false
}

// domainPattern reads one entry of a domain-behavior rule set; wildcards
// other than a leading "*." become a regex.
func domainPattern(value string) (kind, payload string, ok bool) {
	if !ValidPattern(value) {
		return "", "", false
	}
	value = strings.ToLower(value)
	if strings.Contains(value, "*") && !(strings.HasPrefix(value, "*.") && !strings.Contains(value[2:], "*")) {
		prefix := ""
		if rest, found := strings.CutPrefix(value, "+."); found {
			prefix, value = `(?:[^.]+\.)*`, rest
		} else if rest, found := strings.CutPrefix(value, "."); found {
			prefix, value = `(?:[^.]+\.)+`, rest
		}
		parts := strings.Split(value, ".")
		for i, part := range parts {
			if part == "*" {
				parts[i] = `[^.]+`
			} else {
				parts[i] = regexp.QuoteMeta(part)
			}
		}
		return "DOMAIN-REGEX", "^" + prefix + strings.Join(parts, `\.`) + "$", true
	}
	for _, p := range []struct{ prefix, kind string }{{"+.", "DOMAIN-SUFFIX"}, {"*.", "DIRECT-SUBDOMAIN"}, {".", "SUBDOMAIN-ONLY"}} {
		if rest, found := strings.CutPrefix(value, p.prefix); found {
			return p.kind, rest, true
		}
	}
	return "DOMAIN", value, true
}

// ruleIndex holds the entries of one rule set by kind. Every map stores the
// position of the entry's source line; entries keep their source order, so
// skipping duplicates and unsupported rules cannot change the earliest hit.
type ruleIndex struct {
	exact, suffix, subdomain, directSubdomain map[string]int
	patterns                                  []indexedPattern // keywords and regexes, in order
	ips                                       map[netip.Addr]int
	networks                                  map[netip.Prefix]int
	prefixLengths                             [2][]int // present prefix lengths per family
	lines                                     []string
}

type indexedPattern struct {
	position int
	keyword  string
	regex    *Regex
}

// ruleEntry is one parsed provider entry and its display line.
type ruleEntry struct{ kind, value, line string }

func newRuleIndex(entries iter.Seq[ruleEntry]) *ruleIndex {
	index := &ruleIndex{
		exact: map[string]int{}, suffix: map[string]int{}, subdomain: map[string]int{}, directSubdomain: map[string]int{},
		ips: map[netip.Addr]int{}, networks: map[netip.Prefix]int{},
	}
	add := func(line string) int {
		index.lines = append(index.lines, line)
		return len(index.lines) - 1
	}
	addFirst := func(target map[string]int, key, line string) {
		if _, seen := target[key]; !seen {
			target[key] = add(line)
		}
	}
	for entry := range entries {
		kind, value, line := entry.kind, entry.value, entry.line
		switch kind {
		case "DOMAIN":
			addFirst(index.exact, strings.ToLower(value), line)
		case "DOMAIN-SUFFIX":
			addFirst(index.suffix, strings.ToLower(value), line)
		case "SUBDOMAIN-ONLY":
			addFirst(index.subdomain, strings.ToLower(value), line)
		case "DIRECT-SUBDOMAIN":
			addFirst(index.directSubdomain, strings.ToLower(value), line)
		case "DOMAIN-KEYWORD":
			index.patterns = append(index.patterns, indexedPattern{position: add(line), keyword: strings.ToLower(value)})
		case "REGEX", "REGEXP", "DOMAIN-REGEX":
			// MosDNS regexes have always run lowercased; DOMAIN-REGEX ignores
			// case without rewriting escapes, like Mihomo.
			var re *Regex
			var err error
			if kind == "DOMAIN-REGEX" {
				re, err = CompilePyRegex(value, true)
			} else {
				re, err = CompilePyRegex(strings.ToLower(value), false)
			}
			if err == nil {
				index.patterns = append(index.patterns, indexedPattern{position: add(line), regex: re})
			}
		case "IP":
			if ip, ok := parseAddr(value); ok {
				if _, seen := index.ips[ip]; !seen {
					index.ips[ip] = add(line)
				}
			}
		case "IP-CIDR", "IP-CIDR6":
			network, ok := parseNetwork(value)
			if !ok {
				continue
			}
			if _, seen := index.networks[network]; !seen {
				index.networks[network] = add(line)
				family := &index.prefixLengths[family(network.Addr())]
				if !slices.Contains(*family, network.Bits()) {
					*family = append(*family, network.Bits())
				}
			}
		}
	}
	return index
}

// family is 0 for IPv4, 1 for IPv6.
func family(addr netip.Addr) int {
	if addr.Is4() {
		return 0
	}
	return 1
}

// earliest keeps the smaller of two line positions (-1 = none).
func earliest(best int, candidate int, found bool) int {
	if found && (best < 0 || candidate < best) {
		return candidate
	}
	return best
}

func (index *ruleIndex) line(position int) (string, bool) {
	if position < 0 {
		return "", false
	}
	return index.lines[position], true
}

// matchDomain returns the earliest line hitting a lowercase domain.
func (index *ruleIndex) matchDomain(domain string) (string, bool) {
	position, found := index.exact[domain]
	best := earliest(-1, position, found)
	for suffix, sub := domain, false; suffix != ""; sub = true {
		position, found = index.suffix[suffix]
		best = earliest(best, position, found)
		if sub {
			position, found = index.subdomain[suffix]
			best = earliest(best, position, found)
		}
		_, rest, more := strings.Cut(suffix, ".")
		if !more {
			break
		}
		suffix = rest
	}
	if _, parent, more := strings.Cut(domain, "."); more {
		position, found = index.directSubdomain[parent]
		best = earliest(best, position, found)
	}
	for _, p := range index.patterns {
		if best >= 0 && p.position >= best {
			break
		}
		if p.regex == nil && strings.Contains(domain, p.keyword) || p.regex != nil && p.regex.Search(domain) {
			best = p.position
			break
		}
	}
	return index.line(best)
}

// matchIP returns the earliest line hitting an address: one lookup per
// prefix length present (at most 33 for IPv4, 129 for IPv6).
func (index *ruleIndex) matchIP(ip netip.Addr) (string, bool) {
	position, found := index.ips[ip]
	best := earliest(-1, position, found)
	bare := ip.WithZone("")
	for _, bits := range index.prefixLengths[family(ip)] {
		network, _ := bare.Prefix(bits)
		position, found = index.networks[network]
		best = earliest(best, position, found)
	}
	return index.line(best)
}

// ContentLoader returns the text of a rule set: the reference item and its
// library entry (nil when unbound). An error is logged and treated as empty.
type ContentLoader func(item *model.RuleConfig, library *model.RuleLibraryItem) (string, error)

// SourceKey opts a loader into index reuse across references that share one
// library entry; ok=false keeps a per-item index.
type SourceKey func(item *model.RuleConfig, library *model.RuleLibraryItem) (key string, ok bool)

type indexSlot struct {
	source   string // SourceKey's key, or "" with the item position
	position int
	behavior string
	format   string
}

// Matcher matches a domain or IP against rule_configs in order and
// returns the first hit. Each rule set is loaded and indexed on first use;
// domains are looked up by label and IPs by prefix length.
type Matcher struct {
	items    []matcherItem
	library  map[string]*model.RuleLibraryItem
	formats  map[int]string
	load     ContentLoader
	indexKey SourceKey
	logger   Logger

	mu      sync.RWMutex
	indexes map[indexSlot]*ruleIndex
}

type matcherItem struct {
	position int // 1-based over all rules, disabled ones included (the rule list's row numbers)
	item     *model.RuleConfig
}

// NewMatcher builds a matcher over rule_configs and rule_library
// (lists of objects). indexKey and logger may be nil.
func NewMatcher(ruleConfigs []model.RuleConfig, ruleLibrary []model.RuleLibraryItem, load ContentLoader, indexKey SourceKey, logger Logger) *Matcher {
	if logger == nil {
		logger = matcherLogger
	}
	m := &Matcher{library: map[string]*model.RuleLibraryItem{}, formats: map[int]string{}, load: load,
		indexKey: indexKey, logger: logger, indexes: map[indexSlot]*ruleIndex{}}
	for i := range ruleConfigs {
		item := &ruleConfigs[i]
		if ruleEnabled(item) {
			m.items = append(m.items, matcherItem{i + 1, item})
			if ruleKind(item) == "ruleset" {
				result, _ := rulesetfmt.RuleFormat(*item, ruleLibrary, "")
				if result == "" {
					result = "text"
				}
				m.formats[i+1] = result
			}
		}
	}
	for i := range ruleLibrary {
		item := &ruleLibrary[i]
		if item.ID != "" {
			m.library[item.ID] = item
		}
	}
	return m
}

func ruleEnabled(item *model.RuleConfig) bool {
	return model.Flag(item.Enabled, true) &&
		model.Flag(item.LibraryEnabled, true)
}
func ruleKind(item *model.RuleConfig) string {
	if item.ItemType == "" {
		return "rule"
	}
	return item.ItemType
}
func valueOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
func (m *Matcher) libraryOf(item *model.RuleConfig) *model.RuleLibraryItem {
	return m.library[item.LibraryRuleID]
}
func ruleBehavior(item *model.RuleConfig, library *model.RuleLibraryItem) string {
	if item.Behavior != nil {
		return *item.Behavior
	}
	if library != nil && library.Behavior != nil {
		return *library.Behavior
	}
	return "domain"
}

func (m *Matcher) slot(position int, item *model.RuleConfig, library *model.RuleLibraryItem) indexSlot {
	slot := indexSlot{position: position, behavior: ruleBehavior(item, library), format: m.formats[position]}
	if m.indexKey != nil {
		if key, ok := m.indexKey(item, library); ok {
			slot.source, slot.position = key, 0
		}
	}
	return slot
}

// InvalidateRulesets discards the indexes of the selected references,
// including every reference sharing the same source, and returns how many
// were dropped. Callers own retry policy and cached host results.
func (m *Matcher) InvalidateRulesets(selected func(item *model.RuleConfig, library *model.RuleLibraryItem) bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	dropped := 0
	for _, it := range m.items {
		if ruleKind(it.item) != "ruleset" {
			continue
		}
		library := m.libraryOf(it.item)
		slot := m.slot(it.position, it.item, library)
		if _, built := m.indexes[slot]; built && selected(it.item, library) {
			delete(m.indexes, slot)
			dropped++
		}
	}
	return dropped
}

// IndexCount is the number of built rule-set indexes (diagnostics and tests).
func (m *Matcher) IndexCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.indexes)
}

func (m *Matcher) rulesetIndex(position int, item *model.RuleConfig, library *model.RuleLibraryItem) *ruleIndex {
	slot := m.slot(position, item, library)
	m.mu.RLock()
	index := m.indexes[slot]
	m.mu.RUnlock()
	if index != nil {
		return index
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if index = m.indexes[slot]; index != nil {
		return index
	}
	name := item.Name
	if name == "" {
		name = msgRulesetName
	}
	var content string
	var err error
	if m.load != nil {
		content, err = m.load(item, library)
	}
	if err != nil {
		m.logger.Errorf("Error processing ruleset \"%s\": %s", name, weburl.SafeErrorDetails(err))
		content = ""
	}
	if content == "" {
		m.logger.Warningf("Skipping ruleset \"%s\": unable to fetch content", name)
	}
	// Readers only ever see a complete index.
	index = newRuleIndex(func(yield func(ruleEntry) bool) {
		for line := range rulesetLines(content, slot.format) {
			if kind, value, ok := parseRulesetLine(line.value, slot.behavior); ok && !yield(ruleEntry{kind, value, line.source}) {
				return
			}
		}
	})
	m.indexes[slot] = index
	return index
}

// Match returns the first rule or rule set that hits the query (a domain or
// an IP), or nil: rule_name, rule_type, matched_line, policy, source,
// priority, rule_id, item_type, behavior.
func (m *Matcher) Match(query string) *Hit {
	query = NormalizeMatchQuery(query)
	ip, isIP := parseAddr(query)
	if !isIP && !IsValidDomain(query, true) {
		return nil
	}
	domain := strings.ToLower(query)
	for _, it := range m.items {
		item := it.item
		policy := valueOr(item.Policy, "DIRECT")
		switch ruleKind(item) {
		case "rule":
			ruleType, ruleValue := item.RuleType, valueOr(item.Value, "")
			matched := false
			if isIP {
				matched = matchAddr(ip, ruleType, ruleValue)
			} else {
				matched = MatchDomain(domain, ruleType, ruleValue)
			}
			if matched {
				return &Hit{RuleName: ruleType + msgRuleSuffix, RuleType: "rule", MatchedLine: ruleType + "," + ruleValue,
					Policy: policy, Source: msgDirectRuleSource, Priority: it.position, RuleID: optionalID(item.ID), ItemType: "rule", Behavior: "classical"}
			}
		case "ruleset":
			name := item.Name
			if name == "" {
				name = msgRulesetName
			}
			library := m.libraryOf(item)
			index := m.rulesetIndex(it.position, item, library)
			var line string
			var found bool
			if isIP {
				line, found = index.matchIP(ip)
			} else {
				line, found = index.matchDomain(domain)
			}
			if found {
				return &Hit{RuleName: name, RuleType: "ruleset", MatchedLine: line, Policy: policy,
					Source: msgRulesetSource + name, Priority: it.position, RuleID: optionalID(item.ID), ItemType: "ruleset", Behavior: ruleBehavior(item, library)}
			}
		}
	}
	return nil
}

// Hit is the first matching rule returned by discovery and the connection page.
type Hit struct {
	RuleName    string  `json:"rule_name"`
	RuleType    string  `json:"rule_type"`
	MatchedLine string  `json:"matched_line"`
	Policy      string  `json:"policy"`
	Source      string  `json:"source"`
	Priority    int     `json:"priority"`
	RuleID      *string `json:"rule_id"`
	ItemType    string  `json:"item_type"`
	Behavior    string  `json:"behavior"`
}

func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}
