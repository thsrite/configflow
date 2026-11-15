// Package pyyaml is a port of PyYAML 6.0.1's pure-Python reader, scanner,
// parser and composer (yaml/reader.py, scanner.py, parser.py, composer.py,
// resolver.py) for text input: yaml.scan(text) and yaml.compose(text) with
// the SafeLoader resolver (ComposeWith takes another resolver).
//
// internal/yamlx constructs Python values from these nodes (yaml.safe_load).
// backend/utils/ruleset_content.py works on PyYAML's token stream and node
// tree, slicing the source text by mark indices and showing PyYAML's error
// text (marks and snippets included) to the user, so the exact tokens, marks,
// accepted documents and error messages matter. gopkg.in/yaml.v3 (libyaml)
// differs in all of these. Marks count code points, like Python string
// indices. The libyaml fast path of ruleset_content (CSafeLoader for simple
// payload lists) is not reproduced: it is chosen only where it gives the same
// nodes and tokens as the pure-Python loader.
//
// Differences: Python's RecursionError for deeply nested collections is
// modelled by a fixed nesting limit (RecursionLimit); a \U escape naming a
// lone surrogate becomes U+FFFD (Go strings cannot hold surrogates).
package pyyaml

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/jsonx"
)

// Mark is yaml.error.Mark for a text stream.
type Mark struct {
	Name                string
	Index, Line, Column int
	buffer              []rune
	pointer             int
}

func inSet(ch rune, set string) bool { return strings.ContainsRune(set, ch) }

// snippet is Mark.get_snippet(indent=4, max_length=75).
func (m Mark) snippet() (string, bool) {
	if m.buffer == nil {
		return "", false
	}
	const indent = 4
	const half = 75.0/2 - 1
	head := ""
	start := m.pointer
	for start > 0 && !inSet(m.buffer[start-1], "\x00\r\n\u0085  ") {
		start--
		if float64(m.pointer-start) > half {
			head = " ... "
			start += 5
			break
		}
	}
	tail := ""
	end := m.pointer
	for end < len(m.buffer) && !inSet(m.buffer[end], "\x00\r\n\u0085  ") {
		end++
		if float64(end-m.pointer) > half {
			tail = " ... "
			end -= 5
			break
		}
	}
	text := ""
	if start < end {
		text = string(m.buffer[start:end])
	}
	return strings.Repeat(" ", indent) + head + text + tail + "\n" +
		strings.Repeat(" ", indent+m.pointer-start+len(head)) + "^", true
}

// String is Mark.__str__.
func (m Mark) String() string {
	where := fmt.Sprintf("  in \"%s\", line %d, column %d", m.Name, m.Line+1, m.Column+1)
	if s, ok := m.snippet(); ok {
		where += ":\n" + s
	}
	return where
}

// Error is a MarkedYAMLError (ScannerError, ParserError, ComposerError) or a
// ReaderError (Class "ReaderError"). Empty strings and nil marks stand for
// Python's None.
type Error struct {
	Class       string
	Context     string
	ContextMark *Mark
	Problem     string
	ProblemMark *Mark
	// ReaderError fields.
	Position  int
	Character rune
	Name      string
}

// Error is str(exc).
func (e *Error) Error() string {
	if e.Class == "ReaderError" {
		name := e.Name
		if name == "" {
			name = defaultName
		}
		return fmt.Sprintf("unacceptable character #x%04x: special characters are not allowed\n  in \"%s\", position %d",
			e.Character, name, e.Position)
	}
	var lines []string
	if e.Context != "" {
		lines = append(lines, e.Context)
	}
	if e.ContextMark != nil && (e.Problem == "" || e.ProblemMark == nil ||
		e.ContextMark.Name != e.ProblemMark.Name || e.ContextMark.Line != e.ProblemMark.Line ||
		e.ContextMark.Column != e.ProblemMark.Column) {
		lines = append(lines, e.ContextMark.String())
	}
	if e.Problem != "" {
		lines = append(lines, e.Problem)
	}
	if e.ProblemMark != nil {
		lines = append(lines, e.ProblemMark.String())
	}
	return strings.Join(lines, "\n")
}

// Marked reports whether the error has `problem`/`problem_mark` attributes
// (every class but ReaderError).
func (e *Error) Marked() bool { return e.Class != "ReaderError" }

// ValueError is a Python ValueError raised while scanning (chr() of a code
// point beyond U+10FFFF).
type ValueError struct{ Message string }

func (e *ValueError) Error() string { return e.Message }

// RecursionError stands for Python's RecursionError on deeply nested input.
type RecursionError struct{ Message string }

func (e *RecursionError) Error() string { return e.Message }

// RecursionLimit is the collection nesting depth at which composing fails
// with RecursionError (CPython's default recursion limit of 1000 frames
// allows about 495 nested collections in yaml.compose).
var RecursionLimit = 490

func newError(class, context string, contextMark *Mark, problem string, problemMark Mark) *Error {
	return &Error{Class: class, Context: context, ContextMark: contextMark, Problem: problem, ProblemMark: &problemMark}
}

func scannerError(context string, contextMark *Mark, problem string, problemMark Mark) *Error {
	return newError("ScannerError", context, contextMark, problem, problemMark)
}

// TokenKind identifies a token class.
type TokenKind int

// Token classes of yaml/tokens.py.
const (
	TokenDirective TokenKind = iota
	TokenDocumentStart
	TokenDocumentEnd
	TokenStreamStart
	TokenStreamEnd
	TokenBlockSequenceStart
	TokenBlockMappingStart
	TokenBlockEnd
	TokenFlowSequenceStart
	TokenFlowMappingStart
	TokenFlowSequenceEnd
	TokenFlowMappingEnd
	TokenKey
	TokenValue
	TokenBlockEntry
	TokenFlowEntry
	TokenAlias
	TokenAnchor
	TokenTag
	TokenScalar
)

