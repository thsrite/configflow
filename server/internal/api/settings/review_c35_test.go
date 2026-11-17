package settings_test

import (
	"configflow/internal/webdav"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestDotPathsRejectedBySaveAndExecutionBeforeAnyNetwork(t *testing.T) {
	var requests atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(200) }))
	defer remote.Close()
	t.Cleanup(webdav.SetDefault(webdav.NewService(remote.Client())))
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(settings.Register)})
	admin := srv.Login()
	srv.Post("/api/backup/config", httpxtest.JSON(jsonx.Obj("webdav_url", remote.URL, "webdav_username", "fixture-user", "webdav_password", "fixture-secret", "webdav_path", "/ordinary/")), admin).WantStatus(200)
	badPaths := []string{"/./", "/a/../b/", "/%2e/", "/%2E%2E/", "/%252e%252e/", "/a%2f..%2fb/", `a\..\b`, "/%ZZ/%2e%2e/"}
	for _, bad := range badPaths {
		for _, field := range []string{"webdav_path", "webdav_url"} {
			t.Run(field+bad, func(t *testing.T) {
				before, err := srv.Repo.Store().Fingerprint()
				if err != nil {
					t.Fatal(err)
				}
				body := jsonx.Obj("webdav_url", remote.URL, "webdav_username", "fixture-user", "webdav_password", "******", "webdav_path", "/ordinary/")
				value := bad
				if field == "webdav_url" {
					value = remote.URL + bad
				}
				body.Set(field, value)
				for _, endpoint := range []string{"config", "test", "now"} {
					srv.Post("/api/backup/"+endpoint, httpxtest.JSON(body), admin).WantStatus(400).WantJSON(jsonx.Obj("success", false, "message", webdav.ErrUnsafePath.Error()))
				}
				after, err := srv.Repo.Store().Fingerprint()
				if err != nil || before != after {
					t.Fatal("unsafe path changed saved configuration")
				}
			})
		}
	}
	if requests.Load() != 0 {
		t.Fatal("unsafe path reached network")
	}
}
