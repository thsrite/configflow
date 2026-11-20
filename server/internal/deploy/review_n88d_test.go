package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"configflow/internal/clock"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

type reviewPreviewClock struct {
	clock.Real
	beforeNow func()
}

func TestReviewN88dReceiptShutdownBeforeReplacement(t *testing.T) {
	for _, boundary := range []int{1, 2} {
		for _, shutdown := range []bool{false, true} {
			t.Run(fmt.Sprintf("write=%d/shutdown=%t", boundary, shutdown), func(t *testing.T) {
				base := testContext(t)
				r, err := repo.GetRepository(base)
				if err != nil {
					t.Fatal(err)
				}
				owner := NewReceipts(r.DataDir())
				bundle := sample(t, "mihomo", 3)
				source := testSource(t, map[string]any{"profile_id": "profile-example"})
				runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				runtime.Start(base)
				defer runtime.Shutdown(context.Background())
				request, disconnect := context.WithCancel(runtime.BindRequest(base))
				defer disconnect()
				writes := 0
				// Observe the actual atomic file owner. Every write still uses its
				// real temp/fsync/rename implementation and production parameters.
				owner.writeFile = func(ctx context.Context, path string, data []byte) error {
					writes++
					if writes == boundary {
						disconnect()
						if shutdown {
							runtime.Shutdown(context.Background())
						}
					}
					return atomicWrite(ctx, path, data)
				}
				err = owner.Capture(request, "agent-example", bundle, source, "https://panel.example.com", nil)
				folder := owner.folder("agent-example", bundle.manifest.DeploymentID)
				files, readErr := os.ReadDir(folder)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if shutdown {
					if !errors.Is(err, context.Canceled) || writes != boundary || len(files) != boundary-1 {
						t.Fatalf("shutdown crossed receipt boundary: writes=%d files=%d err=%v", writes, len(files), err)
					}
					if _, err := os.Stat(filepath.Join(folder, "receipt.json")); !os.IsNotExist(err) {
						t.Fatal("shutdown published receipt", err)
					}
				} else if err != nil || writes != 2 || len(files) != 2 {
					t.Fatalf("disconnect lost receipt: writes=%d files=%d err=%v", writes, len(files), err)
				}
			})
		}
	}
}

func (c *reviewPreviewClock) Now() time.Time {
	if c.beforeNow != nil {
		c.beforeNow()
	}
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

// The second clock read is inside the real store lock, after quota inspection
// and before either eviction or publication. It never changes production limits.
func TestReviewN88dPreviewShutdownBeforePublication(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			c := &reviewPreviewClock{}
			owner := NewPreviews(t.TempDir(), c)
			in := PreviewInput{Target: testTarget(t, map[string]any{"id": "manual", "service_type": "surge", "profile_id": "default"}), Source: testSource(t, map[string]any{"profile_id": "default"}), ConfigContent: "[General]\n"}
			if _, err := owner.Save(context.Background(), in); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadDir(owner.root())
			if err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			defer runtime.Shutdown(context.Background())
			request, disconnect := context.WithCancel(runtime.BindRequest(context.Background()))
			defer disconnect()
			reads := 0
			c.beforeNow = func() {
				reads++
				if reads == 2 {
					disconnect()
					if shutdown {
						runtime.Shutdown(context.Background())
					}
				}
			}
			got, err := owner.Save(request, in)
			after, readErr := os.ReadDir(owner.root())
			if readErr != nil || reads < 2 {
				t.Fatalf("boundary not reached: reads=%d err=%v", reads, readErr)
			}
			if shutdown {
				if !errors.Is(err, context.Canceled) || got != nil {
					t.Fatalf("shutdown published preview: result=%v err=%v", got != nil, err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatal("shutdown changed preview directories")
				}
			} else {
				if err != nil || got == nil || len(after) != len(before)+1 {
					t.Fatalf("disconnect lost preview: %v", err)
				}
				if _, err := os.Stat(filepath.Join(owner.root(), got.ID, "record.json")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
