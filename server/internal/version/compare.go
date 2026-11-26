package version

import (
	"configflow/internal/textx"
	"strconv"
	"strings"
)

// part parses one dotted component: ASCII digits that fit in a uint64.
// Signs, underscores, spaces and overflow make it invalid.
func part(text string) (uint64, bool) {
	if text == "" || text[0] < '0' || text[0] > '9' {
		return 0, false
	}
	n, err := strconv.ParseUint(text, 10, 64)
	return n, err == nil
}

// compareDigits compares two ASCII digit runs as numbers of any length.
func compareDigits(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// CompareAgent compares two Agent versions and returns -1, 0 or 1. A "-suffix"
// (such as "-go") is ignored, the shorter list of dotted components is padded
// with "0", and each pair of components is compared as numbers when both are
// numeric (ASCII digits of any length), otherwise as text.
func CompareAgent(a, b string) int {
	left := strings.Split(strings.SplitN(a, "-", 2)[0], ".")
	right := strings.Split(strings.SplitN(b, "-", 2)[0], ".")
	for len(left) < len(right) {
		left = append(left, "0")
	}
	for len(right) < len(left) {
		right = append(right, "0")
	}
	for i := range left {
		if textx.ASCIIDigits(left[i]) && textx.ASCIIDigits(right[i]) {
			if c := compareDigits(left[i], right[i]); c != 0 {
				return c
			}
			continue
		}
		if c := strings.Compare(left[i], right[i]); c != 0 {
			return c
		}
	}
	return 0
}

// Release is a parsed release version such as "v2.43.1": its numeric dotted
// components. The zero value (nil) is an unparsable version.
type Release []uint64

// ParseRelease parses a release version after trimming spaces and any leading
// "v"/"V". Every dotted component must be numeric; otherwise it returns nil.
func ParseRelease(text string) Release {
	text = strings.TrimLeft(strings.TrimSpace(text), "vV")
	parts := strings.Split(text, ".")
	out := make(Release, 0, len(parts))
	for _, p := range parts {
		n, ok := part(p)
		if !ok {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// Compare compares r with o component by component; when one is a prefix of
// the other, the longer one is newer ("1.0.0" > "1.0").
func (r Release) Compare(o Release) int {
	for i := 0; i < len(r) && i < len(o); i++ {
		if r[i] != o[i] {
			if r[i] < o[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(r) < len(o):
		return -1
	case len(r) > len(o):
		return 1
	}
	return 0
}

// SameRelease reports whether a and b parse to the same release; two
// unparsable versions are the same.
func SameRelease(a, b string) bool {
	x, y := ParseRelease(a), ParseRelease(b)
	if (x == nil) != (y == nil) {
		return false
	}
	return x.Compare(y) == 0
}

// NewerRelease reports whether candidate is a valid release newer than
// current; any valid candidate is newer than an unparsable current.
func NewerRelease(candidate, current string) bool {
	x, y := ParseRelease(candidate), ParseRelease(current)
	return x != nil && (y == nil || x.Compare(y) > 0)
}
