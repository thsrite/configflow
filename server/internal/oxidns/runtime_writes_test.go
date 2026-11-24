package oxidns

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

type ownedWriteState struct {
	document string
	files    map[string]string
	commits  int64
	revision int64
}

func ownedState(t *testing.T, ctx context.Context, r *repo.Repository) ownedWriteState {
	t.Helper()
	raw, err := r.ExportAllJSON(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := r.ConfigRevision(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	if err := filepath.WalkDir(r.DataDir(), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() == ".config.lock" || strings.HasPrefix(entry.Name(), "configflow.sqlite3") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(r.DataDir(), path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return ownedWriteState{string(raw), files, r.CommitCount(), revision}
}
func TestC29RuntimeShutdownRejectsEveryOwner(t *testing.T) {
	for _, owner := range []string{"settings", "archive"} {
		t.Run(owner, func(t *testing.T) {
			ctx, r := settingsRepository(t)
			if err := os.WriteFile(filepath.Join(r.DataDir(), "unrelated.bin"), []byte{0, 1, 255}, 0600); err != nil {
				t.Fatal(err)
			}
			// Warm the real request cache, so a fast owner cannot rely on a
			// later repository read happening to notice shutdown.
			repo.SetRequest(ctx, repo.RequestInfo{Endpoint: "oxidns.oxidns_settings"})
			if _, err := repo.Config(ctx, "default"); err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Grace: 2 * time.Second})
			runtime.Start(ctx)
			bound := runtime.BindRequest(ctx)
			runtime.Shutdown(context.Background())
			if !errors.Is(lifecycle.Detach(bound).Err(), context.Canceled) {
				t.Fatal("request was not bound to the stopped real Runtime")
			}
			before := ownedState(t, ctx, r)
			var err error
			if owner == "settings" {
				_, err = SaveSettings(bound, r, "default", M("worker_threads", I(7)), "")
			} else {
				var result []byte
				result, err = BuildTypedZIP(bound, &model.Config{Profile: &model.Profile{}}, "plugins: []", nil, []File{{Path: "notes.txt", Content: []byte("must not be packed")}}, "")
				if result != nil {
					t.Fatal("archive escaped after shutdown")
				}
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("shutdown write: %v", err)
			}
			if after := ownedState(t, ctx, r); !reflect.DeepEqual(after, before) {
				t.Fatalf("shutdown changed complete DB/files/commit count/revision: before=%#v after=%#v", before, after)
			}
		})
	}
}
func TestC29RealRuntimeDisconnectCompletesEveryOwner(t *testing.T) {
	ctx, r := settingsRepository(t)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Grace: 2 * time.Second})
	runtime.Start(ctx)
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	request, cancel := context.WithCancel(runtime.BindRequest(ctx))
	cancel()
	if !errors.Is(request.Err(), context.Canceled) || lifecycle.Detach(request).Err() != nil {
		t.Fatal("fixture did not isolate request disconnect from Runtime shutdown")
	}
	saved, err := SaveSettings(request, r, "default", M("worker_threads", I(7)), "")
	if err != nil {
		t.Fatal(err)
	}
	config, err := r.Profile(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	node, err := config.OxiDNS.Node()
	if err != nil {
		t.Fatal(err)
	}
	if workers := get(node, "worker_threads"); workers == nil || workers.Value != "7" || saved.Guard.ConfigRevision != 1 {
		t.Fatal("disconnected request did not complete settings commit", saved.Guard)
	}
	output, err := BuildTypedZIP(request, &model.Config{Profile: &model.Profile{}}, "plugins: []", nil, []File{{Path: "notes.bin", Content: []byte{0, 1, 255}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(output), int64(len(output)))
	if err != nil {
		t.Fatal(err)
	}
	members := map[string][]byte{}
	for _, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		members[entry.Name] = data
	}
	if len(members) != 2 || string(members["config.yaml"]) != "plugins: []" || !bytes.Equal(members["notes.bin"], []byte{0, 1, 255}) {
		t.Fatalf("disconnected request left a partial archive: %#v", members)
	}
}
