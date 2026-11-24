package store

// Test-only ordered document fixtures. Production store tests also exercise
// the raw API directly in raw_test.go. No decoding or schema logic lives in store.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type testDoc struct {
	keys   []string
	values map[string]any
}

func newTestDoc() *testDoc {
	return &testDoc{values: map[string]any{}}
}

func testObj(pairs ...any) *testDoc {
	doc := newTestDoc()
	for i := 0; i+1 < len(pairs); i += 2 {
		doc.Set(pairs[i].(string), pairs[i+1])
	}
	return doc
}

func (d *testDoc) Set(key string, value any) {
	if d.values == nil {
		d.values = map[string]any{}
	}
	if _, exists := d.values[key]; !exists {
		d.keys = append(d.keys, key)
	}
	d.values[key] = value
}

func (d *testDoc) Delete(key string) {
	if d == nil {
		return
	}
	if _, exists := d.values[key]; !exists {
		return
	}
	delete(d.values, key)
	for i, k := range d.keys {
		if k == key {
			d.keys = append(d.keys[:i], d.keys[i+1:]...)
			return
		}
	}
}

func (d *testDoc) Has(key string) bool {
	if d == nil {
		return false
	}
	_, ok := d.values[key]
	return ok
}

func (d *testDoc) Keys() []string {
	if d == nil {
		return nil
	}
	return append([]string(nil), d.keys...)
}

func (d *testDoc) Value(key string) any {
	if d == nil {
		return nil
	}
	return d.values[key]
}

func (d *testDoc) GetObject(key string) *testDoc {
	if d == nil {
		return nil
	}
	v, _ := d.values[key].(*testDoc)
	return v
}

func (d *testDoc) GetList(key string) []any {
	if d == nil {
		return nil
	}
	v, _ := d.values[key].([]any)
	return v
}

func (d *testDoc) GetString(key string) string {
	if d == nil {
		return ""
	}
	v, _ := d.values[key].(string)
	return v
}

func (d *testDoc) GetBool(key string) bool {
	if d == nil {
		return false
	}
	v, _ := d.values[key].(bool)
	return v
}

