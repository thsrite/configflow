package substore

import (
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestN89ReleaseProtocolRecord(t *testing.T) {
	var payload releasePayload
	err := json.Unmarshal([]byte(`{"tag_name":"V2.43.1","body":"notes","assets":[{"name":"other"},{"name":"sub-store.bundle.js","browser_download_url":"https://example.com/bundle.js","size":600000,"digest":"sha256:abc"}]}`), &payload)
	if err != nil {
		t.Fatal(err)
	}
	if issues := model.Issues(&payload); len(issues) != 0 {
		t.Fatalf("protocol issues %#v, payload %#v", issues, payload)
	}
	if payload.TagName != "V2.43.1" || len(payload.Assets) != 2 {
		t.Fatalf("lost payload %#v", payload)
	}
}

func TestN89UpdateCommitLifetime(t *testing.T) {
	for _, stage := range []string{"bundle-write", "bundle-replace", "version-write"} {
		for _, shutdown := range []bool{false, true} {
			name := stage + "/disconnect"
			if shutdown {
				name = stage + "/shutdown"
			}
			t.Run(name, func(t *testing.T) {
				h := newDepsHarness(t)
				h.writeRuntime("2.40.5")
				h.store.setVersion("2.40.5")
				h.commands.runVersion = "2.41.0"
				content := []byte(`console.log("new bundle")`)
				h.serve(content)
				release := h.releaseFor(content, "2.41.0")
				rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
				rt.Start(h.ctx)
				defer rt.Shutdown(context.Background())
				request, disconnect := context.WithCancel(rt.BindRequest(h.ctx))
				defer disconnect()
				entered, resume := make(chan struct{}), make(chan struct{})
				unblock := sync.OnceFunc(func() { close(resume) })
				defer unblock()
				h.d.beforeCommit = func(point string) {
					if point == stage {
						close(entered)
						<-resume
					}
				}
				held, unlock, ok := h.d.updateLock.TryLock(lifecycle.Detach(request))
				if !ok {
					t.Fatal("job lock")
				}
				if err := rt.Spawn(held, "substore-commit-test", func(ctx context.Context) { h.d.runUpdate(ctx, release, unlock) }); err != nil {
					unlock()
					t.Fatal(err)
				}
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("commit not reached")
				}
				if shutdown {
					rt.Shutdown(context.Background())
				} else {
					disconnect()
				}
				unblock()
				wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := rt.WaitIdle(wait); err != nil {
					t.Fatal(err)
				}
				bundle := readText(t, filepath.Join(h.runtime, SubStoreAsset))
				version := readText(t, filepath.Join(h.runtime, "VERSION"))
				state := h.d.UpdateSnapshot()
				if shutdown {
					wantBundle := "runtime"
					if stage == "version-write" {
						wantBundle = string(content)
					}
					if bundle != wantBundle || version != "2.40.5\n" || state.State != "failed" {
						t.Fatalf("post shutdown bundle=%q version=%q state=%#v", bundle, version, state)
					}
					if h.commands.restarts != 0 {
						t.Fatal("restart or compensation after shutdown")
					}
					if stage != "bundle-write" && readText(t, filepath.Join(h.runtime+".bak", SubStoreAsset)) != "runtime" {
						t.Fatal("recovery backup lost")
					}
				} else if bundle != string(content) || version != "2.41.0\n" || state.State != "success" || h.commands.restarts != 1 {
					t.Fatalf("disconnect result %q %q %#v", bundle, version, state)
				}
				if _, err := os.Stat(filepath.Join(h.runtime, "sub-store.download.js")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("temporary file retained: %v", err)
				}
				h.unlocked(t)
			})
		}
	}
}

func TestN89UpdateRejectedAfterShutdown(t *testing.T) {
	h := newDepsHarness(t)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(h.ctx)
	request := rt.BindRequest(h.ctx)
	rt.Shutdown(context.Background())
	if _, err := h.d.StartUpdate(request); !errors.Is(err, context.Canceled) {
		t.Fatalf("start after shutdown: %v", err)
	}
	if state := h.d.UpdateSnapshot(); state.State != "idle" {
		t.Fatalf("state mutated %#v", state)
	}
	if _, err := os.Stat(h.runtime); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created runtime: %v", err)
	}
	h.unlocked(t)
}
