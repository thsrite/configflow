package rules

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/dlclark/regexp2"

	"configflow/internal/logx"
)

// Regular expressions users store (DOMAIN-REGEX rules, node and provider
// filters, renaming rules) are written in Python's re syntax. They run on
// Go's engines: RE2 (package regexp) whenever it can express the pattern,
// otherwise regexp2, the backtracking engine Mihomo itself uses for its
// filters (look-around, back references, atomic groups, conditionals,
// repeat counts above RE2's limit of 1000).
//
// A small syntax shim (translate) rewrites the Python spellings the engines
// spell differently; it does not validate. Whether a pattern is valid is
// decided by the engines, and their error texts are reported.

// BacktrackTimeout bounds one match on the backtracking engine. A hostile
// pattern must not hang the server: a timeout counts as "no match" (Search)
// or ErrMatchTimeout (Sub).
var BacktrackTimeout = time.Second

// ErrMatchTimeout is returned by Sub when a match exceeded BacktrackTimeout.
var ErrMatchTimeout = errors.New("regex match exceeded the time limit")

// ErrNegativeSubCount rejects a replacement count outside Sub's API contract.
var ErrNegativeSubCount = errors.New("regex replacement count must not be negative")

// UnsupportedRegexError is a valid Python pattern that neither engine can
// run: \N{name} escapes (no Unicode name table). Callers treat such filters
// as "not applied".
type UnsupportedRegexError struct{ Reason string }

func (e *UnsupportedRegexError) Error() string {
	return "regex construct not supported by the Go engine: " + e.Reason
}

// ErrRepeatTooLarge rejects a repeat count beyond the engines' range
// (int32); Python refuses counts from 2^32-1 on as well.
var ErrRepeatTooLarge = errors.New("the repetition number is too large")

// IsPyRegexError reports whether err rejects the pattern itself (as opposed
// to a pattern the Go engines cannot run).
func IsPyRegexError(err error) bool {
	var unsupported *UnsupportedRegexError
	return err != nil && !errors.As(err, &unsupported)
}

// regexLogger reports backtracking timeouts (once per pattern).
var regexLogger Logger = logx.Get("backend.utils.rule_matcher")

// Regex is a compiled pattern (re.compile).
type Regex struct {
	pattern    string
	ignoreCase bool
	re2        *regexp.Regexp // nil when the pattern needs the backtracking engine
	groups     int
	names      map[string]int
	warned     atomic.Bool

	bt struct {
		once     sync.Once
		any      *regexp2.Regexp
		nonEmpty *regexp2.Regexp // a non-empty match exactly at the start position
		err      error
	}
}

// PyRegex is the former name of Regex, kept while consumers move over.
type PyRegex = Regex

// CompilePyRegex compiles a Python-syntax pattern, optionally with
// re.IGNORECASE. The error is the engine's syntax error or
// *UnsupportedRegexError.
func CompilePyRegex(pattern string, ignoreCase bool) (*Regex, error) {
	tr := translate(pattern, false)
	if tr.tooLarge {
		return nil, ErrRepeatTooLarge
	}
	if tr.unsupported != "" {
		return nil, &UnsupportedRegexError{Reason: tr.unsupported}
	}
	if tr.badEscape != "" { // RE2 would read some (\Q, \p, \z) differently
		return nil, fmt.Errorf("bad escape %s", tr.badEscape)
	}
	if tr.badRange { // RE2 would take the dash literally
		return nil, errors.New("bad character range: a class escape cannot be a range end")
	}
	r := &Regex{pattern: pattern, ignoreCase: ignoreCase, groups: tr.groups, names: tr.names}
	if !tr.backtrack {
		re, err := regexp.Compile(tr.source(ignoreCase))
		if err == nil {
			r.re2 = re
			return r, nil
		}
		if !needsBacktracker(err) {
			return nil, err
		}
	}
	if _, err := r.backtracker(); err != nil {
		return nil, err
	}
	return r, nil
}