var tokenIDs = [...]string{
	"<directive>", "<document start>", "<document end>", "<stream start>", "<stream end>",
	"<block sequence start>", "<block mapping start>", "<block end>", "[", "{", "]", "}",
	"?", ":", "-", ",", "<alias>", "<anchor>", "<tag>", "<scalar>",
}

// ID is the token's `id` attribute.
func (k TokenKind) ID() string { return tokenIDs[k] }

// Token is one scanner token.
type Token struct {
	Kind       TokenKind
	Start, End Mark
	// Value of ALIAS, ANCHOR, SCALAR; the directive name for DIRECTIVE.
	Value string
	Plain bool
	Style rune // 0 (None), '\'', '"', '|', '>'
	// TAG: handle (HasHandle false = None) and suffix.
	Handle    string
	HasHandle bool
	Suffix    string
	// DIRECTIVE values: YAML (major, minor) or TAG (handle, prefix).
	Major, Minor   int
	TagHandle, Tag string
}

type simpleKey struct {
	tokenNumber         int
	required            bool
	index, line, column int
	mark                Mark
}

type scanner struct {
	buffer              []rune
	pointer             int
	index, line, column int

	done              bool
	flowLevel         int
	tokens            []Token
	tokensTaken       int
	indent            int
	indents           []int
	allowSimpleKey    bool
	possibleSimpleKey map[int]*simpleKey

	name string
	// libyaml switches to libyaml's (CSafeLoader's) scanner rules where
	// they differ from PyYAML's pure scanner (see ComposeOptions.Libyaml).
	libyaml bool
}

const defaultName = "<unicode string>"

func printable(r rune) bool {
	return r == 0x09 || r == 0x0A || r == 0x0D || (r >= 0x20 && r <= 0x7E) || r == 0x85 ||
		(r >= 0xA0 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF)
}

func newScanner(text, name string) (*scanner, error) {
	if name == "" {
		name = defaultName
	}
	runes := []rune(text)
	for i, r := range runes {
		if !printable(r) {
			return nil, &Error{Class: "ReaderError", Position: i, Character: r, Name: name}
		}
	}
	s := &scanner{buffer: append(runes, 0), indent: -1, allowSimpleKey: true, possibleSimpleKey: map[int]*simpleKey{}, name: name}
	mark := s.mark()
	s.tokens = append(s.tokens, Token{Kind: TokenStreamStart, Start: mark, End: mark})
	return s, nil
}

// Reader methods.

func (s *scanner) peek(i int) rune {
	if s.pointer+i < len(s.buffer) {
		return s.buffer[s.pointer+i]
	}
	return 0
}

func (s *scanner) prefix(length int) string {
	end := s.pointer + length
	if end > len(s.buffer) {
		end = len(s.buffer)
	}
	return string(s.buffer[s.pointer:end])
}

func (s *scanner) forward(length int) {
	for ; length > 0; length-- {
		ch := s.buffer[s.pointer]
		s.pointer++
		s.index++
		if inSet(ch, "\n\u0085  ") || (ch == '\r' && s.peek(0) != '\n') {
			s.line++
			s.column = 0
		} else if ch != '\uFEFF' || s.libyaml {
			s.column++
		}
	}
}

// inlineSpace is a separating blank inside a line: a space, and with
// libyaml's rules also a tab.
func (s *scanner) inlineSpace(ch rune) bool { return ch == ' ' || (s.libyaml && ch == '\t') }

func (s *scanner) mark() Mark {
	return Mark{Name: s.name, Index: s.index, Line: s.line, Column: s.column, buffer: s.buffer, pointer: s.pointer}
}

func reprRune(ch rune) string { return jsonx.Repr(string(ch)) }

// Public methods (the scanner reports errors through panics internally).

type scanPanic struct{ err error }

func (s *scanner) fail(err error) { panic(scanPanic{err}) }

func (s *scanner) checkToken(kinds ...TokenKind) bool {
	for s.needMoreTokens() {
		s.fetchMoreTokens()
	}
	if len(s.tokens) > 0 {
		if len(kinds) == 0 {
			return true
		}
		for _, k := range kinds {
			if s.tokens[0].Kind == k {
				return true
			}
		}
	}
	return false
}

func (s *scanner) peekToken() *Token {
	for s.needMoreTokens() {
		s.fetchMoreTokens()
	}
	if len(s.tokens) > 0 {
		return &s.tokens[0]
	}
	return nil
}

func (s *scanner) getToken() Token {
	for s.needMoreTokens() {
		s.fetchMoreTokens()
	}
	t := s.tokens[0]
	s.tokens = s.tokens[1:]
	s.tokensTaken++
	return t
}

func (s *scanner) needMoreTokens() bool {
	if s.done {
		return false
	}
	if len(s.tokens) == 0 {
		return true
	}
	s.staleSimpleKeys()
	n, ok := s.nextPossibleSimpleKey()
	return ok && n == s.tokensTaken
}

