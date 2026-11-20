package testjson

import (
	"encoding/json"
	"testing"
)

func TestDecode(t *testing.T) {
	type fixture struct {
		Name   string      `json:"name"`
		Number json.Number `json:"number"`
	}
	got := Decode[fixture](t, json.RawMessage(`{"name":"中文 <fixture>","number":9007199254740993}`))
	if got.Name != "中文 <fixture>" || got.Number.String() != "9007199254740993" {
		t.Fatalf("decoded fixture: %+v", got)
	}
	copy := Decode[fixture](t, got)
	if copy != got {
		t.Fatalf("round trip: %+v", copy)
	}
}