// needsBacktracker tells RE2 limits regexp2 does not have (repeat counts
// over 1000, program size) from real syntax errors.
func needsBacktracker(err error) bool {
	var se *syntax.Error
	return errors.As(err, &se) && (se.Code == syntax.ErrInvalidRepeatSize || se.Code == syntax.ErrLarge || se.Code == syntax.ErrNestingDepth)
}

// backtracker compiles the regexp2 form once: patterns RE2 cannot run, and
// Match/Sub for every pattern.
func (r *Regex) backtracker() (*regexp2.Regexp, error) {
	r.bt.once.Do(func() {
		tr := translate(r.pattern, true)
		source := tr.source(false)
		options := regexp2.None
		if r.ignoreCase {
			options = regexp2.IgnoreCase
		}
		compile := func(src string) *regexp2.Regexp {
			re, err := regexp2.Compile(src, options)
			if err != nil {
				r.bt.err = err
				return nil
			}
			re.MatchTimeout = BacktrackTimeout
			return re
		}
		if r.bt.any = compile(source); r.bt.err == nil {
			r.bt.nonEmpty = compile(`\G(?:` + source + `)(?!\G)`)
		}
	})
	return r.bt.any, r.bt.err
}

// Groups is the number of capturing groups.
func (r *Regex) Groups() int { return r.groups }

// Search reports whether the pattern matches anywhere in text (re.search).
func (r *Regex) Search(text string) bool {
	if r.re2 != nil {
		return r.re2.MatchString(text)
	}
	re, _ := r.backtracker()
	matched, err := re.MatchString(text)
	if err != nil {
		if r.warned.CompareAndSwap(false, true) {
			regexLogger.Warningf("Regex %q exceeded the %s match time limit; treating it as no match", r.pattern, BacktrackTimeout)
		}
		return false
	}
	return matched
}

// Match is one match with its groups.
type Match struct {
	groups []string
	found  []bool
	names  map[string]int
}

// Group is the text of group i; ok is false when the group did not take part
// or does not exist.
func (m *Match) Group(i int) (string, bool) {
	if i < 0 || i >= len(m.groups) || !m.found[i] {
		return "", false
	}
	return m.groups[i], true
}

// Groups is the number of capturing groups of the pattern.
func (m *Match) Groups() int { return len(m.groups) - 1 }

// NamedGroup is the text of a named group.
func (m *Match) NamedGroup(name string) (string, bool) {
	i, ok := m.names[name]
	if !ok {
		return "", false
	}
	return m.Group(i)
}

func (r *Regex) newMatch(found *regexp2.Match) *Match {
	m := &Match{groups: make([]string, r.groups+1), found: make([]bool, r.groups+1), names: r.names}
	for i := range m.groups {
		if g := found.GroupByNumber(i); g != nil && len(g.Captures) > 0 {
			m.groups[i], m.found[i] = g.String(), true
		}
	}
	return m
}

// Match returns the first match in text (re.search), or nil.
func (r *Regex) Match(text string) (*Match, error) {
	re, err := r.backtracker()
	if err != nil {
		return nil, err
	}
	found, err := re.FindStringMatch(text)
	if err != nil {
		return nil, ErrMatchTimeout
	}
	if found == nil {
		return nil, nil
	}
	return r.newMatch(found), nil
}

