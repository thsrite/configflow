package rules

import (
	"configflow/internal/clock"
	"configflow/internal/jsonx"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"encoding/json"
	"sync"
)

// MatcherCacheOptions wires the cache to its environment. Every field except
// RulesDir is optional.
type MatcherCacheOptions struct {
	// RulesDir is the shared rule cache directory (DATA_DIR/shared/rules).
	RulesDir func() string
	// StoredLibrary returns the current shared rule_library from the
	// repository (RuleCacheReadPath consults it for legacy cache names).
	StoredLibrary func() []any
	// ValidateRemote checks a url source's cache file against its
	// declaration; an error marks the file unusable. nil accepts everything.
	ValidateRemote func(content string, rule *jsonx.Object) error
	// Clock supplies scheduling time and timers; nil uses real time.
	Clock clock.Clock
	// Spawner tracks cleanup work. When nil, eviction runs only on access or
	// explicit EvictIdle calls; no background work is started.
	Spawner lockx.Spawner
	Logger  Logger

	// Tunables, normally the Default* constants (tests shrink them).
	MaxMatchers, MaxHostResults                    int
	IdleSeconds, ReadRetrySeconds, CleanupInterval float64
}

// MatcherCache bridges unported discovery to the native registry.
type MatcherCache struct {
	*Cache
	opt        MatcherCacheOptions
	wrappersMu sync.Mutex
	wrappers   map[*Cached]*CachedMatcher
}

// NewMatcherCache creates the temporary discovery adapter.
func NewMatcherCache(opt MatcherCacheOptions) *MatcherCache {
	c := &MatcherCache{opt: opt, wrappers: make(map[*Cached]*CachedMatcher)}
	native := CacheOptions{RulesDir: func() string { return c.opt.RulesDir() }, Clock: opt.Clock, Spawner: opt.Spawner, Logger: opt.Logger,
		MaxMatchers: opt.MaxMatchers, MaxHostResults: opt.MaxHostResults, IdleSeconds: opt.IdleSeconds, ReadRetrySeconds: opt.ReadRetrySeconds, CleanupInterval: opt.CleanupInterval}
	native.StoredLibrary = func() []model.RuleLibraryItem {
		if c.opt.StoredLibrary == nil {
			return nil
		}
		return legacyLibrary(c.opt.StoredLibrary())
	}
	if opt.ValidateRemote != nil {
		native.ValidateRemote = func(content string, rule model.RuleConfig) error {
			data, _ := model.Marshal(rule)
			value, _ := jsonx.Parse(data)
			return c.opt.ValidateRemote(content, value.(*jsonx.Object))
		}
	}
	c.Cache = NewCache(native)
	// Preserve defaults for existing diagnostic callers.
	c.opt.MaxMatchers = c.Cache.opt.MaxMatchers
	c.opt.MaxHostResults = c.Cache.opt.MaxHostResults
	c.opt.IdleSeconds = c.Cache.opt.IdleSeconds
	c.opt.ReadRetrySeconds = c.Cache.opt.ReadRetrySeconds
	c.opt.CleanupInterval = c.Cache.opt.CleanupInterval
	return c
}

// MatcherInputFromJSON projects only rule collections. Unrelated config members
// are never marshaled or decoded, even when a caller has a large node catalog.
func MatcherInputFromJSON(config *jsonx.Object) MatcherInput {
	var rules []model.RuleConfig
	// Retain disabled/malformed row positions through the established boundary.
	for _, entry := range config.GetList("rule_configs") {
		var rule model.RuleConfig
		if decodeRecord(entry, &rule) != nil {
			disabled := false
			rule.Enabled = &disabled
		}
		rules = append(rules, rule)
	}
	refs := map[string]bool{}
	for _, rule := range rules {
		if ruleKind(&rule) == "ruleset" {
			refs[rule.LibraryRuleID] = true
		}
	}
	result := MatcherInput{Rules: rules}
	for _, entry := range config.GetList("rule_library") {
		object, ok := entry.(*jsonx.Object)
		if !ok {
			continue
		}
		id, name := object.GetString("id"), object.GetString("name")
		result.CacheLibrary = append(result.CacheLibrary, model.RuleLibraryItem{ID: id, Name: name})
		if refs[id] {
			var item model.RuleLibraryItem
			if decodeRecord(object, &item) == nil {
				result.Library = append(result.Library, item)
			}
		}
	}
	return result
}

// LocalMatcher preserves handle identity for existing discovery callers.
func (c *MatcherCache) LocalMatcher(config *jsonx.Object, profileID string) *CachedMatcher {
	native := c.Cache.LocalMatcher(MatcherInputFromJSON(config), profileID)
	c.wrappersMu.Lock()
	defer c.wrappersMu.Unlock()
	if value := c.wrappers[native]; value != nil {
		return value
	}
	// Wrapper ownership follows bounded registry ownership; old handles still work.
	c.Cache.mu.Lock()
	owned := make(map[*Cached]bool, len(c.Cache.slots))
	for _, element := range c.Cache.slots {
		owned[element.Value.(*cacheEntry).matcher] = true
	}
	c.Cache.mu.Unlock()
	for old := range c.wrappers {
		if !owned[old] {
			delete(c.wrappers, old)
		}
	}
	result := &CachedMatcher{Cached: native}
	c.wrappers[native] = result
	return result
}

// Clear releases both registry and adapter ownership.
func (c *MatcherCache) Clear() {
	c.Cache.Clear()
	c.wrappersMu.Lock()
	clear(c.wrappers)
	c.wrappersMu.Unlock()
}

// CachedMatcher adapts the native Hit DTO for unported discovery callers.
type CachedMatcher struct{ *Cached }

// Match returns a private legacy JSON view of the native result.
func (m *CachedMatcher) Match(host string) *jsonx.Object {
	hit := m.Cached.Match(host)
	if hit == nil {
		return nil
	}
	data, _ := json.Marshal(hit)
	value, _ := jsonx.Parse(data)
	return value.(*jsonx.Object)
}
