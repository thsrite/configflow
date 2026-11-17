package webdav

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	for _, remotePath := range []string{"/uri-private-secret/", "/uri-user/", "/%66ixture-secret/"} {
		t.Run(remotePath, func(t *testing.T) {
			fake, remote := newDAV(t)
			fake.failMethod, fake.failStatus = "MKCOL", 403
			settings := fixtureSettings(remote)
			address, err := url.Parse(remote.URL)
			if err != nil {
				t.Fatal(err)
			}
			address.User = url.UserPassword("uri-user", "uri-private-secret")
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
		for _, secret := range []string{"uri-private-secret", "uri-user", "fixture-secret", "fixture-user"} {
			if strings.Contains(text, secret) {
				t.Errorf("WebDAV diagnostic exposed credential %q", secret)
			}
		}
		decoded, err := url.PathUnescape(text)
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
