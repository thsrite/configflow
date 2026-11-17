package gateway

import (
	"net/netip"
	"regexp"
	"strings"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/netipx"
)

// Limits of one rule set's content.
const (
	maxContentBytes = 16 * 1024 * 1024
	maxRules        = 250000
)

// yamlPayload detects a YAML rule set ("payload:" on some line).
var yamlPayload = regexp.MustCompile(`(?m)^\s*payload\s*:`)

// contentLines are the entries of a rule set: the strings of a YAML
// payload (or top-level list), else the text lines. Errors are *Error.
func contentLines(content string) ([]string, error) {
	if len(content) > maxContentBytes {
		return nil, gatewayError(msgContentTooLarge)
	}
	if strings.ContainsAny(content, "\x00\ufffd") || !utf8.ValidString(content) {
		return nil, gatewayError(msgContentBinary)
	}
	stripped := strings.TrimLeft(content, "\ufeff\r\n \t")
	var values []string
	if yamlPayload.MatchString(stripped) || strings.HasPrefix(stripped, "[") ||
		strings.HasPrefix(stripped, "- ") || strings.HasPrefix(stripped, "---") {
		var err error
		if values, err = yamlPayloadStrings(stripped); err != nil {
			return nil, err
		}
	} else {
		values = convert.PySplitlines(stripped)
	}
	if len(values) > maxRules {
		return nil, gatewayError(msgTooManyRules)
	}
	return values, nil
}

// withoutComment is a line up to its first "#", trimmed.
func withoutComment(line string) string {
	before, _, _ := strings.Cut(line, "#")
	return strings.TrimSpace(before)
}

// splitFields splits a rule line at commas and trims every field.
func splitFields(line string) []string {
	fields := strings.Split(line, ",")
	for i, field := range fields {
		fields[i] = strings.TrimSpace(field)
	}
	return fields
}

// parseNetwork reads an IP address or network (an IPv4 netmask or hostmask
// allowed), clearing host bits. An IPv6 address with a zone ("fe80::1%eth0")
// is accepted with the zone dropped.
func parseNetwork(text string) (netip.Prefix, bool) {
	if network, ok := netipx.ParseNetwork(text, false); ok {
		return network, true
	}
	address, mask, hasMask := strings.Cut(text, "/")
	host, zone, scoped := strings.Cut(address, "%")
	if !scoped || zone == "" || strings.Contains(zone, "%") {
		return netip.Prefix{}, false
	}
	if addr, err := netip.ParseAddr(host); err != nil || !addr.Is6() {
		return netip.Prefix{}, false
	}
	if hasMask {
		host += "/" + mask
	}
	return netipx.ParseNetwork(host, false)
}

// ExtractCIDRs returns the IPv4 networks a rule set routes unconditionally
// by destination, collapsed and sorted: every entry of an "ipcidr" set, the
// IP-CIDR entries of a "classical" set (domain, source-IP, logical and other
// rules are ignored; IPv6 is skipped). Networks wider than /8 or covering all
// of IPv4 together are refused. Errors are *Error.
func ExtractCIDRs(content, behavior string) ([]netip.Prefix, error) {
	if !ipBehavior(behavior) {
		return nil, gatewayError(msgExtractBehavior)
	}
	lines, err := contentLines(content)
	if err != nil {
		return nil, err
	}
	var networks []netip.Prefix
	for _, line := range lines {
		line = withoutComment(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := splitFields(line)
		var value string
		if behavior == "classical" {
			kind := strings.ToUpper(fields[0])
			if kind != "IP-CIDR" && kind != "IP-CIDR6" {
				continue
			}
			if (len(fields) != 2 && len(fields) != 3) || (len(fields) == 3 && strings.ToLower(fields[2]) != "no-resolve") {
				return nil, gatewayError(msgClassicalShape)
			}
			value = fields[1]
		} else {
			if len(fields) != 1 {
				return nil, gatewayError(msgIPCIDRShape)
			}
			value = fields[0]
		}
		network, ok := parseNetwork(value)
		if !ok {
			return nil, gatewayError(msgInvalidCIDR)
		}
		if behavior == "classical" && (strings.ToUpper(fields[0]) == "IP-CIDR6") != network.Addr().Is6() {
			return nil, gatewayError(msgVersionMismatch)
		}
		if network.Addr().Is6() {
			continue
		}
		if network.Bits() < 8 {
			return nil, gatewayError(msgPrefixTooShort)
		}
		networks = append(networks, network)
	}
	collapsed, err := netipx.Collapse(networks)
	if err != nil {
		return nil, err
	}
	for _, network := range collapsed {
		if network.Bits() == 0 {
			return nil, gatewayError(msgFullCapture)
		}
	}
	return collapsed, nil
}

// ruleNetworks are the IPv4 networks of rule set lines for the overlap
// check: any prefix length, malformed lines skipped.
func ruleNetworks(lines []string, behavior string) []netip.Prefix {
	var out []netip.Prefix
	for _, line := range lines {
		fields := splitFields(strings.SplitN(line, "#", 2)[0])
		if behavior == "classical" {
			if len(fields) < 2 || strings.ToUpper(fields[0]) != "IP-CIDR" {
				continue
			}
			fields = fields[1:]
		}
		if network, ok := parseNetwork(fields[0]); ok && network.Addr().Is4() {
			out = append(out, network)
		}
	}
	return out
}

func prefixStrings(networks []netip.Prefix) []string {
	out := make([]string, len(networks))
	for i, network := range networks {
		out[i] = network.String()
	}
	return out
}
