package store

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestFieldReadsOwnedOriginalValueFromOneSnapshot(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), Filename), seconds(2.5))
	original := json.RawMessage(" \n{\"x\":2.00,\"token\":7}\t ")
	document := &Document{Order: []string{"system"}, System: &Section{Fields: []Field{{Name: "agents", Value: original}}}}
	created, err := s.Create(document, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Reading(func(connection *Conn) error {
		head, err := connection.Head()
		if err != nil {
			return err
		}
		if head != created.Head {
			t.Fatal("field read moved snapshot")
		}
		raw, present, err := connection.Field("system", "", "agents")
		if err != nil || !present || !bytes.Equal(raw, original) {
			t.Fatal(string(raw), present, err)
		}
		raw[0] = 'x'
		again, present, err := connection.Field("system", "", "agents")
		if err != nil || !present || !bytes.Equal(again, original) {
			t.Fatal("caller changed row", string(again), err)
		}
		if raw, present, err := connection.Field("system", "", "missing"); err != nil || present || raw != nil {
			t.Fatal(string(raw), present, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
