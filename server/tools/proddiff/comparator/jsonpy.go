package main

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// errDuplicateKey reports a document a strict consumer would reject: two equal
// mapping keys (JSON/YAML) or two equal ZIP member names.
var errDuplicateKey = errors.New("duplicate key")

// loadJSON parses exactly one JSON document like Python's json.loads with a
// duplicate-key hook: integers stay integers, floats stay floats, NaN and
// Infinity literals are accepted, object order is kept and duplicates fail.
func loadJSON(body []byte) (any, error) {
	if len(body) >= 3 && body[0] == 0xEF && body[1] == 0xBB && body[2] == 0xBF {
		body = body[3:]
	}
	if !utf8.Valid(body) {
		return nil, errors.New("json: invalid utf-8")
	}
	return parseJSON(string(body), false)
}

// parseJSON parses one document; loose keeps the last of duplicate keys
// (plain json.loads) instead of failing.
func parseJSON(s string, loose bool) (any, error) {
	p := &jsonParser{s: s, loose: loose}
	p.space()
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("json: extra data at %d", p.i)
	}
	return v, nil
}

type jsonParser struct {
	s     string
	i     int
	loose bool
}

func (p *jsonParser) space() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsonParser) fail(what string) error { return fmt.Errorf("json: %s at %d", what, p.i) }

func (p *jsonParser) value(depth int) (any, error) {
	if depth > 10000 {
		return nil, p.fail("nesting too deep")
	}
	if p.i >= len(p.s) {
		return nil, p.fail("expecting value")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		return p.str()
	case strings.HasPrefix(p.s[p.i:], "null"):
		p.i += 4
		return nil, nil
	case strings.HasPrefix(p.s[p.i:], "true"):
		p.i += 4
		return true, nil
	case strings.HasPrefix(p.s[p.i:], "false"):
		p.i += 5
		return false, nil
	case strings.HasPrefix(p.s[p.i:], "NaN"):
		p.i += 3
		return math.NaN(), nil
	case strings.HasPrefix(p.s[p.i:], "Infinity"):
		p.i += 8
		return math.Inf(1), nil
	case strings.HasPrefix(p.s[p.i:], "-Infinity"):
		p.i += 9
		return math.Inf(-1), nil
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	}
	return nil, p.fail("expecting value")
}

func (p *jsonParser) object(depth int) (any, error) {
	p.i++
	m := newMap()
	p.space()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return m, nil
	}
	for {
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			return nil, p.fail("expecting property name")
		}
		key, err := p.str()
		if err != nil {
			return nil, err
		}
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, p.fail("expecting ':'")
		}
		p.i++
		p.space()
		value, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		if !p.loose && m.Has(key) {
			return nil, errDuplicateKey
		}
		m.Set(key, value)
		p.space()
		if p.i >= len(p.s) {
			return nil, p.fail("unterminated object")
		}
		if p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.s[p.i] == '}' {
			p.i++
			return m, nil
		}
		return nil, p.fail("expecting ',' delimiter")
	}
}

func (p *jsonParser) array(depth int) (any, error) {
	p.i++
	out := []any{}
	p.space()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		return out, nil
	}
	for {
		p.space()
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.space()
		if p.i >= len(p.s) {
			return nil, p.fail("unterminated array")
		}
		if p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.s[p.i] == ']' {
			p.i++
			return out, nil
		}
		return nil, p.fail("expecting ',' delimiter")
	}
}

func (p *jsonParser) str() (string, error) {
	p.i++
	var b strings.Builder
	for {
		if p.i >= len(p.s) {
			return "", p.fail("unterminated string")
		}
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return b.String(), nil
		case c < 0x20:
			return "", p.fail("invalid control character")
		case c == '\\':
			if p.i+1 >= len(p.s) {
				return "", p.fail("unterminated string")
			}
			e := p.s[p.i+1]
			p.i += 2
			switch e {
			case '"', '\\', '/':
				b.WriteByte(e)
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				r, err := p.hex4()
				if err != nil {
					return "", err
				}
				if utf16.IsSurrogate(r) && r < 0xDC00 && strings.HasPrefix(p.s[p.i:], `\u`) {
					save := p.i
					p.i += 2
					low, err := p.hex4()
					if err == nil && low >= 0xDC00 && low <= 0xDFFF {
						r = utf16.DecodeRune(r, low)
					} else {
						p.i = save
					}
				}
				b.WriteRune(r)
			default:
				return "", p.fail("invalid \\escape")
			}
		default:
			b.WriteByte(c)
			p.i++
		}
	}
}

