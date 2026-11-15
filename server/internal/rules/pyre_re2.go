package rules

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/dlclark/regexp2"
)

// UnsupportedRegexError reports a pattern that Python's re accepts but that
// neither engine can run. After the backtracking fallback this is only
// \N{name} escapes (no Unicode name table) and repeat counts beyond the
// fallback engine's int32 range.
type UnsupportedRegexError struct{ Reason string }

func (e *UnsupportedRegexError) Error() string {
	return "regex construct not supported by the Go engine: " + e.Reason
}

// errFallback is the translator's signal that RE2 cannot express a pattern
// exactly and the backtracking engine must run it.
var errFallback = errors.New("needs the backtracking engine")

// BacktrackTimeout bounds one match of a pattern run by the backtracking
// engine. A hostile pattern must not hang the server: on timeout the match
// counts as "no match" and one warning is logged per pattern.
var BacktrackTimeout = time.Second

var (
	regexLoggerMu sync.RWMutex
	regexLogger   Logger = nopLogger{}
)

// SetRegexLogger sets the logger for backtracking timeouts.
func SetRegexLogger(logger Logger) {
	regexLoggerMu.Lock()
	defer regexLoggerMu.Unlock()
	if logger == nil {
		logger = nopLogger{}
	}
	regexLogger = logger
}

// PyRegex is a compiled Python pattern. RE2 runs it when it can do so
// exactly; otherwise a backtracking engine (regexp2) does.
type PyRegex struct {
	re      *regexp.Regexp
	net     *regexp2.Regexp
	pattern string
	warned  atomic.Bool
	parsed  *parsedRegex
	sub     subEngines
}

// Engine names the engine behind the pattern: "re2" or "backtrack"
// (diagnostics and tests).
func (r *PyRegex) Engine() string {
	if r.re != nil {
		return "re2"
	}
	return "backtrack"
}

// Search is re.search(...) is not None. A timeout of the backtracking engine
// is "no match" plus a single warning.
func (r *PyRegex) Search(text string) bool {
	if r.re != nil {
		return r.re.MatchString(text)
	}
	matched, err := r.net.MatchString(text)
	if err != nil {
		if r.warned.CompareAndSwap(false, true) {
			regexLoggerMu.RLock()
			logger := regexLogger
			regexLoggerMu.RUnlock()
			logger.Warningf("Regex %q exceeded the %s match time limit; treating it as no match", r.pattern, BacktrackTimeout)
		}
		return false
	}
	return matched
}

var (
	regexCacheMu sync.Mutex
	regexCache   = map[regexKey]regexEntry{}
)

type regexKey struct {
	pattern    string
	ignoreCase bool
}

type regexEntry struct {
	re  *PyRegex
	err error
}

// CompilePyRegex is re.compile(pattern[, re.IGNORECASE]). The error is one of
// *reError (Python's re.error, whose Error() text is Python's), errRecursion /
// errOverflow (the other exceptions compile can raise) or
// *UnsupportedRegexError. Results are cached like Python's re module does.
func CompilePyRegex(pattern string, ignoreCase bool) (*PyRegex, error) {
	key := regexKey{pattern, ignoreCase}
	regexCacheMu.Lock()
	entry, ok := regexCache[key]
	regexCacheMu.Unlock()
	if ok {
		return entry.re, entry.err
	}
	re, err := compilePyRegex(pattern, ignoreCase)
	regexCacheMu.Lock()
	if len(regexCache) >= 512 {
		regexCache = map[regexKey]regexEntry{}
	}
	regexCache[key] = regexEntry{re, err}
	regexCacheMu.Unlock()
	return re, err
}

// IsPyRegexError reports whether err is a pattern Python itself rejects (as
// opposed to a pattern only this implementation cannot run).
func IsPyRegexError(err error) bool {
	var unsupported *UnsupportedRegexError
	return err != nil && !errors.As(err, &unsupported)
}

