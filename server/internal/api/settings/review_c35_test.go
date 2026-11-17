package settings_test

import (
	"strings"
	"testing"

	"configflow/internal/api/settings"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

func TestBackupEditorRemovesURLUserinfoAndPreservesBlankPassword(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(settings.Register)})
	admin := srv.Login()
	initial := jsonx.Obj("webdav_url", "https://uri-user:uri-private-secret@example.com/dav/", "webdav_username", "editor-user", "webdav_password", "saved-private-secret")
	srv.Post("/api/backup/config", httpxtest.JSON(initial), admin).WantStatus(200)
	got := srv.Get("/api/backup/config", admin).WantStatus(200).Object()
	if got.GetString("webdav_url") != "https://example.com/dav/" || got.GetString("webdav_username") != "editor-user" || got.GetString("webdav_password") != "******" {
		t.Error("backup editor exposed URL userinfo or lost editable settings")
	}
	for _, password := range []string{"******", ""} {
		srv.Post("/api/backup/config", httpxtest.JSON(jsonx.Obj("webdav_password", password)), admin).WantStatus(200)
		saved, err := srv.Repo.GetSystem(srv.Context())
		if err != nil {
			t.Fatal(err)
		}
		if saved.GetObject("backup").GetString("webdav_password") != "saved-private-secret" {
			t.Error("blank/masked editor value replaced saved credential")
		}
	}
	// A real replacement is still accepted; protected responses keep masking it.
	srv.Post("/api/backup/config", httpxtest.JSON(jsonx.Obj("webdav_password", "replacement-private-secret")), admin).WantStatus(200)
	if strings.Contains(srv.Get("/api/backup/config", admin).Text(), "replacement-private-secret") {
		t.Error("replacement password exposed")
	}
}