// Sub replaces matches by what repl returns (pattern.sub with a function);
// count 0 replaces every match. An error from repl stops and is returned.
// Negative counts return ErrNegativeSubCount.
// Sub always runs on regexp2: like Python, it also replaces an empty match
// right after a non-empty one ("abc" with .* gives two matches), which
// RE2's ReplaceAll skips.
func (r *Regex) Sub(text string, repl func(*Match) (string, error), count int) (string, error) {
	if count < 0 {
		return "", ErrNegativeSubCount
	}
	re, err := r.backtracker()
	if err != nil {
		return "", err
	}
	runes := []rune(text)
	var out strings.Builder
	last, pos := 0, 0
	afterEmpty := false
	for n := 0; (count <= 0 || n < count) && pos <= len(runes); n++ {
		var found *regexp2.Match
		if afterEmpty { // the next match may not be empty at the same place
			if found, err = r.bt.nonEmpty.FindRunesMatchStartingAt(runes, pos); err == nil && found == nil {
				if pos++; pos > len(runes) {
					break
				}
			}
		}
		if found == nil && err == nil {
			found, err = re.FindRunesMatchStartingAt(runes, pos)
		}
		if err != nil {
			return "", ErrMatchTimeout
		}
		if found == nil {
			break
		}
		begin, end := found.Index, found.Index+found.Length
		out.WriteString(string(runes[last:begin]))
		replacement, err := repl(r.newMatch(found))
		if err != nil {
			return "", err
		}
		out.WriteString(replacement)
		last, pos, afterEmpty = end, end, begin == end
	}
	out.WriteString(string(runes[last:]))
	return out.String(), nil
}

// translation is the engine spelling of a Python pattern.
type translation struct {
	out         strings.Builder
	flags       string // global inline flags for the engine (i, m, s)
	backtrack   bool   // the pattern needs regexp2
	unsupported string
	badEscape   string // the first unknown letter escape, such as \p or \Q
	badRepeat   bool   // {m,n} with m > n
	tooLarge    bool   // a repeat count beyond int32
	badRange    bool   // a character class as a range end
	groups      int
	names       map[string]int
}

func (t *translation) source(ignoreCase bool) string {
	flags := t.flags
	if ignoreCase {
		flags += "i"
	}
	if flags == "" {
		return t.out.String()
	}
	return "(?" + flags + ")" + t.out.String()
}

// translate rewrites a Python pattern for RE2 (net=false) or regexp2
// (net=true). For RE2, constructs only regexp2 has are replaced by plain
// groups and backtrack is set, so the RE2 text still shows the pattern's
// structure to the RE2 parser (ValidateRegexSyntax). What the shim changes:
//   - every group is a plain capture; names live in a Go map, so Unicode
//     names work on both engines; (?P=name) and \N become \k<N>;
//   - \d \w \s and their negations are Python's Unicode classes (ASCII under
//     (?a)), also inside [...]; \b/\B use each engine's native boundaries;
//     \Z is \z; {,n} is {0,n};
//   - (?x) whitespace and comments are dropped; possessive x*+ is (?>x*);
//   - inside [...] every ASCII punctuation is escaped ([[:alpha:]] is a
//     literal set in Python).
//
// Malformed input is copied through for the engine to reject.
func translate(pattern string, net bool) *translation {
	w := &walker{src: []rune(pattern), net: net, t: &translation{names: map[string]int{}}}
	w.alternation(true)
	for w.pos < len(w.src) { // a stray ")": let the engine report it
		w.t.out.WriteString(string(w.src[w.pos:]))
		w.pos = len(w.src)
		w.alternation(false)
	}
	return w.t
}

type walker struct {
	src            []rune
	pos            int
	net            bool
	ascii, verbose bool
	t              *translation
}

func (w *walker) more() bool { return w.pos < len(w.src) }

func (w *walker) eat(r rune) bool {
	if w.more() && w.src[w.pos] == r {
		w.pos++
		return true
	}
	return false
}

func (w *walker) write(s string) { w.t.out.WriteString(s) }

func (w *walker) alternation(first bool) {
	for {
		w.sequence(first)
		if !w.eat('|') {
			return
		}
		w.write("|")
		first = false
	}
}

func (w *walker) sequence(first bool) {
	lastAt, empty := -1, true // output offset of the last item, for possessive repeats
	for w.more() {
		c := w.src[w.pos]
		if c == '|' || c == ')' {
			return
		}
		w.pos++
		if w.verbose && strings.ContainsRune(" \t\n\r\v\f", c) {
			continue
		}
		if w.verbose && c == '#' {
			for w.more() && w.src[w.pos] != '\n' {
				w.pos++
			}
			continue
		}
		at := w.t.out.Len()
		switch c {
		case '\\':
			w.escape()
		case '[':
			w.set()
		case '(':
			if !w.group(first && empty) {
				continue
			}
		case '*', '+', '?':
			w.write(string(c))
			w.quantifierTail(lastAt)
			continue
		case '{':
			if w.repeat(lastAt) {
				continue
			}
			w.write(`\{`)
		case '.', '^', '$':
			w.write(string(c))
		default:
			w.write(w.literal(c))
		}
		lastAt, empty = at, false
	}
}

