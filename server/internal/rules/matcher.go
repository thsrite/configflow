package rules

import (
	"configflow/internal/pystr"
	"iter"
	"strings"
	"sync"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/urlx"
	"configflow/internal/pyyaml"
)

// Port of backend/utils/rule_matcher.py: rule-line parsing and the indexed
// matcher the domain discovery and rule IP presence checks run over rule sets
// with hundreds of thousands of lines.

// matcherLogger is rule_matcher's module logger.
var matcherLogger Logger = logx.Get("backend.utils.rule_matcher")

// IsValidDomain is is_valid_domain. allowDNSLabels admits underscores and
// leading/trailing hyphens in labels (rule tests); domain discovery keeps the
// strict hostname form.
func IsValidDomain(query string, allowDNSLabels bool) bool {
	if query == "" || len(query) > 253 || !isASCII(query) {
		// Non-ASCII text can never satisfy either label pattern.
		return false
	}
	if strings.Contains(query, ".") {
		allDigits := true
		for _, part := range strings.Split(query, ".") {
			if !pyIsASCIIDigits(part) {
				allDigits = false
				break
			}
		}
		if allDigits {
			// Invalid dotted IPs must not re-enter discovery as hostnames after
			// ip_address rejects them; ordinary numeric labels like 3com remain valid.
			return false
		}
	}
	for _, label := range strings.Split(query, ".") {
		if allowDNSLabels {
			if len(label) < 1 || len(label) > 63 {
				return false
			}
			for i := 0; i < len(label); i++ {
				c := label[i]
				if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '-') {
					return false
				}
			}
		} else if !validDNSLabel(label) {
			return false
		}
	}
	return true
}

// IsValidIP is is_valid_ip.
func IsValidIP(query string) bool { return isValidIP(query) }

// NormalizeMatchQuery ignores one DNS root dot without turning a dotted IP
// input into an address.
func NormalizeMatchQuery(query string) string {
	if strings.HasSuffix(query, ".") && !isValidIP(query[:len(query)-1]) {
		return query[:len(query)-1]
	}
	return query
}

