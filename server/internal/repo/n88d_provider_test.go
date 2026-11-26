package repo_test

import (
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestN88dCommitRuntimeAndLeafLocks(t *testing.T) {
	for _, kind := range []string{"cache", "artifact"} {
		for _, shutdown := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/shutdown=%t", kind, shutdown), func(t *testing.T) {
				r, runtime, base := commitRuntime(t)
				request, disconnect := context.WithCancel(runtime.BindRequest(base))
				defer disconnect()
				relative := "providers/agg.yaml"
				var path string
				var err error
				if kind == "cache" {
					path, err = r.SharedPath("subscribes/s.json")
				} else {
					path, err = r.ProfilePath("default", relative)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				_, release, err := repo.AcquireFileForTest(base, path)
				if err != nil {
					t.Fatal(err)
				}
				held := true
				defer func() {
					if held {
						release()
					}
				}()
				bundle := []*provider.Artifact{{Content: "proxies: []", AggregationID: "agg"}}
				if kind == "cache" {
					bundle[0].AggregationID = ""
					bundle[0].CacheUpdates = []provider.CacheWrite{{SubID: "s", Nodes: []model.Node{{Name: "Node", Type: "http", Server: "example.com", Port: model.IntPtr(80)}}}}
				}
				done := make(chan error, 1)
				pv := provider.New(provider.Options{Repo: r})
				go func() { done <- pv.CommitArtifacts(request, "default", bundle) }()
				select {
				case err := <-done:
					t.Fatalf("escaped real leaf lock: %v", err)
				case <-time.After(40 * time.Millisecond):
				}
				disconnect()
				if shutdown {
					runtime.Shutdown(context.Background())
				}
				release()
				held = false
				select {
				case err := <-done:
					data, readErr := os.ReadFile(path)
					if shutdown {
						if !errors.Is(err, context.Canceled) || !os.IsNotExist(readErr) {
							t.Fatalf("shutdown landed write: %v %v %q", err, readErr, data)
						}
					} else if err != nil || readErr != nil || len(data) == 0 {
						t.Fatalf("disconnected write incomplete: %v %v", err, readErr)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("write waiter stranded")
				}
			})
		}
	}
}