func compilePyRegex(pattern string, ignoreCase bool) (*PyRegex, error) {
	flags := 0
	if ignoreCase {
		flags = flagIgnoreCase
	}
	parsed, err := parsePyRegex(pattern, flags)
	if err != nil {
		return nil, err
	}
	if source, err := translateRE2(parsed); err == nil {
		if re, err := regexp.Compile(source); err == nil {
			return &PyRegex{re: re, pattern: pattern, parsed: parsed}, nil
		}
		// Too-large repeat counts and similar: let the backtracker try.
	} else if !errors.Is(err, errFallback) {
		return nil, err
	}
	source, err := translateNet(parsed)
	if err != nil {
		return nil, err
	}
	net, err := regexp2.Compile(source, regexp2.None)
	if err != nil {
		return nil, &UnsupportedRegexError{Reason: err.Error()}
	}
	net.MatchTimeout = BacktrackTimeout
	return &PyRegex{net: net, pattern: pattern, parsed: parsed}, nil
}

func flagLetters(add, del int) string {
	letters := func(flags int) string {
		var b strings.Builder
		if flags&flagIgnoreCase != 0 {
			b.WriteByte('i')
		}
		if flags&flagMultiline != 0 {
			b.WriteByte('m')
		}
		if flags&flagDotAll != 0 {
			b.WriteByte('s')
		}
		return b.String()
	}
	result := letters(add)
	if d := letters(del); d != "" {
		result += "-" + d
	}
	return result
}

// translateRE2 builds the RE2 program, or errFallback where RE2 would differ
// from Python (look-around, back references, atomic groups, possessive
// quantifiers, conditionals, Unicode word boundaries, "$" that is not the very
// end of the pattern).
func translateRE2(parsed *parsedRegex) (string, error) {
	tr := &translator{ascii: parsed.flags&flagASCII != 0, multiline: parsed.flags&flagMultiline != 0}
	if prefix := flagLetters(parsed.flags, 0); prefix != "" {
		tr.b.WriteString("(?" + prefix + ")")
	}
	if err := tr.nodes(parsed.nodes, true); err != nil {
		return "", err
	}
	return tr.b.String(), nil
}

type translator struct {
	b         strings.Builder
	ascii     bool
	multiline bool
}

func (tr *translator) nodes(nodes []*reNode, tail bool) error {
	for i, n := range nodes {
		if err := tr.node(n, tail && i == len(nodes)-1); err != nil {
			return err
		}
	}
	return nil
}

func (tr *translator) lit(r rune) {
	if r < 128 && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
		tr.b.WriteRune(r)
		return
	}
	fmt.Fprintf(&tr.b, `\x{%x}`, r)
}

// scoped applies a group's own flags to the translator state for body.
func (tr *translator) scoped(add, del int, body func() error) error {
	ascii, multiline := tr.ascii, tr.multiline
	defer func() { tr.ascii, tr.multiline = ascii, multiline }()
	if add&flagASCII != 0 {
		tr.ascii = true
	}
	if add&flagUnicode != 0 {
		tr.ascii = false
	}
	if add&flagMultiline != 0 {
		tr.multiline = true
	}
	if del&flagMultiline != 0 {
		tr.multiline = false
	}
	return body()
}

func (tr *translator) node(n *reNode, tail bool) error {
	switch n.op {
	case opLiteral:
		tr.lit(n.ch)
	case opNotLiteral:
		tr.b.WriteString(`[^`)
		tr.lit(n.ch)
		tr.b.WriteString(`]`)
	case opAny:
		tr.b.WriteString(`.`)
	case opIn:
		tr.b.WriteString(`[`)
		for _, item := range n.set {
			switch item.kind {
			case setNegate:
				tr.b.WriteString(`^`)
			case setLiteral:
				tr.lit(item.lo)
			case setRange:
				tr.lit(item.lo)
				tr.b.WriteString(`-`)
				tr.lit(item.hi)
			case setCategory:
				tr.b.WriteString(categoryClass(item.cat, tr.ascii))
			}
		}
		tr.b.WriteString(`]`)
	case opAt:
		switch n.at {
		case atBeginning:
			tr.b.WriteString(`^`)
		case atBeginningString:
			tr.b.WriteString(`\A`)
		case atEndString:
			tr.b.WriteString(`\z`)
		case atEnd:
			switch {
			case tr.multiline:
				tr.b.WriteString(`$`)
			case tail:
				// Python's "$" also matches before one final newline; nothing
				// follows it on this path, so consuming that newline is safe.
				tr.b.WriteString(`(?:\n?\z)`)
			default:
				return errFallback
			}
		default: // \b, \B: RE2's are ASCII-only
			return errFallback
		}
	case opBranch:
		tr.b.WriteString(`(?:`)
		for i, alt := range n.alts {
			if i > 0 {
				tr.b.WriteString(`|`)
			}
			if err := tr.nodes(alt, tail); err != nil {
				return err
			}
		}
		tr.b.WriteString(`)`)
	case opSubpattern:
		flags := flagLetters(n.add, n.del)
		switch {
		case n.group != 0 && flags == "":
			tr.b.WriteString(`(`)
		case flags == "":
			tr.b.WriteString(`(?:`)
		default:
			tr.b.WriteString(`(?` + flags + `:`)
		}
		if err := tr.scoped(n.add, n.del, func() error { return tr.nodes(n.body, tail) }); err != nil {
			return err
		}
		tr.b.WriteString(`)`)
	case opMaxRepeat, opMinRepeat:
		tr.b.WriteString(`(?:`)
		if err := tr.nodes(n.body, false); err != nil {
			return err
		}
		tr.b.WriteString(`)`)
		if n.max == reMaxRepeat {
			fmt.Fprintf(&tr.b, `{%d,}`, n.min)
		} else {
			fmt.Fprintf(&tr.b, `{%d,%d}`, n.min, n.max)
		}
		if n.op == opMinRepeat {
			tr.b.WriteString(`?`)
		}
	default: // possessive, atomic, assertions, references, conditionals
		return errFallback
	}
	return nil
}

