package rules

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/dlclark/regexp2"
)

// Substitution and match objects for PyRegex: re.sub / Pattern.sub with a
// function or a replacement template, and re.search returning a match.
//
// Both run on the backtracking engine (regexp2), whatever engine Search
// uses, because only it can resume a search at an offset with the text
// before it as context and require a non-empty match there: CPython's sub
// loop (pattern_subx with state->must_advance) accepts an empty match right
// after a non-empty one and, after an empty match, a non-empty match at the
// same position; Go's regexp skips both. Positions are code points, like
// Python string indices.

// ErrMatchTimeout reports that the backtracking engine exceeded
// BacktrackTimeout during Sub or Match (Python would keep running).
var ErrMatchTimeout = errors.New("regex match exceeded the time limit")

// TemplateIndexError is the IndexError re.sub raises for an unknown group
// name in a replacement template (`\g<name>`).
type TemplateIndexError struct{ Message string }

func (e *TemplateIndexError) Error() string { return e.Message }

type subEngines struct {
	once     sync.Once
	any      *regexp2.Regexp // the pattern
	nonEmpty *regexp2.Regexp // \G(?:pattern)(?!\G): a non-empty match at the start position
	err      error
}

func (r *PyRegex) engines() (*subEngines, error) {
	r.sub.once.Do(func() {
		source, err := translateNet(r.parsed)
		if err != nil {
			r.sub.err = err
			return
		}
		compile := func(src string) *regexp2.Regexp {
			re, err := regexp2.Compile(src, regexp2.None)
			if err != nil {
				r.sub.err = &UnsupportedRegexError{Reason: err.Error()}
				return nil
			}
			re.MatchTimeout = BacktrackTimeout
			return re
		}
		r.sub.any = compile(source)
		if r.sub.err == nil {
			r.sub.nonEmpty = compile(`\G(?:` + source + `)(?!\G)`)
		}
	})
	return &r.sub, r.sub.err
}

// Groups is Pattern.groups: the number of capturing groups.
func (r *PyRegex) Groups() int { return r.parsed.groups }

// GroupIndex is Pattern.groupindex (a copy).
func (r *PyRegex) GroupIndex() map[string]int {
	out := make(map[string]int, len(r.parsed.groupdict))
	for k, v := range r.parsed.groupdict {
		out[k] = v
	}
	return out
}

// Match is a re.Match.
type Match struct {
	text  []rune
	spans []int // start, end per group; -1 when the group did not take part
	names map[string]int
}

// Group is match.group(i): the text and whether the group took part (Python
// returns None when it did not). ok is false too for an index out of range;
// use Groups to tell them apart.
func (m *Match) Group(i int) (text string, ok bool) {
	if i < 0 || 2*i+1 >= len(m.spans) || m.spans[2*i] < 0 {
		return "", false
	}
	return string(m.text[m.spans[2*i]:m.spans[2*i+1]]), true
}

// Groups is the number of capturing groups of the pattern.
func (m *Match) Groups() int { return len(m.spans)/2 - 1 }

// NamedGroup is match.group(name); ok false for a name the pattern lacks or
// a group that did not take part.
func (m *Match) NamedGroup(name string) (string, bool) {
	i, found := m.names[name]
	if !found {
		return "", false
	}
	return m.Group(i)
}

// Span is match.span(i) in code points; (-1, -1) for a group that did not
// take part.
func (m *Match) Span(i int) (int, int) { return m.spans[2*i], m.spans[2*i+1] }

func (r *PyRegex) newMatch(text []rune, found *regexp2.Match) *Match {
	m := &Match{text: text, spans: make([]int, 2*(r.parsed.groups+1)), names: r.parsed.groupdict}
	for i := 0; i <= r.parsed.groups; i++ {
		m.spans[2*i], m.spans[2*i+1] = -1, -1
		if g := found.GroupByNumber(i); g != nil && len(g.Captures) > 0 {
			m.spans[2*i], m.spans[2*i+1] = g.Index, g.Index+g.Length
		}
	}
	return m
}