func (d *testDoc) GetInt(key string) int64 {
	if d == nil {
		return 0
	}
	switch v := d.values[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}

func (d *testDoc) GetFloat(key string) float64 {
	if d == nil {
		return 0
	}
	switch v := d.values[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}

func (d *testDoc) Clone() *testDoc {
	if d == nil {
		return nil
	}
	out := &testDoc{keys: append([]string(nil), d.keys...), values: make(map[string]any, len(d.values))}
	for k, v := range d.values {
		out.values[k] = v
	}
	return out
}

func (d *testDoc) DeepCopy() *testDoc {
	if d == nil {
		return nil
	}
	out := &testDoc{keys: append([]string(nil), d.keys...), values: make(map[string]any, len(d.values))}
	for k, v := range d.values {
		out.values[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch x := v.(type) {
	case *testDoc:
		return x.DeepCopy()
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = deepCopyValue(item)
		}
		return out
	default:
		return v
	}
}

func (d *testDoc) MarshalJSON() ([]byte, error) {
	return marshalCompact(d)
}

func marshalCompact(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCompactJSON(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeCompactString encodes s as an RFC 8259 UTF-8 JSON string without
// escaping HTML characters or U+2028/U+2029, matching Python's
// json.dumps(..., ensure_ascii=False, separators=(",", ":")).
func writeCompactString(buf *bytes.Buffer, s string) {
	const hexDigits = "0123456789abcdef"
	buf.WriteByte('"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '"' && c != '\\' {
				i++
				continue
			}
			buf.WriteString(s[start:i])
			switch c {
			case '"', '\\':
				buf.WriteByte('\\')
				buf.WriteByte(c)
			case '\b':
				buf.WriteString(`\b`)
			case '\f':
				buf.WriteString(`\f`)
			case '\n':
				buf.WriteString(`\n`)
			case '\r':
				buf.WriteString(`\r`)
			case '\t':
				buf.WriteString(`\t`)
			default:
				buf.WriteString(`\u00`)
				buf.WriteByte(hexDigits[c>>4])
				buf.WriteByte(hexDigits[c&0xf])
			}
			i++
			start = i
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	buf.WriteString(s[start:])
	buf.WriteByte('"')
}

func writeCompactJSON(buf *bytes.Buffer, v any) error {
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
		writeCompactString(buf, x)
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
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fmt.Errorf("non-finite float: %v", x)
		}
		if x == 0 && math.Signbit(x) {
			buf.WriteString("-0.0")
			return nil
		}
		s := strconv.FormatFloat(x, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		buf.WriteString(s)
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCompactJSON(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case []string:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeCompactString(buf, item)
		}
		buf.WriteByte(']')
	case *testDoc:
		if x == nil {
			buf.WriteString("null")
			return nil
		}
		buf.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeCompactString(buf, k)
			buf.WriteByte(':')
			if err := writeCompactJSON(buf, x.values[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		var raw bytes.Buffer
		enc := json.NewEncoder(&raw)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return err
		}
		parsed, err := parseOrderedJSON(bytes.TrimSuffix(raw.Bytes(), []byte("\n")))
		if err != nil {
			return err
		}
		return writeCompactJSON(buf, parsed)
	}
	return nil
}

func marshalIndent2(v any) ([]byte, error) {
	raw, err := marshalCompact(v)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func parseOrderedJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	val, err := decodeOrderedValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON tokens")
	}
	return val, nil
}

func decodeOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		doc := newTestDoc()
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, errors.New("expected string key")
			}
			val, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			doc.Set(key, val)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return doc, nil
	case '[':
		list := []any{}
		for dec.More() {
			item, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			list = append(list, item)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return list, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %q", delim)
	}
}

func toStrings(list []any) []string {
	out := make([]string, len(list))
	for i, item := range list {
		out[i], _ = item.(string)
	}
	return out
}

func formatInt64(n int64) string { return strconv.FormatInt(n, 10) }

type legacyEntry struct {
	Entry
	Document *testDoc
}
type legacyStore struct {
	*Store
	ctx context.Context
}

func legacyNew(t testing.TB, path string, timeout func() time.Duration) *legacyStore {
	return &legacyStore{Store: New(path, timeout), ctx: t.Context()}
}
func legacyEntryOf(raw Entry, err error) (legacyEntry, error) {
	if err != nil {
		return legacyEntry{}, err
	}
	doc, err := legacyDecode(raw.Document)
	return legacyEntry{raw, doc}, err
}
func (s *legacyStore) Load() (legacyEntry, error) { return legacyEntryOf(s.Store.Load()) }
func (s *legacyStore) Create(doc, meta *testDoc) (legacyEntry, error) {
	raw, err := legacyEncode(doc, nil)
	if err != nil {
		return legacyEntry{}, err
	}
	extra, err := legacyMeta(meta)
	if err != nil {
		return legacyEntry{}, err
	}
	return legacyEntryOf(s.Store.Create(raw, extra))
}
func (s *legacyStore) Commit(base legacyEntry, doc, meta *testDoc) (legacyEntry, bool, error) {
	raw, err := legacyEncode(doc, base.Entry.Document)
	if err != nil {
		return legacyEntry{}, false, err
	}
	extra, err := legacyMeta(meta)
	if err != nil {
		return legacyEntry{}, false, err
	}
	entry, changed, err := s.Store.Commit(s.ctx, base.Entry, raw, extra)
	if err != nil || !changed {
		return legacyEntry{}, changed, err
	}
	result, err := legacyEntryOf(entry, nil)
	return result, changed, err
}
func (s *legacyStore) Meta(key string) (any, error) {
	raw, err := s.Store.Meta(key)
	if err != nil || raw == nil {
		return nil, err
	}
	return parseOrderedJSON(raw)
}
func (s *legacyStore) SalvageMeta() *testDoc {
	raw := s.Store.SalvageMeta()
	if raw == nil {
		return nil
	}
	out := newTestDoc()
	keys := []string{}
	for key := range raw {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		value := raw[key]
		v, err := parseOrderedJSON(value)
		if err != nil {
			return nil
		}
		out.Set(key, v)
	}
	return out
}
func (s *legacyStore) BuildTemporary(doc, meta *testDoc) (string, legacyEntry, error) {
	raw, err := legacyEncode(doc, nil)
	if err != nil {
		return "", legacyEntry{}, err
	}
	extra, err := legacyMeta(meta)
	if err != nil {
		return "", legacyEntry{}, err
	}
	path, entry, err := s.Store.BuildTemporary(raw, extra)
	result, err := legacyEntryOf(entry, err)
	return path, result, err
}

// The old corpus is authored as whole ordered documents. This test-only codec
// constructs their JSON text directly; it does not copy repo's byte-preserving
// bridge or decide whether a field changed. The assertions compare real SQL rows.
func legacyDecode(raw *Document) (*testDoc, error) {
	object := func(fields []Field) json.RawMessage {
		var out bytes.Buffer
		out.WriteByte('{')
		for i, field := range fields {
			if i > 0 {
				out.WriteByte(',')
			}
			key, _ := marshalCompact(field.Name)
			out.Write(key)
			out.WriteByte(':')
			out.Write(field.Value)
		}
		out.WriteByte('}')
		return out.Bytes()
	}
	byName := map[string]json.RawMessage{}
	for _, field := range raw.Root {
		byName[field.Name] = field.Value
	}
	if raw.System != nil {
		byName["system"] = object(raw.System.Fields)
	}
	if raw.Shared != nil {
		byName["shared"] = object(raw.Shared.Fields)
	}
	profiles := []Field{}
	for _, profile := range raw.Profiles {
		profiles = append(profiles, Field{profile.ID, object(profile.Fields)})
	}
	byName["profiles"] = object(profiles)
	fields := []Field{}
	for _, key := range raw.Order {
		fields = append(fields, Field{key, byName[key]})
	}
	value, err := parseOrderedJSON(object(fields))
	if err != nil {
		return nil, err
	}
	return value.(*testDoc), nil
}

func legacyEncode(doc *testDoc, _ *Document) (*Document, error) {
	if doc == nil {
		return nil, newError(ErrDocument, msgDocumentObject)
	}
	fields := func(value any, scope, owner string) ([]Field, error) {
		section, ok := value.(*testDoc)
		if !ok || section == nil {
			return nil, newError(ErrDocument, fmt.Sprintf("配置节 %s/%s 必须是对象", scope, owner))
		}
		result := []Field{}
		for _, key := range section.keys {
			text, err := marshalCompact(section.values[key])
			if err != nil {
				return nil, err
			}
			result = append(result, Field{key, text})
		}
		return result, nil
	}
	raw := &Document{Order: doc.Keys()}
	for _, key := range doc.keys {
		value := doc.values[key]
		switch key {
		case "system", "shared":
			f, err := fields(value, key, "")
			if err != nil {
				return nil, err
			}
			if key == "system" {
				raw.System = &Section{f}
			} else {
				raw.Shared = &Section{f}
			}
		case "profiles":
			profiles, ok := value.(*testDoc)
			if !ok || profiles == nil {
				return nil, newError(ErrDocument, "profiles 必须是对象")
			}
			for _, id := range profiles.keys {
				f, err := fields(profiles.values[id], "profile", id)
				if err != nil {
					return nil, err
				}
				raw.Profiles = append(raw.Profiles, ProfileRows{id, f})
			}
		default:
			text, err := marshalCompact(value)
			if err != nil {
				return nil, err
			}
			raw.Root = append(raw.Root, Field{key, text})
		}
	}
	return raw, nil
}

func legacyMeta(meta *testDoc) (map[string]json.RawMessage, error) {
	result := map[string]json.RawMessage{}
	if meta == nil {
		return result, nil
	}
	for _, key := range meta.keys {
		encoded, err := marshalCompact(meta.values[key])
		if err != nil {
			return nil, err
		}
		result[key] = encoded
	}
	return result, nil
}
