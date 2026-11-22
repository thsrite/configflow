package convert

import (
	"encoding/json"
	"sync"

	"configflow/internal/subs"
)

// decodedSubscriptionCache decodes a stored subscription node cache, reusing
// the decode of identical bytes (O10 R9): one generated configuration reads
// each subscription's cache for hosts and again for dialer groups, and the
// owner hands out the same encoding while the file is unchanged. The caller
// gets its own copy; errors are not remembered. The table is bounded by an
// estimate of the heap it keeps alive: a decoded cache keeps about 14 bytes
// per byte of its encoding (measured on a 0.6 MB, 2000-node cache).
func decodedSubscriptionCache(data []byte) (*subs.SubscriptionCache, error) {
	key := string(data)
	subscriptionCaches.mu.Lock()
	kept, ok := subscriptionCaches.entries[key]
	subscriptionCaches.mu.Unlock()
	if ok {
		return kept.Clone(), nil
	}
	var cache subs.SubscriptionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	if cost := len(key) * subscriptionCacheRetainedPerByte; cost <= subscriptionCacheRetainedLimit/2 {
		kept = cache.Clone()
		subscriptionCaches.mu.Lock()
		if subscriptionCaches.entries == nil || len(subscriptionCaches.entries) >= 32 || subscriptionCaches.bytes+cost > subscriptionCacheRetainedLimit {
			subscriptionCaches.entries, subscriptionCaches.bytes = map[string]*subs.SubscriptionCache{}, 0
		}
		if _, ok := subscriptionCaches.entries[key]; !ok {
			subscriptionCaches.entries[key] = kept
			subscriptionCaches.bytes += cost
		}
		subscriptionCaches.mu.Unlock()
	}
	return &cache, nil
}

const (
	subscriptionCacheRetainedPerByte = 14
	subscriptionCacheRetainedLimit   = 32 << 20
)

var subscriptionCaches struct {
	mu      sync.Mutex
	entries map[string]*subs.SubscriptionCache
	bytes   int // estimated retained heap
}
