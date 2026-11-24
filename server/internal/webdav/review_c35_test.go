package webdav

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"weak"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func TestCredentialVariantsNeverReachDiagnostics(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "webdav.log")
	root, err := logx.Setup(logx.Config{File: logFile, Level: logx.Debug})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	for _, remotePath := range []string{"/uri-private-secret/", "/uri-user/", "/%66ixture-secret/", "/uri+private+secret/"} {
		t.Run(remotePath, func(t *testing.T) {
			fake, remote := newDAV(t)
			fake.failMethod, fake.failStatus = "MKCOL", 403
			settings := fixtureSettings(remote)
			address, err := url.Parse(remote.URL)
			if err != nil {
				t.Fatal(err)
			}
			password := "uri-private-secret"
			if strings.Contains(remotePath, "+") {
				password = "uri private secret"
			}
			address.User = url.UserPassword("uri-user", password)
			settings.URL, settings.Path = address.String(), remotePath
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			svc := NewService(remote.Client())
			for _, automatic := range []bool{false, true} {
				_, err := svc.Run(context.Background(), Snapshot{Document: json.RawMessage(`{}`), Settings: settings, DataDir: r.DataDir(), repository: r}, automatic)
				if err == nil {
					t.Fatal("expected denied directory")
				}
				assertNoDAVSecrets(t, []byte(err.Error()))
			}
			err = filepath.WalkDir(r.DataDir(), func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() && filepath.Ext(path) == ".json" {
					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					assertNoDAVSecrets(t, data)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	assertNoDAVSecrets(t, data)
}
func assertNoDAVSecrets(t *testing.T, data []byte) {
	t.Helper()
	text := string(data)
	for i := 0; i < 4; i++ {
		for _, secret := range []string{"uri-private-secret", "uri private secret", "uri-user", "fixture-secret", "fixture-user"} {
			if strings.Contains(text, secret) {
				t.Errorf("WebDAV diagnostic exposed credential %q", secret)
			}
		}
		decoded, err := url.QueryUnescape(text)
		if err != nil || decoded == text {
			break
		}
		text = decoded
	}
}
func TestDisabledCommitNeverStartsOrReschedulesWorker(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	worker := testWorker(t, time.Hour, func(context.Context, Snapshot) error { return nil })
	r.OnConfigurationCommitted(commitObserver(worker))
	t.Cleanup(func() { _ = worker.Close(context.Background()) })
	update := func(enabled bool) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"auto_backup": enabled})
		err := r.UpdateSystem(context.Background(), func(_ context.Context, system *model.System) error {
			return system.SetJSON("backup", raw)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	update(false)
	worker.mu.Lock()
	active := worker.generation.active != nil
	worker.mu.Unlock()
	if active {
		t.Error("disabled commit started a worker unlike Python")
	}
	update(true)
	worker.mu.Lock()
	deadline := worker.generation.deadline
	worker.mu.Unlock()
	update(false)
	worker.mu.Lock()
	changed := !worker.generation.deadline.Equal(deadline)
	worker.mu.Unlock()
	if changed {
		t.Error("disable commit restarted pending debounce unlike Python")
	}
}

func TestWorkerReleasesCompletedSource(t *testing.T) {
	worker, reference := completedSource(t)
	deadline := time.Now().Add(3 * time.Second)
	for {
		runtime.GC()
		if reference.Value() == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle worker retained completed source/snapshot")
		}
		time.Sleep(10 * time.Millisecond)
	}
	runtime.KeepAlive(worker)
}
func completedSource(t *testing.T) (*Worker, weak.Pointer[memorySource]) {
	t.Helper()
	source := &memorySource{}
	source.set(strings.Repeat("x", 8<<20), true)
	reference := weak.Make(source)
	worker := testWorker(t, 0, func(context.Context, Snapshot) error { return nil })
	worker.Schedule(source)
	waitWorker(t, worker)
	return worker, reference
}

func TestDefaultTransportRejectsCrossOriginRedirect(t *testing.T) {
	var received atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1); w.WriteHeader(200) }))
	defer other.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/not-allowed", http.StatusTemporaryRedirect)
	}))
	defer first.Close()
	_, err := NewService(nil).Test(context.Background(), fixtureSettings(first))
	if err == nil || received.Load() != 0 {
		t.Fatal("default DAV transport followed cross-origin redirect")
	}
	assertNoDAVSecrets(t, []byte(err.Error()))
}

