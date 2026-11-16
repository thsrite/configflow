package rules

import (
	"container/list"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

// Bounded, idle-evicted local rule indexes (backend/utils/domain_matcher_cache.py)
// for domain discovery. Never caches Agent or traffic status.

// Defaults of the cache limits.
const (
	DefaultMaxMatchers     = 4
	DefaultMaxHostResults  = 10000
	DefaultIdleSeconds     = 1800 // reuse across the automatic probe's 10-minute rounds
	DefaultReadRetry       = 30
	DefaultCleanupInterval = 60
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
	// Clock is a monotonic clock in seconds. nil uses the process clock.
	Clock  func() float64
	Logger Logger

	// Tunables, normally the Default* constants (tests shrink them).
	MaxMatchers, MaxHostResults                    int
	IdleSeconds, ReadRetrySeconds, CleanupInterval float64
}

// MatcherCache is the process-wide registry of CachedMatcher values, keyed by
// (rules directory, profile id).
type MatcherCache struct {
	opt MatcherCacheOptions

	mu         sync.Mutex
	order      *list.List // front = oldest
	slots      map[cacheSlot]*list.Element
	timer      *time.Timer
	generation int
}

type cacheSlot struct{ rulesDir, profile string }

type cacheEntry struct {
	slot      cacheSlot
	signature [32]byte
	matcher   *CachedMatcher
}

var processStart = time.Now()

// NewMatcherCache creates a cache.
func NewMatcherCache(opt MatcherCacheOptions) *MatcherCache {
	if opt.MaxMatchers == 0 {
		opt.MaxMatchers = DefaultMaxMatchers
	}
	if opt.MaxHostResults == 0 {
		opt.MaxHostResults = DefaultMaxHostResults
	}
	if opt.IdleSeconds == 0 {
		opt.IdleSeconds = DefaultIdleSeconds
	}
	if opt.ReadRetrySeconds == 0 {
		opt.ReadRetrySeconds = DefaultReadRetry
	}
	if opt.CleanupInterval == 0 {
		opt.CleanupInterval = DefaultCleanupInterval
	}
	if opt.Clock == nil {
		opt.Clock = func() float64 { return time.Since(processStart).Seconds() }
	}
	if opt.Logger == nil {
		opt.Logger = logx.Get("backend.utils.domain_matcher_cache")
	}
	if opt.StoredLibrary == nil {
		opt.StoredLibrary = func() []any { return nil }
	}
	return &MatcherCache{opt: opt, order: list.New(), slots: map[cacheSlot]*list.Element{}}
}

// readKey names what a load read: a library entry, or the individual item.
type readKey struct {
	library string
	item    *jsonx.Object
}

// libraryKey opts into a shared index only when loading is wholly decided
// by the library entry (a missing name falls back to the item's name).
func libraryKey(library *jsonx.Object) (string, bool) {
	if library != nil && library.GetString("id") != "" && library.GetString("name") != "" {
		return library.GetString("id"), true
	}
	return "", false
}

func readKeyOf(item, library *jsonx.Object) readKey {
	if key, ok := libraryKey(library); ok {
		return readKey{library: key}
	}
	return readKey{item: item}
}

// localSource loads rule text from the local cache files or inline content,
// with retry state and no reference back to its owner.
type localSource struct {
	cache    *MatcherCache
	rulesDir string
	stamps   map[string]*fileStamp
	library  []any
	paths    map[readKey]string

	mu       sync.Mutex // guards failed, retryAt
	failed   map[readKey]struct{}
	retryAt  float64
	hasRetry bool
}

func (s *localSource) retry(item, library *jsonx.Object) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed[readKeyOf(item, library)] = struct{}{}
	if !s.hasRetry {
		s.retryAt = s.cache.opt.Clock() + s.cache.opt.ReadRetrySeconds
		s.hasRetry = true
	}
}

// readTextFile reads a UTF-8 text file and returns stamps of the open
// handle taken before and after the read.
func readTextFile(path string) (content string, before, after *fileStamp, err error) {
	handle, err := os.Open(path)
	if err != nil {
		return "", nil, nil, err
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return "", nil, nil, err
	}
	before = stampOf(info)
	data, err := io.ReadAll(handle)
	if err != nil {
		return "", nil, nil, err
	}
	info, err = handle.Stat()
	if err != nil {
		return "", nil, nil, err
	}
	after = stampOf(info)
	if !utf8.Valid(data) {
		return "", nil, nil, errUnicode
	}
	return string(data), before, after, nil
}

