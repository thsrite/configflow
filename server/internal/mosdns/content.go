package mosdns

import (
	"errors"
	"iter"
	"slices"
	"strings"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"gopkg.in/yaml.v3"
)

// Rule file parts (RULE_PART_DOMAIN, RULE_PART_IP, RULE_PARTS).
const (
	PartDomain = "domain"
	PartIP     = "ip"
)

// RuleParts is RULE_PARTS.
var RuleParts = []string{PartDomain, PartIP}

var mosdnsDomainPrefixes = []string{"domain", "full", "keyword", "regexp"}

// clashDomainPrefixes is _CLASH_DOMAIN_PREFIXES (insertion order kept).
var clashDomainPrefixes = [][2]string{
	{"DOMAIN-SUFFIX", "domain"},
	{"DOMAIN", "full"},
	{"DOMAIN-KEYWORD", "keyword"},
	{"DOMAIN-REGEX", "regexp"},
}

var clashIPTypes = []string{"IP-CIDR", "IP-CIDR6"}

// DNSRuleTypes is DNS_RULE_TYPES: the single-rule types that can become DNS
// rules (GEOIP/GEOSITE need dat files; ports and logic rules have no match).
var DNSRuleTypes = []string{"DOMAIN-SUFFIX", "DOMAIN", "DOMAIN-KEYWORD", "DOMAIN-REGEX", "IP-CIDR", "IP-CIDR6"}

func clashDomainPrefix(kind string) (string, bool) {
	for _, p := range clashDomainPrefixes {
		if p[0] == kind {
			return p[1], true
		}
	}
	return "", false
}

