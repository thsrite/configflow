package model

import (
	"encoding/json"
	"testing"
)

func TestProxyGroupSetJSONRetainsOrderAndIsolatesCopy(t *testing.T) {
	var original ProxyGroup
	input := []byte(`{"id":"g","unknown":9007199254740993,"enabled":true}`)
	if err := json.Unmarshal(input, &original); err != nil {
		t.Fatal(err)
	}
	copy := original
	for _, entry := range []struct{ name, value string }{{"enabled", "null"}, {"manual_nodes", "[]"}, {"proxies_order", "[]"}} {
		if err := copy.SetJSON(entry.name, json.RawMessage(entry.value)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Marshal(copy)
	if err != nil || string(got) != `{"id":"g","unknown":9007199254740993,"enabled":null,"manual_nodes":[],"proxies_order":[]}` {
		t.Fatalf("owned record %s %v", got, err)
	}
	before, err := Marshal(original)
	if err != nil || string(before) != string(input) {
		t.Fatalf("input changed %s %v", before, err)
	}
	if err := copy.SetJSON("unknown", json.RawMessage(`1`)); err == nil {
		t.Fatal("opaque member bypassed Fields owner")
	}
}
