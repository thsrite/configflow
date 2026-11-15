package metrics

// Private copies of Python str/ipaddress behaviour owned by internal/rules
// (unexported there), for traffic_history._target_key.
// TODO(port): replace with exported helpers of internal/rules
// (parseIPAddress/formatIP in ipaddr.go, pyLower in pystr.go) if they are
// ever exported.

import (
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ipAddressString is str(ipaddress.ip_address(text)) of CPython 3.11; ok
// is false where ip_address raises ValueError.
func ipAddressString(text string) (string, bool) {
	// IPv6Address refuses any "/"; the scope id after the first "%" may be
	// any non-empty text without another "%" (spaces, case and non-ASCII
	// are kept as they are). The address part is checked by netip, which
	// refuses whitespace there as Python's hextet/octet parsers do.
	if text == "" || strings.Contains(text, "/") {
		return "", false
	}
	if _, zone, found := strings.Cut(text, "%"); found && strings.Contains(zone, "%") {
		return "", false
	}
	addr, err := netip.ParseAddr(text)
	if err != nil {
		return "", false
	}
	zone := addr.Zone()
	if addr.Is4() {
		b := addr.As4()
		return strconv.Itoa(int(b[0])) + "." + strconv.Itoa(int(b[1])) + "." + strconv.Itoa(int(b[2])) + "." + strconv.Itoa(int(b[3])), true
	}
	b := addr.As16()
	var hi, lo uint64
	for i := 0; i < 8; i++ {
		hi = hi<<8 | uint64(b[i])
		lo = lo<<8 | uint64(b[8+i])
	}
	text = formatIPv6(hi, lo)
	if zone != "" {
		text += "%" + zone
	}
	return text, true
}

// formatIPv6 is str(IPv6Address): compressed (longest zero run of two or
// more groups), IPv4-mapped addresses with a dotted tail (CPython 3.11.17).
func formatIPv6(hi, lo uint64) string {
	if hi == 0 && lo>>32 == 0xffff {
		x := uint32(lo)
		return "::ffff:" + strconv.Itoa(int(x>>24)) + "." + strconv.Itoa(int(x>>16&255)) + "." +
			strconv.Itoa(int(x>>8&255)) + "." + strconv.Itoa(int(x&255))
	}
	var groups [8]uint64
	for i := 0; i < 4; i++ {
		groups[i] = hi >> (48 - 16*uint(i)) & 0xffff
		groups[4+i] = lo >> (48 - 16*uint(i)) & 0xffff
	}
	bestStart, bestLen := -1, 0
	for i := 0; i < 8; {
		if groups[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && groups[j] == 0 {
			j++
		}
		if j-i > bestLen {
			bestStart, bestLen = i, j-i
		}
		i = j
	}
	if bestLen < 2 {
		bestStart = -1
	}
	var b strings.Builder
	for i := 0; i < 8; i++ {
		if i == bestStart {
			b.WriteString("::")
			i += bestLen - 1
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), ":") {
			b.WriteByte(':')
		}
		b.WriteString(strconv.FormatUint(groups[i], 16))
	}
	return b.String()
}

// pyLower is s.lower() of CPython 3.11: the full lowercase mapping of
// Unicode 14.0 (pyLowerTable, generated from Python: Go's own tables are
// newer) and the Final_Sigma rule for U+03A3.
func pyLower(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if r == 0x3a3 {
			if finalSigma(runes, i) {
				b.WriteRune(0x3c2)
			} else {
				b.WriteRune(0x3c3)
			}
			continue
		}
		if lower, ok := pyLowerTable[r]; ok {
			b.WriteString(lower)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func inRuneRanges(table [][2]rune, r rune) bool {
	i := sort.Search(len(table), func(i int) bool { return table[i][1] >= r })
	return i < len(table) && table[i][0] <= r
}

func caseIgnorable(r rune) bool { return inRuneRanges(pyCaseIgnorableRanges, r) }

// cased is _PyUnicode_IsCased for a character that is not case-ignorable.
func cased(r rune) bool { return inRuneRanges(pyCasedRanges, r) }

// finalSigma implements handle_capital_sigma of CPython's unicodeobject.c.
func finalSigma(runes []rune, i int) bool {
	before := false
	for j := i - 1; j >= 0; j-- {
		if caseIgnorable(runes[j]) {
			continue
		}
		before = cased(runes[j])
		break
	}
	if !before {
		return false
	}
	for j := i + 1; j < len(runes); j++ {
		if caseIgnorable(runes[j]) {
			continue
		}
		return !cased(runes[j])
	}
	return true
}
