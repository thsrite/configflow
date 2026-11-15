package rules

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// A port of CPython 3.11's re._parser (sre_parse). The rule engine needs it
// for two things: telling exactly which user-supplied patterns Python's re
// accepts (with Python's own error text, positions included), and a syntax
// tree from which a RE2 program can be built (pyre_re2.go). Only the parse
// phase is ported; errors raised by Python's compile phase (for example
// variable-width look-behind) never surface in the places that matter.

const (
	reMaxRepeat    = 4294967295
	reMaxGroups    = 1073741823
	reMaxNesting   = 490 // CPython 3.11 raises RecursionError near 494 nested groups
	flagIgnoreCase = 2
	flagLocale     = 4
	flagMultiline  = 8
	flagDotAll     = 16
	flagUnicode    = 32
	flagVerbose    = 64
	flagDebug      = 128
	flagASCII      = 256
	flagTemplate   = 1
)

// reError is re.error.
type reError struct {
	Msg     string
	pattern []rune
	pos     int
	hasPos  bool
}

func (e *reError) Error() string {
	if !e.hasPos {
		return e.Msg
	}
	msg := fmt.Sprintf("%s at position %d", e.Msg, e.pos)
	pos := e.pos
	if pos > len(e.pattern) {
		pos = len(e.pattern)
	}
	if pos < 0 {
		pos = 0
	}
	newlines := 0
	last := -1
	for i := 0; i < pos; i++ {
		if e.pattern[i] == '\n' {
			newlines++
			last = i
		}
	}
	hasNewline := false
	for _, r := range e.pattern {
		if r == '\n' {
			hasNewline = true
			break
		}
	}
	if hasNewline {
		msg = fmt.Sprintf("%s (line %d, column %d)", msg, newlines+1, e.pos-last)
	}
	return msg
}

// errRecursion stands for Python's RecursionError, errOverflow for the
// OverflowError of an oversized repeat count.
type errRecursion struct{}

func (errRecursion) Error() string { return "maximum recursion depth exceeded" }

type errOverflow struct{}

func (errOverflow) Error() string { return "the repetition number is too large" }

type reOp int

const (
	opLiteral reOp = iota
	opNotLiteral
	opAny
	opIn
	opAt
	opBranch
	opSubpattern
	opMaxRepeat
	opMinRepeat
	opPossessive
	opAtomic
	opAssert
	opAssertNot
	opGroupRef
	opGroupRefExists
)

type atKind int

const (
	atBeginning atKind = iota
	atBeginningString
	atBoundary
	atNonBoundary
	atEnd
	atEndString
)

type catKind int

const (
	catDigit catKind = iota
	catNotDigit
	catSpace
	catNotSpace
	catWord
	catNotWord
)

type setKind int

const (
	setLiteral setKind = iota
	setRange
	setCategory
	setNegate
)

type setItem struct {
	kind   setKind
	lo, hi rune
	cat    catKind
}

type reNode struct {
	op       reOp
	ch       rune
	at       atKind
	set      []setItem
	min, max int64
	body     []*reNode
	group    int // capture index, 0 when none
	named    bool
	add, del int
	alts     [][]*reNode
	dir      int
	ref      int
	yes, no  []*reNode
	hasNo    bool
}

type reState struct {
	flags       int
	groupdict   map[string]int
	closed      []bool // index = group number; 0 is the whole pattern
	widths      []reWidth
	lookbehind  int // -1 for None
	grouprefpos map[int]int
}

func (s *reState) groups() int { return len(s.closed) }

type tokenizer struct {
	src     []rune
	index   int
	next    string
	hasNext bool
	pattern []rune
}

func (t *tokenizer) fail(msg string, offset int) {
	panic(&reError{Msg: msg, pattern: t.pattern, pos: t.tell() - offset, hasPos: true})
}

func (t *tokenizer) advance() {
	index := t.index
	if index >= len(t.src) {
		t.next, t.hasNext = "", false
		return
	}
	char := string(t.src[index])
	if t.src[index] == '\\' {
		index++
		if index >= len(t.src) {
			panic(&reError{Msg: "bad escape (end of pattern)", pattern: t.pattern, pos: len(t.pattern) - 1, hasPos: true})
		}
		char += string(t.src[index])
	}
	t.index = index + 1
	t.next, t.hasNext = char, true
}

