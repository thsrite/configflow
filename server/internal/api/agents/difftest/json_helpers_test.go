package difftest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	utf16enc "unicode/utf16"
	"unicode/utf8"
)

type orderedField struct {
	key   string
	value any
}

type orderedObject struct {
	fields []orderedField
	index  map[string]int
}

func oObj(pairs ...any) *orderedObject {
	o := &orderedObject{index: make(map[string]int, len(pairs)/2)}
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1])
	}
	return o
}

func oList(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

func (o *orderedObject) Has(key string) bool {
	if o == nil || o.index == nil {
		return false
	}
	_, ok := o.index[key]
	return ok
}

func (o *orderedObject) Get(key string) (any, bool) {
	if o == nil || o.index == nil {
		return nil, false
	}
	i, ok := o.index[key]
	if !ok {
		return nil, false
	}
	return o.fields[i].value, true
}

func (o *orderedObject) Value(key string) any {
	v, _ := o.Get(key)
	return v
}

func (o *orderedObject) Set(key string, value any) {
	if o.index == nil {
		o.index = map[string]int{}
	}
	if i, ok := o.index[key]; ok {
		o.fields[i].value = value
		return
	}
	o.index[key] = len(o.fields)
	o.fields = append(o.fields, orderedField{key: key, value: value})
}

func (o *orderedObject) Delete(key string) {
	if o == nil || o.index == nil {
		return
	}
	i, ok := o.index[key]
	if !ok {
		return
	}
	o.fields = append(o.fields[:i], o.fields[i+1:]...)
	delete(o.index, key)
	for j := i; j < len(o.fields); j++ {
		o.index[o.fields[j].key] = j
	}
}

func (o *orderedObject) Keys() []string {
	if o == nil {
		return nil
	}
	keys := make([]string, len(o.fields))
	for i, f := range o.fields {
		keys[i] = f.key
	}
	return keys
}

func (o *orderedObject) Len() int {
	if o == nil {
		return 0
	}
	return len(o.fields)
}

func (o *orderedObject) GetString(key string) string {
	s, _ := o.Value(key).(string)
	return s
}

func (o *orderedObject) GetBool(key string) bool {
	b, _ := o.Value(key).(bool)
	return b
}

func (o *orderedObject) GetInt(key string) int64 {
	return intValue(o.Value(key))
}

func (o *orderedObject) GetObject(key string) *orderedObject {
	child, _ := o.Value(key).(*orderedObject)
	return child
}

func (o *orderedObject) GetList(key string) []any {
	list, _ := o.Value(key).([]any)
	return list
}

func (o *orderedObject) Update(other *orderedObject) {
	if o == nil || other == nil {
		return
	}
	for _, f := range other.fields {
		o.Set(f.key, f.value)
	}
}

func (o *orderedObject) Clone() *orderedObject {
	if o == nil {
		return nil
	}
	out := &orderedObject{
		fields: make([]orderedField, len(o.fields)),
		index:  make(map[string]int, len(o.fields)),
	}
	for i, f := range o.fields {
		out.fields[i] = orderedField{key: f.key, value: cloneOrderedValue(f.value)}
		out.index[f.key] = i
	}
	return out
}

func cloneOrderedValue(v any) any {
	switch x := v.(type) {
	case *orderedObject:
		return x.Clone()
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = cloneOrderedValue(item)
		}
		return out
	case *big.Int:
		return new(big.Int).Set(x)
	default:
		return v
	}
}

func (o *orderedObject) MarshalJSON() ([]byte, error) {
	return encodeCompactJSON(o)
}

func strValue(v any) string {
	s, _ := v.(string)
	return s
}

func intValue(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		if f, err := x.Float64(); err == nil {
			return int64(f)
		}
	}
	return 0
}

type pyJSONParser struct {
	data []byte
	pos  int
}

func parsePythonJSON(data []byte) (any, error) {
	p := &pyJSONParser{data: data}
	p.skipWS()
	v, err := p.parseValue(0)
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if p.pos != len(p.data) {
		return nil, fmt.Errorf("trailing data at %d", p.pos)
	}
	return v, nil
}

