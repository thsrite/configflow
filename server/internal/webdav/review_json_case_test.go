package webdav

import (
	"bytes"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

func TestReviewLegacyBackupCredentials(t *testing.T) {
	// Python settings.py writes these exact names. Missing retention/path fields
	// are normal for older documents; extensions stay in the stored model.
	raw := []byte(`{ "webdav_url":"https://backup.example.com", "webdav_username":"old-user", "webdav_password":"synthetic-旧密码", "extension":{"Password":"opaque"} }`)
	var stored model.Backup
	if err := jsondecode.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	encoded, err := model.Marshal(&stored)
	if err != nil || !bytes.Equal(encoded, raw) {
		t.Fatalf("stored backup changed: %s (%v)", encoded, err)
	}
	var settings Settings
	if err := jsondecode.Unmarshal(encoded, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Username != stored.WebDAVUsername || settings.Password != stored.WebDAVPassword || settings.Path != DefaultPath || settings.KeepLatest != DefaultKeepLatest || settings.KeepDaily != DefaultKeepDaily {
		t.Fatal("old credentials/defaults changed")
	}
}
