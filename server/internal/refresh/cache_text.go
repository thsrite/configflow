package refresh

import (
	"container/list"
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
// Total retained text is bounded by limitBytes with LRU eviction.
var cacheTexts = cacheTextMemo{limit: 64, limitBytes: 4 << 20}

type cacheTextMemo struct {
	mu         sync.Mutex
	texts      map[string]*list.Element
	order      *list.List
	bytes      int
	limit      int
	limitBytes int
}

// cacheText is the text rendered from one shared record.
type cacheText struct {
	id    string
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
	elem, ok := m.texts[id]
	if !ok {
		return "", false
	}
	kept := elem.Value.(*cacheText)
	if kept.cache != cache {
		return "", false
	}
	m.order.MoveToBack(elem)
	return kept.text, true
}

func (m *cacheTextMemo) store(env *RefreshEnv, id string, cache *NodeCache, text string) {
	if !m.usable(env, cache) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.texts == nil {
		m.texts = map[string]*list.Element{}
		m.order = list.New()
	}
	// The superseded generation goes first, also when the new text is too
	// large to keep, so an old record and its text never outlive a change.
	if prev, ok := m.texts[id]; ok {
		m.bytes -= len(prev.Value.(*cacheText).text)
		m.order.Remove(prev)
		delete(m.texts, id)
	}
	if m.limitBytes > 0 && len(text) > m.limitBytes/2 {
		return
	}
	for m.order.Len() > 0 && (m.order.Len() >= m.limit || (m.limitBytes > 0 && m.bytes+len(text) > m.limitBytes)) {
		front := m.order.Front()
		evicted := front.Value.(*cacheText)
		m.order.Remove(front)
		delete(m.texts, evicted.id)
		m.bytes -= len(evicted.text)
	}
	m.texts[id] = m.order.PushBack(&cacheText{id: id, cache: cache, text: text})
	m.bytes += len(text)
}

// sharedNodeLoader reports whether env loads node caches with loadNodeCache,
// whose records are immutable and stable while the file is unchanged.
func sharedNodeLoader(env *RefreshEnv) bool {
	return env != nil && env.Sources.LoadNodes != nil &&
		reflect.ValueOf(env.Sources.LoadNodes).Pointer() == reflect.ValueOf(loadNodeCache).Pointer()
}
