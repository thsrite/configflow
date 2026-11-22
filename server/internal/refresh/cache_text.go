package refresh

import (
	"reflect"
	"sync"
)

// cacheTexts remembers the provider text FetchSource renders from a stored
// node cache (O10 R9). The default owners hand out the same shared NodeCache
// while the cache file is unchanged (loadNodeCache), and rendering stored
// nodes is a pure function of them, so the text is reused for that record.
// Only records from the default loader are remembered, and only when no
// node carries a proxy_string (those go through the converter, which may log
// or call Sub-Store). One text is kept per subscription: a changed file
// replaces the entry, so a superseded record and its text are not kept alive.
var cacheTexts = cacheTextMemo{limit: 64}

type cacheTextMemo struct {
	mu    sync.Mutex
	texts map[string]cacheText
	limit int
}

// cacheText is the text rendered from one shared record.
type cacheText struct {
	cache *NodeCache
	text  string
}

func (m *cacheTextMemo) usable(env *RefreshEnv, cache *NodeCache) bool {
	if cache == nil || !sharedNodeLoader(env) {
		return false
	}
	for i := range cache.Nodes {
		if p := cache.Nodes[i].ProxyString; p != nil && *p != "" {
			return false
		}
	}
	return true
}

func (m *cacheTextMemo) lookup(env *RefreshEnv, id string, cache *NodeCache) (string, bool) {
	if !m.usable(env, cache) {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	kept, ok := m.texts[id]
	if !ok || kept.cache != cache {
		return "", false
	}
	return kept.text, true
}

func (m *cacheTextMemo) store(env *RefreshEnv, id string, cache *NodeCache, text string) {
	if !m.usable(env, cache) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, replaces := m.texts[id]; m.texts == nil || (!replaces && len(m.texts) >= m.limit) {
		m.texts = map[string]cacheText{}
	}
	m.texts[id] = cacheText{cache: cache, text: text}
}

// sharedNodeLoader reports whether env loads node caches with loadNodeCache,
// whose records are immutable and stable while the file is unchanged.
func sharedNodeLoader(env *RefreshEnv) bool {
	return env != nil && env.Sources.LoadNodes != nil &&
		reflect.ValueOf(env.Sources.LoadNodes).Pointer() == reflect.ValueOf(loadNodeCache).Pointer()
}
