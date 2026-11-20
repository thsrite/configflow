package mosdns

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/store"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
)

func archiveRuntime(t *testing.T, world map[string]any) (context.Context, *repo.Repository, *lifecycle.Runtime) {
	t.Helper()
	base, r := testRepo(t, world)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(base)
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	return runtime.BindRequest(base), r, runtime
}

func archiveDurableState(t *testing.T, r *repo.Repository) (store.Fingerprint, map[string]string) {
	t.Helper()
	fingerprint, err := r.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	err = filepath.WalkDir(r.DataDir(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(r.DataDir(), path)
		if err != nil || strings.HasPrefix(rel, repo.DatabaseFilename) {
			return err
		}
		if entry.IsDir() {
			files[rel] = "@directory"
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil {
			files[rel] = string(data)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint, files
}

func TestO36cArchiveClosedRuntime(t *testing.T) {
	ctx, r, runtime := archiveRuntime(t, nil)
	before, files := archiveDurableState(t, r)
	runtime.Shutdown(context.Background())
	data, err := BuildZIP(ctx, remoteSnapshot(), "plugins: []", nil, nil, ArchiveOptions{})
	after, afterFiles := archiveDurableState(t, r)
	if !errors.Is(err, context.Canceled) || len(data) != 0 || before != after || !reflect.DeepEqual(files, afterFiles) {
		t.Fatalf("closed Runtime delivered an archive: err=%v bytes=%d", err, len(data))
	}
}

func TestO36cArchiveShutdownAfterFetch(t *testing.T) {
	ctx, r, runtime := archiveRuntime(t, nil)
	before, files := archiveDurableState(t, r)
	withFetch(t, func(context.Context, string, *model.Config, string) ([]byte, error) {
		runtime.Shutdown(context.Background())
		return []byte("example.com\n"), nil
	})
	downloads := []Download{{URL: "https://rules.example.com/late.txt", Name: "late", LocalPath: "rules/late.txt"}}
	data, err := BuildZIP(ctx, remoteSnapshot(), "plugins: []", downloads, nil, ArchiveOptions{})
	after, afterFiles := archiveDurableState(t, r)
	if !errors.Is(err, context.Canceled) || len(data) != 0 || before != after || !reflect.DeepEqual(files, afterFiles) {
		t.Fatalf("late source result delivered after shutdown: err=%v bytes=%d", err, len(data))
	}
}

func TestO36cArchiveRuntimeIOBoundary(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			arrived, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var arrivedOnce, cancelledOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				arrivedOnce.Do(func() { close(arrived) })
				select {
				case <-request.Context().Done():
					cancelledOnce.Do(func() { close(cancelled) })
					return
				case <-release:
					w.Header().Set("Content-Type", "text/plain")
					_, _ = w.Write([]byte("example.com\n"))
				}
			}))
			t.Cleanup(server.Close)
			rule := map[string]any{"id": "o3-archive", "name": "O3 archive", "source_type": "url", "url": server.URL,
				"behavior": "domain", "format": "text", "enabled": true}
			request, r, runtime := archiveRuntime(t, map[string]any{"rule_library": []any{rule}})
			t.Cleanup(unblock)
			ctx, disconnect := context.WithCancel(request)
			defer disconnect()
			fetchDone := make(chan struct{})
			withFetch(t, func(ctx context.Context, url string, config *model.Config, host string) ([]byte, error) {
				defer close(fetchDone)
				return fetchNative(ctx, url, config, host)
			})
			before, files := archiveDurableState(t, r)
			rawSnap, err := json.Marshal(map[string]any{"profile_id": "default", "system_config": map[string]any{"server_domain": "https://panel.example.com"},
				"rule_configs": []any{}, "rule_library": []any{rule}})
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := ConfigFromJSON(rawSnap)
			if err != nil {
				t.Fatal(err)
			}
			type outcome struct {
				data []byte
				err  error
			}
			done := make(chan outcome, 1)
			go func() {
				data, err := BuildZIP(ctx, snapshot, "plugins: []", []Download{{URL: server.URL, Name: "O3", LocalPath: "rules/o3.txt"}}, nil, ArchiveOptions{})
				done <- outcome{data, err}
			}()
			select {
			case <-arrived:
			case <-time.After(10 * time.Second):
				t.Fatal("archive did not start its real HTTP download")
			}
			if shutdown {
				runtime.Shutdown(context.Background())
				select {
				case <-cancelled:
				case <-time.After(5 * time.Second):
					t.Fatal("Runtime cancellation did not reach HTTP I/O before the production read timeout")
				}
			} else {
				disconnect()
				select {
				case <-cancelled:
					t.Fatal("client disconnect cancelled the archive download")
				case <-time.After(50 * time.Millisecond):
				}
				unblock()
			}
			select {
			case <-fetchDone:
			case <-time.After(10 * time.Second):
				t.Fatal("HTTP fetch did not finish")
			}
			var result outcome
			select {
			case result = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("archive did not finish")
			}
			after, afterFiles := archiveDurableState(t, r)
			if shutdown {
				if !errors.Is(result.err, context.Canceled) || len(result.data) != 0 || before != after || !reflect.DeepEqual(files, afterFiles) {
					t.Fatalf("archive shutdown changed durable state or delivered bytes: %v", result.err)
				}
			} else {
				if result.err != nil || before != after {
					t.Fatal("disconnected archive did not complete", result.err)
				}
				found := false
				for _, entry := range readZip(t, result.data) {
					if entry[0] == "rules/o3.txt" && entry[1] == "example.com\n" {
						found = true
					}
				}
				if !found {
					t.Fatal("archive does not contain the downloaded bytes")
				}
				path, err := r.SharedPath(filepath.Join("rules", rules.CacheFilename(testjson.Decode[model.RuleConfig](t, rule))))
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "example.com\n" {
					t.Fatal("archive did not publish its cache", err)
				}
				entry, err := taskstatus.New(r.DataDir()).Get(refresh.RuleTaskKey(rule["id"].(string)))
				if err != nil || entry.GetString("last_status") != "success" {
					t.Fatal("archive did not record success", err)
				}
			}
		})
	}
}
