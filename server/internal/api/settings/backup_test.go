package settings_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/api/settings"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/webdav"
)

func TestBackupEndpointsUseMaskedCredentialAndHideRemoteErrors(t *testing.T) {
	var mu sync.Mutex
	dirs := map[string]bool{"/": true}
	var uploaded []byte
	denied := false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		user, password, ok := r.BasicAuth()
		if !ok || user != "synthetic-user" || password != "synthetic-password" {
			w.WriteHeader(401)
			return
		}
		switch r.Method {
		case "HEAD":
			if !dirs[r.URL.Path] {
				w.WriteHeader(404)
			}
		case "MKCOL":
			dirs[r.URL.Path] = true
			w.WriteHeader(201)
		case "PUT":
			if denied {
				w.WriteHeader(403)
				io.WriteString(w, "synthetic-user synthetic-password private-response-body")
				return
			}
			uploaded, _ = io.ReadAll(r.Body)
			w.WriteHeader(201)
		case "PROPFIND":
			w.WriteHeader(207)
			io.WriteString(w, `<multistatus xmlns="DAV:"></multistatus>`)
		default:
			w.WriteHeader(405)
		}
	}))
	t.Cleanup(remote.Close)
	service := webdav.NewService(remote.Client())
	service.Now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 123000, time.UTC) }
	t.Cleanup(webdav.SetDefault(service))
	logFile := filepath.Join(t.TempDir(), "webdav.log")
	root, err := logx.Setup(logx.Config{File: logFile, Level: logx.Debug})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(settings.Register)})
	admin := srv.Login()
	ctx := srv.Context()
	_, err = srv.Repo.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("backup", jsonx.Obj("webdav_url", remote.URL, "webdav_username", "synthetic-user", "webdav_password", "synthetic-password", "auto_backup", false))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	body := jsonx.Obj("webdav_url", remote.URL, "webdav_username", "synthetic-user", "webdav_password", "******", "webdav_path", "one/two")
	srv.Post("/api/backup/test", httpxtest.JSON(body)).WantStatus(401)
	srv.Post("/api/backup/test", httpxtest.JSON(body), admin).WantStatus(200)
	srv.Post("/api/backup/now", httpxtest.JSON(body), admin).WantStatus(200)
	var doc any
	if json.Unmarshal(uploaded, &doc) != nil {
		t.Fatal("backup is not JSON")
	}
	expected, err := srv.Repo.ExportAll(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := jsonx.Compact(expected)
	var wanted any
	_ = json.Unmarshal(want, &wanted)
	if !reflect.DeepEqual(doc, wanted) {
		t.Fatal("manual backup is not full export")
	}
	mu.Lock()
	denied = true
	mu.Unlock()
	reply := srv.Post("/api/backup/now", httpxtest.JSON(body), admin).WantStatus(500)
	if !strings.Contains(string(reply.Body), "HTTP 403") {
		t.Fatalf("missing status: %s", reply.Body)
	}
	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"synthetic-user", "synthetic-password", "private-response-body", remote.URL} {
		if strings.Contains(string(reply.Body), secret) || strings.Contains(string(log), secret) {
			t.Fatalf("credential or remote detail leaked: %s", secret)
		}
	}
	for _, path := range []string{"/api/backup/test", "/api/backup/now"} {
		for _, invalid := range []string{`null`, `[]`, `{}`, `{"webdav_url":null}`, `{"auto_backup":"false"}`, `{"unknown":true}`, `{"keep_latest":true}`, `{"keep_daily":3651}`} {
			parsed, _ := jsonx.Parse([]byte(invalid))
			srv.Post(path, httpxtest.JSON(parsed), admin).WantStatus(400)
		}
	}
}
