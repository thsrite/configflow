package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"configflow/internal/model"
)

// The migrated agents row differs from the stored one only in the private
// format identity of converted applied_content; nulls, empty text and
// undeclared members (which the typed codec drops) stay as stored.
func TestPatchedAgentsChangesOnlyFormatIdentity(t *testing.T) {
	const before = `[{"id":"a","name":"","config_revision":null,"has_update":false,
		"applied_content":{"profile_id":"p","fingerprint":"old","files":{"config.yaml":"x"},"deployed_config_sha256":"d0","extra":null}},
		{"id":"b","applied_content":{"fingerprint":"keep","files":{}},"future":{"k":1}}]`
	var records []model.Agent
	if err := json.Unmarshal([]byte(before), &records); err != nil {
		t.Fatal(err)
	}
	records[0].AppliedContent.Fingerprint = "new"
	files := model.OrderedMap[string]{}
	files.Set("config.yaml", "y")
	records[0].AppliedContent.Files = &files
	records[0].AppliedContent.DeployedConfigSHA256 = new("d1")
	records[1].AppliedContent.Fingerprint = "changed-but-not-migrated"
	got, err := patchedAgents([]byte(before), records, map[int]bool{0: true})
	if err != nil {
		t.Fatal(err)
	}
	var want, have any
	expected := `[{"id":"a","name":"","config_revision":null,"has_update":false,
		"applied_content":{"profile_id":"p","fingerprint":"new","files":{"config.yaml":"y"},"deployed_config_sha256":"d1","extra":null}},
		{"id":"b","applied_content":{"fingerprint":"keep","files":{}},"future":{"k":1}}]`
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &have); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, have) {
		t.Fatalf("patched agents\n got %s\nwant %s", got, expected)
	}
	records[0].ID = "other"
	if _, err := patchedAgents([]byte(before), records, map[int]bool{0: true}); err == nil {
		t.Fatal("a row/record mismatch was accepted")
	}
}
