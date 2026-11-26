package webdav

import (
	"encoding/json"
	"testing"
)

func TestAuditStoredCredentialsExactMembers(t *testing.T) {
	var got Settings
	if err := json.Unmarshal([]byte(`{"webdav_username":"proven","WEBDAV_USERNAME":"spoofed","webdav_password":"synthetic","WEBDAV_PASSWORD":"replaced","AUTO_BACKUP":true}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Username != "proven" || got.Password != "synthetic" || got.Auto {
		t.Fatalf("stored settings consumed case variants: %#v", got)
	}
}