func (s *scanner) fetchMoreTokens() {
	s.scanToNextToken()
	s.staleSimpleKeys()
	s.unwindIndent(s.column)
	ch := s.peek(0)
	switch {
	case ch == 0:
		s.fetchStreamEnd()
	case ch == '%' && s.column == 0:
		s.fetchDirective()
	case ch == '-' && s.checkDocumentIndicator("---"):
		s.fetchDocumentIndicator(TokenDocumentStart)
	case ch == '.' && s.checkDocumentIndicator("..."):
		s.fetchDocumentIndicator(TokenDocumentEnd)
	case ch == '[':
		s.fetchFlowCollectionStart(TokenFlowSequenceStart)
	case ch == '{':
		s.fetchFlowCollectionStart(TokenFlowMappingStart)
	case ch == ']':
		s.fetchFlowCollectionEnd(TokenFlowSequenceEnd)
	case ch == '}':
		s.fetchFlowCollectionEnd(TokenFlowMappingEnd)
	case ch == ',':
		s.fetchFlowEntry()
	case ch == '-' && inSet(s.peek(1), "\x00 \t\r\n\u0085  "):
		s.fetchBlockEntry()
	case ch == '?' && (s.flowLevel != 0 || inSet(s.peek(1), "\x00 \t\r\n\u0085  ")):
		s.fetchKey()
	case ch == ':' && (s.flowLevel != 0 || inSet(s.peek(1), "\x00 \t\r\n\u0085  ")):
		s.fetchValue()
	case ch == '*':
		s.fetchAnchor(TokenAlias)
	case ch == '&':
		s.fetchAnchor(TokenAnchor)
	case ch == '!':
		s.saveSimpleKey()
		s.allowSimpleKey = false
		s.tokens = append(s.tokens, s.scanTag())
	case ch == '|' && s.flowLevel == 0:
		s.fetchBlockScalar('|')
	case ch == '>' && s.flowLevel == 0:
		s.fetchBlockScalar('>')
	case ch == '\'':
		s.fetchFlowScalar('\'')
	case ch == '"':
		s.fetchFlowScalar('"')
	case s.checkPlain():
		s.saveSimpleKey()
		s.allowSimpleKey = false
		s.tokens = append(s.tokens, s.scanPlain())
	default:
		s.fail(scannerError("while scanning for the next token", nil,
			fmt.Sprintf("found character %s that cannot start any token", reprRune(ch)), s.mark()))
	}
}

// Simple keys.

func (s *scanner) nextPossibleSimpleKey() (int, bool) {
	found := false
	min := 0
	for _, key := range s.possibleSimpleKey {
		if !found || key.tokenNumber < min {
			min, found = key.tokenNumber, true
		}
	}
	return min, found
}

func (s *scanner) staleSimpleKeys() {
	for level, key := range s.possibleSimpleKey {
		if key.line != s.line || s.index-key.index > 1024 {
			if key.required {
				mark := key.mark
				s.fail(scannerError("while scanning a simple key", &mark, "could not find expected ':'", s.mark()))
			}
			delete(s.possibleSimpleKey, level)
		}
	}
}

func (s *scanner) saveSimpleKey() {
	required := s.flowLevel == 0 && s.indent == s.column
	if s.allowSimpleKey {
		s.removeSimpleKey()
		s.possibleSimpleKey[s.flowLevel] = &simpleKey{
			tokenNumber: s.tokensTaken + len(s.tokens), required: required,
			index: s.index, line: s.line, column: s.column, mark: s.mark(),
		}
	}
}

func (s *scanner) removeSimpleKey() {
	if key, ok := s.possibleSimpleKey[s.flowLevel]; ok {
		if key.required {
			mark := key.mark
			s.fail(scannerError("while scanning a simple key", &mark, "could not find expected ':'", s.mark()))
		}
		delete(s.possibleSimpleKey, s.flowLevel)
	}
}

// Indentation.

func (s *scanner) unwindIndent(column int) {
	if s.flowLevel != 0 {
		return
	}
	for s.indent > column {
		mark := s.mark()
		s.indent = s.indents[len(s.indents)-1]
		s.indents = s.indents[:len(s.indents)-1]
		s.tokens = append(s.tokens, Token{Kind: TokenBlockEnd, Start: mark, End: mark})
	}
}

func (s *scanner) addIndent(column int) bool {
	if s.indent < column {
		s.indents = append(s.indents, s.indent)
		s.indent = column
		return true
	}
	return false
}

// Fetchers.

func (s *scanner) simpleToken(kind TokenKind, length int) {
	start := s.mark()
	s.forward(length)
	s.tokens = append(s.tokens, Token{Kind: kind, Start: start, End: s.mark()})
}

func (s *scanner) fetchStreamEnd() {
	s.unwindIndent(-1)
	s.removeSimpleKey()
	s.allowSimpleKey = false
	s.possibleSimpleKey = map[int]*simpleKey{}
	mark := s.mark()
	s.tokens = append(s.tokens, Token{Kind: TokenStreamEnd, Start: mark, End: mark})
	s.done = true
}

func (s *scanner) fetchDirective() {
	s.unwindIndent(-1)
	s.removeSimpleKey()
	s.allowSimpleKey = false
	s.tokens = append(s.tokens, s.scanDirective())
}

func (s *scanner) fetchDocumentIndicator(kind TokenKind) {
	s.unwindIndent(-1)
	s.removeSimpleKey()
	s.allowSimpleKey = false
	s.simpleToken(kind, 3)
}

func (s *scanner) fetchFlowCollectionStart(kind TokenKind) {
	s.saveSimpleKey()
	s.flowLevel++
	s.allowSimpleKey = true
	s.simpleToken(kind, 1)
}

func (s *scanner) fetchFlowCollectionEnd(kind TokenKind) {
	s.removeSimpleKey()
	s.flowLevel--
	s.allowSimpleKey = false
	s.simpleToken(kind, 1)
}

func (s *scanner) fetchFlowEntry() {
	s.allowSimpleKey = true
	s.removeSimpleKey()
	s.simpleToken(TokenFlowEntry, 1)
}

func (s *scanner) fetchBlockEntry() {
	if s.flowLevel == 0 {
		if !s.allowSimpleKey {
			s.fail(scannerError("", nil, "sequence entries are not allowed here", s.mark()))
		}
		if s.addIndent(s.column) {
			mark := s.mark()
			s.tokens = append(s.tokens, Token{Kind: TokenBlockSequenceStart, Start: mark, End: mark})
		}
	}
	s.allowSimpleKey = true
	s.removeSimpleKey()
	s.simpleToken(TokenBlockEntry, 1)
}

