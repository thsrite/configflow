package repo

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

// TestPlainRecordsDeclareEveryStoredMember guards O18 T1a's "unknown members
// are dropped": every top-level member that Python wrote (the Python
// reference document, the first-start template) and that the repository's
// own write paths produce (a fresh repository, a client-import profile) must
// be declared by the plain records, so a member the frontend or Python sends
// is never dropped silently. It is RejectUnknownMembers on the stored
// decoder: any IssueUnknown fails.
func TestPlainRecordsDeclareEveryStoredMember(t *testing.T) {
	check := func(t *testing.T, label string, data []byte, rec any) {
		t.Helper()
		issues, err := model.DecodeStored(data, rec, label)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		for _, issue := range issues {
			if issue.Kind == model.IssueUnknown {
				t.Errorf("%s: undeclared member %s", label, issue.Path)
			}
		}
	}
	document := func(t *testing.T, label string, raw json.RawMessage) {
		t.Helper()
		root := viewOfJSON(raw)
		for _, key := range root.fields.Keys() {
			if key != "schema_version" && key != "system" && key != "shared" && key != "profiles" {
				t.Errorf("%s: undeclared root member %s", label, key)
			}
		}
		if raw, ok := root.get("system"); ok {
			check(t, label+".system", raw, &model.System{})
		}
		if raw, ok := root.get("shared"); ok {
			check(t, label+".shared", raw, &model.Shared{})
		}
		if raw, ok := root.get("profiles"); ok {
			for _, profile := range viewOfJSON(raw).fields.All() {
				check(t, label+".profiles[]", profile, &model.Profile{})
			}
		}
	}
	document(t, "python reference", mustMarshalJSON(t, refBase(t)))
	r := newRepo(t)
	document(t, "fresh repository", must(r.ExportAllJSON(bg, false))(t))
	// The first-start template is a single legacy profile with shared lists.
	var template map[string]json.RawMessage
	noError(t, json.Unmarshal(configTemplate, &template))
	for key := range template {
		if !IsProfileField(key) && !slicesContainsString(SharedFields, key) && key != "system_config" && key != "backup" && key != "agents" {
			if _, declared := map[string]bool{"profile_id": true}[key]; !declared {
				t.Errorf("template member %s is neither a profile, shared nor system member", key)
			}
		}
	}
}

func slicesContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
