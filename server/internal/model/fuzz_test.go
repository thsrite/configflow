package model

import (
	"bytes"
	"encoding/json"
	"testing"
)

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
		before, err := parseObject(data)
		if err != nil {
			t.Fatal(err)
		}
		record.Name = "edited <>& 例"
		changed, err := Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(changed) {
			t.Fatal("edited output invalid JSON")
		}
		after, err := parseObject(changed)
		if err != nil {
			t.Fatal(err)
		}
		actual := map[string][]byte{}
		for _, member := range after.members {
			if _, found := actual[member.name]; found {
				t.Fatal("duplicate output key")
			}
			actual[member.name] = member.value
		}
		for _, member := range before.members {
			if member.name != "name" && !bytes.Equal(member.value, actual[member.name]) {
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
	})
}
