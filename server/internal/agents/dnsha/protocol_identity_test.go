package dnsha

import (
	"encoding/json"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/model"
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

func TestOriginalHAIdentityFieldsDoNotUseR5Projection(t *testing.T) {
	for _, bad := range []string{`7`, `null`, `true`, `[]`, `{}`} {
		var journal model.HAJournal
		if err := json.Unmarshal([]byte(`{"id":"pair","children":{"a":`+bad+`,"b":"other"}}`), &journal); err != nil {
			t.Fatal(err)
		}
		if _, valid := journalChildID(&journal, "a"); valid || validJournalChildren(&journal) {
			t.Fatalf("nonstring child accepted: %s", bad)
		}
		if err := json.Unmarshal([]byte(`{"id":`+bad+`,"children":{"a":"one","b":"other"}}`), &journal); err != nil {
			t.Fatal(err)
		}
		if journalID(&journal) != "" || validJournalChildren(&journal) {
			t.Fatalf("nonstring journal ID accepted: %s", bad)
		}
		var a, b model.Agent
		if err := json.Unmarshal([]byte(`{"id":"a","profile_id":`+bad+`}`), &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(`{"id":"b","profile_id":"7"}`), &b); err != nil {
			t.Fatal(err)
		}
		if agents.HAProfilesMatchModel(&a, &b) {
			t.Fatalf("projection established a profile binding: %s", bad)
		}
	}
}

func TestHAPortProofUsesOriginalNumericValue(t *testing.T) {
	var baseline model.HAIdentity
	if err := json.Unmarshal([]byte(`{"host":"192.0.2.10","port":8082,"profile_id":"default","service_type":"oxidns"}`), &baseline); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		port string
		want bool
	}{{`8082.0`, true}, {`8082e0`, true}, {`"8082"`, false}, {`true`, false}, {`8082.5`, false}} {
		var other model.HAIdentity
		if err := json.Unmarshal([]byte(`{"host":"192.0.2.10","port":`+tt.port+`,"profile_id":"default","service_type":"oxidns"}`), &other); err != nil {
			t.Fatal(err)
		}
		if sameHAIdentity(&baseline, &other) != tt.want {
			t.Fatalf("port %s ownership mismatch", tt.port)
		}
	}
}