func (s *scanner) fetchKey() {
	if s.flowLevel == 0 {
		if !s.allowSimpleKey {
			s.fail(scannerError("", nil, "mapping keys are not allowed here", s.mark()))
		}
		if s.addIndent(s.column) {
			mark := s.mark()
			s.tokens = append(s.tokens, Token{Kind: TokenBlockMappingStart, Start: mark, End: mark})
		}
	}
	s.allowSimpleKey = s.flowLevel == 0
	s.removeSimpleKey()
	s.simpleToken(TokenKey, 1)
}

func (s *scanner) insertToken(at int, t Token) {
	s.tokens = append(s.tokens, Token{})
	copy(s.tokens[at+1:], s.tokens[at:])
	s.tokens[at] = t
}

func (s *scanner) fetchValue() {
	if key, ok := s.possibleSimpleKey[s.flowLevel]; ok {
		delete(s.possibleSimpleKey, s.flowLevel)
		s.insertToken(key.tokenNumber-s.tokensTaken, Token{Kind: TokenKey, Start: key.mark, End: key.mark})
		if s.flowLevel == 0 && s.addIndent(key.column) {
			s.insertToken(key.tokenNumber-s.tokensTaken, Token{Kind: TokenBlockMappingStart, Start: key.mark, End: key.mark})
		}
		s.allowSimpleKey = false
	} else {
		if s.flowLevel == 0 {
			if !s.allowSimpleKey {
				s.fail(scannerError("", nil, "mapping values are not allowed here", s.mark()))
			}
			if s.addIndent(s.column) {
				mark := s.mark()
				s.tokens = append(s.tokens, Token{Kind: TokenBlockMappingStart, Start: mark, End: mark})
			}
		}
		s.allowSimpleKey = s.flowLevel == 0
		s.removeSimpleKey()
	}
	s.simpleToken(TokenValue, 1)
}

func (s *scanner) fetchAnchor(kind TokenKind) {
	s.saveSimpleKey()
	s.allowSimpleKey = false
	s.tokens = append(s.tokens, s.scanAnchor(kind))
}

func (s *scanner) fetchBlockScalar(style rune) {
	s.allowSimpleKey = true
	s.removeSimpleKey()
	s.tokens = append(s.tokens, s.scanBlockScalar(style))
}

func (s *scanner) fetchFlowScalar(style rune) {
	s.saveSimpleKey()
	s.allowSimpleKey = false
	s.tokens = append(s.tokens, s.scanFlowScalar(style))
}

// Checkers.

const breakz = "\x00 \t\r\n\u0085  "

func (s *scanner) checkDocumentIndicator(indicator string) bool {
	return s.column == 0 && s.prefix(3) == indicator && inSet(s.peek(3), breakz)
}

func (s *scanner) checkPlain() bool {
	ch := s.peek(0)
	return !inSet(ch, "\x00 \t\r\n\u0085  -?:,[]{}#&*!|>'\"%@`") ||
		(!inSet(s.peek(1), breakz) && (ch == '-' || (s.flowLevel == 0 && inSet(ch, "?:"))))
}

// Scanners.

func (s *scanner) scanToNextToken() {
	if s.index == 0 && s.peek(0) == '\uFEFF' {
		s.forward(1)
		if s.libyaml {
			// libyaml's reader drops a leading byte order mark: marks do
			// not count it.
			s.index, s.column = 0, 0
		}
	}
	for {
		if s.libyaml && s.column == 0 && s.peek(0) == '\uFEFF' {
			// libyaml lets a byte order mark start any line.
			s.forward(1)
		}
		// libyaml also skips tabs in flow context and where no simple key
		// may start (after a scalar, an alias, a tag, a simple key's ":").
		for s.peek(0) == ' ' || (s.libyaml && s.peek(0) == '\t' && (s.flowLevel != 0 || !s.allowSimpleKey)) {
			s.forward(1)
		}
		if s.peek(0) == '#' {
			for !inSet(s.peek(0), "\x00\r\n\u0085  ") {
				s.forward(1)
			}
		}
		if s.scanLineBreak() != "" {
			if s.flowLevel == 0 {
				s.allowSimpleKey = true
			}
		} else {
			return
		}
	}
}

func isAlnumDash(ch rune) bool {
	return ch >= '0' && ch <= '9' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch == '-' || ch == '_'
}

func (s *scanner) scanDirective() Token {
	start := s.mark()
	s.forward(1)
	directive := s.scanDirectiveName(start)
	t := Token{Kind: TokenDirective, Value: directive, Start: start}
	switch directive {
	case "YAML":
		t.Major, t.Minor = s.scanYAMLDirectiveValue(start)
		t.End = s.mark()
	case "TAG":
		t.TagHandle, t.Tag = s.scanTagDirectiveValue(start)
		t.End = s.mark()
	default:
		t.End = s.mark()
		for !inSet(s.peek(0), "\x00\r\n\u0085  ") {
			s.forward(1)
		}
	}
	s.scanDirectiveIgnoredLine(start)
	return t
}

func (s *scanner) scanDirectiveName(start Mark) string {
	length := 0
	ch := s.peek(length)
	for isAlnumDash(ch) {
		length++
		ch = s.peek(length)
	}
	if length == 0 {
		s.fail(scannerError("while scanning a directive", &start,
			"expected alphabetic or numeric character, but found "+reprRune(ch), s.mark()))
	}
	value := s.prefix(length)
	s.forward(length)
	ch = s.peek(0)
	if !inSet(ch, "\x00 \r\n\u0085  ") && !(s.libyaml && ch == '\t') {
		s.fail(scannerError("while scanning a directive", &start,
			"expected alphabetic or numeric character, but found "+reprRune(ch), s.mark()))
	}
	return value
}

