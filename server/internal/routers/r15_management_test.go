package routers

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/redact"
)

func TestR15ManagementDiagnosticsHidePeerCredentials(t *testing.T) {
	config := json.RawMessage(`{"routers":{"items":[{"id":"peer","password":"synthetic-peer-password"}]}}`)
	r := &Router{Password: "synthetic-old-password"}
	for _, text := range []string{"reference synthetic-peer-password", "reference synthetic-old-password", "reference synthetic-peer%2Dpassword"} {
		if actual := managementLogText(config, nil, r, text); actual != redact.Redacted {
			t.Fatalf("credential reference exposed: %s", actual)
		}
	}
	if actual := managementLogText(config, nil, r, "visible-user"); !strings.Contains(actual, "visible-user") {
		t.Fatal("ordinary username masked")
	}
}

func TestR15WorkspacePolicyNamesRetainUnchangedFields(t *testing.T) {
	source := json.RawMessage(`{"oxidns":{"device_policies":[{"ip":"192.0.2.21","name":"Old","mode":"direct"}]},"mosdns": { "opaque" : 1.00 },"extension": [1e+02, "synthetic"]}`)
	rows := json.RawMessage(`[{"ip":"192.0.2.21","name":"Renamed","mode":"direct"}]`)
	next, err := saveWorkspacePolicies(source, rows, "")
	if err != nil {
		t.Fatal(err)
	}
	var before, after model.Fields
	if err = json.Unmarshal(source, &before); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(next, &after); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"oxidns", "mosdns", "extension"} {
		a, _ := before.Get(key)
		b, _ := after.Get(key)
		if !bytes.Equal(a, b) {
			t.Fatalf("name-only edit changed %s", key)
		}
	}
	raw, _ := after.Get("device_notes")
	var names map[string]string
	if err = json.Unmarshal(raw, &names); err != nil {
		t.Fatal(err)
	}
	if names["192.0.2.21"] != "Renamed" {
		t.Fatal("policy display name was not saved")
	}
	unchanged, err := saveWorkspacePolicies(next, rows, "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(next, unchanged) {
		t.Fatal("unchanged workspace wrote configuration")
	}
}
