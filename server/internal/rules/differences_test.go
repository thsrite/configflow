package rules

import (
	"strings"
	"unicode"
)

// Accepted differences from the recorded Python results (functional parity,
// see the S-rules section of server/parity/PROGRESS-config.md). Each
// predicate names one class; the corpus tests skip or tolerate exactly these.
// Native \b/\B, $ and rule-file line endings have no skip predicates:
// regex_review_test.go and matcher_test.go assert their Go-native behavior,
// and CPython boundary expectations were removed from the corpus generators.

// scopedNetwork: an IPv6 scope id inside a network ("fe80::%eth0/64");
// Python keeps it, net/netip refuses zones in prefixes.
func scopedNetwork(text string) bool {
	return strings.Contains(text, "%")
}

// turkishI: Python's IGNORECASE folds İ/ı to i (and ſ to s); RE2 folds
// ſ but not İ/ı, regexp2 (Match, Sub, look-around patterns) none of them.
func turkishI(text string) bool {
	return strings.ContainsAny(text, "İıſ")
}

// regexMoreLenient: patterns Python's re rejects but the Go engines run
// (variable-width look-behind, a class escape as range end, flags not at the
// start or turned on and off, "(?)", an anchor repeated, "\N" without a
// name). Never the other way round: a pattern Python accepts must compile.
func regexMoreLenient(pythonRejects bool, goErr error) bool {
	return pythonRejects && (goErr == nil || !IsPyRegexError(goErr))
}

// hugeRepeat: repeat counts between 2^31 and 2^32-2 are valid in Python;
// the Go engines stop at int32 (ErrRepeatTooLarge).
func hugeRepeat(pattern string) bool {
	return strings.Contains(pattern, "4294967294")
}

// idna2003Only: host names only the IDNA 2003 codec accepts: an ASCII label
// with a fake "xn--" prefix, a label starting with a combining mark.
func idna2003Only(name string) bool {
	if strings.Contains(strings.ToLower(name), "xn--") {
		return true
	}
	for _, label := range strings.FieldsFunc(name, func(r rune) bool { return r == '.' }) {
		if r := []rune(label)[0]; unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me) {
			return true
		}
	}
	return false
}

// specialUpper: characters whose upper case is several characters in
// Python ('ß'.upper() == 'SS'); Go's strings.ToUpper keeps them.
func specialUpper(text string) bool {
	for _, r := range text {
		if unicode.IsLower(r) && unicode.ToUpper(r) == r || r >= 0x1f80 && r <= 0x1fff {
			return true
		}
	}
	return false
}
