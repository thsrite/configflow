package model

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestProxyGroupSetJSONIsolatesCopy (O18 T1c): ProxyGroup is a plain record.
// SetJSON on a copy strictly decodes a declared member without touching the
// original; the undeclared member is dropped on read and cannot be set (it
// used to be kept in Fields and written back in place).
func TestProxyGroupSetJSONIsolatesCopy(t *testing.T) {
	var original ProxyGroup
	input := []byte(`{"id":"g","unknown":9007199254740993,"enabled":true,"manual_nodes":["a"]}`)
	if err := json.Unmarshal(input, &original); err != nil {
		t.Fatal(err)
	}
	copy := original
	for _, entry := range []struct{ name, value string }{{"enabled", "false"}, {"manual_nodes", `["b"]`}, {"proxies_order", "[]"}} {
		if err := copy.SetJSON(entry.name, json.RawMessage(entry.value)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Marshal(copy)
	if err != nil || string(got) != `{"id":"g","name":"","enabled":false,"manual_nodes":["b"],"proxies_order":[]}` {
		t.Fatalf("owned record %s %v", got, err)
	}
	before, err := Marshal(original)
	if err != nil || string(before) != `{"id":"g","name":"","enabled":true,"manual_nodes":["a"]}` {
		t.Fatalf("original changed %s %v", before, err)
	}
	if err := copy.SetJSON("unknown", json.RawMessage(`1`)); err == nil {
		t.Fatal("undeclared member accepted")
	}
	var typed *MemberTypeError
	if err := copy.SetJSON("enabled", json.RawMessage(`"yes"`)); !errors.As(err, &typed) {
		t.Fatalf("wrong-typed input accepted: %v", err)
	}
}