func parsePythonJSONString(s string) (any, error) {
	return parsePythonJSON([]byte(s))
}

func (p *pyJSONParser) skipWS() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.pos++
		default:
			return
		}
	}
}

func (p *pyJSONParser) parseValue(depth int) (any, error) {
	if depth > 512 {
		return nil, errors.New("maximum nesting depth exceeded")
	}
	p.skipWS()
	if p.pos >= len(p.data) {
		return nil, errors.New("unexpected end of JSON")
	}
	switch p.data[p.pos] {
	case '{':
		return p.parseObject(depth + 1)
	case '[':
		return p.parseArray(depth + 1)
	case '"':
		return p.parseString()
	case 't':
		if bytes.HasPrefix(p.data[p.pos:], []byte("true")) {
			p.pos += 4
			return true, nil
		}
		return nil, fmt.Errorf("unexpected token at %d", p.pos)
	case 'f':
		if bytes.HasPrefix(p.data[p.pos:], []byte("false")) {
			p.pos += 5
			return false, nil
		}
		return nil, fmt.Errorf("unexpected token at %d", p.pos)
	case 'n':
		if bytes.HasPrefix(p.data[p.pos:], []byte("null")) {
			p.pos += 4
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected token at %d", p.pos)
	case 'N':
		if bytes.HasPrefix(p.data[p.pos:], []byte("NaN")) {
			p.pos += 3
			return json.Number("NaN"), nil
		}
		return nil, fmt.Errorf("unexpected token at %d", p.pos)
	case 'I':
		if bytes.HasPrefix(p.data[p.pos:], []byte("Infinity")) {
			p.pos += 8
			return json.Number("Infinity"), nil
		}
		return nil, fmt.Errorf("unexpected token at %d", p.pos)
	default:
		if bytes.HasPrefix(p.data[p.pos:], []byte("-Infinity")) {
			p.pos += 9
			return json.Number("-Infinity"), nil
		}
		return p.parseNumber()
	}
}

func (p *pyJSONParser) parseObject(depth int) (*orderedObject, error) {
	p.pos++ // '{'
	p.skipWS()
	out := &orderedObject{index: map[string]int{}}
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return out, nil
	}
	for {
		p.skipWS()
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return nil, fmt.Errorf("expected object key at %d", p.pos)
		}
		key, err := p.parseString()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, fmt.Errorf("expected ':' at %d", p.pos)
		}
		p.pos++
		val, err := p.parseValue(depth)
		if err != nil {
			return nil, err
		}
		out.Set(key, val)
		p.skipWS()
		if p.pos >= len(p.data) {
			return nil, errors.New("unterminated object")
		}
		if p.data[p.pos] == '}' {
			p.pos++
			return out, nil
		}
		if p.data[p.pos] != ',' {
			return nil, fmt.Errorf("expected ',' or '}' at %d", p.pos)
		}
		p.pos++
	}
}

func (p *pyJSONParser) parseArray(depth int) ([]any, error) {
	p.pos++ // '['
	p.skipWS()
	out := []any{}
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return out, nil
	}
	for {
		val, err := p.parseValue(depth)
		if err != nil {
			return nil, err
		}
		out = append(out, val)
		p.skipWS()
		if p.pos >= len(p.data) {
			return nil, errors.New("unterminated array")
		}
		if p.data[p.pos] == ']' {
			p.pos++
			return out, nil
		}
		if p.data[p.pos] != ',' {
			return nil, fmt.Errorf("expected ',' or ']' at %d", p.pos)
		}
		p.pos++
	}
}

func (p *pyJSONParser) parseString() (string, error) {
	start := p.pos
	p.pos++ // '"'
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == '\\' {
			p.pos += 2
			continue
		}
		if c == '"' {
			p.pos++
			var s string
			if err := json.Unmarshal(p.data[start:p.pos], &s); err != nil {
				return "", err
			}
			return s, nil
		}
		if c < 0x20 {
			return "", fmt.Errorf("invalid control character in string at %d", p.pos)
		}
		p.pos++
	}
	return "", errors.New("unterminated string")
}