// Character-category class bodies. The positive Unicode categories use RE2's
// property classes; negated ones, which RE2 cannot express inside a larger
// class, are spelled as explicit complement ranges.
func categoryClass(cat catKind, ascii bool) string {
	switch cat {
	case catDigit:
		if ascii {
			return `0-9`
		}
		return `\p{Nd}`
	case catNotDigit:
		if ascii {
			return `\x{0}-/:-\x{10ffff}`
		}
		return `\P{Nd}`
	case catWord:
		if ascii {
			return `0-9A-Za-z_`
		}
		return `\p{L}\p{N}_`
	case catNotWord:
		if ascii {
			return `\x{0}-/:-@\[-\^` + "`" + `{-\x{10ffff}`
		}
		return notWordRanges()
	case catSpace:
		if ascii {
			return `\t-\r `
		}
		return spaceRanges
	case catNotSpace:
		if ascii {
			return `\x{0}-\x{8}\x{e}-\x{1f}!-\x{10ffff}`
		}
		return notSpaceRanges()
	}
	return ``
}

// spaceRanges is str.isspace() in a class body.
const spaceRanges = `\t-\r\x{1c}-\x{20}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}`

var (
	notWordOnce, notSpaceOnce sync.Once
	notWordClass, notSpaceStr string
)

type span struct{ lo, hi rune }

func complementClass(spans []span) string {
	sort.Slice(spans, func(i, j int) bool { return spans[i].lo < spans[j].lo })
	var b strings.Builder
	next := rune(0)
	for _, s := range spans {
		if s.lo > next {
			fmt.Fprintf(&b, `\x{%x}-\x{%x}`, next, s.lo-1)
		}
		if s.hi+1 > next {
			next = s.hi + 1
		}
	}
	if next <= unicode.MaxRune {
		fmt.Fprintf(&b, `\x{%x}-\x{%x}`, next, unicode.MaxRune)
	}
	return b.String()
}

func tableSpans(tables ...*unicode.RangeTable) []span {
	var spans []span
	for _, table := range tables {
		for _, r := range table.R16 {
			if r.Stride == 1 {
				spans = append(spans, span{rune(r.Lo), rune(r.Hi)})
				continue
			}
			for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
				spans = append(spans, span{c, c})
			}
		}
		for _, r := range table.R32 {
			if r.Stride == 1 {
				spans = append(spans, span{rune(r.Lo), rune(r.Hi)})
				continue
			}
			for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
				spans = append(spans, span{c, c})
			}
		}
	}
	return spans
}

func notWordRanges() string {
	notWordOnce.Do(func() {
		spans := tableSpans(unicode.L, unicode.N)
		spans = append(spans, span{'_', '_'})
		notWordClass = complementClass(spans)
	})
	return notWordClass
}

func notSpaceRanges() string {
	notSpaceOnce.Do(func() {
		var spans []span
		for r := rune(0); r <= 0x3000; r++ {
			if pyIsSpace(r) {
				spans = append(spans, span{r, r})
			}
		}
		notSpaceStr = complementClass(spans)
	})
	return notSpaceStr
}