func TestTLSVerificationMatchesPythonDefault(t *testing.T) {
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer remote.Close()
	_, err := NewService(nil).Test(context.Background(), fixtureSettings(remote))
	var untrusted x509.UnknownAuthorityError
	if !errors.As(err, &untrusted) {
		t.Fatalf("default DAV client did not verify certificate: %v", err)
	}
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	script := `import sys
from webdav3.client import Client
try:
 Client({'webdav_hostname':sys.argv[1], 'webdav_login':'fixture-user', 'webdav_password':'fixture-secret', 'webdav_timeout':2}).check('/')
except Exception as error:
 seen=set()
 while error is not None and id(error) not in seen:
  seen.add(id(error))
  if 'CERTIFICATE_VERIFY_FAILED' in str(error):
   print('TLS_REJECTED')
   break
  error=error.__cause__ or error.__context__
 else:
  raise AssertionError('not a certificate verification failure')
else:
 raise AssertionError('untrusted WebDAV TLS accepted')
`
	out, err := exec.CommandContext(ctx, filepath.Join(home, "cf-test/.venv/bin/python"), "-c", script, remote.URL).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "TLS_REJECTED" {
		t.Fatalf("Python TLS verification probe failed: %v %.250s", err, out)
	}
}

func TestNonRuntimeAgentCommitsUploadAndRuntimeDoesNot(t *testing.T) {
	fake, remote := newDAV(t)
	svc := NewService(remote.Client())
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	worker := testWorker(t, 10*time.Millisecond, func(ctx context.Context, snapshot Snapshot) error { _, err := svc.Run(ctx, snapshot, true); return err })
	r.OnConfigurationCommitted(commitObserver(worker))
	t.Cleanup(func() { _ = worker.Close(context.Background()) })
	err = r.UpdateSystem(context.Background(), func(_ context.Context, system *model.System) error {
		settings := fixtureSettings(remote)
		settings.Auto = true
		raw, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		if err := system.SetJSON("backup", raw); err != nil {
			return err
		}
		system.Agents = []model.Agent{testjson.Decode[model.Agent](t, map[string]any{
			"id": "a", "name": "Agent", "host": "example.com", "profile_id": "default",
			"agent_service": "mihomo", "enabled": true, "token": "synthetic-agent-token",
		})}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	waitWorker(t, worker)
	uploads := func() int { fake.mu.Lock(); defer fake.mu.Unlock(); return fake.uploads }
	if uploads() != 1 {
		t.Fatal("agent creation was not backed up")
	}
	tests := []struct {
		field      string
		value      any
		wantUpload bool
	}{
		{"version", "synthetic-v2", false}, {"status", "online", false}, {"applied_content", map[string]any{"digest": "synthetic"}, false},
		{"name", "Renamed", true}, {"host", "other.example.com", true}, {"enabled", false, true},
		{"logging_config", map[string]any{"enabled": true}, true}, {"rule_sync", map[string]any{"settings": map[string]any{"library": true}}, true},
		{"dns_follow", map[string]any{"enabled": true}, true}, {"oxidns_ha", map[string]any{"enabled": true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			before := uploads()
			err := r.UpdateSystem(context.Background(), func(_ context.Context, system *model.System) error {
				raw, err := json.Marshal(system.Agents[0])
				if err != nil {
					return err
				}
				dec := json.NewDecoder(bytes.NewReader(raw))
				dec.UseNumber()
				var agentMap map[string]any
				if err := dec.Decode(&agentMap); err != nil {
					return err
				}
				agentMap[tt.field] = tt.value
				system.Agents[0] = testjson.Decode[model.Agent](t, agentMap)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			waitWorker(t, worker)
			want := before
			if tt.wantUpload {
				want++
			}
			if uploads() != want {
				t.Errorf("uploads=%d want=%d", uploads(), want)
			}
		})
	}
}

func TestStoredDotPathAutomaticFailureHasClearTaskMessage(t *testing.T) {
	fake, remote := newDAV(t)
	svc := NewService(remote.Client())
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	worker := testWorker(t, 0, func(ctx context.Context, snapshot Snapshot) error { _, err := svc.Run(ctx, snapshot, true); return err })
	r.OnConfigurationCommitted(commitObserver(worker))
	t.Cleanup(func() { _ = worker.Close(context.Background()) })
	// Seed a legacy/imported setting directly, bypassing the newly secured form.
	err = r.UpdateSystem(context.Background(), func(_ context.Context, system *model.System) error {
		settings := fixtureSettings(remote)
		settings.Auto = true
		settings.Path = "/%2E%2E/ordinary/"
		data, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		return system.SetJSON("backup", data)
	})
	if err != nil {
		t.Fatal(err)
	}
	waitWorker(t, worker)
	ctx := context.Background()
	snap, err := notifications.New(notifications.Options{Repo: r}).TaskStatus(ctx, r.DataDir()).Read()
	if err != nil {
		t.Fatal(err)
	}
	receipt := snap.Entry("webdav_backup")
	if receipt == nil || receipt.LastStatus == nil || *receipt.LastStatus != "failed" || receipt.LastError == nil || *receipt.LastError != ErrUnsafePath.Error() {
		t.Fatal("stored invalid path failed silently or used an unclear message")
	}
	fake.mu.Lock()
	count := len(fake.events)
	fake.mu.Unlock()
	if count != 0 {
		t.Fatal("stored unsafe path reached WebDAV")
	}
	saved, err := r.System(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	savedRaw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	parsedSaved, err := testjson.Parse(savedRaw)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := testjson.Path(parsedSaved, "backup", "webdav_path"); got != "/%2E%2E/ordinary/" {
		t.Fatal("failed backup rolled back/rewrote committed configuration")
	}
}

func TestDAVKeepsSessionCookieOnlyWithinOneOperation(t *testing.T) {
	var mu sync.Mutex
	var cookies []string
	dirs := map[string]bool{"/": true}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		cookies = append(cookies, r.Header.Get("Cookie"))
		if r.URL.Path == "/" && r.Method == "HEAD" {
			http.SetCookie(w, &http.Cookie{Name: "dav-session", Value: "synthetic-cookie", Path: "/", HttpOnly: true})
			return
		}
		cookie, err := r.Cookie("dav-session")
		if err != nil || cookie.Value != "synthetic-cookie" {
			w.WriteHeader(401)
			return
		}
		if r.Method == "HEAD" {
			if !dirs[r.URL.Path] {
				w.WriteHeader(404)
			}
			return
		}
		if r.Method == "MKCOL" {
			dirs[r.URL.Path] = true
			w.WriteHeader(201)
			return
		}
		w.WriteHeader(405)
	}))
	defer remote.Close()
	service := NewService(nil)
	settings := fixtureSettings(remote)
	for range 2 {
		mu.Lock()
		start := len(cookies)
		mu.Unlock()
		ok, err := service.Test(context.Background(), settings)
		if err != nil || !ok {
			t.Fatalf("WebDAV did not maintain its HTTP session: %v", err)
		}
		mu.Lock()
		first := cookies[start]
		mu.Unlock()
		if first != "" {
			t.Fatal("WebDAV cookie leaked into a separate backup operation")
		}
	}
}
