// Package textx holds small text helpers shared by several packages: digit
// checks and line splitting with the rules the web editor uses. It does not
// parse any data format.
package textx

// ASCIIDigits reports whether s is a nonempty run of ASCII digits 0-9.
func ASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
