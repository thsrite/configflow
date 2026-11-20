package deploy

import (
	"configflow/internal/jsonx"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDeploymentCollectsCredentialsBeforePrivateProjection(t *testing.T) {
	record := jsonx.Obj("deployment_id", "synthetic", "status", "failed", "message", "synthetic-proof-key-991", "error_details", "synthetic-proof-password-992", "name", "operator", "prepared_content", jsonx.Obj("api_key", "synthetic-proof-key-991"), "oxidns_validation_receipt", jsonx.Obj("password", "synthetic-proof-password-992"))
	before := record.DeepCopy()
	public := fixtureObject(publicDeployment(fixtureTask(record)))
	raw, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "synthetic-proof-key-991") || strings.Contains(string(raw), "synthetic-proof-password-992") {
		t.Fatal("private proof credential reference escaped")
	}
	if public.Has("prepared_content") || public.Has("oxidns_validation_receipt") || public.GetString("name") != "operator" || !reflect.DeepEqual(before, record) {
		t.Fatal("projection changed source or normal metadata")
	}
}
