package store

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
)

func FuzzRawDocument(f *testing.F) {
	f.Add([]byte(`{"Order":[],"Root":[]}`))
	f.Add([]byte(`{"Order":["profiles"],"Profiles":[{"ID":"p","Fields":[{"Name":"x","Value":{"a":1.0,"z":"例"}}]}]}`))
	f.Add(must(json.Marshal(rawFixture(f)))(f))
	f.Fuzz(func(t *testing.T, data []byte) {
		var doc Document
		if json.Unmarshal(data, &doc) != nil {
			return
		}
		rows, err := documentRows(&doc)
		if err != nil {
			return
		}
		rebuilt, err := buildDocument(rows.rootOrder, rows.sections, rows.fields)
		if err != nil {
			t.Fatal(err)
		}
		again, err := documentRows(rebuilt)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(rows.rootOrder, again.rootOrder) || !maps.Equal(rows.sections.values, again.sections.values) || !maps.Equal(rows.fields.values, again.fields.values) {
			t.Fatal("raw row round trip changed data")
		}
	})
}