// find is one step of sre_search from pos; after an empty match
// (mustAdvance) a match at pos must not be empty.
func (r *PyRegex) find(e *subEngines, text []rune, pos int, mustAdvance bool) (*regexp2.Match, error) {
	if pos > len(text) {
		return nil, nil
	}
	if mustAdvance {
		found, err := e.nonEmpty.FindRunesMatchStartingAt(text, pos)
		if err != nil {
			return nil, ErrMatchTimeout
		}
		if found != nil {
			return found, nil
		}
		pos++
		if pos > len(text) {
			return nil, nil
		}
	}
	found, err := e.any.FindRunesMatchStartingAt(text, pos)
	if err != nil {
		return nil, ErrMatchTimeout
	}
	return found, nil
}

// Match is re.search(pattern, text): the first match, or nil.
func (r *PyRegex) Match(text string) (*Match, error) {
	e, err := r.engines()
	if err != nil {
		return nil, err
	}
	runes := []rune(text)
	found, err := r.find(e, runes, 0, false)
	if err != nil || found == nil {
		return nil, err
	}
	return r.newMatch(runes, found), nil
}

// Sub is pattern.sub(repl, text, count) with a function replacement; count
// 0 replaces every match. An error returned by repl stops the substitution
// and is returned as is.
func (r *PyRegex) Sub(text string, repl func(*Match) (string, error), count int) (string, error) {
	e, err := r.engines()
	if err != nil {
		return "", err
	}
	runes := []rune(text)
	var out strings.Builder
	last, pos, n := 0, 0, 0
	mustAdvance := false
	for count <= 0 || n < count {
		found, err := r.find(e, runes, pos, mustAdvance)
		if err != nil {
			return "", err
		}
		if found == nil {
			break
		}
		begin, end := found.Index, found.Index+found.Length
		out.WriteString(string(runes[last:begin]))
		replacement, err := repl(r.newMatch(runes, found))
		if err != nil {
			return "", err
		}
		out.WriteString(replacement)
		last, pos = end, end
		n++
		mustAdvance = begin == end
	}
	out.WriteString(string(runes[last:]))
	return out.String(), nil
}

// SubTemplate is pattern.sub(template, text, count) with a replacement
// string: group references (\1, \g<1>, \g<name>) and the escapes of Python's
// re._parser.parse_template. Template errors are *reError (re.error, with
// the position in the template) or *TemplateIndexError, raised before any
// matching, as in Python.
func (r *PyRegex) SubTemplate(text, template string, count int) (string, error) {
	parts, err := r.ParseTemplate(template)
	if err != nil {
		return "", err
	}
	return r.Sub(text, func(m *Match) (string, error) { return parts.Expand(m), nil }, count)
}

// Template is a parsed replacement template.
type Template struct {
	literals []string // "" placeholders at group positions
	groups   []templateGroup
}

type templateGroup struct{ literal, index int }

// Expand is re._parser.expand_template: a group that did not take part
// gives "".
func (t *Template) Expand(m *Match) string {
	literals := append([]string(nil), t.literals...)
	for _, g := range t.groups {
		text, _ := m.Group(g.index)
		literals[g.literal] = text
	}
	return strings.Join(literals, "")
}

// maxGroups is _sre.MAXGROUPS on 64-bit CPython 3.11.
const maxGroups = 1073741823

var templateEscapes = map[string]rune{
	`\a`: '\a', `\b`: '\b', `\f`: '\f', `\n`: '\n', `\r`: '\r', `\t`: '\t', `\v`: '\v', `\\`: '\\',
}

