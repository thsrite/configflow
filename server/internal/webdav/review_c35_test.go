package webdav

import (
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
	"sync/atomic"
	"testing"
	"time"
	"weak"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/repo"
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
			t.Cleanup(SetDefault(NewService(remote.Client())))
			for _, automatic := range []bool{false, true} {
				_, err := Run(context.Background(), Snapshot{Document: json.RawMessage(`{}`), Settings: settings, DataDir: r.DataDir(), repository: r}, automatic)
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
	previous := repo.SetHooks(repo.Hooks{})
	t.Cleanup(func() { repo.SetHooks(previous) })
	worker := NewWorker(time.Hour, func(context.Context, Snapshot) error { return nil })
	registerHook(worker)
	t.Cleanup(func() { _ = worker.Close(context.Background()) })
	update := func(enabled bool) {
		t.Helper()
		_, err := r.UpdateSystemTransaction(context.Background(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
			system.Set("backup", jsonx.Obj("auto_backup", enabled))
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	update(false)
	worker.mu.Lock()
	active := worker.active
	worker.mu.Unlock()
	if active {
		t.Error("disabled commit started a worker unlike Python")
	}
	update(true)
	worker.mu.Lock()
	deadline := worker.deadline
	worker.mu.Unlock()
	update(false)
	worker.mu.Lock()
	changed := !worker.deadline.Equal(deadline)
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
	worker := NewWorker(0, func(context.Context, Snapshot) error { return nil })
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
