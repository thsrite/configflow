package agents

import (
	"math/big"
	"strings"
	"unicode"

	"configflow/internal/agents/diag"
	"configflow/internal/jsonx"
)

// ChangelogEntry is one VERSION_CHANGELOG item.
type ChangelogEntry struct {
	Version  string
	Date     string
	Features []string
}

// Object is the entry as Python stores it: {"date": ..., "features": [...]}.
func (e ChangelogEntry) Object() *jsonx.Object {
	return jsonx.Obj("date", e.Date, "features", jsonx.ListOf(e.Features))
}

// Changelog is VERSION_CHANGELOG: every entry, newest first (the dict's
// declaration order).
func Changelog() []ChangelogEntry {
	out := make([]ChangelogEntry, len(versionChangelog))
	copy(out, versionChangelog)
	return out
}

// ChangelogObject is VERSION_CHANGELOG as an ordered object.
func ChangelogObject() *jsonx.Object {
	result := jsonx.NewObject()
	for _, entry := range versionChangelog {
		result.Set(entry.Version, entry.Object())
	}
	return result
}

// VersionChangelog is VERSION_CHANGELOG.get(version).
func VersionChangelog(version string) (ChangelogEntry, bool) {
	for _, entry := range versionChangelog {
		if entry.Version == version {
			return entry, true
		}
	}
	return ChangelogEntry{}, false
}

// GetLatestVersion is get_latest_version().
func GetLatestVersion() string { return LatestAgentVersion }

// CompareVersions is compare_versions(version1, version2): -1, 0 or 1. The
// "-suffix" is dropped, the dotted parts are padded with "0" and compared as
// integers where both parse (Python int()), otherwise as strings.
func CompareVersions(version1, version2 string) int {
	v1 := strings.Split(strings.SplitN(version1, "-", 2)[0], ".")
	v2 := strings.Split(strings.SplitN(version2, "-", 2)[0], ".")
	for len(v1) < len(v2) {
		v1 = append(v1, "0")
	}
	for len(v2) < len(v1) {
		v2 = append(v2, "0")
	}
	for i := range v1 {
		n1, ok1 := pyIntText(v1[i])
		n2, ok2 := pyIntText(v2[i])
		if ok1 && ok2 {
			if c := n1.Cmp(n2); c != 0 {
				return c
			}
			continue
		}
		// int() failed on either side: compare the texts.
		if v1[i] < v2[i] {
			return -1
		} else if v1[i] > v2[i] {
			return 1
		}
	}
	return 0
}

// HasUpdate is has_update(current_version).
func HasUpdate(currentVersion string) bool {
	return CompareVersions(currentVersion, LatestAgentVersion) < 0
}

// maxStrDigits is CPython 3.11's default sys.get_int_max_str_digits(): int()
// of a longer decimal text raises ValueError.
const maxStrDigits = 4300

// pyIntText is int(text) for a str: surrounding whitespace, an optional
// sign, decimal digits (any Unicode Nd) with single underscores between
// them, at most 4300 digits. CPython rewrites non-ASCII whitespace to spaces
// first and keeps ASCII characters, so whitespace is " \t\n\v\f\r" or a
// non-ASCII str.isspace() character (U+001C–U+001F do not count).
func pyIntText(text string) (*big.Int, bool) {
	isSpace := func(r rune) bool {
		return r == ' ' || (r >= '\t' && r <= '\r') || (r >= 127 && diag.IsPySpace(r))
	}
	s := strings.TrimFunc(text, isSpace)
	negative := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		negative = s[0] == '-'
		s = s[1:]
	}
	if s == "" {
		return nil, false
	}
	var digits strings.Builder
	previousUnderscore := true
	for _, r := range s {
		switch {
		case r == '_':
			if previousUnderscore {
				return nil, false
			}
			previousUnderscore = true
		case unicode.IsDigit(r):
			digits.WriteByte(byte('0' + digitValue(r)))
			previousUnderscore = false
		default:
			return nil, false
		}
	}
	if previousUnderscore || digits.Len() > maxStrDigits {
		return nil, false
	}
	value, ok := new(big.Int).SetString(digits.String(), 10)
	if !ok {
		return nil, false
	}
	if negative {
		value.Neg(value)
	}
	return value, true
}

// digitValue is the decimal value of a Unicode Nd character. Nd characters
// come in contiguous runs of complete 0–9 sequences (some runs hold several
// sequences back to back, such as the mathematical digits U+1D7CE–U+1D7FF),
// so the value is the distance from the start of the run modulo 10.
func digitValue(r rune) int {
	if r >= '0' && r <= '9' {
		return int(r - '0')
	}
	start := r
	for start > 0 && unicode.IsDigit(start-1) {
		start--
	}
	return int(r-start) % 10
}
