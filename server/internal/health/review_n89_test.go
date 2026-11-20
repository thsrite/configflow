package health

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestReviewN89DeleteHooksRespectOwnerLifetime(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remove func(context.Context, string) error
	}{{"probes", deleteProbeStore}, {"regions", deleteRegionStore}} {
		for _, shutdown := range []bool{true, false} {
			lifetime := "disconnect"
			if shutdown {
				lifetime = "shutdown"
			}
			t.Run(tc.name+"/"+lifetime, func(t *testing.T) {
				r, err := repo.Open(t.TempDir(), repo.Options{})
				if err != nil {
					t.Fatal(err)
				}
				runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				runtime.Start(context.Background())
				t.Cleanup(func() { runtime.Shutdown(context.Background()) })
				ctx, disconnect := context.WithCancel(repo.WithRepository(runtime.BindRequest(context.Background()), r))
				defer disconnect()
				path := filepath.Join(r.DataDir(), tc.name, "synthetic.json")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				const original = `{"keep":"synthetic"}`
				if err := os.WriteFile(path, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				if shutdown {
					runtime.Shutdown(context.Background())
				} else {
					disconnect()
				}
				err = tc.remove(ctx, "synthetic")
				raw, readErr := os.ReadFile(path)
				if shutdown {
					if !errors.Is(err, context.Canceled) {
						t.Errorf("stopped hook returned %v", err)
					}
					if readErr != nil || string(raw) != original {
						t.Errorf("stopped hook changed file: %s %v", raw, readErr)
					}
				} else if err != nil || !errors.Is(readErr, os.ErrNotExist) {
					t.Fatalf("disconnect prevented cleanup: %v %v", err, readErr)
				}
			})
		}
	}
}

func TestReviewN89RealWorkerLifetimeControlsPersistence(t *testing.T) {
	for _, operation := range []string{"latency", "dns"} {
		for _, shutdown := range []bool{true, false} {
			name := "disconnect"
			if shutdown {
				name = "shutdown"
			}
			t.Run(operation+"/"+name, func(t *testing.T) {
				r, err := repo.Open(t.TempDir(), repo.Options{})
				if err != nil {
					t.Fatal(err)
				}
				base := repo.WithRepository(context.Background(), r)
				if err := r.UpdateShared(base, func(_ context.Context, shared *model.Shared) error {
					shared.Nodes = []model.Node{{ID: "synthetic", Name: "Synthetic", Type: "http", Server: "example.test", Port: 80}}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				ready := filepath.Join(t.TempDir(), "ready")
				release := ready + ".release"
				script := ready + ".sh"
				code := "#!/bin/sh\ncat >/dev/null\nprintf ready > \"$REVIEW_READY\"\nwhile [ ! -f \"$REVIEW_RELEASE\" ]; do sleep 0.02; done\nprintf '%s' '{\"version\":4,\"results\":[{\"name\":\"[\\\"node\\\",\\\"synthetic\\\"]\",\"latency\":42}]}'\n"
				if operation == "dns" {
					code = "#!/bin/sh\ncat >/dev/null\nprintf ready > \"$REVIEW_READY\"\nwhile [ ! -f \"$REVIEW_RELEASE\" ]; do sleep 0.02; done\nprintf '%s' '{\"version\":4,\"dns_check\":{\"success\":true,\"addresses\":[\"192.0.2.1\"],\"latency_ms\":42}}'\n"
				}
				if err := os.WriteFile(script, []byte(code), 0700); err != nil {
					t.Fatal(err)
				}
				envCopy := *CurrentProbeEnv()
				env := &envCopy
				env.ProbeBinary = func() (string, error) { return script, nil }
				env.Environ = func() []string { return append(os.Environ(), "REVIEW_READY="+ready, "REVIEW_RELEASE="+release) }
				t.Cleanup(SetProbeEnv(env))
				runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				runtime.Start(context.Background())
				t.Cleanup(func() { runtime.Shutdown(context.Background()) })
				request, disconnect := context.WithCancel(runtime.BindRequest(base))
				defer disconnect()
				done := make(chan error, 1)
				go func() {
					if operation == "dns" {
						result, err := CheckDNSSettingsJSON(request, json.RawMessage(`{"mode":"system"}`))
						if err == nil && !result.Success {
							err = errors.New("DNS check did not succeed")
						}
						done <- err
						return
					}
					_, err := RunLatencyTests(request, nil, nil)
					done <- err
				}()
				finished := false
				t.Cleanup(func() {
					_ = os.WriteFile(release, []byte("go"), 0600)
					runtime.Shutdown(context.Background())
					if !finished {
						select {
						case <-done:
						case <-time.After(15 * time.Second):
							t.Error("worker cleanup deadline")
						}
					}
				})
				deadline := time.Now().Add(30 * time.Second)
				for {
					if _, err := os.Stat(ready); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("worker readiness deadline")
					}
					time.Sleep(5 * time.Millisecond)
				}
				if shutdown {
					runtime.Shutdown(context.Background())
				} else {
					disconnect()
				}
				if err := os.WriteFile(release, []byte("go"), 0600); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					finished = true
					if shutdown && !errors.Is(err, context.Canceled) {
						t.Fatalf("shutdown error %v", err)
					}
					if !shutdown && err != nil {
						t.Fatal(err)
					}
				case <-time.After(15 * time.Second):
					t.Fatal("worker did not finish")
				}
				path := filepath.Join(r.DataDir(), "shared", LatencyFile)
				raw, err := os.ReadFile(path)
				if shutdown || operation == "dns" {
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("shutdown wrote results: %s %v", raw, err)
					}
					if _, err := os.Stat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("shutdown wrote backup: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var stored model.Map[LatencyEntry]
				if err := json.Unmarshal(raw, &stored); err != nil {
					t.Fatal(err)
				}
				entry, ok := stored.Get(`["node","synthetic"]`)
				if !ok || entry.Latency == nil || entry.Latency.String() != "42" || len(entry.History) != 1 {
					t.Fatalf("disconnect lost result: %s", raw)
				}
			})
		}
	}
}

func TestReviewN89DNSWriteRespectsOwnerLifetime(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			base := repo.WithRepository(context.Background(), r)
			before, err := r.System(base)
			if err != nil {
				t.Fatal(err)
			}
			beforeJSON, err := model.Marshal(before)
			if err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, disconnect := context.WithCancel(runtime.BindRequest(base))
			defer disconnect()
			if shutdown {
				runtime.Shutdown(context.Background())
			} else {
				disconnect()
			}
			_, err = SaveDNSConfigJSON(request, json.RawMessage(`{"mode":"custom","servers":["192.0.2.53"]}`))
			if shutdown && !errors.Is(err, context.Canceled) {
				t.Fatalf("shutdown error: %v", err)
			}
			if !shutdown && err != nil {
				t.Fatal(err)
			}
			repo.ResetConfigContext(base)
			after, err := r.System(base)
			if err != nil {
				t.Fatal(err)
			}
			afterJSON, err := model.Marshal(after)
			if err != nil {
				t.Fatal(err)
			}
			if shutdown && string(beforeJSON) != string(afterJSON) {
				t.Fatal("shutdown changed system")
			}
			if !shutdown {
				config, err := LoadDNSConfig(base)
				if err != nil || config.Mode != "custom" || len(config.Servers) != 1 || config.Servers[0] != "192.0.2.53" {
					t.Fatalf("disconnect lost settings: %+v %v", config, err)
				}
			}
		})
	}
}
