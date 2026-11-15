package jsonresp

import (
	"testing"

	"configflow/internal/jsonx"
)

// The Python tests (backend/test_json_response.py) drive Flask end to end; what
// is observable here is the encoding and the decisions around it.

func TestExternalEncodingMatchesTheGlobalProvider(t *testing.T) {
	payload := jsonx.Obj("z", 1, "items", []any{jsonx.Obj("name", "例子.test", "b", jsonx.Float(1.5), "a", nil)},
		"values", []any{nil, true, false, 0, -3, jsonx.Float(1.5), jsonx.Float(1)})
	got, err := External(payload)
	if err != nil {
		t.Fatal(err)
	}
	// json.dumps(..., ensure_ascii=True, sort_keys=True) with default separators.
	want := `{"items": [{"a": null, "b": 1.5, "name": "\u4f8b\u5b50.test"}], "values": [null, true, false, 0, -3, 1.5, 1.0], "z": 1}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	for _, text := range []string{`[]`, `{}`, `[1, 2]`} {
		value, _ := jsonx.ParseString(text)
		out, _ := External(value)
		if string(out) != text {
			t.Errorf("%s -> %s", text, out)
		}
	}
}

func TestExternalDiffersFromJsonifyOnlyInSeparatorsAndNewline(t *testing.T) {
	payload := jsonx.Obj("b", 1, "a", []any{1, 2})
	external, _ := External(payload)
	response, _ := jsonx.Response(payload)
	if string(external) != `{"a": [1, 2], "b": 1}` || string(response) != "{\"a\":[1,2],\"b\":1}\n" {
		t.Errorf("%q vs %q", external, response)
	}
}

func TestPlainTree(t *testing.T) {
	deep := any(jsonx.Obj("value", "x"))
	for i := 0; i < 127; i++ {
		deep = []any{deep}
	}
	if !PlainTree(deep) {
		t.Error("128 levels are fine")
	}
	if PlainTree([]any{deep}) {
		t.Error("129 levels are too deep")
	}
	for _, scalar := range []any{nil, "private-secret", 3, true} {
		if PlainTree(scalar) {
			t.Errorf("%v is not an object or list", scalar)
		}
	}
	if !PlainTree([]any{}) || !PlainTree(jsonx.NewObject()) {
		t.Error("empty containers are plain")
	}
	cyclic := jsonx.NewObject()
	cyclic.Set("self", cyclic)
	if PlainTree(cyclic) {
		t.Error("a cycle is not plain")
	}
	shared := jsonx.Obj("a", 1)
	if !PlainTree(jsonx.Obj("x", shared, "y", shared)) {
		t.Error("sharing without a cycle is plain")
	}
}

func TestConsumeOnlyForAnUnchangedBodySanitizedAgainstCurrentCredentials(t *testing.T) {
	body := []byte(`{"value": "safe"}`)
	secrets := []string{"private-secret", "agent-secret"}
	marker := NewSanitized(body, secrets, "single")
	if !marker.Consume(body, "single", secrets) {
		t.Fatal("an untouched response must be consumed")
	}
	if marker.Consume([]byte(`{"value": "agent-secret"}`), "single", secrets) {
		t.Error("a changed body needs the global sanitizer")
	}
	if marker.Consume(body, "other", secrets) {
		t.Error("another endpoint")
	}
	if marker.Consume(body, "single", append(append([]string{}, secrets...), "new-private-secret")) {
		t.Error("rotated credentials must not skip redaction")
	}
	var none *Sanitized
	if none.Consume(body, "single", secrets) {
		t.Error("no marker, no skip")
	}
	secrets[0] = "mutated" // the marker keeps its own copy
	body[2] = 'X'
	if !marker.Consume([]byte(`{"value": "safe"}`), "single", []string{"private-secret", "agent-secret"}) {
		t.Error("marker must not alias the caller's slices")
	}
}

// Regression (review C02): _plain_json_tree refuses a container nested
// deeper than 128, an empty list included.
func TestPlainTreeRefusesDeepEmptyList(t *testing.T) {
	nest := func(depth int, leaf any) any {
		v := leaf
		for i := 1; i < depth; i++ {
			v = []any{v}
		}
		return v
	}
	if !PlainTree(nest(128, []any{})) {
		t.Error("128 levels ending in an empty list are plain")
	}
	if PlainTree(nest(129, []any{})) {
		t.Error("an empty list at depth 129 is not plain")
	}
}
