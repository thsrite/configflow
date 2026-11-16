package convert

import (
	"fmt"
	"net/netip"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

// Python-semantics helpers of the C18 generator. The generator is written
// like the Python function it ports: an exception Python would raise on a
// malformed value is raised as a panic(raised{err}) and turned back into the
// returned error by catch in the exported function.

type raised struct{ err error }

func raise(err error) { panic(raised{err}) }

func must(err error) {
	if err != nil {
		raise(err)
	}
}

func mustValue[T any](v T, err error) T {
	must(err)
	return v
}

// catch converts a raised error into *errp; other panics continue.
func catch(errp *error) {
	if r := recover(); r != nil {
		if p, ok := r.(raised); ok {
			*errp = p.err
			return
		}
		panic(r)
	}
}

// typeName is type(v).__name__ including the YAML-only types.
func typeName(v any) string {
	switch x := v.(type) {
	case yamlx.Timestamp:
		if !x.HasTime {
			return "date"
		}
		return "datetime"
	case []byte:
		return "bytes"
	}
	return pyTypeName(v)
}

// dictGet is v.get(key, def): AttributeError when v is not a dict.
func dictGet(v any, key string, def any) any {
	obj, ok := v.(*jsonx.Object)
	if !ok || obj == nil {
		raise(attributeError(v, "get"))
	}
	if value, present := obj.Get(key); present {
		return value
	}
	return def
}

// dictItem is v[key].
func dictItem(v any, key string) any {
	obj, ok := v.(*jsonx.Object)
	if !ok || obj == nil {
		switch v.(type) {
		case []any:
			raise(pyError("TypeError", "list indices must be integers or slices, not str"))
		case string:
			raise(pyError("TypeError", "string indices must be integers, not 'str'"))
		}
		raise(pyError("TypeError", "'"+typeName(v)+"' object is not subscriptable"))
	}
	value, present := obj.Get(key)
	if !present {
		raise(pyError("KeyError", jsonx.Repr(key)))
	}
	return value
}

// iterate is `for x in v`: lists, dict keys and string characters.
func iterate(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case *jsonx.Object:
		out := make([]any, 0, x.Len())
		for _, key := range x.Keys() {
			out = append(out, key)
		}
		return out
	case string:
		out := []any{}
		for _, r := range x {
			out = append(out, string(r))
		}
		return out
	case []byte:
		out := []any{}
		for _, b := range x {
			out = append(out, jsonx.Int(int64(b)))
		}
		return out
	}
	raise(pyError("TypeError", "'"+typeName(v)+"' object is not iterable"))
	return nil
}

// strMethod returns v as a string or raises AttributeError for method.
func strMethod(v any, method string) string {
	s, ok := v.(string)
	if !ok {
		raise(attributeError(v, method))
	}
	return s
}

// key is hashID that raises.
func key(v any) string { return mustValue(hashID(v)) }

// inStrings is `v in (a, b, ...)` for string members.
func inStrings(v any, options ...string) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, option := range options {
		if s == option {
			return true
		}
	}
	return false
}

// fstr is str(v) inside an f-string.
func fstr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case yamlx.Timestamp:
		return x.String()
	case []byte:
		return pyBytesRepr(x)
	}
	return jsonx.Str(v)
}

func pyBytesRepr(b []byte) string {
	quote := byte('\'')
	if strings.IndexByte(string(b), '\'') >= 0 && strings.IndexByte(string(b), '"') < 0 {
		quote = '"'
	}
	var out strings.Builder
	out.WriteString("b")
	out.WriteByte(quote)
	for _, c := range b {
		switch {
		case c == quote || c == '\\':
			out.WriteByte('\\')
			out.WriteByte(c)
		case c == '\t':
			out.WriteString(`\t`)
		case c == '\n':
			out.WriteString(`\n`)
		case c == '\r':
			out.WriteString(`\r`)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&out, `\x%02x`, c)
		default:
			out.WriteByte(c)
		}
	}
	out.WriteByte(quote)
	return out.String()
}

