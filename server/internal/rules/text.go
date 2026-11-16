package rules

import (
	"iter"
	"net/netip"
	"strings"

	"golang.org/x/net/idna"

	"configflow/internal/netipx"
)

// idnaProfile converts internationalised host names for DNS: UTS #46
// mapping (full-width forms, upper case, ß → ss like IDNA2003), label
// lengths checked. Host name characters are checked by the callers.
var idnaProfile = idna.New(idna.MapForLookup(), idna.Transitional(true), idna.BidiRule(),
	idna.StrictDomainName(false), idna.CheckHyphens(false), idna.VerifyDNSLength(true))

// IDNAEncode is the ASCII (punycode) form of a host name; ok is false when
// the name cannot be encoded. A trailing root dot is kept.
func IDNAEncode(name string) (string, bool) {
	if name == "" {
		return "", true
	}
	trimmed := strings.TrimSuffix(name, ".")
	if isASCII(name) { // already the DNS form; only label lengths are checked
		for _, label := range strings.Split(trimmed, ".") {
			if label == "" && trimmed != "" || len(label) > 63 {
				return "", false
			}
		}
		return name, true
	}
	ascii, err := idnaProfile.ToASCII(trimmed)
	if err != nil || trimmed == "" {
		return "", false
	}
	if trimmed != name {
		ascii += "."
	}
	return ascii, true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// parseAddr parses an IPv4 or IPv6 address (IPv6 may carry a zone, which
// cannot hold "%" or "/": "fe80::1%eth0/64" is a network).
func parseAddr(text string) (netip.Addr, bool) {
	if strings.Contains(text, "/") || strings.Count(text, "%") > 1 {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(text)
	return addr, err == nil
}

// parseNetwork parses a network with host bits allowed: "addr", "addr/len"
// or an IPv4 netmask/hostmask.
func parseNetwork(text string) (netip.Prefix, bool) {
	return netipx.ParseNetwork(text, false)
}

// IsValidIP reports whether query is an IPv4 or IPv6 address.
func IsValidIP(query string) bool {
	_, ok := parseAddr(query)
	return ok
}

// IPAddressVersion is 4 or 6 for an address, 0 otherwise.
func IPAddressVersion(text string) int {
	addr, ok := parseAddr(text)
	switch {
	case !ok:
		return 0
	case addr.Is4():
		return 4
	}
	return 6
}

// lines yields CR, LF and CRLF-delimited rule-file lines, without adding an
// empty line after a final delimiter. Other Unicode characters remain content.
func lines(content string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for content != "" {
			end, separatorSize := strings.IndexAny(content, "\r\n"), 1
			if end < 0 {
				yield(content)
				return
			}
			if !yield(content[:end]) {
				return
			}
			if strings.HasPrefix(content[end:], "\r\n") {
				separatorSize = 2
			}
			content = content[end+separatorSize:]
		}
	}
}

// closestMatch is the candidate most similar to word (Ratcliff/Obershelp:
// twice the matched characters over the total length) if the similarity is
// at least cutoff; on a tie the alphabetically last candidate wins.
func closestMatch(word string, candidates []string, cutoff float64) (string, bool) {
	best, bestScore := "", -1.0
	w := []rune(word)
	for _, candidate := range candidates {
		c := []rune(candidate)
		if len(c)+len(w) == 0 {
			continue
		}
		score := 2 * float64(matchedRunes(c, w)) / float64(len(c)+len(w))
		if score >= cutoff && (score > bestScore || score == bestScore && candidate > best) {
			best, bestScore = candidate, score
		}
	}
	return best, bestScore >= 0
}

// matchedRunes counts the characters of the longest common substring of a
// and b (the first one found), plus recursively those left and right of it.
func matchedRunes(a, b []rune) int {
	size, ai, bi := 0, 0, 0
	previous := make([]int, len(b)+1)
	for i := range a {
		current := make([]int, len(b)+1)
		for j := range b {
			if a[i] == b[j] {
				current[j+1] = previous[j] + 1
				if current[j+1] > size {
					size, ai, bi = current[j+1], i+1-current[j+1], j+1-current[j+1]
				}
			}
		}
		previous = current
	}
	if size == 0 {
		return 0
	}
	return size + matchedRunes(a[:ai], b[:bi]) + matchedRunes(a[ai+size:], b[bi+size:])
}
