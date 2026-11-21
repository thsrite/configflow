package repo

// Helpers shared by the test files of this package. Add file-specific
// helpers to your own _test.go file with a distinctive prefix.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"testing"

	"configflow/internal/testjson"
)

var bg = context.Background()

// openRepo is ProfileRepository(tmp_path): no factories.
func openRepo(t *testing.T, dataDir string) *Repository {
	t.Helper()
	r, err := Open(dataDir, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Every View callback and borrowed read in the tests is checked for
	// read-only use, once more when the test ends.
	EnableViewGuardForTest(t, r)
	return r
}

// newRepo opens a repository in a fresh temporary data directory.
func newRepo(t *testing.T) *Repository {
	t.Helper()
	return openRepo(t, t.TempDir())
}

// must returns a checker for the (value, err) pair of a call: must(f())(t).
func must[T any](value T, err error) func(testing.TB) T {
	return func(t testing.TB) T {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return value
	}
}

func noError(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// wantError asserts pytest.raises(kind, match=message): err is of kind and,
// when message is not empty, its text contains message.
func wantError(t testing.TB, err error, kind error, message string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %v containing %q, got nil", kind, message)
	}
	if kind != nil && !errors.Is(err, kind) {
		t.Fatalf("expected error kind %v, got %T: %v", kind, err, err)
	}
	if message != "" && !containsText(err.Error(), message) {
		t.Fatalf("expected error containing %q, got %q", message, err.Error())
	}
}

func containsText(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

// testObj is a mutable ordered JSON object helper for repository tests.
type testObj struct {
	keys   []string
	values map[string]any
}

func newTestObj() *testObj { return &testObj{values: map[string]any{}} }

func testObjKV(kv ...any) *testObj {
	o := newTestObj()
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

func (o *testObj) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.values[key]
	return v, ok
}

func (o *testObj) Value(key string) any {
	if o == nil {
		return nil
	}
	return o.values[key]
}

func (o *testObj) GetObject(key string) *testObj {
	v, _ := o.Value(key).(*testObj)
	return v
}

func (o *testObj) GetList(key string) []any {
	v, _ := o.Value(key).([]any)
	return v
}

func (o *testObj) GetString(key string) string {
	v, _ := o.Value(key).(string)
	return v
}

func (o *testObj) Set(key string, value any) {
	if _, exists := o.values[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *testObj) Delete(key string) {
	if _, exists := o.values[key]; !exists {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

func (o *testObj) Has(key string) bool {
	if o == nil {
		return false
	}
	_, ok := o.values[key]
	return ok
}

func (o *testObj) GetBool(key string) bool {
	v, _ := o.Value(key).(bool)
	return v
}

func (o *testObj) GetOr(key string, fallback any) any {
	if v, ok := o.Get(key); ok {
		return v
	}
	return fallback
}

func (o *testObj) DeepCopy() *testObj {
	if o == nil {
		return nil
	}
	cp := newTestObj()
	for _, k := range o.keys {
		cp.Set(k, testDeepCopy(o.values[k]))
	}
	return cp
}

func testDeepCopy(v any) any {
	switch x := v.(type) {
	case *testObj:
		return x.DeepCopy()
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = testDeepCopy(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = testDeepCopy(item)
		}
		return out
	default:
		return v
	}
}

func testStrings(list []any) []string {
	out := make([]string, len(list))
	for i, item := range list {
		out[i], _ = item.(string)
	}
	return out
}

func testListOf(items []string) []any {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = item
	}
	return out
}

func testTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.Number:
		f, _ := x.Float64()
		return f != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case *testObj:
		return x != nil && len(x.keys) > 0
	default:
		return true
	}
}

func testCompact(t testing.TB, value any) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		t.Fatalf("testCompact %T: %v", value, err)
	}
	return string(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}

func (o *testObj) Keys() []string { return append([]string(nil), o.keys...) }

func (o *testObj) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for _, k := range o.keys {
			if !yield(k, o.values[k]) {
				return
			}
		}
	}
}

func (o *testObj) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		val, err := json.Marshal(o.values[k])
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(val)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func replaceNonFiniteJSON(raw []byte) []byte {
	if !bytes.Contains(raw, []byte("NaN")) && !bytes.Contains(raw, []byte("Infinity")) {
		return raw
	}
	out := make([]byte, 0, len(raw))
	inString := false
	escaped := false
	for i := 0; i < len(raw); {
		b := raw[i]
		if inString {
			out = append(out, b)
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			i++
			continue
		}
		if b == '"' {
			inString = true
			out = append(out, b)
			i++
			continue
		}
		if bytes.HasPrefix(raw[i:], []byte("-Infinity")) {
			out = append(out, `"<non-finite:-Infinity>"`...)
			i += len("-Infinity")
			continue
		}
		if bytes.HasPrefix(raw[i:], []byte("Infinity")) {
			out = append(out, `"<non-finite:Infinity>"`...)
			i += len("Infinity")
			continue
		}
		if bytes.HasPrefix(raw[i:], []byte("NaN")) {
			out = append(out, `"<non-finite:NaN>"`...)
			i += len("NaN")
			continue
		}
		out = append(out, b)
		i++
	}
	return out
}

func parseJSONVal(t testing.TB, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(replaceNonFiniteJSON(raw)))
	dec.UseNumber()
	val, err := readTestJSON(dec)
	if err != nil {
		t.Fatalf("parse JSON %q: %v", string(raw), err)
	}
	return val
}

func readTestJSON(dec *json.Decoder) (any, error) {
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
		o := newTestObj()
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := readTestJSON(dec)
			if err != nil {
				return nil, err
			}
			o.Set(k.(string), v)
		}
		_, err := dec.Token()
		return o, err
	case '[':
		list := []any{}
		for dec.More() {
			v, err := readTestJSON(dec)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		_, err := dec.Token()
		return list, err
	}
	return nil, errors.New("unexpected json delimiter")
}

func parseJSONObj(t testing.TB, raw []byte) *testObj {
	t.Helper()
	obj, ok := parseJSONVal(t, raw).(*testObj)
	if !ok {
		t.Fatalf("expected JSON object, got %s", raw)
	}
	return obj
}

func mustMarshalJSON(t testing.TB, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return raw
}

func decodeJSON[T any](t testing.TB, text string) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decodeJSON: %v", err)
	}
	return out
}

// wantJSONEqual asserts structural JSON equality via testjson.
func wantJSONEqual(t testing.TB, got, want any) {
	t.Helper()
	toVal := func(v any) any {
		var raw []byte
		switch x := v.(type) {
		case json.RawMessage:
			raw = x
		case []byte:
			raw = x
		default:
			var err error
			raw, err = json.Marshal(x)
			if err != nil {
				t.Fatalf("marshal %T: %v", x, err)
			}
		}
		parsed, err := testjson.Parse(raw)
		if err != nil {
			t.Fatalf("testjson.Parse %s: %v", raw, err)
		}
		return parsed
	}
	left, right := toVal(got), toVal(want)
	if !testjson.Equal(right, left) {
		t.Fatalf("JSON mismatch:\n%s", testjson.Diff(right, left))
	}
}

func readFileString(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}
