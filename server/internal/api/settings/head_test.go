package settings_test

import (
 "context"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"configflow/internal/api/settings"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/weblistener"
)

type listenerServer struct {
	mu     sync.Mutex
	token  string
	server http.Server
}

func (s *listenerServer) Serve(l net.Listener) error { return s.server.Serve(l) }
func (s *listenerServer) SetListenerToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = token
}
func (s *listenerServer) ListenerToken() string { s.mu.Lock(); defer s.mu.Unlock(); return s.token }

func activeListener(t *testing.T, dir string) {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	s := &listenerServer{}
	s.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(s.ListenerToken() + "\n")) })
	if err := os.WriteFile(filepath.Join(dir, "web-listener.json"), []byte(`{"port":`+strconv.Itoa(port)+`}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := &weblistener.Manager{DataDir: dir, Server: s, LookupEnv: func(string) (string, bool) { return "", false }}
	if _, err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	settings.SetWebListener(m)
	t.Cleanup(func() { settings.SetWebListener(nil); m.Close(); _ = s.server.Close() })
}

func storedFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil {
			relative, _ := filepath.Rel(dir, path)
			files[relative] = string(data)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestSettingsHEADIsReadOnly(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"/api/settings/rule-fetch-proxy", `{"rule_fetch_proxy":"http://proxy.example.com:8080"}`},
		{"/api/settings/web-port", `{"port":18082}`},
		{"/api/server-domain", `{"new_domain":"https://changed.example.com"}`},
		{"/api/config-token", `{"token":"changed-token","generate":true}`},
		{"/api/backup/config", `{"webdav_url":"https://backup.example.com","keep_latest":9}`},
		{"/api/settings/subscription-aggregation", `{"enabled":true}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			s := newServer(t, true)
			if tc.path == "/api/settings/web-port" {
				activeListener(t, s.DataDir)
			}
			admin := s.Login()
			get := s.Get(tc.path, admin).WantStatus(200)
			before, err := s.Repo.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			files := storedFiles(t, s.DataDir)
			for _, body := range []string{tc.body, "{", "[]"} {
				head := s.Do("HEAD", tc.path, admin, httpxtest.Body([]byte(body), "application/json")).WantStatus(get.Status)
				if len(head.Body) != 0 {
					t.Fatal("HEAD returned a body")
				}
				if !reflect.DeepEqual(head.Header, get.Header) {
					t.Fatalf("HEAD headers=%v GET=%v", head.Header, get.Header)
				}
				after, err := s.Repo.Store().Fingerprint()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatal("HEAD changed DB rows or revision")
				}
				if !reflect.DeepEqual(files, storedFiles(t, s.DataDir)) {
					t.Fatal("HEAD changed files")
				}
			}
		})
	}
}
