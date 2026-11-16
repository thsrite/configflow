package rules

import (
	"fmt"
	"strings"
)

// Translation of the parsed Python pattern into regexp2 (.NET) syntax for the
// patterns RE2 cannot run exactly. It works from the syntax tree, never the
// raw text, so Python's meaning is kept: group numbers follow Python (names
// are dropped), \Z becomes \z, \d \w \s \b follow Python's Unicode/ASCII
// rules through explicit classes, possessive quantifiers become atomic
// groups, and scoped flags stay scoped.

type netTranslator struct {
	b     strings.Builder
	ascii bool
}

func translateNet(parsed *parsedRegex) (string, error) {
	tr := &netTranslator{ascii: parsed.flags&flagASCII != 0}
	if prefix := flagLetters(parsed.flags, 0); prefix != "" {
		tr.b.WriteString("(?" + prefix + ")")
	}
	if err := tr.nodes(parsed.nodes); err != nil {
		return "", err
	}
	return tr.b.String(), nil
}

func (tr *netTranslator) nodes(nodes []*reNode) error {
	for _, n := range nodes {
		if err := tr.node(n); err != nil {
			return err
		}
	}
	return nil
}

// netLiteral escapes one literal for use outside a class.
func netLiteral(r rune) string {
	switch {
	case r < 0x80 && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_'):
		return string(r)
	case r < 0x20 || r == 0x7f:
		return fmt.Sprintf(`\u%04X`, r)
	case r < 0x80:
		return `\` + string(r)
	}
	return string(r)
}

// netClassRune escapes one character inside a [...] class.
func netClassRune(r rune) string {
	switch {
	case r < 0x20 || r == 0x7f:
		return fmt.Sprintf(`\u%04X`, r)
	case r == '\\' || r == ']' || r == '[' || r == '^' || r == '-':
		return `\` + string(r)
	}
	return string(r)
}

const netSpaceBody = `\t-\r\u001c- \u0085   -     　`

// categoryNet gives the class body of a positive category and, for a
// negated one, the standalone class that matches its complement.
func (tr *netTranslator) categoryNet(cat catKind) (body, standalone string) {
	switch cat {
	case catDigit:
		if tr.ascii {
			return `0-9`, ""
		}
		return `\p{Nd}`, ""
	case catWord:
		if tr.ascii {
			return `0-9A-Za-z_`, ""
		}
		return `\p{L}\p{N}_`, ""
	case catSpace:
		if tr.ascii {
			return `\t-\r `, ""
		}
		return netSpaceBody, ""
	case catNotDigit:
		if tr.ascii {
			return "", `[^0-9]`
		}
		return "", `[^\p{Nd}]`
	case catNotWord:
		if tr.ascii {
			return "", `[^0-9A-Za-z_]`
		}
		return "", `[^\p{L}\p{N}_]`
	default: // catNotSpace
		if tr.ascii {
			return "", `[^\t-\r ]`
		}
		return "", `[^` + netSpaceBody + `]`
	}
}

func (tr *netTranslator) set(n *reNode) string {
	negate := false
	var body strings.Builder
	var alternatives []string
	for _, item := range n.set {
		switch item.kind {
		case setNegate:
			negate = true
		case setLiteral:
			body.WriteString(netClassRune(item.lo))
		case setRange:
			body.WriteString(netClassRune(item.lo) + "-" + netClassRune(item.hi))
		case setCategory:
			positive, standalone := tr.categoryNet(item.cat)
			body.WriteString(positive)
			if standalone != "" {
				alternatives = append(alternatives, standalone)
			}
		}
	}
	if len(alternatives) == 0 {
		if negate {
			return `[^` + body.String() + `]`
		}
		return `[` + body.String() + `]`
	}
	if body.Len() > 0 {
		alternatives = append([]string{`[` + body.String() + `]`}, alternatives...)
	}
	union := strings.Join(alternatives, "|")
	if negate {
		return `(?:(?!` + union + `)[\s\S])`
	}
	return `(?:` + union + `)`
}

// wordClass is the \w class of the current scope, as a lookaround operand.
func (tr *netTranslator) wordClass() string {
	if tr.ascii {
		return `[0-9A-Za-z_]`
	}
	return `[\p{L}\p{N}_]`
}

func (tr *netTranslator) node(n *reNode) error {
	switch n.op {
	case opLiteral:
		tr.b.WriteString(netLiteral(n.ch))
	case opNotLiteral:
		tr.b.WriteString(`[^` + netClassRune(n.ch) + `]`)
	case opAny:
		tr.b.WriteString(`.`)
	case opIn:
		tr.b.WriteString(tr.set(n))
	case opAt:
		w := tr.wordClass()
		switch n.at {
		case atBeginning:
			tr.b.WriteString(`^`)
		case atBeginningString:
			tr.b.WriteString(`\A`)
		case atEnd:
			tr.b.WriteString(`$`)
		case atEndString:
			tr.b.WriteString(`\z`)
		case atBoundary:
			tr.b.WriteString(`(?:(?<=` + w + `)(?!` + w + `)|(?<!` + w + `)(?=` + w + `))`)
		case atNonBoundary:
			// Python: never matches on an empty string.
			tr.b.WriteString(`(?!\A\z)(?:(?<=` + w + `)(?=` + w + `)|(?<!` + w + `)(?!` + w + `))`)
		}
	case opBranch:
		tr.b.WriteString(`(?:`)
		for i, alt := range n.alts {
			if i > 0 {
				tr.b.WriteString(`|`)
			}
			if err := tr.nodes(alt); err != nil {
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
		ascii := tr.ascii
		if n.add&flagASCII != 0 {
			tr.ascii = true
		}
		if n.add&flagUnicode != 0 {
			tr.ascii = false
		}
		err := tr.nodes(n.body)
		tr.ascii = ascii
		if err != nil {
			return err
		}
		tr.b.WriteString(`)`)
	case opMaxRepeat, opMinRepeat, opPossessive:
		if n.min > 2147483646 || (n.max != reMaxRepeat && n.max > 2147483646) {
			return unsupported(fmt.Sprintf("repeat count %d beyond the backtracking engine's int32 range", n.max))
		}
		if n.op == opPossessive {
			tr.b.WriteString(`(?>`)
		}
		tr.b.WriteString(`(?:`)
		if err := tr.nodes(n.body); err != nil {
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
		if n.op == opPossessive {
			tr.b.WriteString(`)`)
		}
	case opAtomic:
		tr.b.WriteString(`(?>`)
		if err := tr.nodes(n.body); err != nil {
			return err
		}
		tr.b.WriteString(`)`)
	case opAssert, opAssertNot:
		open := `(?`
		if n.dir < 0 {
			open += `<`
		}
		if n.op == opAssert {
			open += `=`
		} else {
			open += `!`
		}
		tr.b.WriteString(open)
		if err := tr.nodes(n.body); err != nil {
			return err
		}
		tr.b.WriteString(`)`)
	case opGroupRef:
		if n.icase {
			// GROUPREF_UNI_IGNORE compares simple lowercase forms.
			fmt.Fprintf(&tr.b, `(?i:\%d)`, n.ref)
		} else {
			fmt.Fprintf(&tr.b, `(?:\%d)`, n.ref)
		}
	case opGroupRefExists:
		fmt.Fprintf(&tr.b, `(?(%d)`, n.ref)
		if err := tr.nodes(n.yes); err != nil {
			return err
		}
		// Without an else branch Python matches empty; .NET would fail, so the
		// empty branch is written out.
		tr.b.WriteString(`|`)
		if err := tr.nodes(n.no); err != nil {
			return err
		}
		tr.b.WriteString(`)`)
	}
	return nil
}

func unsupported(reason string) error { return &UnsupportedRegexError{Reason: reason} }