var errUnicode = errors.New("invalid utf-8")

// testHookAfterRead runs between the handle's last fstat and the path stat
// (tests replace the file there).
var testHookAfterRead func(path string)

func stampsEqual(a, b *fileStamp) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s *localSource) load(item, library *jsonx.Object) (string, error) {
	source := item
	if library != nil {
		source = library
	}
	if nameOf(library, item) != "" {
		path := RuleCacheReadPath(source, s.library, s.cache.opt.StoredLibrary(), s.rulesDir)
		key := readKeyOf(item, library)
		if previous, ok := s.paths[key]; ok && previous != path {
			s.retry(item, library)
		}
		s.paths[key] = path
		content, before, after, err := readTextFile(path)
		if err == nil {
			if testHookAfterRead != nil {
				testHookAfterRead(path)
			}
			info, statErr := os.Stat(path)
			if statErr != nil {
				err = statErr
			} else {
				current := stampOf(info)
				// A lazy read may happen after the signature was taken. Do not
				// retain a raced result indefinitely, even through an old handle.
				if !stampsEqual(before, after) || !stampsEqual(after, s.stamps[path]) || !stampsEqual(current, after) {
					s.retry(item, library)
				}
				if stampsEqual(before, after) && stampsEqual(after, current) {
					// Keep an old handle's retry bounded after a stable read.
					s.stamps[path] = after
				}
				if source.GetOr("source_type", "url") == "url" && s.cache.opt.ValidateRemote != nil {
					err = s.cache.opt.ValidateRemote(content, source)
				}
				if err == nil {
					return content, nil
				}
			}
		}
		if errors.Is(err, fs.ErrNotExist) {
			if s.stamps[path] != nil {
				s.retry(item, library)
			}
		} else {
			s.retry(item, library)
		}
	}
	if library != nil && library.GetOr("source_type", "url") == "content" {
		content, _ := library.GetOr("content", "").(string)
		return content, nil
	}
	// Unavailable URL sources never fetch from the network on this path.
	s.retry(item, library)
	return "", nil
}

// nameOf is the library entry's name, else the item's.
func nameOf(library, item *jsonx.Object) string {
	if library != nil && library.GetString("name") != "" {
		return library.GetString("name")
	}
	return item.GetString("name")
}

// CachedMatcher is a RuleConfigMatcher with a bounded host result cache and
// retry handling for unavailable sources.
type CachedMatcher struct {
	cache *MatcherCache
	// guarded by cache.mu
	lastUsed float64
	active   int

	mu      sync.Mutex
	results map[string]*list.Element
	order   *list.List // front = oldest; values are *hostResult
	source  *localSource
	matcher *RuleConfigMatcher
}

type hostResult struct {
	host   string
	result *jsonx.Object
}

// Match is the first hit for host, cached per host (also
// negative results). The returned object is a private copy.
func (m *CachedMatcher) Match(host string) *jsonx.Object {
	cache := m.cache
	// Never hold the registry lock while waiting for an index or doing I/O.
	cache.mu.Lock()
	m.active++
	m.lastUsed = cache.opt.Clock()
	cache.mu.Unlock()
	defer func() {
		cache.mu.Lock()
		m.active--
		m.lastUsed = cache.opt.Clock()
		cache.mu.Unlock()
	}()

	m.mu.Lock()
	defer m.mu.Unlock()
	source := m.source
	source.mu.Lock()
	due := source.hasRetry && cache.opt.Clock() >= source.retryAt
	failed := source.failed
	if due {
		source.failed = map[readKey]struct{}{}
		source.hasRetry = false
	}
	source.mu.Unlock()
	if due {
		removed := m.matcher.InvalidateRulesets(func(item, library *jsonx.Object) bool {
			_, bad := failed[readKeyOf(item, library)]
			return bad
		})
		// A failed earlier rule can change both positive and negative results,
		// but unrelated parsed indexes remain reusable.
		m.results = map[string]*list.Element{}
		m.order.Init()
		cache.opt.Logger.Debugf("Rule index cache retried %d unavailable sources", removed)
	}
	element, ok := m.results[host]
	if !ok {
		element = m.order.PushBack(&hostResult{host, m.matcher.Match(host)})
		m.results[host] = element
		if m.order.Len() > cache.opt.MaxHostResults {
			oldest := m.order.Front()
			m.order.Remove(oldest)
			delete(m.results, oldest.Value.(*hostResult).host)
		}
	}
	m.order.MoveToBack(element)
	if result := element.Value.(*hostResult).result; result != nil {
		return result.DeepCopy()
	}
	return nil
}

