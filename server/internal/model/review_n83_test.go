package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"configflow/internal/yamlx"
)

// reviewRecord exercises the independent N8-3 review cases: nested records,
// ordered maps of records, exact numbers in lists, Dynamic and raw members.
type reviewRecord struct {
	Name    string             `json:"name"`
	Numbers []json.Number      `json:"numbers"`
	Next    *reviewRecord      `json:"next"`
	Byname  Map[*reviewRecord] `json:"byname"`
	Dyn     Dynamic            `json:"dyn"`
	Raw     json.RawMessage    `json:"raw"`
	Ints    *[]Int             `json:"ints"`
	Nested  [][]json.Number    `json:"nested"`
	Fields  Fields
	raw     Raw
}

func (r *reviewRecord) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }
func (r reviewRecord) MarshalJSON() ([]byte, error)     { return EncodeRecord(&r, &r.raw) }

func reviewDecode(t *testing.T, data string) *reviewRecord {
	t.Helper()
	var record reviewRecord
	if err := DecodeRecord([]byte(data), &record, &record.raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return &record
}

// reviewAssertUntouched checks R1/R2 independently of the codec: output is
// valid, has unique keys at every depth, keeps the original member order and
// the bytes of every member except the edited ones.
func reviewAssertUntouched(t *testing.T, before, after []byte, edited ...string) {
	t.Helper()
	order, members := fuzzMembers(t, before)
	afterOrder, afterMembers := fuzzMembers(t, after)
	var kept []string
	for _, name := range afterOrder {
		if _, ok := members[name]; ok {
			kept = append(kept, name)
		}
	}
	var expected []string
	for _, name := range order {
		if _, ok := afterMembers[name]; ok {
			expected = append(expected, name)
		}
	}
	if !slices.Equal(kept, expected) {
		t.Fatalf("member order changed: %v -> %v", order, afterOrder)
	}
	for name, value := range members {
		if slices.Contains(edited, name) {
			continue
		}
		if !bytes.Equal(value, afterMembers[name]) {
			t.Fatalf("member %s changed: %s -> %s", name, value, afterMembers[name])
		}
	}
}

func TestReviewN83AdversarialLayoutsKeepBytes(t *testing.T) {
	tests := []struct{ name, source string }{
		{"empty", `{}`},
		{"empty with whitespace", " \r\n{ \t\n } \n"},
		{"nested raw whitespace", `{"raw" : [ { "b" : 1.0 , "a" : [ ] } ,null ] ,"name":"x"}`},
		{"escapes", `{"name":"😀\/\"","füture":"<>&"}`},
		{"raw line separators", "{\"future\":\"a\u2028b\u2029c\",\"name\":\"\u2028\"}"},
		{"huge numbers", `{"raw":123456789012345678901234567890e999999,"numbers":[-0.0e-0,1E+400],"ints":[6e1,-9223372036854775808]}`},
		{"nested records", `{"next":{"next":{"name":"deep","z":{ }}, "y":1},"byname":{"user key":{"name":"v","k":[]}, "":null}}`},
		{"dynamic", `{"dyn": { "list" : [1.50, "x"], "map": {"b":null} } }`},
		{"invalid members kept", `{"name":["x"],"numbers":"1","next":7,"byname":[],"ints":{"a":1},"nested":[["1"]]}`},
		{"lone surrogate", `{"name":"\udc00","future":"\ud800"}`},
		{"many members", `{"a":1,"b":2,"c":3,"name":"x","d":4}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := reviewDecode(t, tt.source)
			encoded, err := Marshal(record)
			if err != nil || !bytes.Equal(encoded, []byte(tt.source)) {
				t.Fatalf("no-op changed bytes: %v\n%s", err, encoded)
			}
			record.Name = "edited \u2028\u2029<>&\x00 例"
			changed, err := Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			reviewAssertUntouched(t, []byte(tt.source), changed, "name")
			// New scalars: SetEscapeHTML(false), line separators escaped by encoding/json.
			if !bytes.Contains(changed, []byte("\"edited \\u2028\\u2029<>&\\u0000 例\"")) {
				t.Fatalf("new scalar encoding: %s", changed)
			}
			again := reviewDecode(t, string(changed))
			if again.Name != record.Name {
				t.Fatal("edit did not round trip")
			}
			stable, err := Marshal(again)
			if err != nil || !bytes.Equal(stable, changed) {
				t.Fatal("re-encode not stable")
			}
		})
	}
}

func TestReviewN83DeletingMembersKeepsValidCommas(t *testing.T) {
	tests := []struct {
		name, source, want string
		edit               func(*reviewRecord)
	}{
		{"delete last unknown", "{\"name\":\"x\" ,\n \"z\":1\n}", `{"name":"x" }`, func(r *reviewRecord) { r.Fields.Delete("z") }},
		{"delete first unknown", "{ \"z\":1 , \"name\":\"x\" }", `{ "name":"x" }`, func(r *reviewRecord) { r.Fields.Delete("z") }},
		{"delete only unknown", "{ \"z\" : 1 }", "{}", func(r *reviewRecord) { r.Fields.Delete("z") }},
		{"delete last pointer", `{"name":"x","next":{"name":"y"}}`, `{"name":"x"}`, func(r *reviewRecord) { r.Next = nil }},
		{"delete middle pointer", `{"a":1,"next":{},"b":2}`, `{"a":1,"b":2}`, func(r *reviewRecord) { r.Next = nil }},
		{"delete then re-add appends", `{"z":1,"name":"x"}`, `{"name":"x","z":2}`, func(r *reviewRecord) {
			r.Fields.Delete("z")
			_ = r.Fields.Set("z", json.RawMessage(`2`))
		}},
		{"add into empty", "{\n}", "{\"name\":\"n\"\n}", func(r *reviewRecord) { r.Name = "n" }},
		{"nil pointer to original null stays", `{"next":null}`, `{"next":null}`, func(r *reviewRecord) { r.Next = nil }},
		{"map delete last", `{"byname":{"a":{},"b":{"name":"x"}}}`, `{"byname":{"a":{}}}`, func(r *reviewRecord) { r.Byname.Delete("b") }},
		{"map delete first", `{"byname":{ "a":{} , "b":{} }}`, `{"byname":{ "b":{} }}`, func(r *reviewRecord) { r.Byname.Delete("a") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := reviewDecode(t, tt.source)
			tt.edit(record)
			encoded, err := Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != tt.want {
				t.Fatalf("got %s want %s", encoded, tt.want)
			}
			fuzzUniqueJSON(t, encoded)
		})
	}
}

func TestReviewN83DuplicateMembersAreRejected(t *testing.T) {
	for _, source := range []string{
		`{"name":"a","name":"b"}`,
		`{"name":"a","name":"b"}`,
		`{"raw":{"a":1,"a":2}}`,
		`{"raw":[{"x":{"k":1,"k":2}}]}`,
		`{"dyn":{"a":1,"a":1}}`,
		`{"byname":{"k":{},"k":{}}}`,
	} {
		var record reviewRecord
		if err := DecodeRecord([]byte(source), &record, &record.raw); !errors.Is(err, ErrJSON) {
			t.Fatalf("%s: err=%v", source, err)
		}
	}
	record := reviewDecode(t, `{"name":"x"}`)
	if err := record.Fields.Set("future", json.RawMessage(`{"a":1,"a":2}`)); !errors.Is(err, ErrJSON) {
		t.Fatal("Fields accepted a duplicate member")
	}
	record.Raw = json.RawMessage(`{"a":1,"a":2}`)
	if _, err := Marshal(record); !errors.Is(err, ErrJSON) {
		t.Fatal("raw member with duplicate names was emitted")
	}
	record.Raw = json.RawMessage(``)
	if _, err := Marshal(record); err == nil {
		t.Fatal("empty raw member was emitted")
	}
	record.Raw = nil
	if err := record.Fields.Set("name", json.RawMessage(`1`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Marshal(record); !errors.Is(err, ErrJSON) {
		t.Fatal("opaque member shadowing a declared member was emitted")
	}
}

// R8/R5: a string is never an exact number, also inside lists. encoding/json
// would otherwise coerce "1" into json.Number for slice elements.
func TestReviewN83NumberListsRejectStrings(t *testing.T) {
	tests := []struct {
		source  string
		invalid string
	}{
		{`{"numbers":["1"]}`, "numbers"},
		{`{"numbers":[1,"2"]}`, "numbers"},
		{`{"nested":[[1],["2"]]}`, "nested"},
		{`{"numbers":[true]}`, "numbers"},
		{`{"numbers":[1,2.5,1e400]}`, ""},
		{`{"numbers":null,"nested":[null,[3]]}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			record := reviewDecode(t, tt.source)
			issues := record.raw.Issues()
			if tt.invalid == "" {
				if len(issues) != 0 {
					t.Fatalf("issues=%v", issues)
				}
				return
			}
			if len(issues) != 1 || issues[0].Path != tt.invalid || issues[0].Kind != IssueType {
				t.Fatalf("issues=%v", issues)
			}
			encoded, err := Marshal(record)
			if err != nil || string(encoded) != tt.source {
				t.Fatalf("invalid member not preserved: %s", encoded)
			}
		})
	}
	record := reviewDecode(t, `{"numbers":[1,2.50,123456789012345678901234567890],"nested":[[1],null]}`)
	if !slices.Equal(record.Numbers, []json.Number{"1", "2.50", "123456789012345678901234567890"}) || record.Nested[1] != nil {
		t.Fatalf("numbers=%v nested=%v", record.Numbers, record.Nested)
	}
}

