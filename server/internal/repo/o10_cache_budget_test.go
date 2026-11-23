package repo

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"configflow/internal/model"
	"configflow/internal/store"
)

func TestSetCacheDropsSupersededAgentsCache(t *testing.T) {
	r := &Repository{cacheMaxBytes: DocumentCacheMaxBytes}
	r.agentsCache = &agentSnapshot{
		head:    store.Head{Generation: 1},
		records: []model.Agent{{ID: "a1"}},
	}
	r.setCache(storedEntry{
		Entry: store.Entry{Head: store.Head{Generation: 2}, Size: 128},
		Model: &model.Document{},
	})
	r.agentsCacheMu.Lock()
	remaining := r.agentsCache
	r.agentsCacheMu.Unlock()
	if remaining != nil {
		t.Fatalf("setCache left superseded agentsCache alive: %+v", remaining)
	}

	r.agentsCache = &agentSnapshot{head: store.Head{Generation: 3}}
	r.dropCache()
	r.agentsCacheMu.Lock()
	remaining = r.agentsCache
	r.agentsCacheMu.Unlock()
	if remaining != nil {
		t.Fatalf("dropCache left agentsCache alive: %+v", remaining)
	}
}

// Readers that borrowed the previous generation's registry keep a valid,
// unchanged slice while writers replace the document cache and drop the
// standalone snapshot (batch-47 review): dropping only unlinks the pointer.
func TestDroppedAgentsCacheKeepsBorrowedRecords(t *testing.T) {
	r := agentViewRepo(t)
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := r.ViewAgents(t.Context(), func(records []model.Agent) error {
					if len(records) != 1 {
						t.Errorf("registry size=%d", len(records))
						return nil
					}
					name := records[0].Name
					r.dropCache()
					if records[0].Name != name || records[0].ID != "device" {
						t.Errorf("borrowed record changed: %q -> %q", name, records[0].Name)
					}
					return nil
				}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	for i := range 20 {
		if err := r.UpdateSystem(t.Context(), func(_ context.Context, system *model.System) error {
			system.Agents[0].Name = fmt.Sprintf("gen-%d", i)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	readers.Wait()
	if got := agentViewName(t, r, t.Context()); got != "gen-19" {
		t.Fatalf("latest registry not visible: %q", got)
	}
}