func (p *jsonParser) hex4() (rune, error) {
	if p.i+4 > len(p.s) {
		return 0, p.fail("invalid \\uXXXX escape")
	}
	n, err := strconv.ParseUint(p.s[p.i:p.i+4], 16, 32)
	if err != nil {
		return 0, p.fail("invalid \\uXXXX escape")
	}
	p.i += 4
	return rune(n), nil
}

func (p *jsonParser) number() (any, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	if p.i < len(p.s) && p.s[p.i] == '0' {
		p.i++
	} else if digits() == 0 {
		p.i = start
		return nil, p.fail("expecting value")
	}
	isFloat := false
	if p.i+1 < len(p.s) && p.s[p.i] == '.' && p.s[p.i+1] >= '0' && p.s[p.i+1] <= '9' {
		p.i++
		digits()
		isFloat = true
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		save := p.i
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			p.i = save
		} else {
			isFloat = true
		}
	}
	text := p.s[start:p.i]
	if isFloat {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return nil, p.fail("invalid number")
		}
		return f, nil
	}
	return parseInt(text), nil
}

// parseInt returns int64 when the decimal fits, otherwise *big.Int.
func parseInt(text string) any {
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		return n
	}
	n, _ := new(big.Int).SetString(text, 10)
	return n
}

// dumpOptions mirrors json.dumps keyword arguments.
type dumpOptions struct {
	indent      int // <0: single line with ", " and ": " separators
	sortKeys    bool
	ensureASCII bool
}

// dumpJSON is Python json.dumps for the value model (default=str for bytes).
func dumpJSON(v any, o dumpOptions) string {
	var b strings.Builder
	writeJSON(&b, v, o, 0)
	return b.String()
}

func writeJSON(b *strings.Builder, v any, o dumpOptions, level int) {
	newline := func(level int) {
		if o.indent >= 0 {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(" ", o.indent*level))
		}
	}
	item := ", "
	if o.indent >= 0 {
		item = ","
	}
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int64, *big.Int:
		b.WriteString(pyStr(x))
	case float64:
		switch {
		case math.IsNaN(x):
			b.WriteString("NaN")
		case math.IsInf(x, 1):
			b.WriteString("Infinity")
		case math.IsInf(x, -1):
			b.WriteString("-Infinity")
		default:
			b.WriteString(pyFloatRepr(x))
		}
	case string:
		writeJSONString(b, x, o.ensureASCII)
	case []byte:
		writeJSONString(b, pyRepr(x), o.ensureASCII)
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, item0 := range x {
			if i > 0 {
				b.WriteString(item)
			}
			newline(level + 1)
			writeJSON(b, item0, o, level+1)
		}
		newline(level)
		b.WriteByte(']')
	case *Map:
		if x.Len() == 0 {
			b.WriteString("{}")
			return
		}
		order := make([]int, x.Len())
		for i := range order {
			order[i] = i
		}
		if o.sortKeys {
			sort.SliceStable(order, func(i, j int) bool { return jsonKey(x.keys[order[i]]) < jsonKey(x.keys[order[j]]) })
		}
		b.WriteByte('{')
		for n, i := range order {
			if n > 0 {
				b.WriteString(item)
			}
			newline(level + 1)
			writeJSONString(b, jsonKey(x.keys[i]), o.ensureASCII)
			b.WriteString(": ")
			writeJSON(b, x.vals[i], o, level+1)
		}
		newline(level)
		b.WriteByte('}')
	default:
		writeJSONString(b, fmt.Sprint(x), o.ensureASCII)
	}
}

// jsonKey is how json.dumps spells a non-string key.
func jsonKey(k any) string {
	switch x := k.(type) {
	case string:
		return x
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return pyStr(k)
}

func writeJSONString(b *strings.Builder, s string, ascii bool) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r < 0x20 || ascii && r == 0x7f:
				fmt.Fprintf(b, `\u%04x`, r)
			case ascii && r > 0x7f:
				if r > 0xFFFF {
					hi, lo := utf16.EncodeRune(r)
					fmt.Fprintf(b, `\u%04x\u%04x`, hi, lo)
				} else {
					fmt.Fprintf(b, `\u%04x`, r)
				}
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
