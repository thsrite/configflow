package jsonresp

import (
	"encoding/json"
	"testing"

	"configflow/internal/jsonx"
)

func TestEncodeMixedNativeAndLegacyValues(t *testing.T) {
	value := struct {
		Name   string          `json:"name"`
		Legacy *jsonx.Object   `json:"legacy"`
		Raw    json.RawMessage `json:"raw"`
	}{"中文<&>", jsonx.Obj("z", jsonx.Number("6e1"), "a", true), json.RawMessage(`{"value":"<&>"}`)}
	data, err := Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"name":"中文<&>","legacy":{"z":6e1,"a":true},"raw":{"value":"<&>"}}` {
		t.Fatal(string(data))
	}
	if _, err := Encode(make(chan int)); err == nil {
		t.Fatal("unsupported values must fail")
	}
}