func (t *tokenizer) match(char string) bool {
	if t.hasNext && t.next == char {
		t.advance()
		return true
	}
	return false
}

func (t *tokenizer) get() (string, bool) {
	this, ok := t.next, t.hasNext
	t.advance()
	return this, ok
}

func (t *tokenizer) nextLen() int {
	if !t.hasNext {
		return 0
	}
	return len([]rune(t.next))
}

func (t *tokenizer) tell() int { return t.index - t.nextLen() }

func (t *tokenizer) seek(index int) {
	t.index = index
	t.advance()
}

func (t *tokenizer) getwhile(n int, valid func(rune) bool) string {
	var result strings.Builder
	for i := 0; i < n; i++ {
		if !t.hasNext {
			break
		}
		r := []rune(t.next)
		if len(r) != 1 || !valid(r[0]) {
			break
		}
		result.WriteString(t.next)
		t.advance()
	}
	return result.String()
}

func (t *tokenizer) getuntil(terminator, name string) string {
	var result strings.Builder
	count := 0
	for {
		c, ok := t.next, t.hasNext
		t.advance()
		if !ok {
			if count == 0 {
				t.fail("missing "+name, 0)
			}
			t.fail("missing "+terminator+", unterminated name", count)
		}
		if c == terminator {
			if count == 0 {
				t.fail("missing "+name, 1)
			}
			break
		}
		result.WriteString(c)
		count += len([]rune(c))
	}
	return result.String()
}

func (t *tokenizer) checkGroupName(name string, offset int) {
	if !pyIsIdentifier(name) {
		t.fail(fmt.Sprintf("bad character in group name %s", pyReprString(name)), len([]rune(name))+offset)
	}
}