func (s *scanner) scanYAMLDirectiveValue(start Mark) (int, int) {
	for s.inlineSpace(s.peek(0)) {
		s.forward(1)
	}
	major := s.scanYAMLDirectiveNumber(start)
	if s.peek(0) != '.' {
		s.fail(scannerError("while scanning a directive", &start,
			"expected a digit or '.', but found "+reprRune(s.peek(0)), s.mark()))
	}
	s.forward(1)
	minor := s.scanYAMLDirectiveNumber(start)
	if !inSet(s.peek(0), "\x00 \r\n\u0085  ") && !(s.libyaml && s.peek(0) == '\t') {
		s.fail(scannerError("while scanning a directive", &start,
			"expected a digit or ' ', but found "+reprRune(s.peek(0)), s.mark()))
	}
	return major, minor
}

func (s *scanner) scanYAMLDirectiveNumber(start Mark) int {
	ch := s.peek(0)
	if !(ch >= '0' && ch <= '9') {
		s.fail(scannerError("while scanning a directive", &start,
			"expected a digit, but found "+reprRune(ch), s.mark()))
	}
	length := 0
	for c := s.peek(length); c >= '0' && c <= '9'; c = s.peek(length) {
		length++
	}
	text := strings.TrimLeft(s.prefix(length), "0")
	value := 0
	if len(text) > 9 {
		value = 2 // only compared with 1 ("version 1.* is required")
	} else if text != "" {
		value, _ = strconv.Atoi(text)
	}
	s.forward(length)
	return value
}

func (s *scanner) scanTagDirectiveValue(start Mark) (string, string) {
	for s.inlineSpace(s.peek(0)) {
		s.forward(1)
	}
	handle := s.scanTagHandle("directive", start)
	if ch := s.peek(0); !s.inlineSpace(ch) {
		s.fail(scannerError("while scanning a directive", &start, "expected ' ', but found "+reprRune(ch), s.mark()))
	}
	for s.inlineSpace(s.peek(0)) {
		s.forward(1)
	}
	prefix := s.scanTagURI("directive", start)
	if ch := s.peek(0); !inSet(ch, "\x00 \r\n\u0085  ") && !(s.libyaml && ch == '\t') {
		s.fail(scannerError("while scanning a directive", &start, "expected ' ', but found "+reprRune(ch), s.mark()))
	}
	return handle, prefix
}

func (s *scanner) scanDirectiveIgnoredLine(start Mark) {
	for s.inlineSpace(s.peek(0)) {
		s.forward(1)
	}
	if s.peek(0) == '#' {
		for !inSet(s.peek(0), "\x00\r\n\u0085  ") {
			s.forward(1)
		}
	}
	if ch := s.peek(0); !inSet(ch, "\x00\r\n\u0085  ") {
		s.fail(scannerError("while scanning a directive", &start,
			"expected a comment or a line break, but found "+reprRune(ch), s.mark()))
	}
	s.scanLineBreak()
}

func (s *scanner) scanAnchor(kind TokenKind) Token {
	start := s.mark()
	what := "anchor"
	if s.peek(0) == '*' {
		what = "alias"
	}
	s.forward(1)
	length := 0
	ch := s.peek(length)
	for isAlnumDash(ch) {
		length++
		ch = s.peek(length)
	}
	if length == 0 {
		s.fail(scannerError("while scanning an "+what, &start,
			"expected alphabetic or numeric character, but found "+reprRune(ch), s.mark()))
	}
	value := s.prefix(length)
	s.forward(length)
	if ch := s.peek(0); !inSet(ch, "\x00 \t\r\n\u0085  ?:,]}%@`") {
		s.fail(scannerError("while scanning an "+what, &start,
			"expected alphabetic or numeric character, but found "+reprRune(ch), s.mark()))
	}
	return Token{Kind: kind, Value: value, Start: start, End: s.mark()}
}

func (s *scanner) scanTag() Token {
	start := s.mark()
	ch := s.peek(1)
	t := Token{Kind: TokenTag, Start: start}
	switch {
	case ch == '<':
		s.forward(2)
		t.Suffix = s.scanTagURI("tag", start)
		if s.peek(0) != '>' {
			s.fail(scannerError("while parsing a tag", &start, "expected '>', but found "+reprRune(s.peek(0)), s.mark()))
		}
		s.forward(1)
	case inSet(ch, breakz):
		t.Suffix = "!"
		s.forward(1)
	default:
		length := 1
		useHandle := false
		for !inSet(ch, "\x00 \r\n\u0085  ") {
			if ch == '!' {
				useHandle = true
				break
			}
			length++
			ch = s.peek(length)
		}
		t.HasHandle = true
		if useHandle {
			t.Handle = s.scanTagHandle("tag", start)
		} else {
			t.Handle = "!"
			s.forward(1)
		}
		t.Suffix = s.scanTagURI("tag", start)
	}
	if ch := s.peek(0); !inSet(ch, "\x00 \r\n\u0085  ") && !(s.libyaml && (ch == '\t' || (ch == ',' && s.flowLevel != 0))) {
		s.fail(scannerError("while scanning a tag", &start, "expected ' ', but found "+reprRune(ch), s.mark()))
	}
	t.End = s.mark()
	return t
}