func inList(s string, list []string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ParseClashRuleValue is parse_clash_rule_value: the type and payload of a
// classical provider line (Mihomo needTarget=false), not of a full rule.
// DOMAIN-REGEX keeps every remaining comma in its payload.
func ParseClashRuleValue(line string) (kind, value string) {
	kind, value, _ = strings.Cut(line, ",")
	kind, value = strings.TrimSpace(kind), strings.TrimSpace(value)
	if asciiRuleWord(kind) {
		kind = strings.ToUpper(kind)
	} else {
		kind = cases.Upper(language.Und).String(kind)
	}
	if kind != "DOMAIN-REGEX" {
		first, _, _ := strings.Cut(value, ",")
		value = strings.TrimSpace(first)
	}
	return kind, value
}

func asciiRuleWord(text string) bool {
	for i := range len(text) {
		if text[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func lowerRulePrefix(prefix string) string {
	if asciiRuleWord(prefix) {
		return strings.ToLower(prefix)
	}
	return cases.Lower(language.Und).String(prefix)
}

// isIPOrCIDR is _is_ip_or_cidr.
func isIPOrCIDR(value string) bool { return convert.IsIPNetwork(value) }

// Parts lists the file parts a rule set needs in MosDNS
// (classical sets are split into a domain and an IP file).
func Parts(behavior string) []string {
	if behavior == "ipcidr" {
		return []string{PartIP}
	}
	if behavior == "classical" {
		return []string{PartDomain, PartIP}
	}
	return []string{PartDomain}
}

// EmptyRulePart is empty_rule_part: what rule-proxy returns for an empty
// split part; deployment bundles must hold the same bytes.
func EmptyRulePart(part string) string { return "# no " + part + " rules" }

// joinRuleLines is _join_rule_lines (part "" stands for a falsy part).
func joinRuleLines(lines []string, part string) string {
	if len(lines) == 0 && part != "" {
		return EmptyRulePart(part)
	}
	return strings.Join(lines, "\n")
}

// DomainPatternForMosDNS is domain_pattern_for_mosdns: Mihomo Domain label
// patterns translated without widening their scope (ok=false is None).
func DomainPatternForMosDNS(pattern string) (string, bool) {
	if !rules.ValidPattern(pattern) {
		return "", false
	}
	pattern = cases.Lower(language.Und).String(pattern)
	if strings.HasPrefix(pattern, "+.") && !strings.Contains(pattern, "*") {
		return "domain:" + pattern[2:], true
	}
	if strings.HasPrefix(pattern, ".") && !strings.Contains(pattern, "*") {
		return `regexp:.+\.` + convert.RuleLiteral(pattern[1:]) + "$", true
	}
	if strings.HasPrefix(pattern, "*.") && !strings.Contains(pattern[2:], "*") {
		return `regexp:^[^.]+\.` + convert.RuleLiteral(pattern[2:]) + "$", true
	}
	if !strings.Contains(pattern, "*") {
		return "full:" + pattern, true
	}
	prefix := ""
	if strings.HasPrefix(pattern, "+.") {
		prefix, pattern = `(?:[^.]+\.)*`, pattern[2:]
	} else if strings.HasPrefix(pattern, ".") {
		prefix, pattern = `(?:[^.]+\.)+`, pattern[1:]
	}
	labels := strings.Split(pattern, ".")
	for i, label := range labels {
		if label == "*" {
			labels[i] = `[^.]+`
		} else {
			labels[i] = convert.RuleLiteral(label)
		}
	}
	return "regexp:^" + prefix + strings.Join(labels, `\.`) + "$", true
}

// ConvertRuleContentForMosDNS is convert_rule_content_for_mosdns(content,
// part): Clash, list and YAML payload rules as MosDNS text. Without part
// (Python None) the legacy output is produced (domain rules and bare IP
// lines; Clash IP-CIDR lines dropped); PartDomain keeps domain rules only,
// PartIP keeps IPs/CIDRs only, including Clash IP-CIDR/IP-CIDR6. Any other
// part keeps nothing and yields its placeholder comment.
//
// The error is Python's TypeError when a YAML payload is a scalar that
// cannot be iterated.
func ConvertRuleContentForMosDNS(content string, part ...string) (out string, err error) {
	hasPart := len(part) > 0
	p := ""
	if hasPart {
		p = part[0]
	}
	content = strings.TrimPrefix(content, "\ufeff")
	yamlPayload := rules.HasYAMLPayload(content)
	if !hasPart && !yamlPayload && nativeRuleFile(content) {
		return content, nil
	}
	lines, err := contentLines(content, yamlPayload)
	if err != nil {
		return "", err
	}
	sel := ruleSelection{domain: !hasPart || p == PartDomain, ip: !hasPart || p == PartIP, clashIP: p == PartIP}
	var converted []string
	for line := range lines {
		if value := sel.convert(line); value != "" {
			converted = append(converted, value)
		}
	}
	return joinRuleLines(converted, p), nil
}

// HasIPRules reports whether PartIP would contain a rule. YAML syntax and
// policy checks apply to the complete document before inspecting its payload.
func HasIPRules(content string) (bool, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	lines, err := contentLines(content, rules.HasYAMLPayload(content))
	if err != nil {
		return false, err
	}
	sel := ruleSelection{ip: true, clashIP: true}
	for line := range lines {
		if sel.convert(line) != "" {
			return true, nil
		}
	}
	return false, nil
}

func nativeRuleFile(content string) bool {
	count := 0
	for line := range convert.RuleLineSeq(content) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if count == 20 {
			break
		}
		count++
		if before, _, ok := strings.Cut(line, ":"); ok {
			prefix := lowerRulePrefix(strings.TrimSpace(before))
			if inList(prefix, mosdnsDomainPrefixes) || prefix == "ip" {
				return true
			}
		}
	}
	return false
}

func contentLines(content string, yamlPayload bool) (iter.Seq[string], error) {
	if !yamlPayload {
		return convert.RuleLineSeq(content), nil
	}
	doc, err := yamlx.Parse([]byte(content))
	if err != nil && rejectYAML(err) {
		return nil, customYAMLError(err)
	}
	var payload *yamlx.Node
	if err == nil {
		payload = yamlx.Resolve(yamlx.Get(doc, "payload"))
	}
	if payload == nil {
		return convert.RuleLineSeq(content), nil
	}

	switch payload.Kind {
	case yaml.SequenceNode:
		return func(yield func(string) bool) {
			for _, n := range payload.Content {
				n = yamlx.Resolve(n)
				if textScalar(n) && !yield(n.Value) {
					return
				}
			}
		}, nil
	case yaml.MappingNode:
		return func(yield func(string) bool) {
			for key := range yamlx.AllPairs(payload) {
				n := yamlx.Resolve(key)
				if textScalar(n) && !yield(n.Value) {
					return
				}
			}
		}, nil
	case yaml.ScalarNode:
		if textScalar(payload) {
			return func(yield func(string) bool) {
				for _, r := range payload.Value {
					if !yield(string(r)) {
						return
					}
				}
			}, nil
		}
		var v any
		if err := yamlx.Decode(payload, &v); err != nil {
			return nil, err
		}
		if v != nil && v != false && v != 0 && v != 0.0 {
			return nil, errors.New(PayloadCollectionMessage)
		}
	}
	return slices.Values([]string(nil)), nil
}

type ruleSelection struct {
	domain, ip, clashIP bool
}

func (s ruleSelection) convert(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
		return ""
	}
	if prefix, value, ok := strings.Cut(line, ":"); ok {
		prefix = lowerRulePrefix(prefix)
		if inList(prefix, mosdnsDomainPrefixes) {
			if s.domain && value != "" {
				return prefix + ":" + value
			}
			return ""
		}
		if prefix == "ip" {
			value = strings.TrimSpace(value)
			if s.ip && isIPOrCIDR(value) {
				return value
			}
			return ""
		}
	}
	if strings.Contains(line, ",") {
		kind, value := ParseClashRuleValue(line)
		if value == "" {
			return ""
		}
		if s.domain {
			if prefix, ok := clashDomainPrefix(kind); ok {
				return prefix + ":" + value
			}
		}
		if s.clashIP && inList(kind, clashIPTypes) && isIPOrCIDR(value) {
			return value
		}
		return ""
	}
	if isIPOrCIDR(line) {
		if s.ip {
			return line
		}
		return ""
	}
	if s.domain {
		if value, ok := DomainPatternForMosDNS(line); ok {
			return value
		}
	}
	return ""
}
