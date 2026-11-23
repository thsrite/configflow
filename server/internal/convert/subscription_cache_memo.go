package convert

import (
	"container/list"
	"encoding/json"
	"sync"

	"configflow/internal/subs"
)

// decodedSubscriptionCache decodes a stored subscription node cache, reusing
// the decode of identical bytes (O10 R9) when a caller supplies the legacy
// JSON CacheReader instead of TypedCacheReader. The caller gets its own copy;
// errors are not remembered. The table is bounded by an estimate of the heap
// it keeps alive (14 bytes per byte of encoding) and evicts least-recently
// used entries first.
func decodedSubscriptionCache(data []byte) (*subs.SubscriptionCache, error) {
	key := string(data)
	subscriptionCaches.mu.Lock()
	if elem, ok := subscriptionCaches.entries[key]; ok {
		subscriptionCaches.order.MoveToBack(elem)
		kept := elem.Value.(*subscriptionCacheEntry).value
		subscriptionCaches.mu.Unlock()
		return kept.Clone(), nil
	}
	subscriptionCaches.mu.Unlock()
	var cache subs.SubscriptionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	if cost := len(key) * subscriptionCacheRetainedPerByte; cost <= subscriptionCacheRetainedLimit/2 {
		kept := cache.Clone()
		subscriptionCaches.mu.Lock()
		if subscriptionCaches.entries == nil {
			subscriptionCaches.entries = map[string]*list.Element{}
			subscriptionCaches.order = list.New()
		}
		if _, ok := subscriptionCaches.entries[key]; !ok {
			for subscriptionCaches.order.Len() > 0 && (subscriptionCaches.order.Len() >= subscriptionCacheMaxEntries || subscriptionCaches.bytes+cost > subscriptionCacheRetainedLimit) {
				front := subscriptionCaches.order.Front()
				evicted := front.Value.(*subscriptionCacheEntry)
				subscriptionCaches.order.Remove(front)
				delete(subscriptionCaches.entries, evicted.key)
				subscriptionCaches.bytes -= evicted.cost
			}
			subscriptionCaches.entries[key] = subscriptionCaches.order.PushBack(&subscriptionCacheEntry{key: key, value: kept, cost: cost})
			subscriptionCaches.bytes += cost
		}
		subscriptionCaches.mu.Unlock()
	}
	return &cache, nil
}

const (
	subscriptionCacheRetainedPerByte = 14
	subscriptionCacheRetainedLimit   = 4 << 20
	subscriptionCacheMaxEntries      = 32
)

type subscriptionCacheEntry struct {
	key   string
	value *subs.SubscriptionCache
	cost  int
}

var subscriptionCaches struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List
	bytes   int // estimated retained heap
}
