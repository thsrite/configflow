package jsonx

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONBridgeRoundTrip(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"order and literal numbers", `{"z":6e1,"a":[60.00,-0,9007199254740993123456789,{"b":1,"a":2}],"text":"中文<&>"}`, `{"z":6e1,"a":[60.00,-0,9007199254740993123456789,{"b":1,"a":2}],"text":"中文<&>"}`},
		{"duplicate members", `{"b":1,"a":2,"b":3}`, `{"b":3,"a":2}`},
		{"null", `null`, `{}`},
		{"empty", `{}`, `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var obj Object
			if err := json.Unmarshal([]byte(tt.input), &obj); err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			enc := json.NewEncoder(&b)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(&obj); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSuffix(b.String(), "\n"); got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}

func TestJSONBridgeMixedValues(t *testing.T) {
	type record struct {
		Legacy *Object         `json:"legacy"`
		Raw    json.RawMessage `json:"raw"`
		Count  Number          `json:"count"`
	}
	v := record{Legacy: Obj("nested", struct {
		Value string `json:"value"`
	}{"ok"}), Raw: json.RawMessage(`{"n":6e1}`), Count: Number("60.00")}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"legacy":{"nested":{"value":"ok"}},"raw":{"n":6e1},"count":60.00}` {
		t.Fatal(string(data))
	}
	var got record
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Count != "60.00" || got.Legacy.GetObject("nested").GetString("value") != "ok" {
		t.Fatal(got)
	}
}

func TestJSONBridgeRejectsInvalidValues(t *testing.T) {
	cyclic := NewObject()
	cyclic.Set("self", cyclic)
	list := make([]any, 1)
	list[0] = list
	tests := []struct {
		name  string
		value any
	}{
		{"object cycle", cyclic}, {"list cycle", Obj("list", list)},
		{"NaN", Obj("n", Number("NaN"))}, {"infinity", Obj("n", Number("Infinity"))},
		{"bad number", Obj("n", Number("01"))}, {"unsupported value", Obj("v", make(chan int))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := json.Marshal(tt.value); err == nil {
				t.Fatal("expected encoding error")
			}
		})
	}
	for _, input := range []string{`[]`, `1`, `{"a":NaN}`, `{"a":1} {}`, `{"a":`} {
		obj := Obj("original", true)
		if err := obj.UnmarshalJSON([]byte(input)); err == nil {
			t.Errorf("accepted %s", input)
		}
		if !obj.GetBool("original") {
			t.Error("failed decode mutated receiver")
		}
	}
}