func (s *scanner) scanBlockScalar(style rune) Token {
	folded := style == '>'
	var chunks []string
	start := s.mark()
	s.forward(1)
	chomping, increment := s.scanBlockScalarIndicators(start)
	s.scanBlockScalarIgnoredLine(start)
	minIndent := s.indent + 1
	if minIndent < 1 {
		minIndent = 1
	}
	var breaks []string
	var end Mark
	var indent int
	if increment == 0 {
		var maxIndent int
		breaks, maxIndent, end = s.scanBlockScalarIndentation()
		indent = max(minIndent, maxIndent)
	} else {
		indent = minIndent + increment - 1
		breaks, end = s.scanBlockScalarBreaks(indent)
	}
	lineBreak := ""
	for s.column == indent && s.peek(0) != 0 {
		chunks = append(chunks, breaks...)
		leadingNonSpace := !inSet(s.peek(0), " \t")
		length := 0
		for !inSet(s.peek(length), "\x00\r\n\u0085  ") {
			length++
		}
		chunks = append(chunks, s.prefix(length))
		s.forward(length)
		lineBreak = s.scanLineBreak()
		breaks, end = s.scanBlockScalarBreaks(indent)
		if s.column == indent && s.peek(0) != 0 {
			if folded && lineBreak == "\n" && leadingNonSpace && !inSet(s.peek(0), " \t") {
				if len(breaks) == 0 {
					chunks = append(chunks, " ")
				}
			} else {
				chunks = append(chunks, lineBreak)
			}
		} else {
			break
		}
	}
	if chomping != chompStrip {
		chunks = append(chunks, lineBreak)
	}
	if chomping == chompKeep {
		chunks = append(chunks, breaks...)
	}
	return Token{Kind: TokenScalar, Value: strings.Join(chunks, ""), Style: style, Start: start, End: end}
}

const (
	chompClip = iota
	chompKeep
	chompStrip
)

func (s *scanner) scanBlockScalarIndicators(start Mark) (int, int) {
	chomping, increment := chompClip, 0
	ch := s.peek(0)
	badZero := func() {
		s.fail(scannerError("while scanning a block scalar", &start,
			"expected indentation indicator in the range 1-9, but found 0", s.mark()))
	}
	if ch == '+' || ch == '-' {
		chomping = chompStrip
		if ch == '+' {
			chomping = chompKeep
		}
		s.forward(1)
		ch = s.peek(0)
		if ch >= '0' && ch <= '9' {
			increment = int(ch - '0')
			if increment == 0 {
				badZero()
			}
			s.forward(1)
		}
	} else if ch >= '0' && ch <= '9' {
		increment = int(ch - '0')
		if increment == 0 {
			badZero()
		}
		s.forward(1)
		ch = s.peek(0)
		if ch == '+' || ch == '-' {
			chomping = chompStrip
			if ch == '+' {
				chomping = chompKeep
			}
			s.forward(1)
		}
	}
	if ch := s.peek(0); !inSet(ch, "\x00 \r\n\u0085  ") && !(s.libyaml && ch == '\t') {
		s.fail(scannerError("while scanning a block scalar", &start,
			"expected chomping or indentation indicators, but found "+reprRune(ch), s.mark()))
	}
	return chomping, increment
}

func (s *scanner) scanBlockScalarIgnoredLine(start Mark) {
	for s.inlineSpace(s.peek(0)) {
		s.forward(1)
	}
	if s.peek(0) == '#' {
		for !inSet(s.peek(0), "\x00\r\n\u0085  ") {
			s.forward(1)
		}
	}
	if ch := s.peek(0); !inSet(ch, "\x00\r\n\u0085  ") {
		s.fail(scannerError("while scanning a block scalar", &start,
			"expected a comment or a line break, but found "+reprRune(ch), s.mark()))
	}
	s.scanLineBreak()
}

func (s *scanner) scanBlockScalarIndentation() ([]string, int, Mark) {
	var chunks []string
	maxIndent := 0
	end := s.mark()
	for inSet(s.peek(0), " \r\n\u0085  ") && s.peek(0) != 0 {
		if s.peek(0) != ' ' {
			chunks = append(chunks, s.scanLineBreak())
			end = s.mark()
		} else {
			s.forward(1)
			if s.column > maxIndent {
				maxIndent = s.column
			}
		}
	}
	return chunks, maxIndent, end
}

func (s *scanner) scanBlockScalarBreaks(indent int) ([]string, Mark) {
	var chunks []string
	end := s.mark()
	for s.column < indent && s.peek(0) == ' ' {
		s.forward(1)
	}
	for inSet(s.peek(0), "\r\n\u0085  ") && s.peek(0) != 0 {
		chunks = append(chunks, s.scanLineBreak())
		end = s.mark()
		for s.column < indent && s.peek(0) == ' ' {
			s.forward(1)
		}
	}
	return chunks, end
}

func (s *scanner) scanFlowScalar(style rune) Token {
	double := style == '"'
	var chunks []string
	start := s.mark()
	quote := s.peek(0)
	s.forward(1)
	chunks = append(chunks, s.scanFlowScalarNonSpaces(double, start)...)
	for s.peek(0) != quote {
		chunks = append(chunks, s.scanFlowScalarSpaces(double, start)...)
		chunks = append(chunks, s.scanFlowScalarNonSpaces(double, start)...)
	}
	s.forward(1)
	return Token{Kind: TokenScalar, Value: strings.Join(chunks, ""), Style: style, Start: start, End: s.mark()}
}

var escapeReplacements = map[rune]string{
	'0': "\x00", 'a': "\x07", 'b': "\x08", 't': "\x09", '\t': "\x09", 'n': "\x0A", 'v': "\x0B",
	'f': "\x0C", 'r': "\x0D", 'e': "\x1B", ' ': "\x20", '"': "\"", '\\': "\\", '/': "/",
	'N': "\u0085", '_': " ", 'L': " ", 'P': " ",
}

var escapeCodes = map[rune]int{'x': 2, 'u': 4, 'U': 8}

func isHexRune(ch rune) bool {
	return ch >= '0' && ch <= '9' || ch >= 'A' && ch <= 'F' || ch >= 'a' && ch <= 'f'
}

