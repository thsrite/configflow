package rules

import (
	"configflow/internal/jsondecode"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"configflow/internal/clock"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/model"
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

// CacheOptions wires the cache to its environment. Every field except
// RulesDir is optional.
type CacheOptions struct {
	// RulesDir is the shared rule cache directory (DATA_DIR/shared/rules).
	RulesDir func() string
	// StoredLibrary returns the current shared rule_library from the
	// repository (RuleCacheReadPath consults it for legacy cache names).
	StoredLibrary func() []model.RuleLibraryItem
	// ReadPath lets a native owner validate a current declaration inside its
	// borrowed repository View. When set, StoredLibrary is never copied/read.
	ReadPath func(model.RuleConfig, []model.RuleLibraryItem, string) string
	// ValidateRemote checks a url source's cache file against its
	// declaration; an error marks the file unusable. nil accepts everything.
	ValidateRemote func(content string, rule model.RuleConfig) error
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

func (c *Cache) readPath(rule model.RuleConfig, library, stored []model.RuleLibraryItem, directory string) string {
	if c.opt.ReadPath != nil {
		return c.opt.ReadPath(rule, library, directory)
	}
	return CacheReadPath(rule, library, stored, directory)
}

// Cache is the process-wide registry of Cached values, keyed by
// (rules directory, profile id).
type Cache struct {
	opt CacheOptions

	mu         sync.Mutex
	order      *list.List // front = oldest
	slots      map[cacheSlot]*list.Element
	timer      clock.Timer
	cleanup    *cleanupWait
	origin     time.Time
	generation int
}

type cacheSlot struct{ rulesDir, profile string }

type cacheEntry struct {
	slot      cacheSlot
	signature [32]byte
	matcher   *Cached
}

type cleanupWait struct{ stop chan struct{} }

// NewCache creates a cache.
func NewCache(opt CacheOptions) *Cache {
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
		opt.Clock = clock.Real{}
	}
	if opt.Logger == nil {
		opt.Logger = logx.Get("backend.utils.domain_matcher_cache")
	}
	if opt.StoredLibrary == nil {
		opt.StoredLibrary = func() []model.RuleLibraryItem { return nil }
	}
	return &Cache{opt: opt, origin: opt.Clock.Now(), order: list.New(), slots: map[cacheSlot]*list.Element{}}
}

// readKey names what a load read: a library entry, or the individual item.
type readKey struct {
	library string
	item    *model.RuleConfig
}

// libraryKey opts into a shared index only when loading is wholly decided
// by the library entry (a missing name falls back to the item's name).
func libraryKey(library *model.RuleLibraryItem) (string, bool) {
	if library != nil && library.ID != "" && library.Name != "" {
		return library.ID, true
	}
	return "", false
}

func readKeyOf(item *model.RuleConfig, library *model.RuleLibraryItem) readKey {
	if key, ok := libraryKey(library); ok {
		return readKey{library: key}
	}
	return readKey{item: item}
}

// localSource loads rule text from the local cache files or inline content,
// with retry state and no reference back to its owner.
type localSource struct {
	cache    *Cache
	rulesDir string
	stamps   map[string]*fileStamp
	library  []model.RuleLibraryItem
	paths    map[readKey]string

	mu       sync.Mutex // guards failed, retryAt
	failed   map[readKey]struct{}
	retryAt  float64
	hasRetry bool
}

func (s *localSource) retry(item *model.RuleConfig, library *model.RuleLibraryItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed[readKeyOf(item, library)] = struct{}{}
	if !s.hasRetry {
		s.retryAt = s.cache.now() + s.cache.opt.ReadRetrySeconds
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

func (s *localSource) load(item *model.RuleConfig, library *model.RuleLibraryItem) (string, error) {
	source := *item
	if library != nil {
		source = LibraryRule(*library)
	}
	if nameOf(library, item) != "" {
		var stored []model.RuleLibraryItem
		if s.cache.opt.ReadPath == nil {
			stored = s.cache.opt.StoredLibrary()
		}
		path := s.cache.readPath(source, s.library, stored, s.rulesDir)
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
				if valueOr(source.SourceType, "url") == "url" && s.cache.opt.ValidateRemote != nil {
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
	if library != nil && valueOr(library.SourceType, "url") == "content" {
		content := valueOr(library.Content, "")
		return content, nil
	}
	// Unavailable URL sources never fetch from the network on this path.
	s.retry(item, library)
	return "", nil
}

// nameOf is the library entry's name, else the item's.
func nameOf(library *model.RuleLibraryItem, item *model.RuleConfig) string {
	if library != nil && library.Name != "" {
		return library.Name
	}
	return item.Name
}

// Cached is a RuleConfigMatcher with a bounded host result cache and
// retry handling for unavailable sources.
type Cached struct {
	cache *Cache
	// guarded by cache.mu
	lastUsed float64
	active   int

	mu      sync.Mutex
	results map[string]*list.Element
	order   *list.List // front = oldest; values are *hostResult
	source  *localSource
	matcher *Matcher
}

type hostResult struct {
	host   string
	result *Hit
}

// Match is the first hit for host, cached per host (also
// negative results). The returned object is a private copy.
func (m *Cached) Match(host string) *Hit {
	cache := m.cache
	// Never hold the registry lock while waiting for an index or doing I/O.
	cache.mu.Lock()
	m.active++
	m.lastUsed = cache.now()
	cache.mu.Unlock()
	defer func() {
		cache.mu.Lock()
		m.active--
		m.lastUsed = cache.now()
		cache.mu.Unlock()
	}()

	m.mu.Lock()
	defer m.mu.Unlock()
	source := m.source
	source.mu.Lock()
	due := source.hasRetry && cache.now() >= source.retryAt
	failed := source.failed
	if due {
		source.failed = map[readKey]struct{}{}
		source.hasRetry = false
	}
	source.mu.Unlock()
	if due {
		removed := m.matcher.InvalidateRulesets(func(item *model.RuleConfig, library *model.RuleLibraryItem) bool {
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
		copy := *result
		if result.RuleID != nil {
			id := *result.RuleID
			copy.RuleID = &id
		}
		return &copy
	}
	return nil
}

// HostResultKeys lists cached hosts, oldest first (diagnostics and tests).
func (m *Cached) HostResultKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for e := m.order.Front(); e != nil; e = e.Next() {
		keys = append(keys, e.Value.(*hostResult).host)
	}
	return keys
}

// RetryPending reports whether an unavailable source is waiting for a retry.
func (m *Cached) RetryPending() bool {
	m.source.mu.Lock()
	defer m.source.mu.Unlock()
	return m.source.hasRetry
}

// Matcher exposes the underlying matcher (index counts in tests).
func (m *Cached) Matcher() *Matcher { return m.matcher }

// MatcherInput is the narrow projection consumed by matching and cache keys.
// CacheLibrary contains only IDs/names for legacy filename disambiguation.
type MatcherInput struct {
	Rules        []model.RuleConfig
	Library      []model.RuleLibraryItem
	CacheLibrary []model.RuleLibraryItem
}

func (c *Cache) newCached(input MatcherInput, rulesDir string, stamps map[string]*fileStamp, paths map[readKey]string, now float64) *Cached {
	source := &localSource{cache: c, rulesDir: rulesDir, stamps: stamps, library: input.CacheLibrary, paths: paths, failed: map[readKey]struct{}{}}
	cm := &Cached{cache: c, lastUsed: now, results: map[string]*list.Element{}, order: list.New(), source: source}
	cm.matcher = NewMatcher(input.Rules, input.Library, source.load, func(item *model.RuleConfig, library *model.RuleLibraryItem) (string, bool) {
		return libraryKey(library)
	}, nil)
	return cm
}
func (c *Cache) signature(input MatcherInput, rulesDir string) ([32]byte, map[string]*fileStamp, map[readKey]string) {
	library := make(map[string]*model.RuleLibraryItem, len(input.Library))
	for i := range input.Library {
		library[input.Library[i].ID] = &input.Library[i]
	}
	referenced := make([]*model.RuleLibraryItem, 0)
	stamps := map[string]*fileStamp{}
	paths := map[readKey]string{}
	var stored []model.RuleLibraryItem
	if c.opt.ReadPath == nil {
		stored = c.opt.StoredLibrary()
	}
	for i := range input.Rules {
		item := &input.Rules[i]
		if ruleKind(item) != "ruleset" || !ruleEnabled(item) {
			continue
		}
		source := library[item.LibraryRuleID]
		referenced = append(referenced, source)
		if nameOf(source, item) == "" {
			continue
		}
		target := *item
		if source != nil {
			target = LibraryRule(*source)
		}
		path := c.readPath(target, input.CacheLibrary, stored, rulesDir)
		paths[readKeyOf(item, source)] = path
		info, err := os.Stat(path)
		if err != nil {
			stamps[path] = nil
		} else {
			stamps[path] = stampOf(info)
		}
	}
	// Native JSON hashes are process-private (D2); no persisted Python encoder.
	stampDoc := make(map[string]*[4]int64, len(stamps))
	for path, stamp := range stamps {
		if stamp == nil {
			stampDoc[path] = nil
		} else {
			stampDoc[path] = &[4]int64{int64(stamp.ino), int64(stamp.size), stamp.mtime, stamp.ctime}
		}
	}
	payload, _ := json.Marshal(struct {
		Rules   []model.RuleConfig
		Library []*model.RuleLibraryItem
		Stamps  map[string]*[4]int64
	}{input.Rules, referenced, stampDoc})
	return sha256.Sum256(payload), stamps, paths
}

func (c *Cache) evictIdleLocked(now float64) {
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

// now keeps TTL arithmetic monotonic when the injected clock provides it.
func (c *Cache) now() float64 { return c.opt.Clock.Now().Sub(c.origin).Seconds() }

func (c *Cache) scheduleCleanupLocked() {
	if c.order.Len() == 0 || c.timer != nil || c.opt.Spawner == nil {
		return
	}
	generation := c.generation
	// Wake no later than the earliest inactive expiry deadline.
	delay := c.opt.CleanupInterval
	now := c.now()
	for e := c.order.Front(); e != nil; e = e.Next() {
		m := e.Value.(*cacheEntry).matcher
		if m.active == 0 {
			delay = min(delay, max(0, c.opt.IdleSeconds-(now-m.lastUsed)))
		}
	}
	timer := c.opt.Clock.NewTimer(time.Duration(delay * float64(time.Second)))
	wait := &cleanupWait{stop: make(chan struct{})}
	c.timer, c.cleanup = timer, wait
	err := c.opt.Spawner.Spawn(context.Background(), "matcher-cache-cleanup", func(ctx context.Context) {
		defer func() {
			timer.Stop()
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.cleanup == wait {
				c.timer, c.cleanup = nil, nil
			}
		}()
		select {
		case <-ctx.Done():
		case <-wait.stop:
		case <-timer.C():
			c.cleanupIdle(generation)
		}
	})
	if err != nil {
		timer.Stop()
		c.timer, c.cleanup = nil, nil
		c.opt.Logger.Warningf("Rule index cleanup admission failed (%T)", err)
	}
}

func (c *Cache) cleanupIdle(generation int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return
	}
	c.timer, c.cleanup = nil, nil
	c.evictIdleLocked(c.now())
	c.scheduleCleanupLocked()
}

// Clear releases cache ownership and cancels the cleanup timer (shutdown and
// test isolation).
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if c.cleanup != nil {
		close(c.cleanup.stop)
		c.cleanup = nil
	}
	c.order.Init()
	c.slots = map[cacheSlot]*list.Element{}
}

// Size is the number of registered matchers.
func (c *Cache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// CleanupScheduled reports whether the idle-eviction timer is armed.
func (c *Cache) CleanupScheduled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.timer != nil
}

// EvictIdle runs one idle-eviction pass at elapsed seconds since cache creation.
// Normal lookups and background sweeps obtain that reading from Clock directly.
func (c *Cache) EvictIdle(now float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictIdleLocked(now)
}

// LocalMatcher is the matcher for a
// profile's rules, reused while rules, referenced library entries and cache
// file stamps are unchanged. config is a profile document merged with the
// shared one (rule_configs, rule_library).
func (c *Cache) LocalMatcher(input MatcherInput, profileID string) *Cached {
	rulesDir := c.opt.RulesDir()
	if real, err := filepath.EvalSymlinks(rulesDir); err == nil {
		rulesDir = real
	} else if abs, err := filepath.Abs(rulesDir); err == nil {
		rulesDir = abs
	}
	// Clone only consumed records, never nodes/subscriptions or a full Config.
	snapshot := MatcherInput{}
	data, _ := json.Marshal(input)
	_ = jsondecode.Unmarshal(data, &snapshot)
	signature, stamps, paths := c.signature(snapshot, rulesDir)
	slot := cacheSlot{rulesDir, profileID}
	now := c.now()

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
		entry = &cacheEntry{slot: slot, signature: signature, matcher: c.newCached(snapshot, rulesDir, stamps, paths, now)}
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
