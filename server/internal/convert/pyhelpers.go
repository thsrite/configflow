package convert

// Small Python built-ins the converters rely on (private copies; urlx, rules
// and subs keep their own unexported versions).

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"unicode"

	"configflow/internal/jsonx"
)

// pyIsSpace is str.isspace() for one character.
func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// pyStrip is str.strip().
func pyStrip(s string) string { return strings.TrimFunc(s, pyIsSpace) }

// pyStripChars is str.strip(chars).
func pyStripChars(s, chars string) string {
	return strings.Trim(s, chars)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// pyLower is str.lower(): Go's ToLower plus the two full-case-mapping rules
// Python applies (U+0130 becomes "i" + U+0307, a capital sigma ending a word
// becomes the final sigma).
func pyLower(s string) string {
	if isASCII(s) {
		return strings.ToLower(s)
	}
	runes := []rune(s)
	var out strings.Builder
	for i, r := range runes {
		switch {
		case r == 0x130:
			out.WriteString("i̇")
		case r == 0x3A3:
			if finalSigma(runes, i) {
				out.WriteRune(0x3C2)
			} else {
				out.WriteRune(0x3C3)
			}
		default:
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
}

func finalSigma(runes []rune, i int) bool {
	before := false
	for j := i - 1; j >= 0; j-- {
		if isCaseIgnorable(runes[j]) {
			continue
		}
		before = isCased(runes[j])
		break
	}
	if !before {
		return false
	}
	for j := i + 1; j < len(runes); j++ {
		if isCaseIgnorable(runes[j]) {
			continue
		}
		return !isCased(runes[j])
	}
	return true
}

func isCaseIgnorable(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk) ||
		strings.ContainsRune("'.:·‘’․‧︓﹒﹕＇．：·״", r)
}

func isCased(r rune) bool { return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) }

// decimalValue is the value of a Unicode decimal digit (category Nd), as
// int() reads it; ok is false for anything else.
func decimalValue(r rune) (int, bool) {
	if r >= '0' && r <= '9' {
		return int(r - '0'), true
	}
	for _, rg := range unicode.Nd.R16 {
		if rune(rg.Lo) <= r && r <= rune(rg.Hi) {
			return int(r-rune(rg.Lo)) % 10, true
		}
	}
	for _, rg := range unicode.Nd.R32 {
		if rune(rg.Lo) <= r && r <= rune(rg.Hi) {
			return int(r-rune(rg.Lo)) % 10, true
		}
	}
	return 0, false
}

// pyIsDigit is str.isdigit(): decimal digits plus other digits (superscripts,
// circled digits) whose numeric type is Digit.
func pyIsDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.Is(unicode.Nd, r) {
			continue
		}
		if unicode.Is(unicode.No, r) && isDigitProperty(r) {
			continue
		}
		return false
	}
	return true
}

// isDigitProperty reports the No characters with Numeric_Type=Digit.
func isDigitProperty(r rune) bool {
	switch {
	case r == 0xB2 || r == 0xB3 || r == 0xB9:
		return true
	case r >= 0x1369 && r <= 0x1371:
		return true
	case r == 0x19DA:
		return true
	case r == 0x2070 || (r >= 0x2074 && r <= 0x2079):
		return true
	case r >= 0x2080 && r <= 0x2089:
		return true
	case r >= 0x2460 && r <= 0x2468:
		return true
	case r >= 0x2474 && r <= 0x247C:
		return true
	case r >= 0x2488 && r <= 0x2490:
		return true
	case r == 0x24EA || (r >= 0x24F5 && r <= 0x24FD) || r == 0x24FF:
		return true
	case r >= 0x2776 && r <= 0x277E, r >= 0x2780 && r <= 0x2788, r >= 0x278A && r <= 0x2792:
		return true
	case r >= 0x10A40 && r <= 0x10A43:
		return true
	case r >= 0x10E60 && r <= 0x10E68:
		return true
	case r >= 0x11052 && r <= 0x1105A:
		return true
	case r >= 0x1F100 && r <= 0x1F10A:
		return true
	}
	return false
}

