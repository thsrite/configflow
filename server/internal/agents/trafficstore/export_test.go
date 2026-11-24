package trafficstore

import (
	"os"
	"strings"
	"time"
)

// CtimeNs returns the cache change-time signature for tests.
func CtimeNs(info os.FileInfo) int64 { return signatureOf(info).CtimeNs }

// SetCacheLimit changes _STATUS_CACHE_LIMIT and returns the old value
// (tests).
func SetCacheLimit(limit int) int {
	mu.Lock()
	defer mu.Unlock()
	old := statusCacheLimit
	statusCacheLimit = limit
	return old
}

// CachedPaths lists the cached paths, least recently used first (tests).
func CachedPaths() []string {
	mu.Lock()
	defer mu.Unlock()
	var out []string
	for element := statusCache.Front(); element != nil; element = element.Next() {
		out = append(out, element.Value.(*cacheEntry).path)
	}
	return out
}

// SetNow replaces the store clock for tests.
func SetNow(fn func() time.Time) func() { old := now; now = fn; return func() { now = old } }

// ParseRouteKey decodes a persisted rule decision.
func ParseRouteKey(key string) (Route, bool) {
	p := strings.SplitN(key, routeSeparator, 4)
	if len(p) != 4 {
		return Route{}, false
	}
	return Route{p[0], p[1], p[2], p[3]}, true
}