// pySetRepr is repr(set) of hashID keys in the given order (Python's own
// order is hash-seed dependent; only debug log lines show it).
func pySetRepr(keys []string, values map[string]any) string {
	if len(keys) == 0 {
		return "set()"
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = jsonx.Repr(values[k])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// orderedSet is a Python set of hashable values that remembers insertion
// order (used only for log output and deterministic iteration).
type orderedSet struct {
	keys   []string
	values map[string]any
}

func newSet() *orderedSet { return &orderedSet{values: map[string]any{}} }

func (s *orderedSet) add(v any) {
	k := key(v)
	if _, ok := s.values[k]; !ok {
		s.keys = append(s.keys, k)
		s.values[k] = v
	}
}

func (s *orderedSet) has(v any) bool {
	k, err := hashID(v)
	if err != nil {
		raise(err)
	}
	_, ok := s.values[k]
	return ok
}

func (s *orderedSet) remove(v any) {
	k := key(v)
	if _, ok := s.values[k]; !ok {
		return
	}
	delete(s.values, k)
	for i, existing := range s.keys {
		if existing == k {
			s.keys = append(s.keys[:i:i], s.keys[i+1:]...)
			break
		}
	}
}

func (s *orderedSet) repr() string { return pySetRepr(s.keys, s.values) }

// appendURLQuery is profile_context.append_url_query(url, params): the
// existing query pairs (blank values kept) and the params, each name and
// value percent-encoded (a list value repeats the name). The rest of the URL
// is kept as written: the URLs are the panel's own (a rule name may hold
// "%" or Chinese), so nothing else needs parsing. A server domain whose host
// has an invalid bracketed IPv6 host is refused instead of producing URLs no
// client can use. Only the authority is inspected: a rule path is not a URL.
func appendURLQuery(url string, params [][2]any) string {
	rest, fragment, _ := strings.Cut(url, "#")
	base, query, _ := strings.Cut(rest, "?")
	if _, after, found := strings.Cut(base, "//"); found {
		authority, _, _ := strings.Cut(after, "/")
		if strings.Contains(authority, "[") != strings.Contains(authority, "]") {
			raise(pyError("ValueError", "Invalid IPv6 URL"))
		}
		host := authority
		if at := strings.LastIndexByte(host, '@'); at >= 0 {
			host = host[at+1:]
		}
		if start := strings.IndexByte(host, '['); start >= 0 {
			literal, _, _ := strings.Cut(host[start+1:], "]")
			// net/url handles escaped IPv6 zone IDs; netip also ensures that
			// a bracketed IPv4 address or arbitrary name cannot become a URL.
			parsed, err := weburl.Parse("http://[" + literal + "]")
			if err != nil {
				raise(pyError("ValueError", "Invalid IPv6 URL"))
			}
			address, err := netip.ParseAddr(parsed.Hostname())
			if err != nil || !address.Is6() {
				raise(pyError("ValueError", "Invalid IPv6 URL"))
			}
		}
	}
	var items []string
	for _, nameValue := range strings.Split(query, "&") {
		if nameValue == "" {
			continue
		}
		name, value, _ := strings.Cut(nameValue, "=")
		items = append(items, weburl.Quote(weburl.UnquotePlus(name))+"="+weburl.Quote(weburl.UnquotePlus(value)))
	}
	for _, p := range params {
		k := weburl.Quote(fstr(p[0]))
		switch v := p[1].(type) {
		case string:
			items = append(items, k+"="+weburl.Quote(v))
		case []any:
			for _, elt := range v {
				items = append(items, k+"="+weburl.Quote(fstr(elt)))
			}
		case *jsonx.Object:
			for _, elt := range v.Keys() {
				items = append(items, k+"="+weburl.Quote(elt))
			}
		default:
			items = append(items, k+"="+weburl.Quote(fstr(v)))
		}
	}
	out := base
	if len(items) > 0 {
		out += "?" + strings.Join(items, "&")
	}
	if fragment != "" {
		out += "#" + fragment
	}
	return out
}

// pyIntOf is int(v) for the values a profile can hold.
func pyIntOf(v any) jsonx.Number {
	switch x := v.(type) {
	case bool:
		if x {
			return jsonx.Int(1)
		}
		return jsonx.Int(0)
	case jsonx.Number:
		if x.IsInt() {
			return x
		}
		f := x.Float64()
		if f != f || f > 1e308 || f < -1e308 {
			if f != f {
				raise(pyError("ValueError", "cannot convert float NaN to integer"))
			}
			raise(pyError("OverflowError", "cannot convert float infinity to integer"))
		}
		return jsonx.Int(int64(f))
	case string:
		n, err := pystr.Int(x, 10)
		if err != nil {
			raise(pyError("ValueError", "invalid literal for int() with base 10: "+jsonx.Repr(x)))
		}
		return jsonx.BigInt(n)
	}
	raise(pyError("TypeError", "int() argument must be a string, a bytes-like object or a real number, not '"+typeName(v)+"'"))
	return jsonx.Int(0)
}
