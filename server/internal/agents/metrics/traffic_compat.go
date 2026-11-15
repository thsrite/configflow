package metrics

// Private copies of Python str/ipaddress behaviour owned by internal/rules
// (unexported there), for traffic_history._target_key.
// TODO(port): replace with exported helpers of internal/rules
// (parseIPAddress/formatIP in ipaddr.go, pyLower in pystr.go) if they are
// ever exported.

import (
	"net/netip"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ipAddressString is str(ipaddress.ip_address(text)) of CPython 3.11; ok
// is false where ip_address raises ValueError.
func ipAddressString(text string) (string, bool) {
	if text == "" || strings.ContainsAny(text, " \t\r\n\v\f/") {
		return "", false
	}
	addr, err := netip.ParseAddr(text)
	if err != nil {
		return "", false
	}
	zone := addr.Zone()
	if strings.Contains(zone, "%") {
		return "", false
	}
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

// pyLower is s.lower(): full Unicode lowercasing including U+0130 and the
// final sigma.
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
		switch r {
		case 0x130:
			b.WriteString("i̇")
		case 0x3a3:
			if finalSigma(runes, i) {
				b.WriteRune(0x3c2)
			} else {
				b.WriteRune(0x3c3)
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func caseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', 0xb7, 0x2019, 0x2018, 0x2024, 0x2027, 0xfe13, 0xfe52, 0xfe55, 0xff07, 0xff0e, 0xff1a:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

func cased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r)
}

// finalSigma implements the Final_Sigma condition of the Unicode standard.
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
