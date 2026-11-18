package substore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestN89NumericCacheIDReference(t *testing.T) {
	data, err := os.ReadFile("testdata/n89_cache_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Stored     json.RawMessage   `json:"stored"`
		Projection json.RawMessage   `json:"projection"`
		Files      map[string]string `json:"files"`
		Python     struct {
			Removed []string          `json:"removed"`
			Files   map[string]string `json:"files"`
		} `json:"python"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	var shared model.Shared
	if err := json.Unmarshal(fixture.Stored, &shared); err != nil {
		t.Fatal(err)
	}
	var projection struct {
		Subscriptions []struct {
			ID string `json:"id"`
		} `json:"subscriptions"`
	}
	for _, sub := range shared.Subscriptions {
		projection.Subscriptions = append(projection.Subscriptions, struct {
			ID string `json:"id"`
		}{sub.ID})
	}
	actual, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, fixture.Projection); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, compact.Bytes()) {
		t.Fatalf("native cache reference projection: %s want %s", actual, compact.Bytes())
	}
	ctx, r := sharedRepo(t)
	dir, err := r.SharedCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range must[[]os.DirEntry](t)(os.ReadDir(dir)) {
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range fixture.Files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	previous := getShared
	getShared = func(context.Context, *repo.Repository) (*model.Shared, error) { return &shared, nil }
	t.Cleanup(func() { getShared = previous })
	removed, err := PruneOrphanCaches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(removed)
	files := map[string]string{}
	for _, entry := range must[[]os.DirEntry](t)(os.ReadDir(dir)) {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = string(data)
	}
	got, _ := json.Marshal(struct {
		Removed []string          `json:"removed"`
		Files   map[string]string `json:"files"`
	}{removed, files})
	want, _ := json.Marshal(fixture.Python)
	if !bytes.Equal(got, want) {
		t.Fatalf("native cache reference mismatch: got %s want %s", got, want)
	}
}

func TestN89CacheWriteOwnerLifetime(t *testing.T) {
	for _, operation := range []string{"save", "delete", "prune"} {
		for _, shutdown := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/shutdown=%t", operation, shutdown), func(t *testing.T) {
				base, r := sharedRepo(t)
				if _, err := SaveCache(base, "orphan", []model.Node{{Name: "Old"}}, nil); err != nil {
					t.Fatal(err)
				}
				dir, err := r.SharedCacheDir()
				if err != nil {
					t.Fatal(err)
				}
				snapshot := func() map[string]string {
					t.Helper()
					result := map[string]string{}
					for _, entry := range must[[]os.DirEntry](t)(os.ReadDir(dir)) {
						data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
						if err != nil {
							t.Fatal(err)
						}
						result[entry.Name()] = string(data)
					}
					return result
				}
				before := snapshot()
				runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				runtime.Start(context.Background())
				t.Cleanup(func() { runtime.Shutdown(context.Background()) })
				ctx, disconnect := context.WithCancel(runtime.BindRequest(base))
				defer disconnect()
				if shutdown {
					runtime.Shutdown(context.Background())
				} else {
					disconnect()
				}
				switch operation {
				case "save":
					_, err = SaveCache(ctx, "keep", []model.Node{{Name: "New"}}, nil)
				case "delete":
					_, err = DeleteSubscriptionCache(ctx, "keep")
				case "prune":
					_, err = PruneOrphanCaches(ctx)
				}
				if shutdown {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("stopped owner: %v", err)
					}
					a, _ := json.Marshal(before)
					b, _ := json.Marshal(snapshot())
					if !bytes.Equal(a, b) {
						t.Fatal("shutdown changed primary/backup files")
					}
				} else if err != nil {
					t.Fatalf("request disconnect aborted owner: %v", err)
				} else {
					after := snapshot()
					switch operation {
					case "save":
						if !strings.Contains(after["keep.json"], `"New"`) {
							t.Fatal("disconnected write not persisted")
						}
					case "delete":
						if _, ok := after["keep.json"]; ok {
							t.Fatal("disconnected delete not persisted")
						}
					case "prune":
						if _, ok := after["orphan.json"]; ok {
							t.Fatal("disconnected prune not persisted")
						}
					}
				}
			})
		}
	}
}

func cacheRecordFixture(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"nodes":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":"n%d","name":%d,"server":123,"port":"443","params":{"password":123},"future":9007199254740993123}`, i, i)
	}
	b.WriteString(`],"metadata":{"future":1e3}}`)
	return []byte(b.String())
}
func BenchmarkN89CacheRecord(b *testing.B) {
	for _, n := range []int{32, 256, 2048} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			data := cacheRecordFixture(n)
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for b.Loop() {
				var cache Cache
				if err := json.Unmarshal(data, &cache); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
func TestN89CacheRecordAllocationGrowth(t *testing.T) {
	allocations := func(n int) float64 {
		data := cacheRecordFixture(n)
		return testing.AllocsPerRun(3, func() {
			var cache Cache
			if err := json.Unmarshal(data, &cache); err != nil {
				t.Fatal(err)
			}
		})
	}
	small, large := allocations(32), allocations(128)
	if large > small*6 {
		t.Fatalf("cache decode allocations grew quadratically: 32=%g 128=%g", small, large)
	}
}

func FuzzN89CacheRecord(f *testing.F) {
	for _, source := range []string{`{"nodes":[{"name":123,"server":123,"port":"443","params":{"password":123},"future":1e3}],"future":9007199254740993123}`, `{"nodes":"bad","metadata":{"traffic":{"total":123456789012345678901234567890,"future":1e3}}}`, `{"nodes":null,"metadata":{"hosts":{"entries":{"example.com":"192.0.2.1"}}}}`} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32768 {
			return
		}
		var cache Cache
		if json.Unmarshal(data, &cache) != nil {
			return
		}
		before, err := model.Marshal(cache)
		if err != nil {
			t.Fatal(err)
		}
		var loaded Cache
		if err := json.Unmarshal(before, &loaded); err != nil {
			t.Fatal(err)
		}
		after, err := model.Marshal(loaded)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("unstable cache: %s %v", after, err)
		}
		cache.UpdatedAt = "synthetic-updated"
		edited, err := model.Marshal(cache)
		if err != nil {
			t.Fatal(err)
		}
		var oldFields, newFields map[string]json.RawMessage
		if err := json.Unmarshal(before, &oldFields); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(edited, &newFields); err != nil {
			t.Fatal(err)
		}
		for name, value := range oldFields {
			if name != "updated_at" && !bytes.Equal(value, newFields[name]) {
				t.Fatalf("unowned field changed: %s", name)
			}
		}
	})
}
