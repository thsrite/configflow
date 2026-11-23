package refresh

import (
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/substore"
	"configflow/internal/taskstatus"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeSourceOwnerKeepsGenerationAtEveryBoundary(t *testing.T) {
	for _, boundary := range []string{"entry", "fetch", "metadata", "save"} {
		for _, stop := range []bool{false, true} {
			t.Run(boundary+map[bool]string{false: "/disconnect", true: "/shutdown"}[stop], func(t *testing.T) {
				f := setup(t, 86400)
				runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				runtime.Start(context.Background())
				t.Cleanup(func() { runtime.Shutdown(context.Background()) })
				request, cancel := context.WithCancel(runtime.BindRequest(f.ctx))
				defer cancel()
				transition := func() {
					if stop {
						runtime.Shutdown(context.Background())
					} else {
						cancel()
					}
				}
				env := refreshEnv()
				saves := 0
				env.Sources.Fetch = func(ctx context.Context, id, url string, _ substore.FetchOptions) (*SourceResult, error) {
					if err := ctx.Err(); err != nil {
						t.Fatal("fetch entered canceled generation", err)
					}
					if boundary == "fetch" {
						transition()
					}
					return &SourceResult{Text: "proxies: [{name: A, type: socks5, server: example.com, port: 1080}]", Source: "direct_yaml"}, nil
				}
				env.Sources.ParseNodes = func(ctx context.Context, _ string) ([]model.Node, error) { return []model.Node{{Name: "A"}}, ctx.Err() }
				env.Sources.Metadata = func(ctx context.Context, _ string) (json.RawMessage, json.RawMessage, error) {
					if boundary == "metadata" {
						transition()
					}
					return nil, nil, nil
				}
				env.Sources.SaveNodes = func(ctx context.Context, _ string, nodes []model.Node, meta *SourceMetadata) (*NodeCache, error) {
					if err := ctx.Err(); err != nil {
						t.Fatal("save entered canceled generation", err)
					}
					saves++
					if boundary == "save" {
						transition()
					}
					return &NodeCache{Nodes: nodes, Count: model.Int(len(nodes)), Metadata: meta}, nil
				}
				env.Sources.FilterNodes = func(_ model.Subscription, nodes []model.Node) []model.Node { return nodes }
				t.Cleanup(SetRefreshEnv(env))
				if boundary == "entry" {
					transition()
				}
				before, err := f.repo.Shared(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				beforeJSON, _ := model.Marshal(before)
				_, _, _, err = RefreshSource(request, model.Subscription{ID: "s", Name: "Feed", URL: subURL}, "manual")
				after, readErr := f.repo.Shared(f.ctx)
				if readErr != nil {
					t.Fatal(readErr)
				}
				afterJSON, _ := model.Marshal(after)
				if string(beforeJSON) != string(afterJSON) {
					t.Fatal("refresh mutated configuration/revision")
				}

				if stop {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("shutdown err=%v", err)
					}
					want := 0
					if boundary == "save" {
						want = 1
					}
					if saves != want {
						t.Fatalf("saves=%d want %d", saves, want)
					}
					for _, file := range []string{HealthFile, taskstatus.Filename} {
						path, _ := f.repo.SharedPath(file)
						if file == taskstatus.Filename {
							path = filepath.Join(f.repo.DataDir(), file)
						}
						if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("post-shutdown receipt %s: %v", file, err)
						}
					}
					if boundary == "entry" || boundary == "fetch" {
						path, _ := f.repo.SharedPath(DownloadCachePath(subURL))
						if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
							t.Fatal("post-shutdown transport receipt", err)
						}
					}
					_, _, _, err = RefreshSource(request, model.Subscription{ID: "s", URL: subURL}, "manual")
					if !errors.Is(err, context.Canceled) {
						t.Fatal("old generation silently restarted", err)
					}
				} else {
					if err != nil || saves != 1 {
						t.Fatalf("disconnect abandoned admitted write: %v saves=%d", err, saves)
					}
					health, err := ReadHealth(f.ctx)
					if err != nil || len(health.Entry("s").History) != 1 {
						t.Fatal("health receipt missing", err)
					}
					status, err := taskstatus.New(f.repo.DataDir()).Read()
					if err != nil || status.Entry("subscription:s").LastStatus == nil {
						t.Fatal("task receipt missing", err)
					}
				}
			})
		}
	}
}

func TestTypedCacheEvidenceNeverClaimsNumericURLOrTimestamp(t *testing.T) {
	env := refreshEnv()
	env.Clock = &fakeClock{now: corpusNow}
	t.Cleanup(SetRefreshEnv(env))
	var valid TransportReceipt
	if !receiptFresh([]byte(`{"text":"proxies: []","fetched_at":"2026-11-01T00:00:00Z"}`), &valid, 86400) {
		t.Fatal("valid receipt not fresh")
	}
	// The stored cache is read as the subscription owner reads it; its
	// decode issues mark the metadata damaged.
	for _, raw := range []string{`{"metadata":{"url":5},"updated_at":"2026-11-01T00:00:00Z"}`, `{"metadata":false}`, `{"metadata":{"url":true}}`} {
		var stored substore.Cache
		issues, err := model.DecodeStored([]byte(raw), &stored, "")
		if err != nil {
			t.Fatal(err)
		}
		stored.Issues = issues
		cache, err := sourceCache(&stored, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, malformed := cacheURL(cache); !malformed {
			t.Fatal("damaged metadata accepted ownership", raw)
		}
	}
	for _, raw := range []string{`{"metadata":null}`, `{"metadata":{"url":null}}`} {
		var stored substore.Cache
		if _, err := model.DecodeStored([]byte(raw), &stored, ""); err != nil {
			t.Fatal(err)
		}
		cache, err := sourceCache(&stored, nil)
		if err != nil {
			t.Fatal(err)
		}
		if address, malformed := cacheURL(cache); malformed || address != nil {
			t.Fatal("legacy cache without URL is not unknown", raw)
		}
	}
	if !fresh("2026-11-01", 86400) {
		t.Fatal("numeric timestamp negative control is not otherwise parseable")
	}
	for _, raw := range []string{`{"text":5,"fetched_at":"2026-11-01T00:00:00Z"}`, `{"text":"proxies: []","fetched_at":20261101}`} {
		var receipt TransportReceipt
		if receiptFresh([]byte(raw), &receipt, 86400) {
			t.Fatal("numeric receipt promoted", raw)
		}
	}
}