func (s *scanner) scanFlowScalarNonSpaces(double bool, start Mark) []string {
	var chunks []string
	for {
		length := 0
		for !inSet(s.peek(length), "'\"\\\x00 \t\r\n\u0085  ") {
			length++
		}
		if length > 0 {
			chunks = append(chunks, s.prefix(length))
			s.forward(length)
		}
		ch := s.peek(0)
		switch {
		case !double && ch == '\'' && s.peek(1) == '\'':
			chunks = append(chunks, "'")
			s.forward(2)
		case (double && ch == '\'') || (!double && (ch == '"' || ch == '\\')):
			chunks = append(chunks, string(ch))
			s.forward(1)
		case double && ch == '\\':
			s.forward(1)
			ch = s.peek(0)
			if replacement, ok := escapeReplacements[ch]; ok {
				chunks = append(chunks, replacement)
				s.forward(1)
			} else if length, ok := escapeCodes[ch]; ok {
				s.forward(1)
				for k := 0; k < length; k++ {
					if !isHexRune(s.peek(k)) {
						s.fail(scannerError("while scanning a double-quoted scalar", &start,
							fmt.Sprintf("expected escape sequence of %d hexadecimal numbers, but found %s", length, reprRune(s.peek(k))),
							s.mark()))
					}
				}
				code, _ := strconv.ParseUint(s.prefix(length), 16, 64)
				if s.libyaml && (code > 0x10FFFF || (code >= 0xD800 && code <= 0xDFFF)) {
					s.fail(scannerError("while parsing a quoted scalar", &start,
						"found invalid Unicode character escape code", s.mark()))
				}
				if code > 0x10FFFF {
					s.fail(&ValueError{"chr() arg not in range(0x110000)"})
				}
				if code >= 0xD800 && code <= 0xDFFF {
					code = utf8.RuneError
				}
				chunks = append(chunks, string(rune(code)))
				s.forward(length)
			} else if inSet(ch, "\r\n\u0085  ") && ch != 0 {
				s.scanLineBreak()
				chunks = append(chunks, s.scanFlowScalarBreaks(double, start)...)
			} else {
				s.fail(scannerError("while scanning a double-quoted scalar", &start,
					"found unknown escape character "+reprRune(ch), s.mark()))
			}
		default:
			return chunks
		}
	}
}

func (s *scanner) scanFlowScalarSpaces(double bool, start Mark) []string {
	var chunks []string
	length := 0
	for inSet(s.peek(length), " \t") && s.peek(length) != 0 {
		length++
	}
	whitespaces := s.prefix(length)
	s.forward(length)
	ch := s.peek(0)
	if ch == 0 {
		s.fail(scannerError("while scanning a quoted scalar", &start, "found unexpected end of stream", s.mark()))
	} else if inSet(ch, "\r\n\u0085  ") {
		lineBreak := s.scanLineBreak()
		breaks := s.scanFlowScalarBreaks(double, start)
		if lineBreak != "\n" {
			chunks = append(chunks, lineBreak)
		} else if len(breaks) == 0 {
			chunks = append(chunks, " ")
		}
		chunks = append(chunks, breaks...)
	} else {
		chunks = append(chunks, whitespaces)
	}
	return chunks
}

func (s *scanner) scanFlowScalarBreaks(double bool, start Mark) []string {
	var chunks []string
	for {
		prefix := s.prefix(3)
		if (prefix == "---" || prefix == "...") && inSet(s.peek(3), breakz) {
			s.fail(scannerError("while scanning a quoted scalar", &start, "found unexpected document separator", s.mark()))
		}
		for s.peek(0) == ' ' || s.peek(0) == '\t' {
			s.forward(1)
		}
		if inSet(s.peek(0), "\r\n\u0085  ") && s.peek(0) != 0 {
			chunks = append(chunks, s.scanLineBreak())
		} else {
			return chunks
		}
	}
}

func (s *scanner) scanPlain() Token {
	var chunks []string
	start := s.mark()
	end := start
	indent := s.indent + 1
	var spaces []string
	for {
		length := 0
		if s.peek(0) == '#' {
			break
		}
		for {
			ch := s.peek(length)
			stop := breakz
			if s.flowLevel != 0 {
				stop += ",[]{}"
			}
			if s.libyaml && s.flowLevel != 0 && ch == ':' && inSet(s.peek(length+1), ",?[]{}") && s.peek(length+1) != 0 {
				s.forward(length)
				s.fail(scannerError("while scanning a plain scalar", &start, "found unexpected ':'", s.mark()))
			}
			ends := ",?[]{}"
			if s.libyaml {
				ends = ",[]{}" // libyaml: "?" does not end a plain scalar
			}
			if inSet(ch, breakz) || (ch == ':' && inSet(s.peek(length+1), stop)) ||
				(s.flowLevel != 0 && inSet(ch, ends)) {
				break
			}
			length++
		}
		if length == 0 {
			break
		}
		s.allowSimpleKey = false
		chunks = append(chunks, spaces...)
		chunks = append(chunks, s.prefix(length))
		s.forward(length)
		end = s.mark()
		var ok bool
		spaces, ok = s.scanPlainSpaces(indent, start)
		if !ok || len(spaces) == 0 || s.peek(0) == '#' || (s.flowLevel == 0 && s.column < indent) {
			break
		}
	}
	return Token{Kind: TokenScalar, Value: strings.Join(chunks, ""), Plain: true, Start: start, End: end}
}

