package repo

import (
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
