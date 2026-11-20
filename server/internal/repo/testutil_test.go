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
	// Every View callback in the tests is checked for read-only use.
	r.viewGuard = func() { t.Error("a View callback modified the shared snapshot") }
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
	fields := make([]testjson.Field, len(o.keys))
	for i, k := range o.keys {
		fields[i] = testjson.Field{Key: k, Value: o.values[k]}
	}
	return json.Marshal(testjson.Obj(fields...))
}

func parseJSONVal(t testing.TB, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
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