// HostResultKeys lists cached hosts, oldest first (diagnostics and tests).
func (m *CachedMatcher) HostResultKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for e := m.order.Front(); e != nil; e = e.Next() {
		keys = append(keys, e.Value.(*hostResult).host)
	}
	return keys
}

// RetryPending reports whether an unavailable source is waiting for a retry.
func (m *CachedMatcher) RetryPending() bool {
	m.source.mu.Lock()
	defer m.source.mu.Unlock()
	return m.source.hasRetry
}

// Matcher exposes the underlying matcher (index counts in tests).
func (m *CachedMatcher) Matcher() *RuleConfigMatcher { return m.matcher }

func (c *MatcherCache) newCachedMatcher(config *jsonx.Object, rulesDir string, stamps map[string]*fileStamp, paths map[readKey]string, now float64) *CachedMatcher {
	library := config.GetList("cache_library")
	if !config.Has("cache_library") {
		library = config.GetList("rule_library")
	}
	source := &localSource{
		cache: c, rulesDir: rulesDir, stamps: stamps, library: library,
		paths: paths, failed: map[readKey]struct{}{},
	}
	cm := &CachedMatcher{
		cache: c, lastUsed: now, results: map[string]*list.Element{}, order: list.New(), source: source,
	}
	cm.matcher = NewRuleConfigMatcher(config.GetList("rule_configs"), config.GetList("rule_library"),
		source.load, func(item, library *jsonx.Object) (string, bool) { return libraryKey(library) }, nil)
	return cm
}

// signature is a digest of everything the matcher depends on
// (rules, referenced library entries, cache file stamps) plus the stamps and
// resolved paths the lazy loader will use.
func (c *MatcherCache) signature(config *jsonx.Object, rulesDir string) ([32]byte, map[string]*fileStamp, map[readKey]string) {
	rules := config.GetList("rule_configs")
	library := map[string]*jsonx.Object{}
	for _, entry := range config.GetList("rule_library") {
		if item, ok := entry.(*jsonx.Object); ok {
			library[item.GetString("id")] = item
		}
	}
	cacheLibrary := config.GetList("cache_library")
	if !config.Has("cache_library") {
		cacheLibrary = config.GetList("rule_library")
	}
	referenced := []any{}
	stamps := map[string]*fileStamp{}
	stampDoc := jsonx.NewObject()
	paths := map[readKey]string{}
	for _, entry := range rules {
		item, ok := entry.(*jsonx.Object)
		if !ok || item.Value("itemType") != "ruleset" || !enabled(item) {
			continue
		}
		source := library[item.GetString("library_rule_id")]
		if source != nil {
			referenced = append(referenced, source)
		} else {
			referenced = append(referenced, nil)
		}
		if nameOf(source, item) == "" {
			continue
		}
		target := item
		if source != nil {
			target = source
		}
		path := RuleCacheReadPath(target, cacheLibrary, c.opt.StoredLibrary(), rulesDir)
		paths[readKeyOf(item, source)] = path
		info, err := os.Stat(path)
		if err != nil {
			stamps[path] = nil
			stampDoc.Set(path, nil)
		} else {
			stamp := stampOf(info)
			stamps[path] = stamp
			stampDoc.Set(path, []any{jsonx.Int(stamp.ino), jsonx.Int(stamp.size), jsonx.Int(stamp.mtime), jsonx.Int(stamp.ctime)})
		}
	}
	payload, _ := jsonx.Sorted([]any{rules, referenced, stampDoc})
	return sha256.Sum256(payload), stamps, paths
}

func (c *MatcherCache) evictIdleLocked(now float64) {
	var stale []*list.Element
	for e := c.order.Front(); e != nil; e = e.Next() {
		m := e.Value.(*cacheEntry).matcher
		if m.active == 0 && now-m.lastUsed >= c.opt.IdleSeconds {
			stale = append(stale, e)
		}
	}
	for _, e := range stale {
		// Only release registry ownership. In-flight or previously returned
		// handles remain valid.
		delete(c.slots, e.Value.(*cacheEntry).slot)
		c.order.Remove(e)
	}
	if len(stale) > 0 {
		c.opt.Logger.Debugf("Rule index cache evicted %d idle matchers", len(stale))
	}
}