// Python ProfileRepository.clone_profile copies a profile. The obvious typed
// form copies the record (unknown members included) and assigns a new ID; the
// copy must become its own section rather than inheriting the source's key.
func TestReviewN83CopiedProfileUsesItsNewID(t *testing.T) {
	source := `{"profiles":{"p":{"id":"p","name":"A","future":{"k":1}},"q":{"id":7,"name":"B"}}}`
	document, issues, err := DecodeDocument([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 || document.Profiles[1].ID != "7" {
		t.Fatalf("issues=%v", issues)
	}
	clone := *document.Profiles[0]
	clone.ID = "copy"
	clone.Name = "A copy"
	document.Profiles = append(document.Profiles, &clone)
	encoded, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"profiles":{"p":{"id":"p","name":"A","future":{"k":1}},"q":{"id":7,"name":"B"},"copy":{"id":"copy","name":"A copy","future":{"k":1}}}}`
	if string(encoded) != want {
		t.Fatalf("got %s", encoded)
	}
	// An untouched malformed id still keeps its original section key.
	document.Profiles[1].Name = "B2"
	encoded, err = Marshal(document)
	if err != nil || !strings.Contains(string(encoded), `"q":{"id":7,"name":"B2"}`) {
		t.Fatalf("owner of malformed id lost: %v %s", err, encoded)
	}
	// Two records claiming the same section are rejected, never merged.
	twin := *document.Profiles[0]
	document.Profiles = append(document.Profiles, &twin)
	if _, err := Marshal(document); !errors.Is(err, ErrJSON) {
		t.Fatalf("duplicate section accepted: %v", err)
	}
}

// R2 for Dynamic: Node/SetNode of an unchanged tree is a no-op; a real change
// is written in yamlx.ToJSON form.
func TestReviewN83DynamicUnchangedTreeKeepsBytes(t *testing.T) {
	source := `{"dyn": { "b" : [1.50, "x", {"k": null}], "a": true }}`
	record := reviewDecode(t, source)
	node, err := record.Dyn.Node()
	if err != nil {
		t.Fatal(err)
	}
	if err := record.Dyn.SetNode(node); err != nil {
		t.Fatal(err)
	}
	encoded, err := Marshal(record)
	if err != nil || string(encoded) != source {
		t.Fatalf("unchanged Dynamic rewritten: %v %s", err, encoded)
	}
	yamlx.Set(node, "a", yamlx.Bool(false))
	if err := record.Dyn.SetNode(node); err != nil {
		t.Fatal(err)
	}
	encoded, err = Marshal(record)
	if err != nil || string(encoded) != `{"dyn": {"b":[1.50,"x",{"k":null}],"a":false}}` {
		t.Fatalf("changed Dynamic: %v %s", err, encoded)
	}
}

func TestReviewN83CycleDetection(t *testing.T) {
	shared := &reviewRecord{Name: "shared"}
	dag := reviewRecord{Next: &reviewRecord{Next: shared}}
	dag.Byname.Set("a", shared)
	dag.Byname.Set("b", shared)
	if _, err := Marshal(dag); err != nil {
		t.Fatalf("shared pointer (DAG) rejected: %v", err)
	}
	loop := &reviewRecord{Name: "loop"}
	loop.Byname.Set("self", loop)
	if _, err := Marshal(loop); !errors.Is(err, ErrJSON) {
		t.Fatalf("cycle through Map not detected: %v", err)
	}
	if _, err := json.Marshal(loop); err == nil {
		t.Fatal("cycle through encoding/json entry not detected")
	}
	indirect := &reviewRecord{}
	indirect.Next = &reviewRecord{Next: indirect}
	if _, err := Marshal(indirect); !errors.Is(err, ErrJSON) {
		t.Fatalf("pointer cycle not detected: %v", err)
	}
}

func TestReviewN83ArraysAreNotStoredTypes(t *testing.T) {
	type arrayRecord struct {
		Pair [2]string `json:"pair"`
		raw  Raw
	}
	var record arrayRecord
	if err := DecodeRecord([]byte(`{"pair":["a","b","c"]}`), &record, &record.raw); !errors.Is(err, ErrType) {
		t.Fatalf("fixed-size array silently truncates stored lists: %v", err)
	}
	if err := storedType(reflect.TypeFor[[]string]()); err != nil {
		t.Fatal(err)
	}
}

// Typed code clears lists the Go way (nil, or `var kept []T` that ends up
// empty) and maps with a zero Map. Stored JSON keeps arrays/objects, as
// Python wrote them; a stored null that the code did not change stays null.
func TestReviewN83EmptyContainersStayContainers(t *testing.T) {
	tests := []struct {
		name, source, want string
		edit               func(*reviewRecord)
	}{
		{"cleared list", `{"numbers":[1],"nested":[[2]]}`, `{"numbers":[],"nested":[]}`, func(r *reviewRecord) {
			var kept []json.Number
			r.Numbers = kept
			r.Nested = nil
		}},
		{"stored null list untouched", `{"numbers":null}`, `{"numbers":null}`, func(r *reviewRecord) { r.Numbers = nil }},
		{"stored empty list untouched", `{"numbers": [ ]}`, `{"numbers": [ ]}`, func(r *reviewRecord) { r.Numbers = []json.Number{} }},
		{"absent list stays absent", `{"name":"x"}`, `{"name":"x"}`, func(r *reviewRecord) { r.Numbers = nil }},
		{"cleared map", `{"byname":{"a":{}}}`, `{"byname":{}}`, func(r *reviewRecord) { r.Byname = Map[*reviewRecord]{} }},
		{"map emptied by delete", `{"byname":{"a":{}}}`, `{"byname":{}}`, func(r *reviewRecord) { r.Byname.Delete("a") }},
		{"stored null map untouched", `{"byname":null}`, `{"byname":null}`, func(r *reviewRecord) {}},
		{"stored null map replaced", `{"byname":null}`, `{"byname":{"k":{"name":"v"}}}`, func(r *reviewRecord) { r.Byname.Set("k", &reviewRecord{Name: "v"}) }},
		{"pointer list removed", `{"ints":[1],"name":"x"}`, `{"name":"x"}`, func(r *reviewRecord) { r.Ints = nil }},
		{"pointer to nil list", `{"name":"x"}`, `{"name":"x","ints":[]}`, func(r *reviewRecord) { r.Ints = new([]Int) }},
		{"raw stays raw", `{"raw":null}`, `{"raw":null}`, func(r *reviewRecord) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := reviewDecode(t, tt.source)
			tt.edit(record)
			encoded, err := Marshal(record)
			if err != nil || string(encoded) != tt.want {
				t.Fatalf("got %s want %s (%v)", encoded, tt.want, err)
			}
		})
	}
	var zero Map[string]
	if encoded, err := json.Marshal(zero); err != nil || string(encoded) != `{}` {
		t.Fatalf("zero Map: %s %v", encoded, err)
	}
	if encoded, err := Marshal([]string(nil)); err != nil || string(encoded) != `[]` {
		t.Fatalf("nil slice: %s %v", encoded, err)
	}
	if encoded, err := Marshal(json.RawMessage(nil)); err != nil || string(encoded) != `null` {
		t.Fatalf("nil raw: %s %v", encoded, err)
	}
}

// Declaration guards for the N8-4 record types: mistakes that would silently
// drop R5 diagnostics or tag semantics are refused before any data is read.
func TestReviewN83RecordDeclarationGuards(t *testing.T) {
	type twoRaws struct {
		Name string `json:"name"`
		raw  Raw
		also Raw
	}
	var two twoRaws
	if err := DecodeRecord([]byte(`{}`), &two, &two.raw); !errors.Is(err, ErrType) {
		t.Fatalf("multiple Raw members: %v", err)
	}
	type stringOption struct {
		Count Int `json:"count,string"`
		raw   Raw
	}
	var option stringOption
	if err := DecodeRecord([]byte(`{"count":"1"}`), &option, &option.raw); !errors.Is(err, ErrType) {
		t.Fatalf("ignored ,string option: %v", err)
	}
	type omit struct {
		Name string `json:"name,omitempty"`
		raw  Raw
	}
	var omitted omit
	if err := DecodeRecord([]byte(`{"name":"x"}`), &omitted, &omitted.raw); err != nil || omitted.Name != "x" {
		t.Fatalf("omitempty: %v", err)
	}
	type noRaw struct {
		Name string `json:"name"`
	}
	var external Raw
	var missing noRaw
	if err := DecodeRecord([]byte(`{"name":7}`), &missing, &external); !errors.Is(err, ErrType) {
		t.Fatalf("record without Raw member: %v", err)
	}
	var first, second reviewRecord
	if err := DecodeRecord([]byte(`{"name":7}`), &first, &second.raw); !errors.Is(err, ErrType) {
		t.Fatalf("borrowed provenance on decode: %v", err)
	}
	if _, err := EncodeRecord(&first, &second.raw); !errors.Is(err, ErrType) {
		t.Fatalf("borrowed provenance on encode: %v", err)
	}
}

// FuzzReviewRecordEdits applies deletions, additions, nil pointers, cleared
// lists and Map deletions chosen by ops, then checks R1/R2/R3 independently:
// valid unique JSON, untouched members keep bytes and relative order, the
// deleted member is gone, and decode→encode of the output is stable.
func FuzzReviewRecordEdits(f *testing.F) {
	for _, seed := range []struct {
		source string
		ops    byte
	}{
		{`{}`, 0xff},
		{"{ \"z\" : 1 ,\"name\":\"x\" , \"next\":{\"a\":1} }", 0x05},
		{`{"numbers":[1,2],"byname":{"k":{"name":"v"},"j":null},"added":true}`, 0x1a},
		{"{\"name\":\" \",\"raw\":[{\"a\":{}}],\"q\":null\n}", 0x21},
		{`{"next":7,"numbers":"x","byname":[]}`, 0x1c},
	} {
		f.Add([]byte(seed.source), seed.ops)
	}
	f.Fuzz(func(t *testing.T, data []byte, ops byte) {
		// Record nesting costs O(depth³) to decode (baselines per level, see
		// review-n8-3.md); stored schemas nest a few levels, so cap the input.
		if len(data) > 1024 {
			return
		}
		var record reviewRecord
		if DecodeRecord(data, &record, &record.raw) != nil {
			return
		}
		order, before := fuzzMembers(t, data)
		edited := map[string]bool{}
		deleted := ""
		if ops&0x01 != 0 && record.Fields.Len() > 0 {
			deleted = record.Fields.Keys()[0]
			record.Fields.Delete(deleted)
			edited[deleted] = true
		}
		if ops&0x02 != 0 && deleted != "added" {
			if err := record.Fields.Set("added", json.RawMessage(`{"x":[1, "<>&"]}`)); err != nil {
				t.Fatal(err)
			}
			edited["added"] = true
		}
		if ops&0x04 != 0 {
			record.Next = nil
			edited["next"] = true
		}
		if ops&0x08 != 0 {
			record.Numbers = nil
			edited["numbers"] = true
		}
		if ops&0x10 != 0 {
			if keys := record.Byname.Keys(); len(keys) > 0 {
				record.Byname.Delete(keys[0])
			}
			edited["byname"] = true
		}
		if ops&0x20 != 0 {
			record.Name = "e<>& "
			edited["name"] = true
		}
		encoded, err := Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		afterOrder, after := fuzzMembers(t, encoded)
		var kept, expected []string
		for _, name := range order {
			if !edited[name] {
				expected = append(expected, name)
				value, ok := after[name]
				if !ok || !bytes.Equal(value, before[name]) {
					t.Fatalf("untouched member %q changed", name)
				}
			}
		}
		for _, name := range afterOrder {
			if _, original := before[name]; original && !edited[name] {
				kept = append(kept, name)
			}
		}
		if !slices.Equal(kept, expected) {
			t.Fatal("untouched member order changed")
		}
		if deleted != "" {
			if _, present := after[deleted]; present {
				t.Fatal("deleted member written")
			}
		}
		var again reviewRecord
		if err := DecodeRecord(encoded, &again, &again.raw); err != nil {
			t.Fatal(err)
		}
		stable, err := Marshal(again)
		if err != nil || !bytes.Equal(stable, encoded) {
			t.Fatal("decode/encode of output not stable")
		}
	})
}