func (p *pyJSONParser) parseNumber() (json.Number, error) {
	start := p.pos
	if p.pos < len(p.data) && p.data[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.data) {
		return "", fmt.Errorf("invalid number at %d", start)
	}
	if p.data[p.pos] == '0' {
		p.pos++
	} else if p.data[p.pos] >= '1' && p.data[p.pos] <= '9' {
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	} else {
		return "", fmt.Errorf("invalid number at %d", start)
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if p.pos >= len(p.data) || p.data[p.pos] < '0' || p.data[p.pos] > '9' {
			return "", fmt.Errorf("invalid fraction at %d", p.pos)
		}
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.data) || p.data[p.pos] < '0' || p.data[p.pos] > '9' {
			return "", fmt.Errorf("invalid exponent at %d", p.pos)
		}
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	}
	return json.Number(string(p.data[start:p.pos])), nil
}

func appendJSONString(out []byte, text string, ensureASCII bool) []byte {
	const hexDigits = "0123456789abcdef"
	appendU := func(b []byte, r rune) []byte {
		return append(b, '\\', 'u', hexDigits[r>>12&0xf], hexDigits[r>>8&0xf], hexDigits[r>>4&0xf], hexDigits[r&0xf])
	}
	out = append(out, '"')
	start := 0
	for i := 0; i < len(text); {
		c := text[i]
		if c < utf8.RuneSelf {
			if c >= ' ' && c != '"' && c != '\\' && (c != 0x7f || !ensureASCII) {
				i++
				continue
			}
			out = append(out, text[start:i]...)
			switch c {
			case '"', '\\':
				out = append(out, '\\', c)
			case '\n':
				out = append(out, '\\', 'n')
			case '\r':
				out = append(out, '\\', 'r')
			case '\t':
				out = append(out, '\\', 't')
			case '\b':
				out = append(out, '\\', 'b')
			case '\f':
				out = append(out, '\\', 'f')
			default:
				out = appendU(out, rune(c))
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		invalid := r == utf8.RuneError && size == 1
		if !ensureASCII && !invalid {
			i += size
			continue
		}
		out = append(out, text[start:i]...)
		switch {
		case !ensureASCII:
			out = utf8.AppendRune(out, utf8.RuneError)
		case r >= 0x10000:
			r1, r2 := utf16enc.EncodeRune(r)
			out = appendU(appendU(out, r1), r2)
		default:
			out = appendU(out, r)
		}
		i += size
		start = i
	}
	out = append(out, text[start:]...)
	return append(out, '"')
}

func formatPyFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0 && math.Signbit(f):
		return "-0.0"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func writeEncodedJSON(buf *bytes.Buffer, v any, sortKeys, compact, ensureASCII bool) error {
	itemSep, kvSep := ", ", ": "
	if compact {
		itemSep, kvSep = ",", ":"
	}
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		buf.Write(appendJSONString(nil, x, ensureASCII))
	case json.Number:
		buf.WriteString(string(x))
	case json.RawMessage:
		buf.Write(x)
	case int:
		buf.WriteString(strconv.Itoa(x))
	case int64:
		buf.WriteString(strconv.FormatInt(x, 10))
	case uint64:
		buf.WriteString(strconv.FormatUint(x, 10))
	case float64:
		buf.WriteString(formatPyFloat(x))
	case *big.Int:
		buf.WriteString(x.String())
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteString(itemSep)
			}
			if err := writeEncodedJSON(buf, item, sortKeys, compact, ensureASCII); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case []string:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteString(itemSep)
			}
			buf.Write(appendJSONString(nil, item, ensureASCII))
		}
		buf.WriteByte(']')
	case *orderedObject:
		if x == nil {
			buf.WriteString("null")
			return nil
		}
		buf.WriteByte('{')
		if sortKeys {
			keys := x.Keys()
			sort.Strings(keys)
			for i, k := range keys {
				if i > 0 {
					buf.WriteString(itemSep)
				}
				buf.Write(appendJSONString(nil, k, ensureASCII))
				buf.WriteString(kvSep)
				if err := writeEncodedJSON(buf, x.Value(k), sortKeys, compact, ensureASCII); err != nil {
					return err
				}
			}
		} else {
			for i, f := range x.fields {
				if i > 0 {
					buf.WriteString(itemSep)
				}
				buf.Write(appendJSONString(nil, f.key, ensureASCII))
				buf.WriteString(kvSep)
				if err := writeEncodedJSON(buf, f.value, sortKeys, compact, ensureASCII); err != nil {
					return err
				}
			}
		}
		buf.WriteByte('}')
	case map[string]any:
		if x == nil {
			buf.WriteString("null")
			return nil
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteString(itemSep)
			}
			buf.Write(appendJSONString(nil, k, ensureASCII))
			buf.WriteString(kvSep)
			if err := writeEncodedJSON(buf, x[k], sortKeys, compact, ensureASCII); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		parsed, err := parsePythonJSON(raw)
		if err != nil {
			return err
		}
		return writeEncodedJSON(buf, parsed, sortKeys, compact, ensureASCII)
	}
	return nil
}

