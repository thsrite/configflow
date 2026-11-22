package deploy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDeploymentCollectsCredentialsBeforePrivateProjection(t *testing.T) {
	record := map[string]any{
		"deployment_id":             "synthetic",
		"status":                    "failed",
		"message":                   "synthetic-proof-key-991",
		"error_details":             "synthetic-proof-password-992",
		"name":                      "operator",
		"prepared_content":          map[string]any{"base_url": "http://operator:synthetic-proof-key-991@panel.example"},
		"oxidns_validation_receipt": map[string]any{"fingerprint": "http://operator:synthetic-proof-password-992@panel.example"},
	}
	before := cloneMap(t, record)
	public := publicDeployment(fixtureTask(record))
	raw, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	// The private proofs declare no credential members (O18 T1b drops
	// undeclared ones on load); their URL text never reaches the projection.
	if strings.Contains(string(raw), "panel.example") {
		t.Fatal("private proof escaped")
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, hasPrepared := fields["prepared_content"]; hasPrepared {
		t.Fatal("projection exposed prepared_content")
	}
	if _, hasReceipt := fields["oxidns_validation_receipt"]; hasReceipt {
		t.Fatal("projection exposed oxidns_validation_receipt")
	}
	if !reflect.DeepEqual(before, record) {
		t.Fatal("projection changed source or normal metadata")
	}
}