// repeat handles "{m,n}"; false means the brace is a literal.
func (w *walker) repeat(lastAt int) bool {
	start := w.pos
	digits := func() string {
		from := w.pos
		for w.more() && w.src[w.pos] >= '0' && w.src[w.pos] <= '9' {
			w.pos++
		}
		return string(w.src[from:w.pos])
	}
	lo := digits()
	hi, comma := lo, w.eat(',')
	if comma {
		hi = digits()
	}
	if (lo == "" && !comma) || !w.eat('}') {
		w.pos = start
		return false
	}
	if lo == "" {
		lo = "0"
	}
	if loValue, _ := strconv.ParseUint(lo, 10, 64); hi != "" {
		hiValue, _ := strconv.ParseUint(hi, 10, 64)
		w.t.badRepeat = w.t.badRepeat || hiValue < loValue
	}
	// RE2 allows counts up to 1000: larger ones run on regexp2 (up to int32);
	// the RE2 text keeps them at 1000 so that its parser still checks the rest.
	count := func(n string) string {
		value, err := strconv.ParseUint(n, 10, 64)
		switch {
		case n == "":
		case err != nil || value > 1<<31-1:
			w.t.tooLarge = true
		case value > 1000:
			w.t.backtrack = true
			if !w.net {
				return "1000"
			}
		}
		return n
	}
	if lo, hi = count(lo), count(hi); comma {
		w.write("{" + lo + "," + hi + "}")
	} else {
		w.write("{" + lo + "}")
	}
	w.quantifierTail(lastAt)
	return true
}

// quantifierTail copies a lazy "?" and turns a possessive "+" into an atomic
// group around the repeated item.
func (w *walker) quantifierTail(lastAt int) {
	if w.eat('?') {
		w.write("?")
		return
	}
	if !w.more() || w.src[w.pos] != '+' || lastAt < 0 {
		return
	}
	w.pos++
	w.t.backtrack = true
	if w.net {
		text := w.t.out.String()
		w.t.out.Reset()
		w.write(text[:lastAt] + "(?>" + text[lastAt:] + ")")
	}
}

