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
		"prepared_content":          map[string]any{"api_key": "synthetic-proof-key-991"},
		"oxidns_validation_receipt": map[string]any{"password": "synthetic-proof-password-992"},
	}
	before := cloneMap(t, record)
	public := publicDeployment(fixtureTask(record))
	raw, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "synthetic-proof-key-991") || strings.Contains(string(raw), "synthetic-proof-password-992") {
		t.Fatal("private proof credential reference escaped")
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
	if fields["name"] != "operator" || !reflect.DeepEqual(before, record) {
		t.Fatal("projection changed source or normal metadata")
	}
}