// ParseRuleLine is parse_rule_line: the rule type and value of one line in the
// Clash, MosDNS or list dialects. ok=false is Python's None.
func ParseRuleLine(line string) (ruleType, ruleValue string, ok bool) {
	line = pystr.Strip(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "//") {
		return "", "", false
	}
	if strings.HasPrefix(line, "-") {
		line = pystr.Strip(line[1:])
		if line == "" {
			return "", "", false
		}
	}
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = pystr.Strip(line[:i])
		if line == "" {
			return "", "", false
		}
	}
	line = pyStripChars(line, `'"`)

	// 1. Clash/YAML comma separated.
	if i := strings.IndexByte(line, ','); i >= 0 {
		ruleType = pystr.Upper(pystr.Strip(line[:i]))
		ruleValue = pystr.Strip(line[i+1:])
		// no-resolve is a parameter, not part of the value; keeping it would
		// break IP matching and duplicate detection.
		if strings.HasSuffix(pystr.Lower(ruleValue), ",no-resolve") {
			runes := []rune(ruleValue)
			ruleValue = pystr.Strip(string(runes[:len(runes)-len(",no-resolve")]))
		}
		return ruleType, ruleValue, true
	}

	// 2. MosDNS (domain: / full: / keyword: / regexp: / ip:).
	if i := strings.IndexByte(line, ':'); i >= 0 {
		prefix := pystr.Lower(pystr.Strip(line[:i]))
		value := pystr.Strip(line[i+1:])
		if prefix == "payload" && value == "" {
			return "", "", false
		}
		switch prefix {
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

	// 3. List format.
	switch {
	case strings.HasPrefix(line, "+."):
		return "DOMAIN-SUFFIX", pystr.Strip(line[2:]), true
	case strings.HasPrefix(line, "*."):
		return "DIRECT-SUBDOMAIN", pystr.Strip(line[2:]), true
	case strings.HasPrefix(line, ".") && !strings.HasPrefix(line, ".."):
		return "SUBDOMAIN-ONLY", pystr.Strip(line[1:]), true
	}

	// 4. Bare IP or CIDR.
	if _, ok := parseIPAddress(line); ok {
		return "IP", line, true
	}
	if _, ok := parseIPNetwork(line); ok {
		return "IP-CIDR", line, true
	}
	// 5. Exact domain.
	return "DOMAIN", line, true
}

// MatchDomain is match_domain.
func MatchDomain(domain, ruleType, ruleValue string) bool {
	domain = pystr.Lower(domain)
	if ruleType == "DOMAIN-REGEX" {
		re, err := CompilePyRegex(ruleValue, true)
		return err == nil && re.Search(domain)
	}
	ruleValue = pystr.Lower(ruleValue)
	switch ruleType {
	case "DOMAIN":
		return domain == ruleValue
	case "DOMAIN-SUFFIX":
		return domain == ruleValue || strings.HasSuffix(domain, "."+ruleValue)
	case "DOMAIN-KEYWORD":
		return strings.Contains(domain, ruleValue)
	case "SUBDOMAIN-ONLY":
		if ruleValue == "" || !strings.HasSuffix(domain, "."+ruleValue) {
			return false
		}
		return domain != ruleValue
	case "DIRECT-SUBDOMAIN":
		if ruleValue == "" || !strings.HasSuffix(domain, "."+ruleValue) {
			return false
		}
		prefix := domain[:len(domain)-(len(ruleValue)+1)]
		return prefix != "" && !strings.Contains(prefix, ".")
	case "REGEX", "REGEXP":
		re, err := CompilePyRegex(ruleValue, false)
		return err == nil && re.Search(domain)
	}
	return false
}

// MatchIP is match_ip.
func MatchIP(ipText, ruleType, ruleValue string) bool {
	ip, ok := parseIPAddress(ipText)
	if !ok {
		return false
	}
	return matchParsedIP(ip, ruleType, ruleValue)
}

func matchParsedIP(ip ipAddr, ruleType, ruleValue string) bool {
	switch ruleType {
	case "IP-CIDR", "IP-CIDR6":
		network, ok := parseIPNetwork(ruleValue)
		return ok && network.contains(ip)
	case "IP":
		other, ok := parseIPAddress(ruleValue)
		return ok && other == ip
	}
	return false
}

// MatchQuery is match_query: classify the query as IP or domain, then match.
func MatchQuery(query, ruleType, ruleValue string) bool {
	query = NormalizeMatchQuery(query)
	if isValidIP(query) {
		return MatchIP(query, ruleType, ruleValue)
	}
	if IsValidDomain(query, true) {
		return MatchDomain(query, ruleType, ruleValue)
	}
	return false
}

// rulesetLine is one decoded provider entry and its original display line.
type rulesetLine struct{ value, source string }

// iterRulesetLines is _iter_ruleset_lines: decoded rules and original display
// lines for a "text" or "yaml" provider body.
func iterRulesetLines(content, format string) iter.Seq[rulesetLine] {
	return func(yield func(rulesetLine) bool) {
		switch format {
		case "text":
			first := true
			iterRuleLines(content, func(raw string) bool {
				value := raw
				if first {
					value = strings.TrimPrefix(raw, "\ufeff")
				}
				first = false
				return yield(rulesetLine{pystr.Strip(value), pystr.Strip(raw)})
			})
		case "yaml":
			for _, entry := range yamlPayloadLines(content) {
				if !yield(entry) {
					return
				}
			}
		}
	}
}

// yamlPayloadLines decodes the single top-level `payload:` sequence of plain
// string scalars; anything else (not a mapping, duplicate or missing payload,
// non-string or multi-line entries, YAML errors) yields nothing, exactly like
// the Python generator.
//
// Python composes with CSafeLoader (libyaml): the PyYAML port runs with
// libyaml's scanner rules (ComposeOptions.Libyaml: tabs, byte order marks,
// ":" in flow scalars, surrogate escapes) and libyaml's marks, which leave out
// a byte order mark at the start of the stream (Python's line lookup then
// runs one character early; that is kept). gopkg.in/yaml.v3 read some
// documents differently ("%YAML 1.2", "[a,0:]", multi-line scalars).
func yamlPayloadLines(content string) []rulesetLine {
	root, err := pyyaml.ComposeWith(content, pyyaml.ComposeOptions{Libyaml: true})
	if err != nil || root == nil || root.Kind != pyyaml.MappingNode {
		return nil
	}
	var payloads []*pyyaml.Node
	for _, pair := range root.Pairs {
		if pair[0].Kind == pyyaml.ScalarNode && pair[0].Value == "payload" {
			payloads = append(payloads, pair[1])
		}
	}
	if len(payloads) != 1 || payloads[0].Kind != pyyaml.SequenceNode {
		return nil
	}
	payload := payloads[0]
	for _, node := range payload.Items {
		if node.Kind != pyyaml.ScalarNode || node.Tag != pyyaml.TagStr || strings.ContainsAny(node.Value, "\n\r") {
			return nil
		}
	}
	result := make([]rulesetLine, 0, len(payload.Items))
	var runes []rune
	for _, node := range payload.Items {
		if payload.FlowStyle {
			result = append(result, rulesetLine{pystr.Strip(node.Value), node.Value})
			continue
		}
		if runes == nil {
			runes = []rune(content)
		}
		// start = content.rfind('\n', 0, start_mark.index) + 1
		// end = content.find('\n', end_mark.index)
		from := 0
		for i := min(node.Start.Index, len(runes)) - 1; i >= 0; i-- {
			if runes[i] == '\n' {
				from = i + 1
				break
			}
		}
		to := len(runes)
		for i := max(node.End.Index, 0); i < len(runes); i++ {
			if runes[i] == '\n' {
				to = i
				break
			}
		}
		source := ""
		if from < to {
			source = string(runes[from:to])
		}
		result = append(result, rulesetLine{pystr.Strip(node.Value), pystr.Strip(source)})
	}
	return result
}

// parseRulesetLine is _parse_ruleset_line: provider entries interpreted
// according to the selected Mihomo behavior. Cross-format fallbacks of
// ParseRuleLine must not create routing hits.
func parseRulesetLine(value, behavior string) (kind, payload string, ok bool) {
	if value == "" || strings.HasPrefix(value, "#") || strings.HasPrefix(value, "//") || strings.HasPrefix(value, "!") {
		return "", "", false
	}
	switch behavior {
	case "ipcidr":
		if CIDRError(value) != "" {
			return "", "", false
		}
		return "IP-CIDR", value, true
	case "domain":
		if !validPattern(value) {
			return "", "", false
		}
		value = pystr.Lower(value)
		if strings.Contains(value, "*") && !(strings.HasPrefix(value, "*.") && !strings.Contains(value[2:], "*")) {
			prefix := ""
			if strings.HasPrefix(value, "+.") {
				prefix, value = `(?:[^.]+\.)*`, value[2:]
			} else if strings.HasPrefix(value, ".") {
				prefix, value = `(?:[^.]+\.)+`, value[1:]
			}
			parts := strings.Split(value, ".")
			for i, part := range parts {
				if part == "*" {
					parts[i] = `[^.]+`
				} else {
					parts[i] = pyReEscape(part)
				}
			}
			return "DOMAIN-REGEX", "^" + prefix + strings.Join(parts, `\.`) + "$", true
		}
		switch {
		case strings.HasPrefix(value, "+."):
			return "DOMAIN-SUFFIX", value[2:], true
		case strings.HasPrefix(value, "*."):
			return "DIRECT-SUBDOMAIN", value[2:], true
		case strings.HasPrefix(value, "."):
			return "SUBDOMAIN-ONLY", value[1:], true
		}
		return "DOMAIN", value, true
	case "classical":
	default:
		return "", "", false
	}
	if !strings.Contains(value, ",") && IsRuleType(pystr.Upper(value)) {
		return "", "", false
	}
	value, err := classicalLine(value)
	if err != nil {
		return "", "", false
	}
	if classicalLineError(value) != "" {
		return "", "", false
	}
	head, rest, found := strings.Cut(value, ",")
	kind = pystr.Upper(pystr.Strip(head))
	if !found {
		return "", "", false
	}
	switch kind {
	case "DOMAIN-REGEX":
		return kind, pystr.Strip(rest), true
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "IP-CIDR", "IP-CIDR6":
		first, _, _ := strings.Cut(rest, ",")
		return kind, pystr.Strip(first), true
	}
	return "", "", false
}

// ruleEntry is one parsed provider entry handed to newRuleIndex.
type ruleEntry struct{ kind, value, line string }

type domainPattern struct {
	position   int
	keyword    string
	hasKeyword bool
	pattern    *PyRegex
}

// ruleIndex indexes common rule types while retaining the first matching
// source line. Dictionaries hold an index into one source-line slice; retained
// entries stay in source order, so skipping duplicates and unsupported rules
// cannot change the earliest match.
type ruleIndex struct {
	exact, suffix, subdomain, directSubdomain map[string]int
	patterns                                  []domainPattern
	ips                                       map[ipAddr]int
	networks                                  [2]networkSet
	lines                                     []string
	// Unsupported counts DOMAIN-REGEX/REGEX entries Python compiles but the
	// Go engine cannot run; they never match here.
	unsupported int
}

type networkSet struct {
	prefixes []int
	byPrefix map[int]map[u128]int
}

func newRuleIndex(entries iter.Seq[ruleEntry]) *ruleIndex {
	index := &ruleIndex{
		exact: map[string]int{}, suffix: map[string]int{}, subdomain: map[string]int{}, directSubdomain: map[string]int{},
		ips: map[ipAddr]int{},
	}
	index.networks[0].byPrefix = map[int]map[u128]int{}
	index.networks[1].byPrefix = map[int]map[u128]int{}
	for entry := range entries {
		switch entry.kind {
		case "DOMAIN", "DOMAIN-SUFFIX", "SUBDOMAIN-ONLY", "DIRECT-SUBDOMAIN":
			var target map[string]int
			switch entry.kind {
			case "DOMAIN":
				target = index.exact
			case "DOMAIN-SUFFIX":
				target = index.suffix
			case "SUBDOMAIN-ONLY":
				target = index.subdomain
			default:
				target = index.directSubdomain
			}
			key := pystr.Lower(entry.value)
			if _, seen := target[key]; !seen {
				target[key] = len(index.lines)
				index.lines = append(index.lines, entry.line)
			}
		case "DOMAIN-KEYWORD":
			index.patterns = append(index.patterns, domainPattern{position: len(index.lines), keyword: pystr.Lower(entry.value), hasKeyword: true})
			index.lines = append(index.lines, entry.line)
		case "REGEX", "REGEXP", "DOMAIN-REGEX":
			// match_domain has always lowercased both pattern and query. Mihomo
			// DOMAIN-REGEX ignores case without rewriting escapes.
			var pattern *PyRegex
			var err error
			if entry.kind == "DOMAIN-REGEX" {
				pattern, err = CompilePyRegex(entry.value, true)
			} else {
				pattern, err = CompilePyRegex(pystr.Lower(entry.value), false)
			}
			if err != nil {
				if !IsPyRegexError(err) {
					index.unsupported++
				}
				continue
			}
			index.patterns = append(index.patterns, domainPattern{position: len(index.lines), pattern: pattern})
			index.lines = append(index.lines, entry.line)
		case "IP":
			ip, ok := parseIPAddress(entry.value)
			if !ok {
				continue
			}
			if _, seen := index.ips[ip]; !seen {
				index.ips[ip] = len(index.lines)
				index.lines = append(index.lines, entry.line)
			}
		case "IP-CIDR", "IP-CIDR6":
			network, ok := parseIPNetwork(entry.value)
			if !ok {
				continue
			}
			set := &index.networks[network.ver/6]
			byNetwork, exists := set.byPrefix[network.prefix]
			if !exists {
				byNetwork = map[u128]int{}
				set.byPrefix[network.prefix] = byNetwork
				set.prefixes = append(set.prefixes, network.prefix)
			}
			if _, seen := byNetwork[network.base]; !seen {
				byNetwork[network.base] = len(index.lines)
				index.lines = append(index.lines, entry.line)
			}
		}
	}
	return index
}

func earlier(current int, hasCurrent bool, candidate int, hasCandidate bool) (int, bool) {
	if hasCandidate && (!hasCurrent || candidate < current) {
		return candidate, true
	}
	return current, hasCurrent
}

// matchDomain is _RuleIndex.match_domain; the domain is already lowercase.
func (index *ruleIndex) matchDomain(domain string) (string, bool) {
	best, ok := index.exact[domain]
	suffix := domain
	isSubdomain := false
	for suffix != "" {
		candidate, found := index.suffix[suffix]
		best, ok = earlier(best, ok, candidate, found)
		if isSubdomain {
			candidate, found = index.subdomain[suffix]
			best, ok = earlier(best, ok, candidate, found)
		}
		dot := strings.IndexByte(suffix, '.')
		if dot < 0 {
			break
		}
		suffix = suffix[dot+1:]
		isSubdomain = true
	}
	if dot := strings.IndexByte(domain, '.'); dot >= 0 {
		candidate, found := index.directSubdomain[domain[dot+1:]]
		best, ok = earlier(best, ok, candidate, found)
	}
	for _, p := range index.patterns {
		if ok && p.position >= best {
			break
		}
		if (p.hasKeyword && strings.Contains(domain, p.keyword)) || (p.pattern != nil && p.pattern.Search(domain)) {
			best, ok = p.position, true
			break
		}
	}
	if !ok {
		return "", false
	}
	return index.lines[best], true
}

// matchIP is _RuleIndex.match_ip.
func (index *ruleIndex) matchIP(ip ipAddr) (string, bool) {
	best, ok := index.ips[ip]
	set := &index.networks[ip.ver/6]
	for _, prefix := range set.prefixes {
		candidate, found := set.byPrefix[prefix][ip.v.and(maskFor(ip.ver, prefix))]
		best, ok = earlier(best, ok, candidate, found)
	}
	if !ok {
		return "", false
	}
	return index.lines[best], true
}

// LoadContent returns the text of a rule set: the reference item and its
// library entry (nil when unbound). An error is logged and treated as empty.
type LoadContent func(item, library *jsonx.Object) (string, error)

// IndexKey opts a loader into index reuse across references that share one
// library entry. ok=false (Python's None) keeps a per-item index.
type IndexKey func(item, library *jsonx.Object) (key string, ok bool)

type indexSlot struct {
	source    string
	hasSource bool
	index     int
	behavior  string
	format    string
}

// RuleConfigMatcher matches a domain or IP against rule_configs in order and
// returns the first hit. Each rule set is loaded and indexed on first use;
// domains are looked up by label and IPs by prefix length.
type RuleConfigMatcher struct {
	items    []matcherItem
	library  map[string]*jsonx.Object
	formats  map[int]string
	load     LoadContent
	indexKey IndexKey
	logger   Logger

	mu      sync.RWMutex
	indexes map[indexSlot]*ruleIndex
}

type matcherItem struct {
	index int
	item  *jsonx.Object
}

// NewRuleConfigMatcher builds a matcher. ruleConfigs and ruleLibrary hold
// *jsonx.Object entries; numbering counts disabled rules too, like the row
// numbers shown in the rule list. indexKey and logger may be nil.
func NewRuleConfigMatcher(ruleConfigs, ruleLibrary []any, load LoadContent, indexKey IndexKey, logger Logger) *RuleConfigMatcher {
	if logger == nil {
		logger = matcherLogger
	}
	m := &RuleConfigMatcher{
		library: map[string]*jsonx.Object{}, formats: map[int]string{}, load: load,
		indexKey: indexKey, logger: logger, indexes: map[indexSlot]*ruleIndex{},
	}
	for position, entry := range ruleConfigs {
		item, ok := entry.(*jsonx.Object)
		if !ok {
			continue
		}
		if enabled(item, "enabled") && enabled(item, "library_enabled") {
			m.items = append(m.items, matcherItem{position + 1, item})
		}
	}
	for _, entry := range ruleLibrary {
		if item, ok := entry.(*jsonx.Object); ok {
			if id, _ := item.Value("id").(string); id != "" {
				m.library[id] = item
			}
		}
	}
	for _, it := range m.items {
		if itemType(it.item) == "ruleset" {
			m.formats[it.index] = RulesetFormat(it.item, ruleLibrary, "")
		}
	}
	return m
}

// enabled is item.get(key, True) used as a truth value.
func enabled(item *jsonx.Object, key string) bool {
	value, ok := item.Get(key)
	return !ok || jsonx.Truthy(value)
}

func itemType(item *jsonx.Object) string {
	if value, ok := item.Get("itemType"); ok {
		text, _ := value.(string)
		return text
	}
	return "rule"
}

func (m *RuleConfigMatcher) libraryOf(item *jsonx.Object) *jsonx.Object {
	id, _ := item.GetOr("library_rule_id", "").(string)
	return m.library[id]
}

func rulesetBehavior(item, library *jsonx.Object) any {
	if value, ok := item.Get("behavior"); ok {
		return value
	}
	if library != nil {
		if value, ok := library.Get("behavior"); ok {
			return value
		}
	}
	return "domain"
}

func behaviorText(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return "\x00" + jsonx.Repr(value)
}

func (m *RuleConfigMatcher) slot(position int, item, library *jsonx.Object) indexSlot {
	slot := indexSlot{index: position, behavior: behaviorText(rulesetBehavior(item, library)), format: m.formats[position]}
	if m.indexKey != nil {
		if key, ok := m.indexKey(item, library); ok {
			slot.source, slot.hasSource, slot.index = key, true, 0
		}
	}
	return slot
}

// InvalidateRulesets discards the indexes of the selected references,
// including every reference sharing the same source; it returns how many
// indexes were dropped. Callers own retry policy and cached host results.
func (m *RuleConfigMatcher) InvalidateRulesets(predicate func(item, library *jsonx.Object) bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := map[indexSlot]struct{}{}
	for _, it := range m.items {
		if itemType(it.item) != "ruleset" {
			continue
		}
		library := m.libraryOf(it.item)
		if predicate(it.item, library) {
			keys[m.slot(it.index, it.item, library)] = struct{}{}
		}
	}
	dropped := 0
	for key := range keys {
		if _, ok := m.indexes[key]; ok {
			delete(m.indexes, key)
			dropped++
		}
	}
	return dropped
}

// UnsupportedRegexCount is the number of regular-expression entries in the
// built indexes that Python's re accepts but the Go engine cannot run (see
// UnsupportedRegexError); such entries never match.
func (m *RuleConfigMatcher) UnsupportedRegexCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0
	for _, index := range m.indexes {
		total += index.unsupported
	}
	return total
}

