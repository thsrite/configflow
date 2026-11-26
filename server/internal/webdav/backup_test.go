package webdav

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeDAV struct {
	mu         sync.Mutex
	dirs       map[string]bool
	files      map[string][]byte
	events     []string
	failMethod string
	failStatus int
	uploads    int
}

func newDAV(t *testing.T) (*fakeDAV, *httptest.Server) {
	t.Helper()
	f := &fakeDAV{dirs: map[string]bool{"/": true}, files: map[string][]byte{}}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	return f, server
}
func (f *fakeDAV) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	user, password, ok := r.BasicAuth()
	if !ok || user != "fixture-user" || password != "fixture-secret" {
		w.WriteHeader(401)
		return
	}
	remote := r.URL.Path
	f.events = append(f.events, r.Method+" "+remote)
	if f.failMethod == r.Method {
		w.WriteHeader(f.failStatus)
		fmt.Fprint(w, "fixture-secret private-remote-response")
		return
	}
	switch r.Method {
	case "HEAD":
		if !f.dirs[remote] && f.files[remote] == nil {
			w.WriteHeader(404)
		}
	case "MKCOL":
		f.dirs[remote] = true
		w.WriteHeader(201)
	case "PUT":
		data, _ := io.ReadAll(r.Body)
		f.files[remote] = data
		f.uploads++
		w.WriteHeader(201)
	case "PROPFIND":
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(207)
		fmt.Fprint(w, `<multistatus xmlns="DAV:"><response><href>`)
		_ = xml.EscapeText(w, []byte(remote))
		fmt.Fprint(w, `</href></response>`)
		for name := range f.files {
			if strings.HasPrefix(name, remote) {
				fmt.Fprint(w, `<response><href>`)
				_ = xml.EscapeText(w, []byte(name))
				fmt.Fprint(w, `</href></response>`)
			}
		}
		fmt.Fprint(w, `</multistatus>`)
	case "DELETE":
		delete(f.files, remote)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}
func fixtureSettings(server *httptest.Server) Settings {
	return Settings{URL: server.URL, Username: "fixture-user", Password: "fixture-secret", Path: "/one/two/", KeepLatest: 2, KeepDaily: 0}
}
func TestUploadCreatesDirectoriesThenPrunesAndDetachesCancellation(t *testing.T) {
	f, server := newDAV(t)
	settings := fixtureSettings(server)
	service := NewService(server.Client())
	service.Now = func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 123000, time.UTC) }
	for i := range 4 {
		f.files[fmt.Sprintf("/one/two/config_backup_20200101_10000%d_000000.json", i)] = []byte(`{}`)
	}
	f.files["/one/two/foreign.txt"] = []byte("keep")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service.Upload(ctx, json.RawMessage(`{"shared":{"nodes":[]},"profiles":{},"system":{}}`), settings, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 3 || result.Filename != "config_backup_20261022_120000_000123.json" {
		t.Fatalf("result %#v", result)
	}
	if f.files["/one/two/foreign.txt"] == nil || len(f.files) != 3 || !f.dirs["/one/"] || !f.dirs["/one/two/"] {
		t.Fatalf("unexpected remote state %#v", f.files)
	}
	var uploaded struct {
		Shared struct {
			Nodes []json.RawMessage `json:"nodes"`
		} `json:"shared"`
	}
	if json.Unmarshal(f.files["/one/two/"+result.Filename], &uploaded) != nil {
		t.Fatal("invalid backup")
	}
}
func TestFailureNeverPrunesOrLeaksCredentials(t *testing.T) {
	for _, method := range []string{"PUT", "MKCOL"} {
		t.Run(method, func(t *testing.T) {
			f, server := newDAV(t)
			f.failMethod = method
			f.failStatus = 403
			settings := fixtureSettings(server)
			_, err := NewService(server.Client()).Upload(context.Background(), json.RawMessage(`{}`), settings, false)
			if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
				t.Fatalf("error %v", err)
			}
			for _, secret := range []string{"fixture-user", "fixture-secret", "private-remote-response", server.URL} {
				if strings.Contains(err.Error(), secret) || strings.Contains(DescribeError(err), secret) {
					t.Fatal("error leaked remote details")
				}
			}
			for _, event := range f.events {
				if strings.HasPrefix(event, "DELETE ") {
					t.Fatal("pruned after failed upload")
				}
			}
		})
	}
}
func TestCleanupFailureKeepsSuccessfulBackup(t *testing.T) {
	f, server := newDAV(t)
	f.dirs["/one/two/"] = true
	f.failMethod = "PROPFIND"
	f.failStatus = 423
	result, err := NewService(server.Client()).Upload(context.Background(), json.RawMessage(`{}`), fixtureSettings(server), true)
	if err != nil || result.Removed != 0 || result.Filename == "" || f.uploads != 1 {
		t.Fatalf("result %#v err %v", result, err)
	}
}
func TestRetentionWindowsAndImportedValues(t *testing.T) {
	names := []string{"config_backup_20261022_100000_000000.json", "config_backup_20261022_110000_000000.json", "config_backup_20261022_120000_000000.json", "config_backup_20261021_090000_000000.json", "config_backup_20261021_230000_000000.json", "config_backup_20261019_120000.json", "foreign.txt"}
	tests := []struct {
		name          string
		latest, daily int
		want          []string
	}{
		{"disabled", 0, 30, []string{}},
		{"latest", 1, 0, []string{names[1], names[0], names[4], names[3], names[5]}},
		{"daily", 1, 3, []string{names[1], names[0], names[3], names[5]}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ExpiredBackups(names, test.latest, test.daily, time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(test.want)
			if string(a) != string(b) {
				t.Fatalf("got %s want %s", a, b)
			}
		})
	}
	for _, raw := range []string{`{}`, `{"keep_latest":"5","keep_daily":-3}`, `{"keep_latest":true,"keep_daily":1.5}`} {
		var settings Settings
		if err := json.Unmarshal([]byte(raw), &settings); err != nil {
			t.Fatal(err)
		}
		if settings.KeepLatest != 30 || settings.KeepDaily != 30 {
			t.Fatalf("defaults %#v", settings)
		}
	}
}
