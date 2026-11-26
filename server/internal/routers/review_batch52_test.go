package routers

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

// Device names are stored as the OxiDNS notes writer encodes them: no HTML
// escaping of &, < and > (batch-52 review).
func TestWorkspacePolicyNotesKeepLiteralHTMLCharacters(t *testing.T) {
	source := json.RawMessage(`{"oxidns":{"device_policies":[{"ip":"192.0.2.21","name":"Old","mode":"direct"}]}}`)
	rows := json.RawMessage(`[{"ip":"192.0.2.21","name":"A&B <x>","mode":"direct"}]`)
	next, err := saveWorkspacePolicies(source, rows, "")
	if err != nil {
		t.Fatal(err)
	}
	var doc model.Fields
	if err = json.Unmarshal(next, &doc); err != nil {
		t.Fatal(err)
	}
	raw, _ := doc.Get("device_notes")
	if got, want := string(raw), `{"192.0.2.21":"A&B <x>"}`; got != want {
		t.Fatalf("device_notes = %s, want %s", got, want)
	}
}
