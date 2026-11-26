package dnsha

import (
	"encoding/json"
	"testing"

	"configflow/internal/deploy"
)

func TestProtocolPayloadUsesExactOwnershipKeys(t *testing.T) {
	for _, data := range []string{
		`{"id":"a","ID":"b","base_url":"https://example.com"}`,
		`{"ID":"b","Base_URL":"https://example.com"}`,
	} {
		var p payload
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			t.Fatal(err)
		}
		if p.ID == "b" {
			t.Fatal("case alias manufactured payload ID")
		}
	}
	for _, data := range []string{`{"id":"a","id":"b"}`, `{"bundle":{"manifest":{"profile_id":"a","profile_id":"b"}}}`} {
		var p payload
		if err := json.Unmarshal([]byte(data), &p); err == nil {
			t.Fatalf("duplicate ownership accepted: %s", data)
		}
	}
	var manifest deploy.Manifest
	if err := json.Unmarshal([]byte(`{"PROFILE_ID":"forged","AGENT_ID":"forged","DEPLOYMENT_ID":"forged","Files":[]}`), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ProfileID != "" || manifest.AgentID != "" || manifest.DeploymentID != "" {
		t.Fatal("manifest case alias manufactured proof")
	}
	var file deploy.File
	if err := json.Unmarshal([]byte(`{"PATH":"x","SIZE":0,"SHA256":"x","ROLE":"rule"}`), &file); err == nil {
		t.Fatal("file aliases satisfied mandatory metadata")
	}
}