// IndexCount is the number of built rule-set indexes (diagnostics and tests).
func (m *RuleConfigMatcher) IndexCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.indexes)
}

func (m *RuleConfigMatcher) rulesetIndex(position int, item, library *jsonx.Object) *ruleIndex {
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
	name := item.GetOr("name", "规则集")
	content, err := m.load(item, library)
	if err != nil {
		m.logger.Errorf("Error processing ruleset \"%s\": %s", jsonx.Str(name), urlx.SafeExceptionDetails(err))
		content = ""
	}
	if content == "" {
		m.logger.Warningf("Skipping ruleset \"%s\": unable to fetch content", jsonx.Str(name))
	}
	behavior, _ := rulesetBehavior(item, library).(string)
	format := m.formats[position]
	// Publish only the complete immutable index to concurrent readers.
	index = newRuleIndex(func(yield func(ruleEntry) bool) {
		for line := range iterRulesetLines(content, format) {
			kind, value, ok := parseRulesetLine(line.value, behavior)
			if ok && !yield(ruleEntry{kind, value, line.source}) {
				return
			}
		}
	})
	m.indexes[slot] = index
	return index
}

// Match returns the first rule or rule set that hits the query (a domain or
// an IP), or nil. The result mirrors the Python dict: rule_name, rule_type,
// matched_line, policy, source, priority, rule_id, item_type, behavior.
func (m *RuleConfigMatcher) Match(query string) *jsonx.Object {
	query = NormalizeMatchQuery(query)
	ip, isIP := parseIPAddress(query)
	if !isIP && !IsValidDomain(query, true) {
		return nil
	}
	domain := ""
	if !isIP {
		domain = pystr.Lower(query)
	}
	for _, it := range m.items {
		item := it.item
		policy := item.GetOr("policy", "DIRECT")
		switch itemType(item) {
		case "rule":
			ruleType := item.GetString("rule_type")
			ruleValue := item.GetString("value")
			var matched bool
			if isIP {
				matched = matchParsedIP(ip, ruleType, ruleValue)
			} else {
				matched = MatchDomain(domain, ruleType, ruleValue)
			}
			if matched {
				return jsonx.Obj(
					"rule_name", ruleType+" 规则", "rule_type", "rule",
					"matched_line", ruleType+","+ruleValue, "policy", policy,
					"source", "直接配置的规则", "priority", jsonx.Int(it.index),
					"rule_id", item.Value("id"), "item_type", "rule", "behavior", "classical",
				)
			}
		case "ruleset":
			name := item.GetOr("name", "规则集")
			library := m.libraryOf(item)
			index := m.rulesetIndex(it.index, item, library)
			var line string
			var found bool
			if isIP {
				line, found = index.matchIP(ip)
			} else {
				line, found = index.matchDomain(domain)
			}
			if found {
				return jsonx.Obj(
					"rule_name", name, "rule_type", "ruleset", "matched_line", line,
					"policy", policy, "source", "规则集: "+jsonx.Str(name),
					"priority", jsonx.Int(it.index), "rule_id", item.Value("id"),
					"item_type", "ruleset", "behavior", rulesetBehavior(item, library),
				)
			}
		}
	}
	return nil
}
