package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

// TestReviewN88dOpusPreviewShutdownAtEachReplacement drives a real Runtime to
// shutdown immediately before each of the two preview file replacements
// (bundle.tar.gz, then record.json). Shutdown must leave the store exactly as
// before (no partially written preview folder), and the earlier preview must
// still load; a request disconnect must still complete both files.
func TestReviewN88dOpusPreviewShutdownAtEachReplacement(t *testing.T) {
	for _, boundary := range []int{1, 2} {
		for _, shutdown := range []bool{false, true} {
			t.Run(fmt.Sprintf("write=%d/shutdown=%t", boundary, shutdown), func(t *testing.T) {
				owner := NewPreviews(t.TempDir(), nil)
				in := PreviewInput{
					Target:  NewTarget(jsonx.Obj("id", "agent-example", "service_type", "mihomo", "profile_id", "profile-example")),
					Source:  NewSource(jsonx.Obj("profile_id", "profile-example"), nil),
					Bundle:  sample(t, "mihomo", 3),
					Members: []Target{},
				}
				existing, err := owner.Save(context.Background(), in)
				if err != nil {
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
				var names []string
				// Observe the real atomic file owner; every write still uses the
				// production temp/fsync/rename path and parameters.
				owner.writeFile = func(ctx context.Context, path string, data []byte) error {
					names = append(names, filepath.Base(path))
					if len(names) == boundary {
						disconnect()
						if shutdown {
							runtime.Shutdown(context.Background())
						}
					}
					return atomicWrite(ctx, path, data)
				}
				got, err := owner.Save(request, in)
				after, readErr := os.ReadDir(owner.root())
				if readErr != nil {
					t.Fatal(readErr)
				}
				if shutdown {
					if !errors.Is(err, context.Canceled) || got != nil || len(names) != boundary {
						t.Fatalf("shutdown crossed preview boundary: writes=%v result=%v err=%v", names, got != nil, err)
					}
					if !reflect.DeepEqual(dirNames(before), dirNames(after)) {
						t.Fatalf("shutdown left a partial preview: before=%v after=%v", dirNames(before), dirNames(after))
					}
					if _, err := owner.Load(existing.ID, "agent-example"); err != nil {
						t.Fatal("earlier preview lost", err)
					}
					return
				}
				if err != nil || got == nil || !reflect.DeepEqual(names, []string{"bundle.tar.gz", "record.json"}) || len(after) != len(before)+1 {
					t.Fatalf("disconnect lost preview: writes=%v err=%v", names, err)
				}
				if loaded, err := owner.Load(got.ID, "agent-example"); err != nil || loaded.Bundle == nil {
					t.Fatalf("disconnected preview not loadable: %v", err)
				}
			})
		}
	}
}

// TestReviewN88dOpusRecaptureShutdownKeepsReferencedArchive covers the
// AdvanceContent shape: a second capture for the same deployment. Shutdown at
// either replacement must leave the previous receipt.json byte-identical and
// the archive it references present, so Load keeps a consistent baseline.
func TestReviewN88dOpusRecaptureShutdownKeepsReferencedArchive(t *testing.T) {
	for _, boundary := range []int{1, 2} {
		t.Run(fmt.Sprintf("write=%d", boundary), func(t *testing.T) {
			base := testContext(t)
			r, err := repo.GetRepository(base)
			if err != nil {
				t.Fatal(err)
			}
			owner := NewReceipts(r.DataDir())
			source := NewSource(jsonx.Obj("profile_id", "profile-example"), nil)
			first, second := sample(t, "mihomo", 3), sample(t, "mihomo", 4)
			if err = owner.Capture(base, "agent-example", first, source, "https://panel.example.com", nil); err != nil {
				t.Fatal(err)
			}
			folder := owner.folder("agent-example", first.manifest.DeploymentID)
			oldReceipt, err := os.ReadFile(filepath.Join(folder, "receipt.json"))
			if err != nil {
				t.Fatal(err)
			}
			oldSHA, err := first.SHA256()
			if err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(base)
			defer runtime.Shutdown(context.Background())
			writes := 0
			owner.writeFile = func(ctx context.Context, path string, data []byte) error {
				writes++
				if writes == boundary {
					runtime.Shutdown(context.Background())
				}
				return atomicWrite(ctx, path, data)
			}
			err = owner.Capture(runtime.BindRequest(base), "agent-example", second, source, "https://panel.example.com", nil)
			if !errors.Is(err, context.Canceled) || writes != boundary {
				t.Fatalf("shutdown crossed receipt boundary: writes=%d err=%v", writes, err)
			}
			now, err := os.ReadFile(filepath.Join(folder, "receipt.json"))
			if err != nil || string(now) != string(oldReceipt) {
				t.Fatalf("receipt.json changed after shutdown: %v", err)
			}
			if _, err = os.Stat(filepath.Join(folder, "bundle-"+oldSHA+".tar.gz")); err != nil {
				t.Fatal("referenced archive lost", err)
			}
		})
	}
}

func dirNames(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