// scanPlainSpaces returns ok false where Python returns None (a document
// separator follows).
func (s *scanner) scanPlainSpaces(indent int, start Mark) ([]string, bool) {
	var chunks []string
	length := 0
	for s.peek(length) == ' ' || (s.libyaml && s.peek(length) == '\t') {
		length++
	}
	whitespaces := s.prefix(length)
	s.forward(length)
	ch := s.peek(0)
	separator := func() bool {
		prefix := s.prefix(3)
		return (prefix == "---" || prefix == "...") && inSet(s.peek(3), breakz)
	}
	if inSet(ch, "\r\n\u0085  ") && ch != 0 {
		lineBreak := s.scanLineBreak()
		s.allowSimpleKey = true
		if separator() {
			return nil, false
		}
		var breaks []string
		for (inSet(s.peek(0), " \r\n\u0085  ") || (s.libyaml && s.peek(0) == '\t')) && s.peek(0) != 0 {
			if s.peek(0) == '\t' && s.column < indent {
				s.fail(scannerError("while scanning a plain scalar", &start, "found a tab character that violates indentation", s.mark()))
			}
			if s.peek(0) == ' ' || s.peek(0) == '\t' {
				s.forward(1)
			} else {
				breaks = append(breaks, s.scanLineBreak())
				if separator() {
					return nil, false
				}
			}
		}
		if lineBreak != "\n" {
			chunks = append(chunks, lineBreak)
		} else if len(breaks) == 0 {
			chunks = append(chunks, " ")
		}
		chunks = append(chunks, breaks...)
	} else if whitespaces != "" {
		chunks = append(chunks, whitespaces)
	}
	return chunks, true
}

func (s *scanner) scanTagHandle(what string, start Mark) string {
	ch := s.peek(0)
	if ch != '!' {
		s.fail(scannerError("while scanning a "+what, &start, "expected '!', but found "+reprRune(ch), s.mark()))
	}
	length := 1
	ch = s.peek(length)
	if ch != ' ' {
		for isAlnumDash(ch) {
			length++
			ch = s.peek(length)
		}
		if ch != '!' {
			s.forward(length)
			s.fail(scannerError("while scanning a "+what, &start, "expected '!', but found "+reprRune(ch), s.mark()))
		}
		length++
	}
	value := s.prefix(length)
	s.forward(length)
	return value
}

func (s *scanner) scanTagURI(what string, start Mark) string {
	var chunks []string
	length := 0
	ch := s.peek(length)
	for (ch >= '0' && ch <= '9' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z') ||
		(ch != 0 && inSet(ch, "-;/?:@&=+$,_.!~*'()[]%")) {
		if ch == '%' {
			chunks = append(chunks, s.prefix(length))
			s.forward(length)
			length = 0
			chunks = append(chunks, s.scanURIEscapes(what, start))
		} else {
			length++
		}
		ch = s.peek(length)
	}
	if length > 0 {
		chunks = append(chunks, s.prefix(length))
		s.forward(length)
	}
	if len(chunks) == 0 {
		s.fail(scannerError("while parsing a "+what, &start, "expected URI, but found "+reprRune(ch), s.mark()))
	}
	return strings.Join(chunks, "")
}

func (s *scanner) scanURIEscapes(what string, start Mark) string {
	var codes []byte
	mark := s.mark()
	for s.peek(0) == '%' {
		s.forward(1)
		for k := 0; k < 2; k++ {
			if !isHexRune(s.peek(k)) {
				s.fail(scannerError("while scanning a "+what, &start,
					"expected URI escape sequence of 2 hexadecimal numbers, but found "+reprRune(s.peek(k)), s.mark()))
			}
		}
		code, _ := strconv.ParseUint(s.prefix(2), 16, 8)
		codes = append(codes, byte(code))
		s.forward(2)
	}
	if message := utf8DecodeError(codes); message != "" {
		s.fail(newError("ScannerError", "while scanning a "+what, &start, message, mark))
	}
	return string(codes)
}

// utf8DecodeError is str(UnicodeDecodeError) of bytes.decode('utf-8'), or ""
// when the bytes are valid UTF-8 (CPython's error ranges).
func utf8DecodeError(b []byte) string {
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			i++
			continue
		}
		var need int
		var lo, hi byte = 0x80, 0xBF
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c >= 0xE0 && c <= 0xEF:
			need = 2
			if c == 0xE0 {
				lo = 0xA0
			} else if c == 0xED {
				hi = 0x9F
			}
		case c >= 0xF0 && c <= 0xF4:
			need = 3
			if c == 0xF0 {
				lo = 0x90
			} else if c == 0xF4 {
				hi = 0x8F
			}
		default:
			return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: invalid start byte", c, i)
		}
		j := 1
		for ; j <= need; j++ {
			if i+j >= len(b) {
				if j == 1 {
					return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: unexpected end of data", c, i)
				}
				return fmt.Sprintf("'utf-8' codec can't decode bytes in position %d-%d: unexpected end of data", i, i+j-1)
			}
			d := b[i+j]
			low, high := byte(0x80), byte(0xBF)
			if j == 1 {
				low, high = lo, hi
			}
			if d < low || d > high {
				if j == 1 {
					return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: invalid continuation byte", c, i)
				}
				return fmt.Sprintf("'utf-8' codec can't decode bytes in position %d-%d: invalid continuation byte", i, i+j-1)
			}
		}
		i += need + 1
	}
	return ""
}

func (s *scanner) scanLineBreak() string {
	ch := s.peek(0)
	if ch == '\r' || ch == '\n' || ch == '\u0085' {
		if s.prefix(2) == "\r\n" {
			s.forward(2)
		} else {
			s.forward(1)
		}
		return "\n"
	} else if ch == ' ' || ch == ' ' {
		s.forward(1)
		return string(ch)
	}
	return ""
}

// Scan is list(yaml.scan(text)): every token of the stream.
func Scan(text string) (tokens []Token, err error) {
	s, err := newScanner(text, "")
	if err != nil {
		return nil, err
	}
	defer recoverScan(&err)
	for s.checkToken() {
		tokens = append(tokens, s.getToken())
	}
	return tokens, nil
}

func recoverScan(err *error) {
	if r := recover(); r != nil {
		p, ok := r.(scanPanic)
		if !ok {
			panic(r)
		}
		*err = p.err
	}
}