func isHex(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
}
func isOct(r rune) bool    { return r >= '0' && r <= '7' }
func isDigitR(r rune) bool { return r >= '0' && r <= '9' }
func isASCIILetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// pyReprString is repr(str) for the small error messages that quote text.
func pyReprString(s string) string {
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, "\"") {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString("\\n")
		case r == '\r':
			b.WriteString("\\r")
		case r == '\t':
			b.WriteString("\\t")
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\x%02x", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

var simpleEscapes = map[string]rune{
	`\a`: 7, `\b`: 8, `\f`: 12, `\n`: 10, `\r`: 13, `\t`: 9, `\v`: 11, `\\`: '\\',
}

func categoryEscape(escape string) (catKind, bool) {
	switch escape {
	case `\d`:
		return catDigit, true
	case `\D`:
		return catNotDigit, true
	case `\s`:
		return catSpace, true
	case `\S`:
		return catNotSpace, true
	case `\w`:
		return catWord, true
	case `\W`:
		return catNotWord, true
	}
	return 0, false
}

func atEscape(escape string) (atKind, bool) {
	switch escape {
	case `\A`:
		return atBeginningString, true
	case `\b`:
		return atBoundary, true
	case `\B`:
		return atNonBoundary, true
	case `\Z`:
		return atEndString, true
	}
	return 0, false
}

func hexValue(text string) rune {
	v, _ := strconv.ParseUint(text, 16, 32)
	return rune(v)
}

// escapeLiteral handles the escapes shared by classes and expressions (hex,
// unicode, octal); ok=false means "not one of those".
func (t *tokenizer) numericEscape(escape string) (rune, bool) {
	c := []rune(escape)[1]
	switch {
	case c == 'x':
		escape += t.getwhile(2, isHex)
		if len(escape) != 4 {
			t.fail("incomplete escape "+escape, len(escape))
		}
		return hexValue(escape[2:]), true
	case c == 'u':
		escape += t.getwhile(4, isHex)
		if len(escape) != 6 {
			t.fail("incomplete escape "+escape, len(escape))
		}
		return hexValue(escape[2:]), true
	case c == 'U':
		escape += t.getwhile(8, isHex)
		if len(escape) != 10 {
			t.fail("incomplete escape "+escape, len(escape))
		}
		v, _ := strconv.ParseUint(escape[2:], 16, 64)
		if v > unicode.MaxRune {
			// chr(c) raises ValueError, which falls through to "bad escape".
			t.fail("bad escape "+escape, len(escape))
		}
		return rune(v), true
	}
	return 0, false
}

func (t *tokenizer) namedEscape() rune {
	if !t.match("{") {
		t.fail("missing {", 0)
	}
	name := t.getuntil("}", "character name")
	// Python resolves the name through unicodedata.lookup. The rule engine has
	// no Unicode name table, so whether the name is valid cannot be decided
	// here: report "unsupported" rather than guess.
	panic(&UnsupportedRegexError{Reason: "named escape \\N{" + name + "}"})
}

type classEscape struct {
	lit   rune
	cat   catKind
	isCat bool
}

// classEscape is _class_escape.
func (t *tokenizer) classEscape(escape string) classEscape {
	if r, ok := simpleEscapes[escape]; ok {
		return classEscape{lit: r}
	}
	if cat, ok := categoryEscape(escape); ok {
		return classEscape{cat: cat, isCat: true}
	}
	runes := []rune(escape)
	c := runes[1]
	if v, ok := t.numericEscape(escape); ok {
		return classEscape{lit: v}
	}
	switch {
	case c == 'N':
		t.namedEscape()
	case isOct(c):
		escape += t.getwhile(2, isOct)
		v, _ := strconv.ParseUint(escape[1:], 8, 32)
		if v > 0o377 {
			t.fail(fmt.Sprintf("octal escape value %s outside of range 0-0o377", escape), len(escape))
		}
		return classEscape{lit: rune(v)}
	case isDigitR(c):
		// ValueError: falls through to bad escape.
	default:
		if len(runes) == 2 {
			if isASCIILetter(c) {
				t.fail("bad escape "+escape, len(escape))
			}
			return classEscape{lit: runes[1]}
		}
	}
	t.fail("bad escape "+escape, len(escape))
	return classEscape{}
}

// escape is _escape.
func (t *tokenizer) escape(escape string, st *reState) *reNode {
	if at, ok := atEscape(escape); ok {
		return &reNode{op: opAt, at: at}
	}
	if cat, ok := categoryEscape(escape); ok {
		return &reNode{op: opIn, set: []setItem{{kind: setCategory, cat: cat}}}
	}
	if r, ok := simpleEscapes[escape]; ok {
		return &reNode{op: opLiteral, ch: r}
	}
	runes := []rune(escape)
	c := runes[1]
	if v, ok := t.numericEscape(escape); ok {
		return &reNode{op: opLiteral, ch: v}
	}
	switch {
	case c == 'N':
		t.namedEscape()
	case c == '0':
		escape += t.getwhile(2, isOct)
		v, _ := strconv.ParseUint(escape[1:], 8, 32)
		return &reNode{op: opLiteral, ch: rune(v)}
	case isDigitR(c):
		if t.hasNext && len([]rune(t.next)) == 1 && isDigitR([]rune(t.next)[0]) {
			got, _ := t.get()
			escape += got
			if isOct(rune(escape[1])) && isOct(rune(escape[2])) && t.hasNext && len([]rune(t.next)) == 1 && isOct([]rune(t.next)[0]) {
				got, _ = t.get()
				escape += got
				v, _ := strconv.ParseUint(escape[1:], 8, 32)
				if v > 0o377 {
					t.fail(fmt.Sprintf("octal escape value %s outside of range 0-0o377", escape), len(escape))
				}
				return &reNode{op: opLiteral, ch: rune(v)}
			}
		}
		group, _ := strconv.Atoi(escape[1:])
		if group < st.groups() {
			if !st.checkGroup(group) {
				t.fail("cannot refer to an open group", len(escape))
			}
			st.checkLookbehindGroup(group, t)
			return &reNode{op: opGroupRef, ref: group}
		}
		t.fail(fmt.Sprintf("invalid group reference %d", group), len(escape)-1)
	default:
		if len(runes) == 2 {
			if isASCIILetter(c) {
				t.fail("bad escape "+escape, len(escape))
			}
			return &reNode{op: opLiteral, ch: runes[1]}
		}
	}
	t.fail("bad escape "+escape, len(escape))
	return nil
}

func (s *reState) checkGroup(gid int) bool { return gid < s.groups() && s.closed[gid] }

func (s *reState) checkLookbehindGroup(gid int, t *tokenizer) {
	if s.lookbehind >= 0 {
		if !s.checkGroup(gid) {
			t.fail("cannot refer to an open group", 0)
		}
		if gid >= s.lookbehind {
			t.fail("cannot refer to group defined in the same lookbehind subpattern", 0)
		}
	}
}

func (s *reState) openGroup(name string, hasName bool) (int, error) {
	gid := s.groups()
	s.closed = append(s.closed, false)
	s.widths = append(s.widths, reWidth{})
	if s.groups() > reMaxGroups {
		return 0, &reError{Msg: "too many groups"}
	}
	if hasName {
		if ogid, ok := s.groupdict[name]; ok {
			return 0, &reError{Msg: fmt.Sprintf("redefinition of group name %s as group %d; was group %d", pyReprString(name), gid, ogid)}
		}
		s.groupdict[name] = gid
	}
	return gid, nil
}

func isSpecialChar(token string) bool {
	return len(token) == 1 && strings.ContainsRune(".\\[{()*+?^$|", rune(token[0]))
}

func isWhitespaceToken(token string) bool {
	return len(token) == 1 && strings.ContainsRune(" \t\n\r\v\f", rune(token[0]))
}

// parseSub is _parse_sub: an alternation.
func (t *tokenizer) parseSub(st *reState, verbose bool, nested int) []*reNode {
	var items [][]*reNode
	for {
		items = append(items, t.parse(st, verbose, nested+1, nested == 0 && len(items) == 0))
		if !t.match("|") {
			break
		}
		if nested == 0 {
			verbose = st.flags&flagVerbose != 0
		}
	}
	if len(items) == 1 {
		return items[0]
	}
	return []*reNode{{op: opBranch, alts: items}}
}

// parse is _parse: a sequence.
func (t *tokenizer) parse(st *reState, verbose bool, nested int, first bool) []*reNode {
	// Every group level adds 2 to nested (parse and parseSub).
	if nested > 2*reMaxNesting {
		panic(errRecursion{})
	}
	var sub []*reNode
	for {
		if !t.hasNext {
			break
		}
		this := t.next
		if this == "|" || this == ")" {
			break
		}
		t.get()
		if verbose {
			if isWhitespaceToken(this) {
				continue
			}
			if this == "#" {
				for {
					tok, ok := t.get()
					if !ok || tok == "\n" {
						break
					}
				}
				continue
			}
		}
		if this[0] == '\\' {
			sub = append(sub, t.escape(this, st))
			continue
		}
		if !isSpecialChar(this) {
			sub = append(sub, &reNode{op: opLiteral, ch: []rune(this)[0]})
			continue
		}
		switch this {
		case "[":
			sub = append(sub, t.parseSet())
		case "*", "+", "?", "{":
			here := t.tell()
			var lo, hi int64
			switch this {
			case "?":
				lo, hi = 0, 1
			case "*":
				lo, hi = 0, reMaxRepeat
			case "+":
				lo, hi = 1, reMaxRepeat
			default:
				if t.hasNext && t.next == "}" {
					sub = append(sub, &reNode{op: opLiteral, ch: '{'})
					continue
				}
				lo, hi = 0, reMaxRepeat
				var loText, hiText string
				for t.hasNext && len(t.next) == 1 && isDigitR(rune(t.next[0])) {
					got, _ := t.get()
					loText += got
				}
				if t.match(",") {
					for t.hasNext && len(t.next) == 1 && isDigitR(rune(t.next[0])) {
						got, _ := t.get()
						hiText += got
					}
				} else {
					hiText = loText
				}
				if !t.match("}") {
					sub = append(sub, &reNode{op: opLiteral, ch: '{'})
					t.seek(here)
					continue
				}
				if loText != "" {
					lo = parseRepeatCount(loText)
					if lo >= reMaxRepeat {
						panic(errOverflow{})
					}
				}
				if hiText != "" {
					hi = parseRepeatCount(hiText)
					if hi >= reMaxRepeat {
						panic(errOverflow{})
					}
					if hi < lo {
						t.fail("min repeat greater than max repeat", t.tell()-here)
					}
				}
			}
			if len(sub) == 0 || sub[len(sub)-1].op == opAt {
				t.fail("nothing to repeat", t.tell()-here+len(this))
			}
			last := sub[len(sub)-1]
			if last.op == opMaxRepeat || last.op == opMinRepeat || last.op == opPossessive {
				t.fail("multiple repeat", t.tell()-here+len(this))
			}
			body := []*reNode{last}
			if last.op == opSubpattern && last.group == 0 && last.add == 0 && last.del == 0 {
				body = last.body
			}
			op := opMaxRepeat
			if t.match("?") {
				op = opMinRepeat
			} else if t.match("+") {
				op = opPossessive
			}
			sub[len(sub)-1] = &reNode{op: op, min: lo, max: hi, body: body}
		case ".":
			sub = append(sub, &reNode{op: opAny})
		case "(":
			node, kind := t.parseGroup(st, verbose, nested, first, len(sub) > 0)
			switch kind {
			case groupNode:
				sub = append(sub, node)
			case groupGlobalFlags:
				verbose = st.flags&flagVerbose != 0
			}
		case "^":
			sub = append(sub, &reNode{op: opAt, at: atBeginning})
		case "$":
			sub = append(sub, &reNode{op: opAt, at: atEnd})
		}
	}
	return sub
}

func parseRepeatCount(text string) int64 {
	value, err := strconv.ParseUint(text, 10, 63)
	if err != nil {
		return reMaxRepeat // any huge number is past MAXREPEAT
	}
	if value > reMaxRepeat {
		return reMaxRepeat
	}
	return int64(value)
}

func (t *tokenizer) parseSet() *reNode {
	here := t.tell() - 1
	var set []setItem
	appendUnique := func(item setItem) {
		for _, existing := range set {
			if existing == item {
				return
			}
		}
		set = append(set, item)
	}
	negate := t.match("^")
	// Python keeps every element in a list first and removes duplicates at the
	// end; only the final list matters, so we build it with duplicates and
	// dedupe afterwards. "set" being empty decides whether "]" ends the class.
	count := 0
	var raw []setItem
	for {
		this, ok := t.get()
		if !ok {
			t.fail("unterminated character set", t.tell()-here)
		}
		if this == "]" && count > 0 {
			break
		}
		var code1 classEscape
		if this[0] == '\\' {
			code1 = t.classEscape(this)
		} else {
			code1 = classEscape{lit: []rune(this)[0]}
		}
		if t.match("-") {
			that, ok := t.get()
			if !ok {
				t.fail("unterminated character set", t.tell()-here)
			}
			if that == "]" {
				raw = append(raw, itemOf(code1), setItem{kind: setLiteral, lo: '-'})
				count += 2
				break
			}
			var code2 classEscape
			if that[0] == '\\' {
				code2 = t.classEscape(that)
			} else {
				code2 = classEscape{lit: []rune(that)[0]}
			}
			if code1.isCat || code2.isCat {
				t.fail(fmt.Sprintf("bad character range %s-%s", this, that), len([]rune(this))+1+len([]rune(that)))
			}
			if code2.lit < code1.lit {
				t.fail(fmt.Sprintf("bad character range %s-%s", this, that), len([]rune(this))+1+len([]rune(that)))
			}
			raw = append(raw, setItem{kind: setRange, lo: code1.lit, hi: code2.lit})
			count++
		} else {
			raw = append(raw, itemOf(code1))
			count++
		}
	}
	for _, item := range raw {
		appendUnique(item)
	}
	if len(set) == 1 && set[0].kind == setLiteral {
		if negate {
			return &reNode{op: opNotLiteral, ch: set[0].lo}
		}
		return &reNode{op: opLiteral, ch: set[0].lo}
	}
	if negate {
		set = append([]setItem{{kind: setNegate}}, set...)
	}
	return &reNode{op: opIn, set: set}
}

func itemOf(c classEscape) setItem {
	if c.isCat {
		return setItem{kind: setCategory, cat: c.cat}
	}
	return setItem{kind: setLiteral, lo: c.lit}
}

type groupKind int

const (
	groupNode        groupKind = iota // the construct produced a node
	groupSkip                         // a comment: nothing to add
	groupGlobalFlags                  // "(?flags)": state flags changed
)

// parseGroup handles "(" ... .
func (t *tokenizer) parseGroup(st *reState, verbose bool, nested int, first bool, hasPrev bool) (*reNode, groupKind) {
	start := t.tell() - 1
	capture, atomic := true, false
	name, hasName := "", false
	add, del := 0, 0
	if t.match("?") {
		char, ok := t.get()
		if !ok {
			t.fail("unexpected end of pattern", 0)
		}
		switch {
		case char == "P":
			if t.match("<") {
				name, hasName = t.getuntil(">", "group name"), true
				t.checkGroupName(name, 1)
			} else if t.match("=") {
				name = t.getuntil(")", "group name")
				t.checkGroupName(name, 1)
				gid, ok := st.groupdict[name]
				if !ok {
					t.fail(fmt.Sprintf("unknown group name %s", pyReprString(name)), len([]rune(name))+1)
				}
				if !st.checkGroup(gid) {
					t.fail("cannot refer to an open group", len([]rune(name))+1)
				}
				st.checkLookbehindGroup(gid, t)
				return &reNode{op: opGroupRef, ref: gid}, groupNode
			} else {
				c2, ok := t.get()
				if !ok {
					t.fail("unexpected end of pattern", 0)
				}
				t.fail("unknown extension ?P"+c2, len([]rune(c2))+2)
			}
		case char == ":":
			capture = false
		case char == "#":
			for {
				if !t.hasNext {
					t.fail("missing ), unterminated comment", t.tell()-start)
				}
				if tok, _ := t.get(); tok == ")" {
					break
				}
			}
			return nil, groupSkip
		case char == "=" || char == "!" || char == "<":
			dir := 1
			var previous int
			if char == "<" {
				c2, ok := t.get()
				if !ok {
					t.fail("unexpected end of pattern", 0)
				}
				if c2 != "=" && c2 != "!" {
					t.fail("unknown extension ?<"+c2, len([]rune(c2))+2)
				}
				char = c2
				dir = -1
				previous = st.lookbehind
				if previous < 0 {
					st.lookbehind = st.groups()
				}
			}
			p := t.parseSub(st, verbose, nested+1)
			if dir < 0 && previous < 0 {
				st.lookbehind = -1
			}
			if !t.match(")") {
				t.fail("missing ), unterminated subpattern", t.tell()-start)
			}
			op := opAssert
			if char == "!" {
				op = opAssertNot
			}
			return &reNode{op: op, dir: dir, body: p}, groupNode
		case char == "(":
			condname := t.getuntil(")", "group name")
			var condgroup int
			if pyIsIdentifier(condname) {
				t.checkGroupName(condname, 1)
				g, ok := st.groupdict[condname]
				if !ok {
					t.fail(fmt.Sprintf("unknown group name %s", pyReprString(condname)), len([]rune(condname))+1)
				}
				condgroup = g
			} else {
				v, err := strconv.Atoi(strings.TrimSpace(condname))
				if err != nil || v < 0 || strings.ContainsAny(condname, "+_") {
					t.fail(fmt.Sprintf("bad character in group name %s", pyReprString(condname)), len([]rune(condname))+1)
				}
				condgroup = v
				if condgroup == 0 {
					t.fail("bad group number", len([]rune(condname))+1)
				}
				if condgroup >= reMaxGroups {
					t.fail(fmt.Sprintf("invalid group reference %d", condgroup), len([]rune(condname))+1)
				}
				if _, seen := st.grouprefpos[condgroup]; !seen {
					st.grouprefpos[condgroup] = t.tell() - len([]rune(condname)) - 1
				}
			}
			st.checkLookbehindGroup(condgroup, t)
			yes := t.parse(st, verbose, nested+1, false)
			var no []*reNode
			hasNo := false
			if t.match("|") {
				no = t.parse(st, verbose, nested+1, false)
				hasNo = true
				if t.hasNext && t.next == "|" {
					t.fail("conditional backref with more than two branches", 0)
				}
			}
			if !t.match(")") {
				t.fail("missing ), unterminated subpattern", t.tell()-start)
			}
			return &reNode{op: opGroupRefExists, ref: condgroup, yes: yes, no: no, hasNo: hasNo}, groupNode
		case char == ">":
			capture, atomic = false, true
		case isFlagChar(char) || char == "-":
			a, d, global := t.parseFlags(st, char)
			if global {
				if !first || hasPrev {
					t.fail("global flags not at the start of the expression", t.tell()-start)
				}
				return nil, groupGlobalFlags
			}
			add, del = a, d
			capture = false
		default:
			t.fail("unknown extension ?"+char, len([]rune(char))+1)
		}
	}
	group := 0
	if capture {
		gid, err := st.openGroup(name, hasName)
		if err != nil {
			re := err.(*reError)
			t.fail(re.Msg, len([]rune(name))+1)
		}
		group = gid
	}
	subVerbose := (verbose || add&flagVerbose != 0) && del&flagVerbose == 0
	p := t.parseSub(st, subVerbose, nested+1)
	if !t.match(")") {
		t.fail("missing ), unterminated subpattern", t.tell()-start)
	}
	if group != 0 {
		st.closed[group] = true
		st.widths[group] = st.widthOf(p)
	}
	if atomic {
		return &reNode{op: opAtomic, body: p}, groupNode
	}
	return &reNode{op: opSubpattern, group: group, named: hasName, add: add, del: del, body: p}, groupNode
}

func isFlagChar(char string) bool {
	return len(char) == 1 && strings.ContainsRune("iLmsxatu", rune(char[0]))
}

func flagValue(char string) int {
	switch char {
	case "i":
		return flagIgnoreCase
	case "L":
		return flagLocale
	case "m":
		return flagMultiline
	case "s":
		return flagDotAll
	case "x":
		return flagVerbose
	case "a":
		return flagASCII
	case "t":
		return flagTemplate
	case "u":
		return flagUnicode
	}
	return 0
}

const typeFlags = flagASCII | flagLocale | flagUnicode
const globalFlags = flagDebug | flagTemplate

func isLetterToken(char string) bool {
	r := []rune(char)
	return len(r) == 1 && unicode.IsLetter(r[0])
}

// parseFlags is _parse_flags; global reports the "(?flags)" form.
func (t *tokenizer) parseFlags(st *reState, char string) (add, del int, global bool) {
	if char != "-" {
		for {
			flag := flagValue(char)
			if char == "L" {
				t.fail("bad inline flags: cannot use 'L' flag with a str pattern", 0)
			}
			add |= flag
			if flag&typeFlags != 0 && add&typeFlags != flag {
				t.fail("bad inline flags: flags 'a', 'u' and 'L' are incompatible", 0)
			}
			next, ok := t.get()
			if !ok {
				t.fail("missing -, : or )", 0)
			}
			char = next
			if char == ")" || char == "-" || char == ":" {
				break
			}
			if !isFlagChar(char) {
				msg := "missing -, : or )"
				if isLetterToken(char) {
					msg = "unknown flag"
				}
				t.fail(msg, len([]rune(char)))
			}
		}
	}
	if char == ")" {
		st.flags |= add
		return 0, 0, true
	}
	if add&globalFlags != 0 {
		t.fail("bad inline flags: cannot turn on global flag", 1)
	}
	if char == "-" {
		next, ok := t.get()
		if !ok {
			t.fail("missing flag", 0)
		}
		char = next
		if !isFlagChar(char) {
			msg := "missing flag"
			if isLetterToken(char) {
				msg = "unknown flag"
			}
			t.fail(msg, len([]rune(char)))
		}
		for {
			flag := flagValue(char)
			if flag&typeFlags != 0 {
				t.fail("bad inline flags: cannot turn off flags 'a', 'u' and 'L'", 0)
			}
			del |= flag
			next, ok := t.get()
			if !ok {
				t.fail("missing :", 0)
			}
			char = next
			if char == ":" {
				break
			}
			if !isFlagChar(char) {
				msg := "missing :"
				if isLetterToken(char) {
					msg = "unknown flag"
				}
				t.fail(msg, len([]rune(char)))
			}
		}
	}
	if del&globalFlags != 0 {
		t.fail("bad inline flags: cannot turn off global flag", 1)
	}
	if add&del != 0 {
		t.fail("bad inline flags: flag turned on and off", 1)
	}
	return add, del, false
}

// parsedRegex is the result of a successful parse.
type parsedRegex struct {
	nodes     []*reNode
	flags     int
	groups    int            // number of capturing groups (Pattern.groups)
	groupdict map[string]int // Pattern.groupindex
}

// parsePyRegex is re._parser.parse(pattern, flags): it returns *reError for
// patterns Python rejects with re.error, errRecursion or errOverflow for the
// other exceptions compile can raise.
func parsePyRegex(pattern string, flags int) (result *parsedRegex, err error) {
	defer func() {
		switch recovered := recover().(type) {
		case nil:
		case *reError:
			result, err = nil, recovered
		case errRecursion:
			result, err = nil, recovered
		case errOverflow:
			result, err = nil, recovered
		case *UnsupportedRegexError:
			result, err = nil, recovered
		default:
			panic(recovered)
		}
	}()
	runes := []rune(pattern)
	t := &tokenizer{src: runes, pattern: runes}
	st := &reState{flags: flags, groupdict: map[string]int{}, closed: []bool{false}, widths: []reWidth{{}}, lookbehind: -1, grouprefpos: map[int]int{}}
	t.advance()
	nodes := t.parseSub(st, flags&flagVerbose != 0, 0)
	if st.flags&flagASCII == 0 {
		st.flags |= flagUnicode
	} else if st.flags&flagUnicode != 0 {
		return nil, fmt.Errorf("ASCII and UNICODE flags are incompatible")
	}
	if t.hasNext {
		t.fail("unbalanced parenthesis", 0)
	}
	for g, pos := range st.grouprefpos {
		if g >= st.groups() {
			return nil, &reError{Msg: fmt.Sprintf("invalid group reference %d", g), pattern: runes, pos: pos, hasPos: true}
		}
	}
	// The compile phase of Python's re rejects variable-width look-behind.
	if st.checkLookbehind(nodes) {
		return nil, &reError{Msg: "look-behind requires fixed-width pattern"}
	}
	return &parsedRegex{nodes: nodes, flags: st.flags, groups: st.groups() - 1, groupdict: st.groupdict}, nil
}

// reWidth is SubPattern.getwidth(): (min, max) match length, saturating.
type reWidth struct{ lo, hi int64 }

const reMaxWidth = int64(1) << 62

func satAdd(a, b int64) int64 {
	if a+b > reMaxWidth || a+b < 0 {
		return reMaxWidth
	}
	return a + b
}

func satMul(a, b int64) int64 {
	if a != 0 && b > reMaxWidth/a {
		return reMaxWidth
	}
	return a * b
}

func (st *reState) widthOf(nodes []*reNode) reWidth {
	var lo, hi int64
	for _, n := range nodes {
		switch n.op {
		case opBranch:
			i, j := reMaxWidth, int64(0)
			for _, alt := range n.alts {
				w := st.widthOf(alt)
				if w.lo < i {
					i = w.lo
				}
				if w.hi > j {
					j = w.hi
				}
			}
			lo, hi = satAdd(lo, i), satAdd(hi, j)
		case opAtomic, opSubpattern:
			w := st.widthOf(n.body)
			lo, hi = satAdd(lo, w.lo), satAdd(hi, w.hi)
		case opMaxRepeat, opMinRepeat, opPossessive:
			w := st.widthOf(n.body)
			lo = satAdd(lo, satMul(w.lo, n.min))
			if n.max == reMaxRepeat && w.hi != 0 {
				hi = reMaxWidth
			} else {
				hi = satAdd(hi, satMul(w.hi, n.max))
			}
		case opAny, opIn, opLiteral, opNotLiteral:
			lo, hi = satAdd(lo, 1), satAdd(hi, 1)
		case opGroupRef:
			w := st.widths[n.ref]
			lo, hi = satAdd(lo, w.lo), satAdd(hi, w.hi)
		case opGroupRefExists:
			w := st.widthOf(n.yes)
			if n.hasNo {
				o := st.widthOf(n.no)
				if o.lo < w.lo {
					w.lo = o.lo
				}
				if o.hi > w.hi {
					w.hi = o.hi
				}
			} else {
				w.lo = 0
			}
			lo, hi = satAdd(lo, w.lo), satAdd(hi, w.hi)
		}
	}
	return reWidth{lo, hi}
}

// checkLookbehind reports a look-behind whose body is not fixed width.
func (st *reState) checkLookbehind(nodes []*reNode) bool {
	for _, n := range nodes {
		if (n.op == opAssert || n.op == opAssertNot) && n.dir < 0 {
			if w := st.widthOf(n.body); w.lo != w.hi {
				return true
			}
		}
		for _, children := range [][]*reNode{n.body, n.yes, n.no} {
			if st.checkLookbehind(children) {
				return true
			}
		}
		for _, alt := range n.alts {
			if st.checkLookbehind(alt) {
				return true
			}
		}
	}
	return false
}
