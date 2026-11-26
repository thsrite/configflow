package repo_test

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// Apply this regression after integrating O3-6c's shared commit core into N8-5a.
func TestReviewN85TypedUpdateRuntimeBoundary(t *testing.T) {
	for _, point := range []string{"before owner", "updater", "before commit"} {
		for _, shutdown := range []bool{true, false} {
			name := point + "/disconnect"
			if shutdown {
				name = point + "/shutdown"
			}
			t.Run(name, func(t *testing.T) {
				r, err := repo.Open(t.TempDir(), repo.Options{})
				if err != nil {
					t.Fatal(err)
				}
				base := context.Background()
				runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				runtime.Start(base)
				t.Cleanup(func() { runtime.Shutdown(context.Background()) })
				request, disconnect := context.WithCancel(runtime.BindRequest(base))
				defer disconnect()
				stop := func() {
					if shutdown {
						runtime.Shutdown(context.Background())
					} else {
						disconnect()
					}
				}
				before, err := r.Store().Fingerprint()
				if err != nil {
					t.Fatal(err)
				}
				if point == "before owner" {
					stop()
				}
				if point == "before commit" {
					r.Store().BeforeCommit = func() error { stop(); return nil }
					defer func() { r.Store().BeforeCommit = nil }()
				}
				err = r.Update(request, func(_ context.Context, d *model.Document) error {
					if point == "updater" {
						stop()
					}
					d.Profiles[0].Name = "synthetic-runtime-edit"
					return nil
				})
				after, loadErr := r.Store().Fingerprint()
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				if shutdown {
					if !errors.Is(err, context.Canceled) || before != after {
						t.Fatalf("shutdown result=%v DB changed=%v", err, before != after)
					}
				} else if err != nil || before == after {
					t.Fatalf("disconnect result=%v DB changed=%v", err, before != after)
				}
			})
		}
	}
}
