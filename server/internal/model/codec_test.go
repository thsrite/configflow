package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"configflow/internal/yamlx"
)

// sampleRecord is a plain struct without codecs, like the DATA_DIR records
// outside this package.
type sampleRecord struct {
	Name     string         `json:"name"`
	Count    Int            `json:"count,omitzero"`
	Enabled  *bool          `json:"enabled,omitzero"`
	Children []sampleRecord `json:"children,omitzero"`
}

// A plain struct without codecs decodes leniently with DecodeStored (unknown
// members dropped and reported) and is written by Marshal in declaration
// order, omitzero members left out.
func TestPlainStructWithoutCodecs(t *testing.T) {
	original := []byte(` {"future":{"z":1.0},"name":"\u4f8b","count":6e1,"children":[{"name":"a","count":"x"}]} `)
	var record sampleRecord
	issues, err := DecodeStored(original, &record, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []FieldIssue{{Path: "children[0].count", Kind: IssueType}, {Path: "future", Kind: IssueUnknown}}
	if !reflect.DeepEqual(issues, want) {
		t.Fatalf("issues=%v", issues)
	}
	encoded, err := Marshal(record)
	if err != nil || string(encoded) != `{"name":"例","count":60,"children":[{"name":"a"}]}` {
		t.Fatalf("encoded=%s %v", encoded, err)
	}
	record.Enabled = new(false)
	encoded, err = Marshal(&record)
	if err != nil || !bytes.Contains(encoded, []byte(`"enabled":false`)) {
		t.Fatalf("explicit false was lost: %s %v", encoded, err)
	}
	type withAny struct {
		Value any `json:"value"`
	}
	if _, err := Marshal(withAny{Value: map[string]any{"b": 1, "a": "<&>"}}); err != nil {
		t.Fatal(err)
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
	for _, text := range []string{`{"x":1,"x":2}`, `{"x":{"a":1,"\u0061":2}}`, `{"x":NaN}`} {
		var record sampleRecord
		if _, err := DecodeStored([]byte(text), &record, ""); !errors.Is(err, ErrJSON) {
			t.Fatalf("accepted invalid record %s: %v", text, err)
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
	if _, err := DecodeStored(encoded, &decoded, ""); err != nil || len(decoded) != 2 || len(decoded[1].Children) != 1 || decoded[1].Children[0].Name != "first" {
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
	original := []byte(`{"second":{"name":"b","future":{"z":1.0}},"first":{"count":6e1},"bad":false}`)
	var entries OrderedMap[sampleRecord]
	issues, err := DecodeStored(original, &entries, "entries")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 || issues[0] != (FieldIssue{Path: "entries{*}", Kind: IssueType}) || issues[1].Kind != IssueUnknown {
		t.Fatalf("map issues=%v", issues)
	}
	encoded, err := Marshal(entries)
	if err != nil || string(encoded) != `{"second":{"name":"b"},"first":{"name":"","count":60},"bad":{"name":""}}` {
		t.Fatalf("map: %s %v", encoded, err)
	}
	second, _ := entries.Get("second")
	second.Name = "updated"
	copy := entries
	copy.Set("second", second)
	unchanged, err := Marshal(entries)
	if err != nil || !bytes.Equal(unchanged, encoded) {
		t.Fatal("OrderedMap.Set changed copied map")
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

// Plain records (O18 T1a–T1c): whole-document input is decoded strictly and a
// mismatch names its member; unknown members are dropped at every level (the
// nested agent and proxy-group records are plain since T1b/T1c); always-written
// members stay; profiles keep their order.
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
			_, err := DecodeDocument([]byte(tt.source))
			var typed *MemberTypeError
			if !errors.As(err, &typed) || typed.Record != tt.record || typed.Member != tt.member || !errors.Is(err, ErrType) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	if _, err := DecodeDocument([]byte(`{"schema_version":5,"system":{},"shared":{}}`)); !errors.As(err, new(*MissingSectionError)) {
		t.Fatalf("missing section: %v", err)
	}

	original := []byte(`{ "schema_version":5,"system":{"agents":[{"token":"synthetic","future": { "z":1.0 }}],"_revision":1,"future_system": { "z":1.0 }},"profiles":{"second":{"id":"second","name":"7","_revision":6e1,"proxy_groups":[{"name":"example","future":{"z":1}}],"future_profile": { "n":6e1 }},"first":{}},"shared":{"nodes":[],"_revision":0,"future_shared": [ "\u4f8b",1.0 ]},"future": { "a":1 } }`)
	document, err := DecodeDocument(original)
	if err != nil {
		t.Fatal(err)
	}
	if document.System.Revision != 1 || document.Profiles[0].Revision != 60 || document.Profiles[0].Name != "7" {
		t.Fatal("typed values")
	}
	encoded, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"future_system", "future_shared", "future_profile", `"future"`} {
		if bytes.Contains(encoded, []byte(fragment)) {
			t.Fatalf("unknown member %s kept: %s", fragment, encoded)
		}
	}
	for _, fragment := range []string{`"_revision":0`, `"description":""`, `"proxy_groups":[{"name":"example"}]`} {
		if !bytes.Contains(encoded, []byte(fragment)) {
			t.Fatalf("lost %s: %s", fragment, encoded)
		}
	}
	again, err := DecodeDocument(encoded)
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

func TestFieldsAreOwnedCopies(t *testing.T) {
	var fields Fields
	if err := fields.UnmarshalJSON([]byte(`{"n":{"z":1.0},"m":[1]}`)); err != nil {
		t.Fatal(err)
	}
	raw, _ := fields.Get("n")
	raw[0] = 'X'
	got, _ := fields.Get("n")
	if string(got) != `{"z":1.0}` {
		t.Fatal("Fields.Get exposes mutable state")
	}
	copy := fields
	if err := copy.Set("n", json.RawMessage(`2`)); err != nil {
		t.Fatal(err)
	}
	copy.Delete("m")
	if encoded, _ := fields.MarshalJSON(); string(encoded) != `{"n":{"z":1.0},"m":[1]}` {
		t.Fatalf("copy changed the original: %s", encoded)
	}
	if encoded, _ := copy.MarshalJSON(); string(encoded) != `{"n":2}` {
		t.Fatalf("copy: %s", encoded)
	}
	for _, text := range []string{`[]`, `{"a":1,"a":2}`, `{`} {
		var other Fields
		if other.UnmarshalJSON([]byte(text)) == nil {
			t.Fatalf("accepted %s", text)
		}
	}
	if err := fields.Set("x", json.RawMessage(`1,2`)); !errors.Is(err, ErrJSON) {
		t.Fatalf("fragment accepted: %v", err)
	}
}
