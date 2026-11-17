package model

import (
	"bytes"
	"configflow/internal/yamlx"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type sampleRecord struct {
	Name     string         `json:"name"`
	Count    Int            `json:"count"`
	Enabled  *bool          `json:"enabled"`
	Children []sampleRecord `json:"children"`
	Fields   Fields
	raw      Raw
}

func (r *sampleRecord) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }
func (r sampleRecord) MarshalJSON() ([]byte, error)     { return EncodeRecord(&r, &r.raw) }

func TestRecordOriginalBytesAndTypedEdits(t *testing.T) {
	original := []byte(" \n{ \"future\" : { \"z\": 1.0, \"a\": \"\\u4f8b <>&\" }, \"name\" : \"\\u4f8b\", \"count\":6e1, \"children\": [ {\"name\":\"a\",\"future\": { \"b\": 1, \"a\": 2 }} ] }\t")
	var record sampleRecord
	if err := DecodeRecord(original, &record, &record.raw); err != nil {
		t.Fatal(err)
	}
	if record.Count != 60 || record.Name != "例" {
		t.Fatal("typed values differ")
	}
	encoded, err := Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, original) {
		t.Fatalf("no-op changed bytes:\n%s", encoded)
	}
	record.Children[0].Name = "changed <>&"
	encoded, err = Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"future": { "b": 1, "a": 2 }`)) || !bytes.Contains(encoded, []byte(`"future" : { "z": 1.0, "a": "\u4f8b <>&" }`)) {
		t.Fatalf("unknown bytes lost: %s", encoded)
	}
	if !bytes.Contains(encoded, []byte(`"count":6e1`)) {
		t.Fatal("unchanged number normalized")
	}
	if !bytes.Contains(encoded, []byte(`changed <>&`)) {
		t.Fatal("HTML escaped")
	}
}

func TestRecordInvalidTypeAndPresence(t *testing.T) {
	var record sampleRecord
	original := []byte(`{"name":5,"count":true,"enabled":false,"future":null}`)
	if err := json.Unmarshal(original, &record); err != nil {
		t.Fatal(err)
	}
	if record.Name != "" || record.Count != 0 || record.Enabled == nil || *record.Enabled {
		t.Fatal("wrong fallback or pointer default")
	}
	if issues := valueIssues(reflect.ValueOf(record), ""); len(issues) != 2 {
		t.Fatalf("issues=%v", issues)
	}
	got, err := Marshal(record)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("bad fields not kept: %s %v", got, err)
	}
	record.Name = "fixed"
	record.Enabled = nil
	got, err = Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte(`"enabled"`)) || !bytes.Contains(got, []byte(`"count":true`)) {
		t.Fatal(string(got))
	}
	if !record.Fields.Delete("future") {
		t.Fatal("unknown field missing")
	}
	if err := record.Fields.Set("later", json.RawMessage(`{ "a": 1 }`)); err != nil {
		t.Fatal(err)
	}
	copy := record
	if err := copy.Fields.Set("later", json.RawMessage(`2`)); err != nil {
		t.Fatal(err)
	}
	value, _ := record.Fields.Get("later")
	if string(value) != `{ "a": 1 }` {
		t.Fatal("Fields mutation aliased a copied record")
	}
}

func TestIntExactJSON(t *testing.T) {
	tests := []struct {
		literal string
		value   Int
		valid   bool
	}{
		{"60", 60, true}, {"60.0", 60, true}, {"6e1", 60, true}, {"600e-1", 60, true}, {"0e9999999999999999999999", 0, true},
		{"9223372036854775807.0", 9223372036854775807, true}, {"-9223372036854775808", -9223372036854775808, true},
		{"1.00000000000000000001", 0, false}, {"1e99999999999999999999", 0, false}, {"1e-999999999999999999", 0, false},
		{"true", 0, false}, {`"60"`, 0, false}, {"null", 0, false}, {"9223372036854775808", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.literal, func(t *testing.T) {
			var value Int
			err := json.Unmarshal([]byte(tt.literal), &value)
			if (err == nil) != tt.valid || tt.valid && value != tt.value {
				t.Fatalf("%d %v", value, err)
			}
		})
	}
}

func TestRejectDuplicateOrInvalidJSON(t *testing.T) {
	for _, text := range []string{`{"x":1,"x":2}`, `{"x":{"a":1,"\u0061":2}}`, `{"x":NaN}`, "{\"x\":\"\xff\"}"} {
		var record sampleRecord
		if err := json.Unmarshal([]byte(text), &record); !errors.Is(err, ErrJSON) && json.Valid([]byte(text)) {
			t.Fatalf("accepted invalid record: %v", err)
		}
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if _, err := Marshal(cyclic); !errors.Is(err, ErrJSON) {
		t.Fatalf("cycle: %v", err)
	}
}

func TestOrderedMapAndDynamic(t *testing.T) {
	original := []byte(`{ "second": {"name":"b","future": { "z": 1.0 }}, "first": {"count":6e1}, "bad":false }`)
	var entries Map[sampleRecord]
	if err := json.Unmarshal(original, &entries); err != nil {
		t.Fatal(err)
	}
	encoded, err := Marshal(entries)
	if err != nil || !bytes.Equal(encoded, original) {
		t.Fatalf("map no-op: %s %v", encoded, err)
	}
	if issues := entries.fieldIssues("entries"); len(issues) != 1 || issues[0].Path != "entries[2]" {
		t.Fatalf("map issues=%v", issues)
	}
	second, _ := entries.Get("second")
	second.Name = "updated"
	copy := entries
	copy.Set("second", second)
	encoded, err = Marshal(copy)
	if err != nil || !bytes.Contains(encoded, []byte(`"future": { "z": 1.0 }`)) {
		t.Fatalf("map edited value: %s %v", encoded, err)
	}
	unchanged, err := Marshal(entries)
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("Map.Set changed copied map")
	}
	var dynamic Dynamic
	source := []byte(`{ "z":1.0,"a":"\u4f8b <>&" }`)
	if err := dynamic.UnmarshalJSON(source); err != nil {
		t.Fatal(err)
	}
	node, err := dynamic.Node()
	if err != nil {
		t.Fatal(err)
	}
	yamlx.Set(node, "a", yamlx.Str("changed"))
	unedited, err := Marshal(dynamic)
	if err != nil || !bytes.Equal(unedited, source) {
		t.Fatal("Node mutated Dynamic")
	}
	if err := dynamic.SetNode(node); err != nil {
		t.Fatal(err)
	}
	edited, err := Marshal(dynamic)
	if err != nil || !bytes.Contains(edited, []byte(`changed`)) {
		t.Fatal("SetNode edit missing")
	}
}
