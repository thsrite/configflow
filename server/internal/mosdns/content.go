package mosdns

import (
	"strings"

	"configflow/internal/convert"
	"configflow/internal/pystr"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
	"errors"
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
	kind, value = pystr.Upper(pystr.Strip(kind)), pystr.Strip(value)
	if kind != "DOMAIN-REGEX" {
		first, _, _ := strings.Cut(value, ",")
		value = pystr.Strip(first)
	}
	return kind, value
}

// isIPOrCIDR is _is_ip_or_cidr.
func isIPOrCIDR(value string) bool { return convert.IsIPNetwork(value) }

// RulesetParts is _ruleset_parts: the file parts a rule set needs in MosDNS
// (classical sets are split into a domain and an IP file).
func RulesetParts(behavior any) []string {
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
	pattern = pystr.Lower(pattern)
	if strings.HasPrefix(pattern, "+.") && !strings.Contains(pattern, "*") {
		return "domain:" + pattern[2:], true
	}
	if strings.HasPrefix(pattern, ".") && !strings.Contains(pattern, "*") {
		return `regexp:.+\.` + convert.PyReEscape(pattern[1:]) + "$", true
	}
	if strings.HasPrefix(pattern, "*.") && !strings.Contains(pattern[2:], "*") {
		return `regexp:^[^.]+\.` + convert.PyReEscape(pattern[2:]) + "$", true
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
			labels[i] = convert.PyReEscape(label)
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
	defer catch(&err)
	hasPart := len(part) > 0
	p := ""
	if hasPart {
		p = part[0]
	}
	content = strings.TrimPrefix(content, "\ufeff")
	wantDomain := !hasPart || p == PartDomain
	wantIP := !hasPart || p == PartIP
	wantClashIP := hasPart && p == PartIP

	isMosdnsFormat := false
	isYAMLFormat := rules.HasYAMLPayload(content)
	if !isYAMLFormat {
		sampleCount := 0
		for _, line := range convert.PySplitlines(content) {
			line = pystr.Strip(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			sampleCount++
			if sampleCount > 20 {
				break
			}
			if strings.Contains(line, ":") {
				before, _, _ := strings.Cut(line, ":")
				prefix := pystr.Lower(pystr.Strip(before))
				if inList(prefix, mosdnsDomainPrefixes) || prefix == "ip" {
					isMosdnsFormat = true
					break
				}
			}
		}
	}

	if isMosdnsFormat && !hasPart {
		return content, nil
	}

	var ruleLines []any
	if isYAMLFormat {
		document, loadErr := yamlx.Parse([]byte(content))
		if loadErr != nil && rejectYAML(loadErr) {
			return "", customYAMLError(loadErr)
		}
		var payload *yamlx.Node
		if loadErr == nil {
			payload = yamlx.Resolve(yamlx.Get(document, "payload"))
		}
		if payload == nil {
			isYAMLFormat = false
		} else {
			addScalar := func(node *yamlx.Node) {
				node = yamlx.Resolve(node)
				if textScalar(node) {
					ruleLines = append(ruleLines, node.Value)
				}
			}
			switch payload.Kind {
			case yaml.SequenceNode:
				for _, node := range payload.Content {
					addScalar(node)
				}
			case yaml.MappingNode:
				for key := range yamlx.AllPairs(payload) {
					addScalar(key)
				}
			case yaml.ScalarNode:
				if textScalar(payload) {
					for _, character := range payload.Value {
						ruleLines = append(ruleLines, string(character))
					}
					break
				}
				switch payload.ShortTag() {
				case "!!null":
				case "!!str", "!!timestamp":
					for _, character := range payload.Value {
						ruleLines = append(ruleLines, string(character))
					}
				default:
					var value any
					if err := yamlx.Decode(payload, &value); err != nil {
						return "", err
					}
					if truthy(value) {
						return "", errors.New(msgPayloadCollection)
					}
				}
			}
		}
	}
	if !isYAMLFormat {
		for _, line := range convert.PySplitlines(content) {
			ruleLines = append(ruleLines, line)
		}
	}

	converted := []string{}
	for _, raw := range ruleLines {
		line, ok := raw.(string)
		if !ok {
			continue
		}
		line = pystr.Strip(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		nativePrefix, nativeValue, separator := strings.Cut(line, ":")
		if separator && inList(pystr.Lower(nativePrefix), mosdnsDomainPrefixes) {
			if wantDomain && nativeValue != "" {
				converted = append(converted, pystr.Lower(nativePrefix)+":"+nativeValue)
			}
			continue
		}
		if separator && pystr.Lower(nativePrefix) == "ip" {
			if wantIP && isIPOrCIDR(pystr.Strip(nativeValue)) {
				converted = append(converted, pystr.Strip(nativeValue))
			}
			continue
		}

		if strings.Contains(line, ",") {
			ruleType, value := ParseClashRuleValue(line)
			if value == "" {
				continue
			}
			if prefix, ok := clashDomainPrefix(ruleType); ok {
				if wantDomain {
					converted = append(converted, prefix+":"+value)
				}
			} else if inList(ruleType, clashIPTypes) {
				if wantClashIP && isIPOrCIDR(value) {
					converted = append(converted, value)
				}
			}
			continue
		}

		if isIPOrCIDR(line) {
			if wantIP {
				converted = append(converted, line)
			}
			continue
		}
		if !wantDomain {
			continue
		}
		if expression, ok := DomainPatternForMosDNS(line); ok {
			converted = append(converted, expression)
		}
	}
	return joinRuleLines(converted, p), nil
}