// pyTypeName is type(v).__name__ for jsonx values.
func pyTypeName(v any) string {
	switch value := v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case *jsonx.Object:
		return "dict"
	case jsonx.Number:
		if value.IsInt() {
			return "int"
		}
		return "float"
	}
	return "object"
}

// ---- urllib.parse.urlsplit (Python 3.11.17), copied from internal/urlx ----

const (
	whatwgC0OrSpace = "\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\x0c\r\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f "
	schemeChars     = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+-."
)

var ipvFuture = regexp.MustCompile(`\Av[a-fA-F0-9]+\..+\z`)

var netlocUnsafe = map[rune]bool{
	0x2047: true, 0x2048: true, 0x2049: true, 0x2100: true, 0x2101: true, 0x2105: true, 0x2106: true,
	0x2A74: true, 0xFE13: true, 0xFE16: true, 0xFE55: true, 0xFE56: true, 0xFE5F: true, 0xFE6B: true,
	0xFF03: true, 0xFF0F: true, 0xFF1A: true, 0xFF1F: true, 0xFF20: true,
}

type splitResult struct {
	scheme, netloc, path, query, fragment string
}

// hostname is SplitResult.hostname ("" for None).
func (r splitResult) hostname() string {
	hostinfo := r.netloc
	if i := strings.LastIndex(hostinfo, "@"); i >= 0 {
		hostinfo = hostinfo[i+1:]
	}
	var hostname string
	if _, bracketed, ok := strings.Cut(hostinfo, "["); ok {
		hostname, _, _ = strings.Cut(bracketed, "]")
	} else {
		hostname, _, _ = strings.Cut(hostinfo, ":")
	}
	if hostname == "" {
		return ""
	}
	name, zone, found := strings.Cut(hostname, "%")
	hostname = pyLower(name)
	if found {
		hostname += "%" + zone
	}
	return hostname
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func checkBracketedHost(hostname string) error {
	if strings.HasPrefix(hostname, "v") {
		if !ipvFuture.MatchString(hostname) {
			return errors.New("IPvFuture address is invalid")
		}
		return nil
	}
	if _, ok := parseIPv4(hostname); ok {
		return errors.New("An IPv4 address cannot be in brackets")
	}
	if !validIPv6(hostname) {
		return fmt.Errorf("%s does not appear to be an IPv4 or IPv6 address", jsonx.Repr(hostname))
	}
	return nil
}

func checkBracketedNetloc(netloc string) error {
	hostnameAndPort := netloc
	if i := strings.LastIndex(netloc, "@"); i >= 0 {
		hostnameAndPort = netloc[i+1:]
	}
	var hostname string
	if before, bracketed, ok := strings.Cut(hostnameAndPort, "["); ok {
		if before != "" {
			return errors.New("Invalid IPv6 URL")
		}
		var port string
		hostname, port, _ = strings.Cut(bracketed, "]")
		if port != "" && !strings.HasPrefix(port, ":") {
			return errors.New("Invalid IPv6 URL")
		}
	} else {
		hostname, _, _ = strings.Cut(hostnameAndPort, ":")
	}
	return checkBracketedHost(hostname)
}

func checkNetloc(netloc string) error {
	if netloc == "" || isASCII(netloc) {
		return nil
	}
	for _, r := range netloc {
		if netlocUnsafe[r] {
			return fmt.Errorf("netloc '%s' contains invalid characters under NFKC normalization", netloc)
		}
	}
	return nil
}

// urlsplit is urllib.parse.urlsplit(url) for str. Errors are the ValueError
// Python raises.
func urlsplit(url string) (splitResult, error) {
	url = strings.TrimLeft(url, whatwgC0OrSpace)
	for _, c := range []string{"\t", "\r", "\n"} {
		url = strings.ReplaceAll(url, c, "")
	}
	var r splitResult
	if i := strings.Index(url, ":"); i > 0 && isASCIILetter(url[0]) {
		ok := true
		for j := 0; j < i; j++ {
			if strings.IndexByte(schemeChars, url[j]) < 0 {
				ok = false
				break
			}
		}
		if ok {
			r.scheme, url = strings.ToLower(url[:i]), url[i+1:]
		}
	}
	if strings.HasPrefix(url, "//") {
		delim := len(url)
		for _, c := range "/?#" {
			if w := strings.IndexRune(url[2:], c); w >= 0 && w+2 < delim {
				delim = w + 2
			}
		}
		r.netloc, url = url[2:delim], url[delim:]
		open, close := strings.Contains(r.netloc, "["), strings.Contains(r.netloc, "]")
		if open != close {
			return r, pyError("ValueError", "Invalid IPv6 URL")
		}
		if open && close {
			if err := checkBracketedNetloc(r.netloc); err != nil {
				return r, pyError("ValueError", err.Error())
			}
		}
	}
	if i := strings.Index(url, "#"); i >= 0 {
		url, r.fragment = url[:i], url[i+1:]
	}
	if i := strings.Index(url, "?"); i >= 0 {
		url, r.query = url[:i], url[i+1:]
	}
	if err := checkNetloc(r.netloc); err != nil {
		return r, pyError("ValueError", err.Error())
	}
	r.path = url
	return r, nil
}

// validIPv6 reports whether ipaddress.IPv6Address(text) accepts text
// (scope ids included, as Python 3.9+ does).
func validIPv6(text string) bool {
	_, ok := parseIPv6(text)
	return ok
}

// parseIPv6 parses an IPv6 address the way ipaddress.IPv6Address does; the
// zone ("%eth0") is accepted and dropped from the returned address.
func parseIPv6(text string) (netip.Addr, bool) {
	addr := text
	if i := strings.IndexByte(text, '%'); i >= 0 {
		zone := text[i+1:]
		if zone == "" || strings.Contains(zone, "%") {
			return netip.Addr{}, false
		}
		addr = text[:i]
	}
	if !strings.Contains(addr, ":") {
		return netip.Addr{}, false
	}
	// Embedded IPv4 tail: Python parses it with the strict IPv4 rules.
	if i := strings.LastIndexByte(addr, ':'); strings.Contains(addr[i+1:], ".") {
		v4, ok := parseIPv4(addr[i+1:])
		if !ok {
			return netip.Addr{}, false
		}
		b := v4.As4()
		addr = addr[:i+1] + fmt.Sprintf("%x:%x", int(b[0])<<8|int(b[1]), int(b[2])<<8|int(b[3]))
	}
	for _, part := range strings.Split(addr, ":") {
		if len(part) > 4 {
			return netip.Addr{}, false
		}
		for i := 0; i < len(part); i++ {
			c := part[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return netip.Addr{}, false
			}
		}
	}
	parsed, err := netip.ParseAddr(addr)
	if err != nil || !parsed.Is6() || parsed.Zone() != "" {
		return netip.Addr{}, false
	}
	return parsed, true
}

// parseIPv4 parses dotted-quad text the way ipaddress.IPv4Address does:
// exactly four ASCII decimal octets, no leading zeros, each at most 255.
func parseIPv4(text string) (netip.Addr, bool) {
	parts := strings.Split(text, ".")
	if len(parts) != 4 {
		return netip.Addr{}, false
	}
	var b [4]byte
	for i, part := range parts {
		if part == "" || len(part) > 3 {
			return netip.Addr{}, false
		}
		value := 0
		for j := 0; j < len(part); j++ {
			c := part[j]
			if c < '0' || c > '9' {
				return netip.Addr{}, false
			}
			value = value*10 + int(c-'0')
		}
		if value > 255 || (len(part) > 1 && part[0] == '0') {
			return netip.Addr{}, false
		}
		b[i] = byte(value)
	}
	return netip.AddrFrom4(b), true
}