// literal spells one character for either engine, outside or inside a class.
func (w *walker) literal(r rune) string {
	switch {
	case r < 0x20 || r == 0x7f:
		return fmt.Sprintf(`\x%02x`, r)
	case r < 0x80 && !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != ' ' && r != '_':
		return `\` + string(r)
	}
	return string(r)
}

func (w *walker) escape() {
	if !w.more() {
		w.write(`\`) // trailing backslash: the engine rejects it
		return
	}
	c := w.src[w.pos]
	w.pos++
	switch c {
	case 'A':
		w.write(`\` + string(c))
	case 'b', 'B':
		w.write(`\` + string(c))
	case 'Z':
		w.write(`\z`)
	case 'd', 'D', 's', 'S', 'w', 'W':
		w.write("[" + w.category(c) + "]")
	default:
		if c >= '1' && c <= '9' && !w.octal(c) {
			n := string(c)
			if w.more() && w.src[w.pos] >= '0' && w.src[w.pos] <= '9' {
				n += string(w.src[w.pos])
				w.pos++
			}
			group, _ := strconv.Atoi(n)
			w.backref(group)
			return
		}
		if r, ok := w.character(c, false); ok {
			w.write(w.literal(r))
		}
	}
}

// octal reports a three-digit octal escape (\123) after the first digit.
func (w *walker) octal(first rune) bool {
	isOct := func(i int) bool { return i < len(w.src) && w.src[i] >= '0' && w.src[i] <= '7' }
	return first <= '7' && isOct(w.pos) && isOct(w.pos+1)
}

// character resolves a character escape after the backslash (c already
// consumed). ok=false: the escape was copied for the engine to judge.
func (w *walker) character(c rune, inClass bool) (rune, bool) {
	hex := func(n int) (rune, bool) {
		if w.pos+n > len(w.src) {
			return 0, false
		}
		v, err := strconv.ParseUint(string(w.src[w.pos:w.pos+n]), 16, 32)
		if err != nil || v > unicode.MaxRune {
			return 0, false
		}
		w.pos += n
		return rune(v), true
	}
	switch c {
	case 'a':
		return 7, true
	case 'f':
		return '\f', true
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	case 'v':
		return '\v', true
	case 'x', 'u', 'U':
		if r, ok := hex(map[rune]int{'x': 2, 'u': 4, 'U': 8}[c]); ok {
			return r, true
		}
	case 'N':
		w.t.unsupported = `named escape \N{...}`
		return 0, false
	}
	if c >= '0' && c <= '7' && (c == '0' || inClass || w.octal(c)) {
		value := c - '0'
		for n := 0; n < 2 && w.more() && w.src[w.pos] >= '0' && w.src[w.pos] <= '7'; n++ {
			value = value*8 + w.src[w.pos] - '0'
			w.pos++
		}
		if value <= 0o377 {
			return value, true
		}
	}
	if c < 0x80 && (unicode.IsLetter(c) || unicode.IsDigit(c)) {
		if w.t.badEscape == "" {
			w.t.badEscape = `\` + string(c)
		}
		w.write(`\` + string(c)) // unknown escape: the engine rejects it
		return 0, false
	}
	return c, true
}

func (w *walker) backref(group int) {
	w.t.backtrack = true
	if w.net {
		w.write(fmt.Sprintf(`\k<%d>`, group))
	} else {
		w.write(`(?:)`)
	}
}

// Python's \d is a Unicode decimal digit, \w a letter, digit or numeric
// character or "_", \s a Unicode white-space character (str.isspace).
const pySpace = "\t-\r\x1c- \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000"

var wordSpans = sync.OnceValue(func() []span {
	var spans []span
	for _, table := range []*unicode.RangeTable{unicode.L, unicode.N} {
		for _, r := range table.R16 {
			spans = appendStrided(spans, rune(r.Lo), rune(r.Hi), rune(r.Stride))
		}
		for _, r := range table.R32 {
			spans = appendStrided(spans, rune(r.Lo), rune(r.Hi), rune(r.Stride))
		}
	}
	return mergeSpans(append(spans, span{'_', '_'}))
})

type span struct{ lo, hi rune }

func appendStrided(spans []span, lo, hi, stride rune) []span {
	if stride == 1 {
		return append(spans, span{lo, hi})
	}
	for r := lo; r <= hi; r += stride {
		spans = append(spans, span{r, r})
	}
	return spans
}

func mergeSpans(spans []span) []span {
	slices.SortFunc(spans, func(a, b span) int { return int(a.lo - b.lo) })
	var out []span
	for _, s := range spans {
		if n := len(out); n > 0 && s.lo <= out[n-1].hi+1 {
			out[n-1].hi = max(out[n-1].hi, s.hi)
			continue
		}
		out = append(out, s)
	}
	return out
}

// classSpans parses a class body made of literal characters and ranges.
func classSpans(body string) []span {
	runes := []rune(body)
	var spans []span
	for i := 0; i < len(runes); i++ {
		s := span{runes[i], runes[i]}
		if i+2 < len(runes) && runes[i+1] == '-' {
			s.hi = runes[i+2]
			i += 2
		}
		spans = append(spans, s)
	}
	return mergeSpans(spans)
}

func complement(spans []span) []span {
	var out []span
	next := rune(0)
	for _, s := range spans {
		if s.lo > next {
			out = append(out, span{next, s.lo - 1})
		}
		next = s.hi + 1
	}
	if next <= unicode.MaxRune {
		out = append(out, span{next, unicode.MaxRune})
	}
	return out
}

func (w *walker) spell(spans []span) string {
	var b strings.Builder
	one := func(r rune) {
		switch {
		case r < 0x80:
			b.WriteString(w.literal(r))
		case w.net && r <= 0xffff:
			fmt.Fprintf(&b, `\u%04x`, r)
		case w.net:
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, `\x{%x}`, r)
		}
	}
	for _, s := range spans {
		one(s.lo)
		if s.hi > s.lo {
			b.WriteByte('-')
			one(s.hi)
		}
	}
	return b.String()
}

// category is the class body of \d \D \s \S \w \W.
func (w *walker) category(c rune) string {
	if !w.ascii {
		switch c {
		case 'd':
			return `\p{Nd}`
		case 'D':
			return `\P{Nd}`
		case 'w':
			return `\p{L}\p{N}_`
		}
	}
	var spans []span
	switch unicode.ToLower(c) {
	case 'd':
		spans = []span{{'0', '9'}}
	case 's':
		if w.ascii {
			spans = classSpans("\t-\r ")
		} else {
			spans = classSpans(pySpace)
		}
	case 'w':
		if w.ascii {
			spans = classSpans("0-9A-Z_a-z")
		} else {
			spans = wordSpans()
		}
	}
	if unicode.IsUpper(c) {
		spans = complement(spans)
	}
	return w.spell(spans)
}

func (w *walker) set() {
	w.write("[")
	if w.eat('^') {
		w.write("^")
	}
	for first := true; w.more(); first = false {
		c := w.src[w.pos]
		w.pos++
		if c == ']' && !first {
			w.write("]")
			return
		}
		if c == '-' { // a range, or a literal dash at either edge (same in all three)
			w.write("-")
			continue
		}
		if c != '\\' {
			w.write(w.literal(c))
			continue
		}
		if !w.more() {
			w.write(`\`)
			return
		}
		c = w.src[w.pos]
		w.pos++
		switch c {
		case 'd', 'D', 's', 'S', 'w', 'W':
			w.write(w.category(c))
			if w.peek() == '-' && w.peekAt(1) != ']' && w.peekAt(1) != 0 {
				w.t.badRange = true // a class cannot start a range ([\d-x])
			}
		case 'b':
			w.write(`\x08`)
		default:
			if r, ok := w.character(c, true); ok {
				w.write(w.literal(r))
			}
		}
	}
	// unterminated: the engine reports the missing "]"
}

// group handles "(" up to its ")". It returns false when nothing was
// produced (a comment or global flags).
func (w *walker) group(atStart bool) bool {
	open, capture, name := "(", true, ""
	verbose, ascii := w.verbose, w.ascii
	if w.eat('?') {
		capture = false
		switch c := w.peek(); {
		case c == 'P' && w.peekAt(1) == '<':
			if n, ok := w.name(w.pos+2, '>'); ok && w.t.names[n] == 0 { // a repeated name is invalid
				w.pos += 2 + len([]rune(n)) + 1
				capture, name = true, n
			} else {
				open = "(?"
			}
		case c == 'P' && w.peekAt(1) == '=':
			if n, ok := w.name(w.pos+2, ')'); ok {
				if g, known := w.t.names[n]; known {
					w.pos += 2 + len([]rune(n)) + 1
					w.backref(g)
					return true
				}
			}
			open = "(?"
		case c == ':':
			w.pos++
			open = "(?:"
		case c == '#':
			for w.more() && w.src[w.pos] != ')' {
				if w.src[w.pos] == '\\' {
					w.pos++
				}
				w.pos++
			}
			if !w.eat(')') {
				w.write("(") // unterminated comment
			}
			return false
		case c == '=' || c == '!' || c == '>', c == '<' && (w.peekAt(1) == '=' || w.peekAt(1) == '!'):
			// look-around and atomic groups; RE2's parser sees a plain group
			from := w.pos
			if w.pos++; c == '<' {
				w.pos++
			}
			w.t.backtrack = true
			open = "(?:"
			if w.net {
				open = "(?" + string(w.src[from:w.pos])
			}
		case c == '(':
			return w.conditional()
		default:
			add, del, end, ok := w.flags()
			switch {
			case !ok:
				open = "(?"
			case end == ')' && atStart:
				w.applyFlags(add, true)
				w.t.flags += engineFlags(add)
				return false
			case end == ')': // Python insists on the start; Go applies them from here on
				w.applyFlags(add, true)
				if f := engineFlags(add); f != "" {
					w.write("(?" + f + ")")
				}
				return false
			default:
				w.applyFlags(add, true)
				w.applyFlags(del, false)
				open = "(?" + engineFlags(add)
				if d := engineFlags(del); d != "" {
					open += "-" + d
				}
				open += ":"
			}
		}
	}
	if capture {
		w.t.groups++
		if name != "" {
			w.t.names[name] = w.t.groups
		}
	}
	w.write(open)
	w.alternation(false)
	w.verbose, w.ascii = verbose, ascii
	if w.eat(')') {
		w.write(")")
	}
	return true
}

func (w *walker) peek() rune { return w.peekAt(0) }

func (w *walker) peekAt(i int) rune {
	if w.pos+i < len(w.src) {
		return w.src[w.pos+i]
	}
	return 0
}

// name reads a group name starting at from up to end; ok only for a valid
// identifier.
func (w *walker) name(from int, end rune) (string, bool) {
	for i := from; i < len(w.src); i++ {
		if w.src[i] == end {
			name := string(w.src[from:i])
			return name, isIdentifier(name)
		}
	}
	return "", false
}

func isIdentifier(s string) bool {
	for i, r := range s {
		start := r == '_' || unicode.In(r, unicode.L, unicode.Nl, unicode.Other_ID_Start)
		if !start && (i == 0 || !unicode.In(r, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc, unicode.Other_ID_Continue)) {
			return false
		}
	}
	return s != ""
}

// conditional handles "(?(group)yes|no)"; the walker is after "(?".
func (w *walker) conditional() bool {
	end := -1
	for i := w.pos + 1; i < len(w.src); i++ {
		if w.src[i] == ')' {
			end = i
			break
		}
	}
	if end < 0 {
		w.write("(?")
		return true
	}
	cond := string(w.src[w.pos+1 : end])
	group, known := w.t.names[cond]
	if !known {
		n, err := strconv.Atoi(cond)
		if err != nil || n < 1 {
			w.write("(?") // the engine rejects it
			return true
		}
		group = n
	}
	w.pos = end + 1
	w.t.backtrack = true
	if w.net {
		w.write(fmt.Sprintf("(?(%d)", group))
	} else {
		w.write("(?:")
	}
	w.sequence(false)
	w.write("|") // regexp2 needs the "no" branch, even an empty one
	if w.eat('|') {
		w.sequence(false)
	}
	if w.eat(')') {
		w.write(")")
	}
	return true
}

// flags reads inline flags after "(?" up to ")" or ":"; ok is false for
// anything else (copied for the engine to reject).
func (w *walker) flags() (add, del string, end rune, ok bool) {
	i, target := w.pos, &add
	for ; i < len(w.src); i++ {
		switch c := w.src[i]; {
		case strings.ContainsRune("aimsux", c):
			*target += string(c)
		case c == '-' && target == &add:
			target = &del
		case c == ')' && del == "" || c == ':':
			if add == "" && del == "" {
				return "", "", 0, false
			}
			w.pos = i + 1
			return add, del, c, true
		default:
			return "", "", 0, false
		}
	}
	return "", "", 0, false
}

func (w *walker) applyFlags(flags string, on bool) {
	for _, c := range flags {
		switch c {
		case 'x':
			w.verbose = on
		case 'a':
			w.ascii = on
		case 'u':
			w.ascii = false
		}
	}
}

func engineFlags(flags string) string {
	var out strings.Builder
	for _, c := range flags {
		if strings.ContainsRune("ims", c) {
			out.WriteRune(c)
		}
	}
	return out.String()
}
