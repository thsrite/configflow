package repo_test

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestScopedRecordTransactionsRuntimeAndSharedBaseline(t *testing.T) {
	t.Run("runtime-shutdown-and-disconnect", func(t *testing.T) {
		for _, sharedPath := range []bool{false, true} {
			for _, shutdown := range []bool{true, false} {
				name := "single"
				if sharedPath {
					name = "shared"
				}
				if shutdown {
					name += "/shutdown"
				} else {
					name += "/disconnect"
				}
				t.Run(name, func(t *testing.T) {
					r := recordRepository(t)
					bg := context.Background()
					if _, err := r.CreateProfileRecord(bg, repo.ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
						t.Fatal(err)
					}
					base := repo.WithScope(repo.WithRepository(bg, r))
					runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
					runtime.Start(bg)
					defer runtime.Shutdown(bg)
					request, disconnect := context.WithCancel(runtime.BindRequest(base))
					defer disconnect()

					for _, id := range []string{"default", "other"} {
						if _, err := repo.GetConfig(request, id); err != nil {
							t.Fatal(err)
						}
						if err := r.UpdateConfig(bg, id, func(_ context.Context, config *model.Config) error {
							return config.Profile.SetJSON("mihomo", []byte(`{"marker":"external"}`))
						}); err != nil {
							t.Fatal(err)
						}
					}
					beforeHead, err := r.Head(bg)
					if err != nil {
						t.Fatal(err)
					}
					if shutdown {
						runtime.Shutdown(bg)
					} else {
						disconnect()
					}
					var errTx error
					if sharedPath {
						errTx = repo.UpdateSharedConfigs(request, func(_ context.Context, shared *model.Shared) error {
							return shared.SetJSON("rule_library", []byte(`[]`))
						}, func(_ context.Context, _ string, _, after *model.Config) error {
							return after.Profile.SetJSON("mihomo", []byte(`{"marker":"ok"}`))
						}, nil)
					} else {
						profileArg := "default"
						if shutdown {
							// Shutdown must fail fast before profile validation/reads.
							profileArg = "bad/profile"
						}
						errTx = repo.UpdateConfig(request, profileArg, func(_ context.Context, config *model.Config) error {
							return config.Profile.SetJSON("mihomo", []byte(`{"marker":"ok"}`))
						})
					}
					afterHead, err := r.Head(bg)
					if err != nil {
						t.Fatal(err)
					}
					if shutdown {
						if !errors.Is(errTx, context.Canceled) {
							t.Fatalf("expected context.Canceled after shutdown, got %v", errTx)
						}
						if afterHead != beforeHead {
							t.Fatal("shutdown transaction modified storage")
						}
						// Cached pre-external snapshot must still be intact.
						snap, err := repo.GetConfig(base, "default")
						if err != nil {
							t.Fatal(err)
						}
						if got := snap.GetObject("mihomo").GetString("marker"); got != "" {
							t.Fatalf("shutdown transaction invalidated scope cache: %q", got)
						}
						return
					}
					if errTx != nil {
						t.Fatalf("disconnected request failed: %v", errTx)
					}
					if afterHead == beforeHead {
						t.Fatal("disconnected request did not commit")
					}
					defSnap, err := repo.GetConfig(base, "default")
					if err != nil {
						t.Fatal(err)
					}
					if got := defSnap.GetObject("mihomo").GetString("marker"); got != "ok" {
						t.Fatalf("default marker after disconnect commit = %q, want ok", got)
					}
					otherSnap, err := repo.GetConfig(base, "other")
					if err != nil {
						t.Fatal(err)
					}
					wantOther := ""
					if sharedPath {
						wantOther = "external"
					}
					if got := otherSnap.GetObject("mihomo").GetString("marker"); got != wantOther {
						t.Fatalf("other marker = %q, want %q", got, wantOther)
					}
				})
			}
		}
	})

	t.Run("shared-record-baseline-three-way-merge", func(t *testing.T) {
		r := recordRepository(t)
		bg := context.Background()
		ctx := repo.WithScope(repo.WithRepository(bg, r))
		if err := r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
			shared.Nodes = []model.Node{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		snap, err := repo.SharedConfig(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// Single-profile UpdateConfig must preserve Scope's shared record baseline.
		if err := repo.UpdateConfig(ctx, "default", func(_ context.Context, config *model.Config) error {
			return config.Profile.SetJSON("mihomo", []byte(`{"marker":"one"}`))
		}); err != nil {
			t.Fatal(err)
		}
		// Concurrent external edit to node b, while this scope edits node a.
		if err := r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
			shared.Nodes[1].Name = "External B"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		// Failed UpdateSharedConfigs must preserve Scope's shared record baseline.
		stop := errors.New("abort shared")
		if err := repo.UpdateSharedConfigs(ctx, func(_ context.Context, shared *model.Shared) error {
			return stop
		}, func(_ context.Context, _ string, _, _ *model.Config) error {
			return nil
		}, nil); !errors.Is(err, stop) {
			t.Fatalf("expected stop, got %v", err)
		}
		snap.Nodes[0].Name = "Scoped A"
		if err := repo.SaveSharedRecord(ctx, snap, nil); err != nil {
			t.Fatalf("three-way merge failed after UpdateConfig and failed UpdateSharedConfigs: %v", err)
		}
		merged, err := r.Shared(bg)
		if err != nil {
			t.Fatal(err)
		}
		if merged.Nodes[0].Name != "Scoped A" || merged.Nodes[1].Name != "External B" {
			t.Fatalf("concurrent edits lost: %+v", merged.Nodes)
		}
		// Re-capture baseline; successful UpdateSharedConfigs must clear it so stale save fails with 409.
		staleSnap, err := repo.SharedConfig(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.UpdateSharedConfigs(ctx, func(_ context.Context, shared *model.Shared) error {
			shared.Nodes[0].Name = "Tx A"
			return nil
		}, func(_ context.Context, _ string, _, _ *model.Config) error {
			return nil
		}, nil); err != nil {
			t.Fatal(err)
		}
		if err := repo.SaveSharedRecord(ctx, staleSnap, nil); !errors.Is(err, repo.ErrInUse) {
			t.Fatalf("UpdateSharedConfigs did not clear shared record baseline: %v", err)
		}
	})
}
