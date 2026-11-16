package oxidns

import (
	"math/big"
	"strings"

	"configflow/internal/pystr"
	"configflow/internal/rules"
)

// Python string/regex helpers of the C23 validators (CPython 3.11 str and
// re semantics for the patterns the modules use).

func isDecimalRune(r rune) bool { return pystr.Decimal(r) >= 0 }

// splitSpaceComma is [t for t in re.split(r'[\s,]+', s) if t].
func splitSpaceComma(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || pystr.IsSpace(r) })
}

// splitMax1 is s.split(None, 1).
func splitMax1(s string) []string {
	s = strings.TrimLeftFunc(s, pystr.IsSpace)
	if s == "" {
		return nil
	}
	end := strings.IndexFunc(s, pystr.IsSpace)
	if end < 0 {
		return []string{s}
	}
	rest := strings.TrimLeftFunc(s[end:], pystr.IsSpace)
	if rest == "" {
		return []string{s[:end]}
	}
	return []string{s[:end], rest}
}

// lstrip is s.lstrip().
func lstrip(s string) string { return strings.TrimLeftFunc(s, pystr.IsSpace) }

// rstripChars is s.rstrip(chars) for ASCII chars.
func rstripChars(s, chars string) string { return strings.TrimRight(s, chars) }

// asciiDigits reports re.fullmatch(r'[0-9]+', s).
func asciiDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// decimalDigits reports re.fullmatch(r'\d+', s) (Unicode decimals).
func decimalDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isDecimalRune(r) {
			return false
		}
	}
	return true
}

// boundedUint is the shared _uint shape: re.fullmatch(digits) and the value
// at most maximum (nil = None).
func boundedUint(raw any, maximum *big.Int, plus bool) *big.Int {
	text, ok := raw.(string)
	if !ok {
		return nil
	}
	body := text
	if plus {
		body = strings.TrimPrefix(text, "+")
	}
	if !asciiDigits(body) {
		return nil
	}
	digits := strings.TrimLeft(body, "0")
	if digits == "" {
		digits = "0"
	}
	value, _ := new(big.Int).SetString(digits, 10)
	if value.Cmp(maximum) > 0 {
		return nil
	}
	return value
}

var (
	bigU32 = big.NewInt(4294967295)
	bigU64 = u64
)

// regexCompileError is re.compile(raw) for a str pattern: "" when it
// compiles; Python's re.error as "error"; its RecursionError/OverflowError
// are raised. Patterns only Python can compile (\N{name}) count as compiled
// (accepted difference of the rules package).
func regexCompileError(raw string) bool {
	_, err := rules.CompilePyRegex(raw, false)
	if err == nil || !rules.IsPyRegexError(err) {
		return false
	}
	switch err.Error() {
	case "the repetition number is too large":
		raise(pyErr("OverflowError", err.Error()))
	case "maximum recursion depth exceeded":
		raise(pyErr("RecursionError", "maximum recursion depth exceeded"))
	}
	return true
}

// wordBoundaryAt is re's \b between text[i-1] and text[i] (runes).
func wordBoundaryAt(text []rune, i int) bool {
	before := i > 0 && isWordRune(text[i-1])
	after := i < len(text) && isWordRune(text[i])
	return before != after
}

// subWord is re.sub(r'\b' + name + r'\b', value, text) for an ASCII word
// name.
func subWord(text, name, value string) string {
	runes := []rune(text)
	word := []rune(name)
	var b strings.Builder
	i := 0
	for i < len(runes) {
		if i+len(word) <= len(runes) && string(runes[i:i+len(word)]) == name &&
			wordBoundaryAt(runes, i) && wordBoundaryAt(runes, i+len(word)) {
			b.WriteString(value)
			i += len(word)
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

// findTemplates returns the ${...} matches of re.finditer(r'\$\{([^}]*)\}',
// s) as [start, end) rune offsets of the whole match.
func findTemplates(s string) [][2]int {
	runes := []rune(s)
	var out [][2]int
	for i := 0; i+1 < len(runes); {
		if runes[i] == '$' && runes[i+1] == '{' {
			j := i + 2
			for j < len(runes) && runes[j] != '}' {
				j++
			}
			if j < len(runes) {
				out = append(out, [2]int{i, j + 1})
				i = j + 1
				continue
			}
		}
		i++
	}
	return out
}

// directiveLine is re.search(r'(?m)^\s*\$(?:INCLUDE|GENERATE)\b', raw,
// re.I): under re.I the "I" also matches U+0131 and U+0130 (simple case
// folding), the other letters ASCII only.
func directiveLine(raw string) bool {
	runes := []rune(raw)
	matchWord := func(at int, word string) int {
		for _, w := range word {
			if at >= len(runes) {
				return -1
			}
			c := runes[at]
			ok := c == w || c == w+('a'-'A')
			if w == 'I' && (c == 0x131 || c == 0x130) {
				ok = true
			}
			if !ok {
				return -1
			}
			at++
		}
		return at
	}
	for start := 0; start <= len(runes); start++ {
		if start > 0 && runes[start-1] != '\n' {
			continue
		}
		// \s* is greedy but may give back characters; '$' is not a space,
		// so the only candidate is after the whole run of spaces.
		i := start
		for i < len(runes) && pystr.IsSpace(runes[i]) {
			i++
		}
		if i >= len(runes) || runes[i] != '$' {
			continue
		}
		for _, word := range []string{"INCLUDE", "GENERATE"} {
			if end := matchWord(i+1, word); end >= 0 && wordBoundaryAt(runes, end) {
				return true
			}
		}
	}
	return false
}