func (c *MatcherCache) scheduleCleanupLocked() {
	if c.order.Len() > 0 && c.timer == nil {
		generation := c.generation
		// A periodic sweep alone can retain an idle matcher for one extra full
		// interval. Wake no later than the earliest inactive expiry deadline.
		delay := c.opt.CleanupInterval
		now := c.opt.Clock()
		for e := c.order.Front(); e != nil; e = e.Next() {
			m := e.Value.(*cacheEntry).matcher
			if m.active == 0 {
				delay = min(delay, max(0, c.opt.IdleSeconds-(now-m.lastUsed)))
			}
		}
		c.timer = time.AfterFunc(time.Duration(delay*float64(time.Second)), func() { c.cleanupIdle(generation) })
	}
}

func (c *MatcherCache) cleanupIdle(generation int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return
	}
	c.timer = nil
	c.evictIdleLocked(c.opt.Clock())
	c.scheduleCleanupLocked()
}

// Clear releases cache ownership and cancels the cleanup timer (shutdown and
// test isolation).
func (c *MatcherCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.order.Init()
	c.slots = map[cacheSlot]*list.Element{}
}

// Size is the number of registered matchers.
func (c *MatcherCache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// CleanupScheduled reports whether the idle-eviction timer is armed.
func (c *MatcherCache) CleanupScheduled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.timer != nil
}

// EvictIdle runs one idle-eviction pass at the given clock reading.
func (c *MatcherCache) EvictIdle(now float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictIdleLocked(now)
}

// LocalMatcher is the matcher for a
// profile's rules, reused while rules, referenced library entries and cache
// file stamps are unchanged. config is a profile document merged with the
// shared one (rule_configs, rule_library).
func (c *MatcherCache) LocalMatcher(config *jsonx.Object, profileID string) *CachedMatcher {
	rulesDir := c.opt.RulesDir()
	if real, err := filepath.EvalSymlinks(rulesDir); err == nil {
		rulesDir = real
	} else if abs, err := filepath.Abs(rulesDir); err == nil {
		rulesDir = abs
	}
	// A request may mutate its config after returning this handle. Keep only
	// matcher inputs, using the same snapshot for the signature and lazy loads.
	rules := config.GetList("rule_configs")
	referenced := map[string]struct{}{}
	for _, entry := range rules {
		if item, ok := entry.(*jsonx.Object); ok && item.Value("itemType") == "ruleset" {
			referenced[item.GetString("library_rule_id")] = struct{}{}
		}
	}
	snapshot := jsonx.NewObject()
	snapshot.Set("rule_configs", jsonx.DeepCopy(rules))
	var kept, cacheLibrary []any
	for _, entry := range config.GetList("rule_library") {
		item, ok := entry.(*jsonx.Object)
		if !ok {
			continue
		}
		if _, hit := referenced[item.GetString("id")]; hit {
			kept = append(kept, item.DeepCopy())
		}
		// Only names/IDs are needed to disambiguate old caches.
		cacheLibrary = append(cacheLibrary, jsonx.Obj("id", item.Value("id"), "name", item.GetOr("name", "")))
	}
	if kept == nil {
		kept = []any{}
	}
	if cacheLibrary == nil {
		cacheLibrary = []any{}
	}
	snapshot.Set("rule_library", kept)
	snapshot.Set("cache_library", cacheLibrary)
	signature, stamps, paths := c.signature(snapshot, rulesDir)
	slot := cacheSlot{rulesDir, profileID}
	now := c.opt.Clock()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictIdleLocked(now)
	var entry *cacheEntry
	if element, ok := c.slots[slot]; ok {
		entry = element.Value.(*cacheEntry)
	}
	if entry == nil || entry.signature != signature {
		reason := "missing"
		if entry != nil {
			reason = "changed"
			c.order.Remove(c.slots[slot])
			delete(c.slots, slot)
		}
		entry = &cacheEntry{slot: slot, signature: signature, matcher: c.newCachedMatcher(snapshot, rulesDir, stamps, paths, now)}
		c.slots[slot] = c.order.PushBack(entry)
		c.opt.Logger.Debugf("Rule index cache created matcher (%s)", reason)
	}
	entry.matcher.lastUsed = now
	c.order.MoveToBack(c.slots[slot])
	for c.order.Len() > c.opt.MaxMatchers {
		oldest := c.order.Front()
		delete(c.slots, oldest.Value.(*cacheEntry).slot)
		c.order.Remove(oldest)
	}
	c.scheduleCleanupLocked()
	return entry.matcher
}
