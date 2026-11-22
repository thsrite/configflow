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
// gets its own copy; errors are not remembered.
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
	if len(key) <= subscriptionCacheBytes/4 {
		kept = cache.Clone()
		subscriptionCaches.mu.Lock()
		if subscriptionCaches.entries == nil || len(subscriptionCaches.entries) >= 32 || subscriptionCaches.bytes+len(key) > subscriptionCacheBytes {
			subscriptionCaches.entries, subscriptionCaches.bytes = map[string]*subs.SubscriptionCache{}, 0
		}
		if _, ok := subscriptionCaches.entries[key]; !ok {
			subscriptionCaches.entries[key] = kept
			subscriptionCaches.bytes += len(key)
		}
		subscriptionCaches.mu.Unlock()
	}
	return &cache, nil
}

const subscriptionCacheBytes = 8 << 20

var subscriptionCaches struct {
	mu      sync.Mutex
	entries map[string]*subs.SubscriptionCache
	bytes   int
}
