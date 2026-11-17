package model

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"testing"
)

// These checks deliberately use only encoding/json, not the codec's parser.
// The token stack detects duplicate names at every depth, including escaped
// spellings of the same key; RawMessage supplies an independent value boundary.
func fuzzUniqueJSON(t *testing.T, data []byte) {
	t.Helper()
	if !json.Valid(data) {
		t.Fatal("output invalid JSON")
	}
	type frame struct {
		object, key bool
		names       map[string]bool
	}
	var stack []frame
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal("output token decode failed")
		}
		if delimiter, ok := token.(json.Delim); ok && (delimiter == '}' || delimiter == ']') {
			stack = stack[:len(stack)-1]
			continue
		}
		if len(stack) > 0 {
			parent := &stack[len(stack)-1]
			if parent.object {
				if parent.key {
					name, ok := token.(string)
					if !ok || parent.names[name] {
						t.Fatal("duplicate output key")
					}
					parent.names[name] = true
					parent.key = false
					continue
				}
				parent.key = true
			}
		}
		if delimiter, ok := token.(json.Delim); ok {
			stack = append(stack, frame{object: delimiter == '{', key: true, names: map[string]bool{}})
		}
	}
}

func fuzzMembers(t *testing.T, data []byte) ([]string, map[string]json.RawMessage) {
	t.Helper()
	fuzzUniqueJSON(t, data)
	dec := json.NewDecoder(bytes.NewReader(data))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		t.Fatal("output is not an object")
	}
	var order []string
	members := map[string]json.RawMessage{}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		name := token.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			t.Fatal(err)
		}
		order = append(order, name)
		members[name] = value
	}
	return order, members
}

func FuzzRecordRoundTrip(f *testing.F) {
	for _, source := range []string{`{}`, ` { "name":"\u4f8b","count":6e1,"future": { "b":1.0,"a":null } } `, `{"count":true,"enabled":false,"children":[{"name":7,"future":[1,2]}]}`, "{\"future\":1 \n,\"name\":\"x\" \t}"} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var record sampleRecord
		if DecodeRecord(data, &record, &record.raw) != nil {
			return
		}
		encoded, err := Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(encoded) || !bytes.Equal(encoded, data) {
			t.Fatal("no-op changed original record bytes")
		}
		order, before := fuzzMembers(t, data)
		record.Name = "edited <>& 例"
		changed, err := Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		afterOrder, actual := fuzzMembers(t, changed)
		if _, present := before["name"]; !present {
			order = append(order, "name")
		}
		if !slices.Equal(afterOrder, order) {
			t.Fatal("member order changed")
		}
		for name, value := range before {
			if name != "name" && !bytes.Equal(value, actual[name]) {
				t.Fatal("untouched member bytes changed")
			}
		}
		var name string
		if json.Unmarshal(actual["name"], &name) != nil || name != record.Name {
			t.Fatal("typed edit lost")
		}
	})
}

func FuzzDocumentRoundTrip(f *testing.F) {
	for _, source := range []string{`{}`, `{"schema_version":5,"profiles":{"p":{"id":"p","_revision":60.0,"name":"\u4f8b","future": { "z":1.0 }}},"system":{"_revision":true},"shared":{}}`, `{"profiles":{"null":null,"wrong":false},"shared":false,"future":null}`, `{"profiles":{},"system":{},"shared":{}}`} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		document, issues, err := DecodeDocument(data)
		if err != nil {
			return
		}
		encoded, err := Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(encoded) || !bytes.Equal(encoded, data) {
			t.Fatal("document no-op changed bytes")
		}
		fuzzUniqueJSON(t, encoded)
		again, _, err := DecodeDocument(encoded)
		if err != nil {
			t.Fatal(err)
		}
		repeated, err := Marshal(again)
		if err != nil || !bytes.Equal(repeated, encoded) {
			t.Fatal("document codec is not stable")
		}
		for _, issue := range issues {
			if issue.Path == "" || issue.Kind != IssueType {
				t.Fatal("invalid issue metadata")
			}
		}
		if len(document.Profiles) > 0 {
			document.Profiles[0].Name = "edited <>& 例"
			changed, err := Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			rootOrder, rootBefore := fuzzMembers(t, encoded)
			changedOrder, rootAfter := fuzzMembers(t, changed)
			if !slices.Equal(rootOrder, changedOrder) {
				t.Fatal("document root order changed")
			}
			for name, value := range rootBefore {
				if name != "profiles" && !bytes.Equal(value, rootAfter[name]) {
					t.Fatal("unrelated root member changed")
				}
			}
			owners, profilesBefore := fuzzMembers(t, rootBefore["profiles"])
			newOwners, profilesAfter := fuzzMembers(t, rootAfter["profiles"])
			if !slices.Equal(owners, newOwners) {
				t.Fatal("profile owner order changed")
			}
			for _, owner := range owners[1:] {
				if !bytes.Equal(profilesBefore[owner], profilesAfter[owner]) {
					t.Fatal("unrelated profile changed")
				}
			}
			order, before := fuzzMembers(t, profilesBefore[owners[0]])
			afterOrder, after := fuzzMembers(t, profilesAfter[owners[0]])
			if _, present := before["name"]; !present {
				order = append(order, "name")
			}
			if !slices.Equal(order, afterOrder) {
				t.Fatal("profile member order changed")
			}
			for name, value := range before {
				if name != "name" && !bytes.Equal(value, after[name]) {
					t.Fatal("unrelated profile member changed")
				}
			}
			var name string
			if json.Unmarshal(after["name"], &name) != nil || name != document.Profiles[0].Name {
				t.Fatal("profile edit lost")
			}
		}
	})
}
