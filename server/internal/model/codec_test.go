package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"configflow/internal/yamlx"
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
	if record.Name != "5" || record.Count != 0 || record.Enabled == nil || *record.Enabled {
		t.Fatal("wrong fallback or pointer default")
	}
	if issues := valueIssues(reflect.ValueOf(record), ""); len(issues) != 1 {
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
		{"true", 0, false}, {`"60"`, 60, true}, {"null", 0, false}, {"9223372036854775808", 0, false},
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

func TestSliceOverlapIsNotACycle(t *testing.T) {
	records := make([]sampleRecord, 2)
	records[0].Name = "first"
	records[1].Name = "second"
	records[1].Children = records[:1]
	encoded, err := Marshal(records)
	if err != nil {
		t.Fatal("acyclic overlapping slices rejected:", err)
	}
	var decoded []sampleRecord
	if err := json.Unmarshal(encoded, &decoded); err != nil || len(decoded) != 2 || len(decoded[1].Children) != 1 || decoded[1].Children[0].Name != "first" {
		t.Fatal("overlapping slice values changed")
	}
	records[0].Children = records[:0]
	if _, err := Marshal(records); err != nil {
		t.Fatal("empty overlapping slice rejected:", err)
	}
	records[0].Children = records[:1]
	if _, err := Marshal(records); !errors.Is(err, ErrJSON) {
		t.Fatal("real slice cycle accepted")
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

// Plain records (O18 T1a): whole-document input is decoded strictly and a
// mismatch names its member; unknown members of plain records are dropped,
// records not yet migrated keep theirs; profiles keep their order.
func TestPlainDocumentStrictInputUnknownMembersAndOrder(t *testing.T) {
	for _, tt := range []struct {
		name, source, record, member string
	}{
		{"system revision", `{"schema_version":5,"system":{"_revision":true},"shared":{},"profiles":{}}`, "system", "_revision"},
		{"profile name", `{"schema_version":5,"system":{},"shared":{},"profiles":{"p":{"id":"p","name":7}}}`, "profile", "name"},
		{"shared list", `{"schema_version":5,"system":{},"shared":{"nodes":{}},"profiles":{}}`, "shared", "nodes"},
		{"section", `{"schema_version":5,"system":[],"shared":{},"profiles":{}}`, "document", "system"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := DecodeDocument([]byte(tt.source))
			var typed *MemberTypeError
			if !errors.As(err, &typed) || typed.Record != tt.record || typed.Member != tt.member || !errors.Is(err, ErrType) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	if _, _, err := DecodeDocument([]byte(`{"schema_version":5,"system":{},"shared":{}}`)); !errors.As(err, new(*MissingSectionError)) {
		t.Fatalf("missing section: %v", err)
	}

	original := []byte(`{ "schema_version":5,"system":{"agents":[{"token":"synthetic","future": { "z":1.0 }}],"_revision":1,"future_system": { "z":1.0 }},"profiles":{"second":{"id":"second","name":"7","_revision":6e1,"proxy_groups":[{"name":"example","future":{"z":1}}],"future_profile": { "n":6e1 }},"first":{}},"shared":{"nodes":[],"_revision":0,"future_shared": [ "\u4f8b",1.0 ]},"future": { "a":1 } }`)
	document, issues, err := DecodeDocument(original)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 || document.System.Revision != 1 || document.Profiles[0].Revision != 60 || document.Profiles[0].Name != "7" {
		t.Fatalf("typed values issues=%v", issues)
	}
	encoded, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"future_system", "future_shared", "future_profile", `"future":{"a":1}`} {
		if bytes.Contains(encoded, []byte(fragment)) {
			t.Fatalf("unknown member %s kept: %s", fragment, encoded)
		}
	}
	for _, fragment := range []string{`"future":{"z":1.0}`, `"future":{"z":1}`, `"_revision":0`, `"description":""`} {
		if !bytes.Contains(encoded, []byte(fragment)) {
			t.Fatalf("lost %s: %s", fragment, encoded)
		}
	}
	again, _, err := DecodeDocument(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := Marshal(again); err != nil || !bytes.Equal(repeated, encoded) {
		t.Fatalf("encoding not stable: %s", repeated)
	}
	document.Profiles[0], document.Profiles[1] = document.Profiles[1], document.Profiles[0]
	encoded, err = Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Index(encoded, []byte(`"first":`)) > bytes.Index(encoded, []byte(`"second":`)) {
		t.Fatal("profile reorder ignored")
	}
}

func TestScalarTypesDefaultsAndMemberLayout(t *testing.T) {
	// Whitespace before commas belongs to the untouched source layout too.
	var record sampleRecord
	original := []byte("{\"future\" : { \"z\":1 } \n, \"name\":\"before\" \t, \"count\":6e1 \n}")
	if err := DecodeRecord(original, &record, &record.raw); err != nil {
		t.Fatal(err)
	}
	record.Name = "after"
	encoded, err := Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("{ \"z\":1 } \n,")) || !bytes.Contains(encoded, []byte("6e1 \n}")) {
		t.Fatal("member layout changed")
	}
	if record.Enabled != nil {
		t.Fatal("missing optional bool became present")
	}
	enabled := func(value *bool) bool { return value == nil || *value }
	if !enabled(record.Enabled) {
		t.Fatal("absent default is not true")
	}
	off := false
	record.Enabled = &off
	encoded, err = Marshal(record)
	if err != nil || !bytes.Contains(encoded, []byte(`"enabled":false`)) {
		t.Fatal("explicit false was lost")
	}
	var fields Fields
	if err := fields.UnmarshalJSON([]byte(`{"n":{"z":1.0}}`)); err != nil {
		t.Fatal(err)
	}
	raw, _ := fields.Get("n")
	raw[0] = 'X'
	got, _ := fields.Get("n")
	if string(got) != `{"z":1.0}` {
		t.Fatal("Fields.Get exposes mutable provenance")
	}
	type numericRecord struct {
		Number *json.Number `json:"number"`
		raw    Raw
	}
	var numeric numericRecord
	if err := DecodeRecord([]byte(`{"number":"60"}`), &numeric, &numeric.raw); err != nil {
		t.Fatal(err)
	}
	if numeric.Number != nil || len(numeric.raw.Issues()) != 1 {
		t.Fatal("quoted json.Number accepted")
	}
	type unsafeRecord struct {
		Value any `json:"value"`
		raw   Raw
	}
	var unsafe unsafeRecord
	if err := DecodeRecord([]byte(`{}`), &unsafe, &unsafe.raw); !errors.Is(err, ErrType) {
		t.Fatal("interface field accepted")
	}
	type integerRecord struct {
		Value int64 `json:"value"`
		raw   Raw
	}
	var integer integerRecord
	if err := DecodeRecord([]byte(`{}`), &integer, &integer.raw); !errors.Is(err, ErrType) {
		t.Fatal("non-model integer field accepted")
	}
	type otherRecord struct {
		Name string `json:"name"`
	}
	var other otherRecord
	if _, err := EncodeRecord(&other, &record.raw); !errors.Is(err, ErrType) {
		t.Fatal("provenance of another record type accepted")
	}
}
