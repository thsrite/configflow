package model

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"slices"
	"testing"

	"configflow/internal/yamlx"
)

// reviewRecord exercises the independent N8-3 review cases on a plain struct
// without codecs: nested records, ordered maps of records, exact numbers in
// lists, Dynamic and raw members.
type reviewRecord struct {
	Name    string                    `json:"name,omitzero"`
	Numbers []json.Number             `json:"numbers,omitzero"`
	Next    *reviewRecord             `json:"next,omitzero"`
	Byname  OrderedMap[*reviewRecord] `json:"byname,omitzero"`
	Dyn     Dynamic                   `json:"dyn,omitzero"`
	Raw     json.RawMessage           `json:"raw,omitzero"`
	Ints    *[]Int                    `json:"ints,omitzero"`
	List    []string                  `json:"list"`
}

func reviewDecode(t *testing.T, source string) *reviewRecord {
	t.Helper()
	var record reviewRecord
	if _, err := DecodeStored([]byte(source), &record, ""); err != nil {
		t.Fatal(err)
	}
	return &record
}

// Python ProfileRepository.clone_profile copies a profile. The obvious typed
// form copies the record (unknown members included) and assigns a new ID; the
// copy must become its own section rather than inheriting the source's key.
func TestReviewN83CopiedProfileUsesItsNewID(t *testing.T) {
	source := `{"schema_version":5,"system":{},"shared":{},"profiles":{"p":{"id":"p","name":"A"},"q":{"id":"other","name":"B"}}}`
	document, err := DecodeDocument([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if document.Profiles[1].ID != "other" || document.Profiles[1].Key() != "q" {
		t.Fatal("section key lost")
	}
	clone := *document.Profiles[0]
	clone.ID = "copy"
	clone.Name = "A copy"
	document.Profiles = append(document.Profiles, &clone)
	owners := func() []string {
		t.Helper()
		encoded, err := Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		var root struct {
			Profiles jsontext.Value `json:"profiles"`
		}
		if err := jsonv2.Unmarshal(encoded, &root); err != nil {
			t.Fatal(err)
		}
		dec := jsontext.NewDecoder(bytes.NewReader(root.Profiles))
		var keys []string
		if _, err := dec.ReadToken(); err != nil {
			t.Fatal(err)
		}
		for dec.PeekKind() != '}' {
			key, err := dec.ReadToken()
			if err != nil {
				t.Fatal(err)
			}
			keys = append(keys, key.String())
			if err := dec.SkipValue(); err != nil {
				t.Fatal(err)
			}
		}
		return keys
	}
	if got := owners(); !slices.Equal(got, []string{"p", "q", "copy"}) {
		t.Fatalf("owners %v", got)
	}
	// An untouched mismatching id keeps its stored section key.
	document.Profiles[1].Name = "B2"
	if got := owners(); !slices.Equal(got, []string{"p", "q", "copy"}) {
		t.Fatalf("owner of mismatching id lost: %v", got)
	}
	// Two records claiming the same section are rejected, never merged.
	twin := *document.Profiles[0]
	document.Profiles = append(document.Profiles, &twin)
	if _, err := Marshal(document); !errors.Is(err, ErrJSON) {
		t.Fatalf("duplicate section accepted: %v", err)
	}
}

// Dynamic: Node/SetNode of an unchanged tree keeps the stored JSON; a real
// change is written in yamlx.ToJSON form.
func TestReviewN83DynamicUnchangedTreeKeepsBytes(t *testing.T) {
	var dynamic Dynamic
	source := `{ "b" : [1.50, "x", {"k": null}], "a": true }`
	if err := dynamic.UnmarshalJSON([]byte(source)); err != nil {
		t.Fatal(err)
	}
	node, err := dynamic.Node()
	if err != nil {
		t.Fatal(err)
	}
	if err := dynamic.SetNode(node); err != nil {
		t.Fatal(err)
	}
	if encoded, err := Marshal(dynamic); err != nil || string(encoded) != source {
		t.Fatalf("unchanged Dynamic rewritten: %v %s", err, encoded)
	}
	yamlx.Set(node, "a", yamlx.Bool(false))
	if err := dynamic.SetNode(node); err != nil {
		t.Fatal(err)
	}
	if encoded, err := Marshal(dynamic); err != nil || string(encoded) != `{"b":[1.50,"x",{"k":null}],"a":false}` {
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
		t.Fatalf("cycle through OrderedMap not detected: %v", err)
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

// Typed code clears lists the Go way (nil, or `var kept []T` that ends up
// empty). An always-written list is then [] (as Python wrote it), an
// optional one is left out; an ordered map emptied by Delete is left out
// when optional.
func TestReviewN83EmptyContainersStayContainers(t *testing.T) {
	tests := []struct {
		name, source, want string
		edit               func(*reviewRecord)
	}{
		{"cleared list", `{"numbers":[1],"list":["a"]}`, `{"list":[]}`, func(r *reviewRecord) {
			var kept []json.Number
			r.Numbers = kept
			r.List = nil
		}},
		{"stored null list", `{"list":null}`, `{"list":[]}`, func(r *reviewRecord) {}},
		{"empty optional list kept", `{"numbers":[]}`, `{"numbers":[],"list":[]}`, func(r *reviewRecord) {}},
		{"map emptied by delete", `{"byname":{"a":{}}}`, `{"list":[]}`, func(r *reviewRecord) { r.Byname.Delete("a") }},
		{"map set", `{"byname":null}`, `{"byname":{"k":{"name":"v","list":[]}},"list":[]}`, func(r *reviewRecord) { r.Byname.Set("k", &reviewRecord{Name: "v"}) }},
		{"pointer list removed", `{"ints":[1],"name":"x"}`, `{"name":"x","list":[]}`, func(r *reviewRecord) { r.Ints = nil }},
		{"pointer to nil list", `{"name":"x"}`, `{"name":"x","ints":[],"list":[]}`, func(r *reviewRecord) { r.Ints = new([]Int) }},
		{"raw null kept", `{"raw":null}`, `{"raw":null,"list":[]}`, func(r *reviewRecord) {}},
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
	if encoded, err := Marshal([]string(nil)); err != nil || string(encoded) != `[]` {
		t.Fatalf("nil slice: %s %v", encoded, err)
	}
	if encoded, err := Marshal(json.RawMessage(nil)); err != nil || string(encoded) != `null` {
		t.Fatalf("nil raw: %s %v", encoded, err)
	}
}