func encodeJSON(v any, sortKeys, compact, ensureASCII bool) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeEncodedJSON(&buf, v, sortKeys, compact, ensureASCII); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeCompactJSON(v any) ([]byte, error) {
	return encodeJSON(v, false, true, false)
}

func encodeSortedJSON(v any) ([]byte, error) {
	return encodeJSON(v, true, true, false)
}

func encodeDefaultJSON(v any) ([]byte, error) {
	return encodeJSON(v, false, false, true)
}

func encodeSortedDefaultJSON(v any) ([]byte, error) {
	return encodeJSON(v, true, false, true)
}

func encodeResponseJSON(v any) ([]byte, error) {
	data, err := encodeJSON(v, true, true, true)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func encodeStringJSON(s string, ensureASCII bool) string {
	return string(appendJSONString(nil, s, ensureASCII))
}

func mustRawJSON(t testing.TB, v any) json.RawMessage {
	t.Helper()
	data, err := encodeCompactJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func numberSpelling(v any) (string, bool) {
	switch x := v.(type) {
	case json.Number:
		return string(x), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case uint64:
		return strconv.FormatUint(x, 10), true
	case float64:
		return formatPyFloat(x), true
	case *big.Int:
		return x.String(), true
	default:
		return "", false
	}
}

func numbersEqual(a, b string) bool {
	if a == b {
		return true
	}
	if a == "NaN" || b == "NaN" || a == "Infinity" || b == "Infinity" || a == "-Infinity" || b == "-Infinity" {
		return a == b
	}
	ra, oka := new(big.Rat).SetString(a)
	rb, okb := new(big.Rat).SetString(b)
	if oka && okb {
		return ra.Cmp(rb) == 0
	}
	return false
}

func asObjectView(v any) (map[string]any, bool) {
	switch x := v.(type) {
	case *orderedObject:
		if x == nil {
			return nil, false
		}
		m := make(map[string]any, len(x.fields))
		for _, f := range x.fields {
			m[f.key] = f.value
		}
		return m, true
	case map[string]any:
		if x == nil {
			return nil, false
		}
		return x, true
	default:
		return nil, false
	}
}

func jsonEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if na, oka := numberSpelling(a); oka {
		nb, okb := numberSpelling(b)
		return okb && numbersEqual(na, nb)
	}
	if oa, oka := asObjectView(a); oka {
		ob, okb := asObjectView(b)
		if !okb || len(oa) != len(ob) {
			return false
		}
		for k, av := range oa {
			bv, exists := ob[k]
			if !exists || !jsonEqual(av, bv) {
				return false
			}
		}
		return true
	}
	switch x := a.(type) {
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// pyStringRepr formats a Go string using Python 3's str.__repr__ rules for
// error-message matching in n810_scalar_test.go.
func pyStringRepr(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == rune(quote):
			b.WriteByte('\\')
			b.WriteByte(quote)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case unicode.IsPrint(r):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteByte(quote)
	return b.String()
}