// ParseTemplate is re._parser.parse_template(template, pattern).
func (r *PyRegex) ParseTemplate(template string) (result *Template, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			switch e := recovered.(type) {
			case *reError:
				result, err = nil, e
			case *TemplateIndexError:
				result, err = nil, e
			default:
				panic(recovered)
			}
		}
	}()
	runes := []rune(template)
	s := &tokenizer{src: runes, pattern: runes}
	s.advance()
	t := &Template{}
	var literal strings.Builder
	hasLiteral := false
	flush := func() {
		if hasLiteral {
			t.literals = append(t.literals, literal.String())
			literal.Reset()
			hasLiteral = false
		}
	}
	addgroup := func(index, pos int) {
		if index > r.parsed.groups {
			s.fail(fmt.Sprintf("invalid group reference %d", index), pos)
		}
		flush()
		t.groups = append(t.groups, templateGroup{len(t.literals), index})
		t.literals = append(t.literals, "")
	}
	appendLiteral := func(text string) {
		literal.WriteString(text)
		hasLiteral = true
	}
	nextIn := func(valid func(rune) bool) bool {
		if !s.hasNext {
			return false
		}
		next := []rune(s.next)
		return len(next) == 1 && valid(next[0])
	}
	for {
		this, ok := s.get()
		if !ok {
			break
		}
		thisRunes := []rune(this)
		if thisRunes[0] != '\\' {
			appendLiteral(this)
			continue
		}
		c := thisRunes[1]
		switch {
		case c == 'g':
			if !s.match("<") {
				s.fail("missing <", 0)
			}
			name := s.getuntil(">", "group name")
			nameLen := len([]rune(name))
			var index int
			if pyIsIdentifier(name) {
				i, found := r.parsed.groupdict[name]
				if !found {
					panic(&TemplateIndexError{fmt.Sprintf("unknown group name %s", pyReprString(name))})
				}
				index = i
			} else {
				value, valid := pyIntLiteral(name)
				if !valid || value < 0 {
					s.fail(fmt.Sprintf("bad character in group name %s", pyReprString(name)), nameLen+1)
				}
				if value >= maxGroups {
					s.fail(fmt.Sprintf("invalid group reference %d", value), nameLen+1)
				}
				index = int(value)
			}
			addgroup(index, nameLen+1)
		case c == '0':
			if nextIn(isOct) {
				next, _ := s.get()
				this += next
				if nextIn(isOct) {
					next, _ := s.get()
					this += next
				}
			}
			value, _ := strconv.ParseInt(this[1:], 8, 32)
			appendLiteral(string(rune(value & 0xff)))
		case isDigitR(c):
			isOctal := false
			if nextIn(isDigitR) {
				next, _ := s.get()
				this += next
				thisRunes = []rune(this)
				if isOct(c) && isOct(thisRunes[2]) && nextIn(isOct) {
					next, _ := s.get()
					this += next
					isOctal = true
					value, _ := strconv.ParseInt(this[1:], 8, 32)
					if value > 0o377 {
						s.fail(fmt.Sprintf("octal escape value %s outside of range 0-0o377", this), len([]rune(this)))
					}
					appendLiteral(string(rune(value)))
				}
			}
			if !isOctal {
				value, _ := strconv.Atoi(this[1:])
				addgroup(value, len([]rune(this))-1)
			}
		default:
			if escaped, ok := templateEscapes[this]; ok {
				appendLiteral(string(escaped))
			} else {
				if isASCIILetter(c) {
					s.fail(fmt.Sprintf("bad escape %s", this), len(thisRunes))
				}
				appendLiteral(this)
			}
		}
	}
	flush()
	return t, nil
}

// pyIntLiteral is int(text) for a str: surrounding whitespace, a sign,
// underscores between digits and Unicode decimal digits are accepted.
// Values beyond int64 saturate (they only meet the MAXGROUPS check).
func pyIntLiteral(text string) (int64, bool) {
	text = strings.TrimFunc(text, pyIsSpace)
	negative := false
	if strings.HasPrefix(text, "+") || strings.HasPrefix(text, "-") {
		negative = text[0] == '-'
		text = text[1:]
	}
	if text == "" {
		return 0, false
	}
	var value int64
	previousUnderscore := true
	for _, r := range text {
		if r == '_' {
			if previousUnderscore {
				return 0, false
			}
			previousUnderscore = true
			continue
		}
		digit := decimalValue(r)
		if digit < 0 {
			return 0, false
		}
		previousUnderscore = false
		if value < 1<<60 {
			value = value*10 + int64(digit)
		}
	}
	if previousUnderscore {
		return 0, false
	}
	if negative {
		value = -value
	}
	return value, true
}

// decimalValue is the Unicode decimal digit value of r (category Nd), or -1.
func decimalValue(r rune) int {
	if r >= '0' && r <= '9' {
		return int(r - '0')
	}
	if !unicode.Is(unicode.Nd, r) {
		return -1
	}
	// Nd digits come in contiguous runs of whole decades starting at a zero.
	zero := r
	for zero > 0 && unicode.Is(unicode.Nd, zero-1) {
		zero--
	}
	return int(r-zero) % 10
}
