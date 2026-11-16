package oxidns

import "strings"

// fnmatchCase is CPython 3.11's fnmatch.fnmatchcase(name, pat): the pattern
// is translated exactly as fnmatch.translate does (including its handling
// of "[...]" sets with ranges, "!" negation and unterminated "["), and the
// resulting character classes are read the way sre_parse reads them. "*"
// matches any sequence (DOTALL), "?" any one character, the whole name must
// match.
func fnmatchCase(name, pat string) bool {
	return globMatch([]rune(name), translateGlob([]rune(pat)))
}

type globKind int

const (
	globLiteral globKind = iota
	globAny
	globStar
	globClass
	globNever
)

type globItem struct {
	lo, hi rune
}

type globToken struct {
	kind   globKind
	r      rune
	negate bool
	items  []globItem
}

// translateGlob is fnmatch.translate as tokens.
func translateGlob(pat []rune) []globToken {
	var res []globToken
	i, n := 0, len(pat)
	for i < n {
		c := pat[i]
		i++
		switch c {
		case '*':
			if len(res) == 0 || res[len(res)-1].kind != globStar {
				res = append(res, globToken{kind: globStar})
			}
		case '?':
			res = append(res, globToken{kind: globAny})
		case '[':
			j := i
			if j < n && pat[j] == '!' {
				j++
			}
			if j < n && pat[j] == ']' {
				j++
			}
			for j < n && pat[j] != ']' {
				j++
			}
			if j >= n {
				res = append(res, globToken{kind: globLiteral, r: '['})
				continue
			}
			stuff := string(pat[i:j])
			if !strings.Contains(stuff, "-") {
				stuff = strings.ReplaceAll(stuff, `\`, `\\`)
			} else {
				var chunks []string
				k := i + 1
				if pat[i] == '!' {
					k = i + 2
				}
				for {
					k = runeFind(pat, '-', k, j)
					if k < 0 {
						break
					}
					chunks = append(chunks, string(pat[i:k]))
					i = k + 1
					k = k + 3
				}
				chunk := string(pat[i:j])
				if chunk != "" {
					chunks = append(chunks, chunk)
				} else {
					chunks[len(chunks)-1] += "-"
				}
				// Remove empty ranges -- invalid in RE.
				for k := len(chunks) - 1; k > 0; k-- {
					prev, next := []rune(chunks[k-1]), []rune(chunks[k])
					if len(prev) == 0 || len(next) == 0 {
						raise(pyErr("IndexError", "string index out of range"))
					}
					if prev[len(prev)-1] > next[0] {
						chunks[k-1] = string(prev[:len(prev)-1]) + string(next[1:])
						chunks = append(chunks[:k], chunks[k+1:]...)
					}
				}
				escaped := make([]string, len(chunks))
				for index, s := range chunks {
					escaped[index] = strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "-", `\-`)
				}
				stuff = strings.Join(escaped, "-")
			}
			// Escape set operations (&&, ~~ and ||).
			var b strings.Builder
			for _, r := range stuff {
				if r == '&' || r == '~' || r == '|' {
					b.WriteRune('\\')
				}
				b.WriteRune(r)
			}
			stuff = b.String()
			i = j + 1
			switch {
			case stuff == "":
				res = append(res, globToken{kind: globNever})
			case stuff == "!":
				res = append(res, globToken{kind: globAny})
			default:
				negate := false
				if stuff[0] == '!' {
					negate, stuff = true, stuff[1:]
				}
				res = append(res, globToken{kind: globClass, negate: negate, items: parseGlobClass([]rune(stuff))})
			}
		default:
			res = append(res, globToken{kind: globLiteral, r: c})
		}
	}
	return res
}

// runeFind is str.find(sub, start, end) for a one-rune sub.
func runeFind(s []rune, r rune, start, end int) int {
	if end > len(s) {
		end = len(s)
	}
	for k := max(start, 0); k < end; k++ {
		if s[k] == r {
			return k
		}
	}
	return -1
}

// parseGlobClass reads the body of a translated set as sre_parse does: a
// backslash escapes the next character, "a-b" is a range, a "-" before the
// end is a literal.
func parseGlobClass(body []rune) []globItem {
	var items []globItem
	pos := 0
	next := func() (rune, bool) {
		if pos >= len(body) {
			return 0, false
		}
		r := body[pos]
		pos++
		if r == '\\' && pos < len(body) {
			r = body[pos]
			pos++
		}
		return r, true
	}
	for pos < len(body) {
		lo, _ := next()
		if pos < len(body) && body[pos] == '-' {
			pos++
			if pos >= len(body) {
				items = append(items, globItem{lo, lo}, globItem{'-', '-'})
				break
			}
			hi, _ := next()
			if hi < lo {
				raise(pyErr("error", "bad character range"))
			}
			items = append(items, globItem{lo, hi})
			continue
		}
		items = append(items, globItem{lo, lo})
	}
	return items
}

func (t globToken) matches(r rune) bool {
	switch t.kind {
	case globLiteral:
		return r == t.r
	case globAny:
		return true
	case globClass:
		in := false
		for _, item := range t.items {
			if item.lo <= r && r <= item.hi {
				in = true
				break
			}
		}
		return in != t.negate
	}
	return false
}

// globMatch is a full match of name against the tokens.
func globMatch(name []rune, tokens []globToken) bool {
	// reachable[i]: tokens consumed so far can match name[:i].
	reachable := make([]bool, len(name)+1)
	reachable[0] = true
	for _, token := range tokens {
		next := make([]bool, len(name)+1)
		if token.kind == globStar {
			seen := false
			for i := range reachable {
				seen = seen || reachable[i]
				next[i] = seen
			}
		} else {
			for i := 0; i < len(name); i++ {
				if reachable[i] && token.matches(name[i]) {
					next[i+1] = true
				}
			}
		}
		reachable = next
	}
	return reachable[len(name)]
}
