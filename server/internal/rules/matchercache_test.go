package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

// Ported from backend/test_domain_matcher_cache.py. Tests that watched Python
// object lifetimes (weakref/GC) check the registry instead; tests that
// monkeypatched open() or _file_stamp use real files (chmod, replacing a file
// between the signature and the first read).

type testClock struct {
	mu  sync.Mutex
	now float64
}

func (c *testClock) get() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(seconds float64) {
	c.mu.Lock()
	c.now += seconds
	c.mu.Unlock()
}

type cacheFixture struct {
	t      *testing.T
	cache  *MatcherCache
	root   string
	clock  *testClock
	config *jsonx.Object
	opt    *MatcherCacheOptions
}

func newFixture(t *testing.T, tune func(*MatcherCacheOptions)) *cacheFixture {
	t.Helper()
	f := &cacheFixture{t: t, root: t.TempDir(), clock: &testClock{now: 1}}
	dir := f.root
	opt := MatcherCacheOptions{RulesDir: func() string { return dir }, Clock: f.clock.get}
	if tune != nil {
		tune(&opt)
	}
	f.cache = NewMatcherCache(opt)
	f.opt = &opt
	t.Cleanup(f.cache.Clear)
	f.config = obj(
		"rule_configs", seq(
			obj("id", "first", "itemType", "rule", "rule_type", "DOMAIN-SUFFIX", "value", "first.example", "policy", "DIRECT"),
			obj("id", "set", "itemType", "ruleset", "library_rule_id", "lib", "name", "domains", "policy", "PROXY"),
		),
		"rule_library", seq(obj("id", "lib", "name", "domains", "source_type", "content", "behavior", "classical",
			"content", "DOMAIN-SUFFIX,matched.example\n")),
	)
	return f
}

func (f *cacheFixture) write(name, content string) string {
	f.t.Helper()
	path := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func (f *cacheFixture) matcher(config *jsonx.Object, profile string) *CachedMatcher {
	return f.cache.LocalMatcher(config, profile)
}

func policyOf(t *testing.T, result *jsonx.Object) string {
	t.Helper()
	if result == nil {
		return "<nil>"
	}
	return result.GetString("policy")
}

func clone(config *jsonx.Object) *jsonx.Object { return config.DeepCopy() }

func TestReusesPositiveAndNegativeMatchesWithoutCachingTraffic(t *testing.T) {
	f := newFixture(t, nil)
	first := f.matcher(f.config, "default")
	result := first.Match("www.matched.example")
	result.Set("policy", "corrupted")
	if first.Match("missing.example") != nil {
		t.Fatal("missing.example matched")
	}
	changed := clone(f.config)
	changed.Set("agents", seq(obj("last_heartbeat", "new")))
	changed.Set("domain_discovery", obj("ignored", seq("ignored.example")))
	second := f.matcher(changed, "default")
	if second != first {
		t.Fatal("unrelated config changes replaced the matcher")
	}
	if got := policyOf(t, second.Match("www.matched.example")); got != "PROXY" {
		t.Fatalf("policy = %q (cached result was mutable)", got)
	}
	if second.Match("missing.example") != nil {
		t.Fatal("missing.example matched")
	}
	if keys := second.HostResultKeys(); len(keys) != 2 || keys[0] != "www.matched.example" || keys[1] != "missing.example" {
		t.Fatalf("host results %v", keys)
	}
}

func TestConfigEditsInvalidateImmediately(t *testing.T) {
	for _, edit := range []string{"inline", "policy", "enabled", "order"} {
		f := newFixture(t, nil)
		f.config.GetList("rule_library")[0].(*jsonx.Object).Set("content", "DOMAIN-SUFFIX,matched.example\nDOMAIN-SUFFIX,first.example\n")
		old := f.matcher(f.config, "default")
		if old.Match("new.example") != nil {
			t.Fatalf("%s: new.example matched", edit)
		}
		if got := policyOf(t, old.Match("first.example")); got != "DIRECT" {
			t.Fatalf("%s: policy = %q", edit, got)
		}
		changed := clone(f.config)
		rules := changed.GetList("rule_configs")
		switch edit {
		case "inline":
			lib := changed.GetList("rule_library")[0].(*jsonx.Object)
			lib.Set("content", lib.GetString("content")+"DOMAIN,new.example\n")
		case "policy":
			rules[0].(*jsonx.Object).Set("policy", "PROXY")
		case "enabled":
			rules[0].(*jsonx.Object).Set("enabled", false)
		default:
			rules[0], rules[1] = rules[1], rules[0]
		}
		fresh := f.matcher(changed, "default")
		if fresh == old {
			t.Fatalf("%s: stale matcher reused", edit)
		}
		query := "first.example"
		if edit == "inline" {
			query = "new.example"
		}
		if got := policyOf(t, fresh.Match(query)); got != "PROXY" {
			t.Fatalf("%s: policy = %q", edit, got)
		}
	}
}

func TestFileCreationReplacementAndDeletionInvalidateNegativeMatches(t *testing.T) {
	f := newFixture(t, nil)
	old := f.matcher(f.config, "default")
	if old.Match("remote.example") != nil {
		t.Fatal("remote.example matched")
	}
	path := f.write("domains.list", "DOMAIN,remote.example\n")
	created := f.matcher(f.config, "default")
	if created == old {
		t.Fatal("file creation did not invalidate")
	}
	if got := policyOf(t, created.Match("remote.example")); got != "PROXY" {
		t.Fatalf("policy = %q", got)
	}
	if created.Match("newone.example") != nil {
		t.Fatal("newone.example matched")
	}
	// Replace by rename, keeping size and mtime: only inode/ctime tell.
	info, _ := os.Stat(path)
	replacement := f.write("replacement", "DOMAIN,newone.example\n")
	if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	replaced := f.matcher(f.config, "default")
	if replaced == created {
		t.Fatal("replacement did not invalidate")
	}
	if replaced.Match("remote.example") != nil {
		t.Fatal("old content still matches")
	}
	if got := policyOf(t, replaced.Match("newone.example")); got != "PROXY" {
		t.Fatalf("policy = %q", got)
	}
	os.Remove(path)
	removed := f.matcher(f.config, "default")
	if removed.Match("newone.example") != nil {
		t.Fatal("deleted file still matches")
	}
	if got := policyOf(t, removed.Match("matched.example")); got != "PROXY" {
		t.Fatalf("inline fallback policy = %q", got)
	}
}

func TestParallelRefreshesComputeEachHostOnce(t *testing.T) {
	f := newFixture(t, nil)
	var wg sync.WaitGroup
	type pair struct{ a, b string }
	results := make([]pair, 8)
	matchers := make([]*CachedMatcher, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := f.matcher(clone(f.config), "default")
			matchers[i] = m
			results[i] = pair{policyOf(t, m.Match("matched.example")), policyOf(t, m.Match("missing.example"))}
		}()
	}
	wg.Wait()
	for i := range results {
		if results[i] != (pair{"PROXY", "<nil>"}) || matchers[i] != matchers[0] {
			t.Fatalf("result %d = %v (shared matcher %v)", i, results[i], matchers[i] == matchers[0])
		}
	}
	if keys := matchers[0].HostResultKeys(); len(keys) != 2 {
		t.Fatalf("host results %v", keys)
	}
}

func TestCacheIsBoundedAndProfilesAreIsolated(t *testing.T) {
	f := newFixture(t, func(o *MatcherCacheOptions) { o.MaxMatchers, o.MaxHostResults = 2, 2 })
	first := f.matcher(f.config, "first")
	second := f.matcher(f.config, "second")
	if first == second {
		t.Fatal("profiles share a matcher")
	}
	f.matcher(f.config, "third")
	if f.cache.Size() != 2 {
		t.Fatalf("size = %d", f.cache.Size())
	}
	if f.matcher(f.config, "first") == first {
		t.Fatal("evicted matcher came back")
	}
	for _, host := range []string{"a.example", "b.example", "c.example"} {
		first.Match(host)
	}
	if keys := first.HostResultKeys(); len(keys) != 2 || keys[0] != "b.example" || keys[1] != "c.example" {
		t.Fatalf("host results %v", keys)
	}
}

func TestHealthyIndexesSurviveMultipleAutomaticProbeRounds(t *testing.T) {
	f := newFixture(t, nil)
	f.write("domains.list", "DOMAIN-SUFFIX,matched.example\n")
	first := f.matcher(f.config, "default")
	if first.Match("matched.example") == nil {
		t.Fatal("no match")
	}
	slotCount := first.Matcher().IndexCount()
	for i := 0; i < 4; i++ {
		f.clock.advance(600)
		current := f.matcher(f.config, "default")
		if current != first {
			t.Fatalf("round %d replaced the matcher", i)
		}
		if current.Match("host"+string(rune('a'+i))+".matched.example") == nil {
			t.Fatalf("round %d: no match", i)
		}
		if current.Matcher().IndexCount() != slotCount {
			t.Fatalf("round %d rebuilt an index", i)
		}
	}
}

func TestIdleEvictionReleasesIndexesAndExistingHandlesRemainUsable(t *testing.T) {
	f := newFixture(t, nil)
	first := f.matcher(f.config, "first")
	first.Match("matched.example")
	kept := f.matcher(f.config, "kept")
	f.clock.advance(DefaultIdleSeconds)
	f.cache.EvictIdle(f.clock.get())
	if f.cache.Size() != 0 {
		t.Fatalf("size = %d", f.cache.Size())
	}
	if got := policyOf(t, kept.Match("matched.example")); got != "PROXY" {
		t.Fatalf("policy = %q", got)
	}
}

func TestBackgroundCleanupRunsWithoutAnotherRequest(t *testing.T) {
	f := newFixture(t, func(o *MatcherCacheOptions) {
		start := time.Now()
		o.Clock = func() float64 { return time.Since(start).Seconds() }
		o.IdleSeconds, o.CleanupInterval = 0.025, 0.01
	})
	f.matcher(f.config, "default").Match("matched.example")
	deadline := time.Now().Add(2 * time.Second)
	for f.cache.Size() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.cache.Size() != 0 {
		t.Fatal("idle matcher not evicted")
	}
	time.Sleep(30 * time.Millisecond)
	if f.cache.CleanupScheduled() {
		t.Fatal("cleanup timer still armed with an empty registry")
	}
}

func TestClearCancelsCleanupAndStaleCallbacksCannotReschedule(t *testing.T) {
	f := newFixture(t, nil)
	f.matcher(f.config, "default")
	f.cache.mu.Lock()
	generation := f.cache.generation
	f.cache.mu.Unlock()
	if !f.cache.CleanupScheduled() {
		t.Fatal("no cleanup timer")
	}
	f.cache.Clear()
	if f.cache.CleanupScheduled() {
		t.Fatal("Clear left the timer armed")
	}
	current := f.matcher(f.config, "default")
	f.cache.mu.Lock()
	timer := f.cache.timer
	f.cache.mu.Unlock()
	f.cache.cleanupIdle(generation)
	f.cache.mu.Lock()
	same := f.cache.timer == timer
	f.cache.mu.Unlock()
	if !same {
		t.Fatal("a stale callback rescheduled the timer")
	}
	if f.matcher(f.config, "default") != current {
		t.Fatal("matcher replaced")
	}
}

func TestMatchTouchesIdleTimeAndActiveMatchIsNeverExpired(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f := newFixture(t, func(o *MatcherCacheOptions) {
		o.ValidateRemote = func(string, *jsonx.Object) error {
			once.Do(func() { close(entered) })
			<-release
			return nil
		}
	})
	// A url source with an existing cache file makes the lazy load block in
	// the validation hook.
	lib := f.config.GetList("rule_library")[0].(*jsonx.Object)
	lib.Set("source_type", "url")
	lib.Set("url", "https://rules.test/domains")
	f.write(RuleCacheFilename(lib), "DOMAIN-SUFFIX,matched.example\n")
	matcher := f.matcher(f.config, "default")
	f.clock.advance(DefaultIdleSeconds - 1)
	done := make(chan *jsonx.Object, 1)
	go func() { done <- matcher.Match("new.matched.example") }()
	<-entered
	f.clock.advance(DefaultIdleSeconds + 1)
	f.cache.EvictIdle(f.clock.get())
	if f.cache.Size() != 1 {
		t.Fatal("an active matcher was evicted")
	}
	// Registry acquisition must not wait for the active index lock.
	if f.matcher(f.config, "default") != matcher {
		t.Fatal("matcher replaced while active")
	}
	close(release)
	if result := <-done; result == nil {
		t.Fatal("no match")
	}
	f.cache.mu.Lock()
	active, last := matcher.active, matcher.lastUsed
	f.cache.mu.Unlock()
	if active != 0 || last != f.clock.get() {
		t.Fatalf("active=%d lastUsed=%v clock=%v", active, last, f.clock.get())
	}
}

func TestFailedFileRetriesWithoutRebuildingHealthyIndexes(t *testing.T) {
	f := newFixture(t, nil)
	f.config.Set("rule_configs", seq(
		obj("id", "broken", "itemType", "ruleset", "library_rule_id", "broken", "policy", "FIRST"),
		f.config.GetList("rule_configs")[1],
	))
	f.config.GetList("rule_library")
	f.config.Set("rule_library", append(f.config.GetList("rule_library"), obj("id", "broken", "name", "broken",
		"source_type", "content", "behavior", "classical", "content", "DOMAIN,fallback.example")))
	broken := f.write("broken.list", "DOMAIN,matched.example\nDOMAIN,recovered.example")
	f.write("domains.list", "DOMAIN,matched.example")
	if err := os.Chmod(broken, 0); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(broken); err == nil {
		f.Close()
		t.Skip("running with privileges that ignore file modes")
	}
	matcher := f.matcher(f.config, "default")
	if got := policyOf(t, matcher.Match("fallback.example")); got != "FIRST" {
		t.Fatalf("fallback policy = %q", got)
	}
	if got := policyOf(t, matcher.Match("matched.example")); got != "PROXY" {
		t.Fatalf("policy = %q", got)
	}
	if matcher.Match("recovered.example") != nil {
		t.Fatal("recovered.example matched while the file was unreadable")
	}
	healthy := matcherIndexFor(matcher, "lib")
	if healthy == nil {
		t.Fatal("no index for the healthy source")
	}
	if err := os.Chmod(broken, 0o644); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(DefaultReadRetry - 1)
	if got := policyOf(t, matcher.Match("matched.example")); got != "PROXY" {
		t.Fatalf("policy before retry = %q", got)
	}
	f.clock.advance(1)
	if got := policyOf(t, matcher.Match("matched.example")); got != "FIRST" {
		t.Fatalf("policy after retry = %q", got)
	}
	if got := policyOf(t, matcher.Match("recovered.example")); got != "FIRST" {
		t.Fatalf("recovered policy = %q", got)
	}
	if matcher.Match("fallback.example") != nil {
		t.Fatal("inline fallback still used after recovery")
	}
	if matcherIndexFor(matcher, "lib") != healthy {
		t.Fatal("healthy index was rebuilt")
	}
	// chmod moved the file's ctime, so the first successful read still differs
	// from the stamp taken at signature time and schedules one more bounded
	// retry (Python's open() monkeypatch left the stamps alone); the second
	// round settles.
	f.clock.advance(DefaultReadRetry)
	if got := policyOf(t, matcher.Match("recovered.example")); got != "FIRST" {
		t.Fatalf("recovered policy after settling = %q", got)
	}
	if matcher.RetryPending() {
		t.Fatal("retry still pending")
	}
	if matcherIndexFor(matcher, "lib") != healthy {
		t.Fatal("healthy index was rebuilt")
	}
}

// matcherIndexFor returns the built index of the source with the library id.
func matcherIndexFor(m *CachedMatcher, libraryID string) *ruleIndex {
	m.matcher.mu.RLock()
	defer m.matcher.mu.RUnlock()
	for slot, index := range m.matcher.indexes {
		if slot.source == libraryID {
			return index
		}
	}
	return nil
}

func TestLegitimateEmptyFileIsReusedAndChangeIsDetected(t *testing.T) {
	f := newFixture(t, nil)
	path := f.write("domains.list", "")
	matcher := f.matcher(f.config, "default")
	if matcher.Match("matched.example") != nil {
		t.Fatal("matched an empty file")
	}
	if matcher.RetryPending() {
		t.Fatal("an empty file scheduled a retry")
	}
	before := matcherIndexFor(matcher, "lib")
	f.clock.advance(600)
	if f.matcher(f.config, "default") != matcher {
		t.Fatal("matcher replaced")
	}
	if matcher.Match("still.missing.example") != nil {
		t.Fatal("matched")
	}
	if matcherIndexFor(matcher, "lib") != before {
		t.Fatal("index rebuilt")
	}
	if err := os.WriteFile(path, []byte("DOMAIN,matched.example"), 0o644); err != nil {
		t.Fatal(err)
	}
	if f.matcher(f.config, "default").Match("matched.example") == nil {
		t.Fatal("change not detected")
	}
}

func TestMissingURLSourceStaysLocalAndRecoversWhenFileAppears(t *testing.T) {
	f := newFixture(t, nil)
	lib := f.config.GetList("rule_library")[0].(*jsonx.Object)
	lib.Set("source_type", "url")
	lib.Set("url", "https://must-not-fetch.invalid/rules")
	old := f.matcher(f.config, "default")
	if old.Match("matched.example") != nil {
		t.Fatal("matched without a cache file")
	}
	if !old.RetryPending() {
		t.Fatal("missing source did not schedule a retry")
	}
	f.write(RuleCacheFilename(lib), "DOMAIN,matched.example")
	current := f.matcher(f.config, "default")
	if current == old {
		t.Fatal("new file did not replace the matcher")
	}
	if current.Match("matched.example") == nil {
		t.Fatal("no match")
	}
}

func TestLazyLoadUsesFrozenConfigAndRepository(t *testing.T) {
	f := newFixture(t, nil)
	f.write("domains.list", "DOMAIN,original.example")
	matcher := f.matcher(f.config, "default")
	other := filepath.Join(f.root, "other")
	f.write("other/domains.list", "DOMAIN,changed.example")
	f.config.GetList("rule_configs")[1].(*jsonx.Object).Set("policy", "CHANGED")
	f.config.GetList("rule_library")[0].(*jsonx.Object).Set("content", "DOMAIN,changed.example")
	// Another repository would resolve elsewhere: swap the directory source.
	f.opt.RulesDir = func() string { return other }
	f.cache.opt.RulesDir = f.opt.RulesDir
	if got := policyOf(t, matcher.Match("original.example")); got != "PROXY" {
		t.Fatalf("policy = %q", got)
	}
	if matcher.Match("changed.example") != nil {
		t.Fatal("lazy load escaped its snapshot")
	}
	current := f.matcher(f.config, "default")
	if current == matcher {
		t.Fatal("matcher reused across repositories")
	}
	if got := policyOf(t, current.Match("changed.example")); got != "CHANGED" {
		t.Fatalf("policy = %q", got)
	}
}

func TestDuplicateLibraryReferencesShareIndexButKeepFirstRuleMetadata(t *testing.T) {
	f := newFixture(t, nil)
	rules := f.config.GetList("rule_configs")
	later := rules[1].(*jsonx.Object).Clone()
	later.Set("id", "later")
	later.Set("policy", "LATER")
	f.config.Set("rule_configs", append(rules, later))
	matcher := f.matcher(f.config, "default")
	if matcher.Match("missing.example") != nil { // visits both references
		t.Fatal("matched")
	}
	if matcher.Matcher().IndexCount() != 1 {
		t.Fatalf("index count = %d", matcher.Matcher().IndexCount())
	}
	hit := matcher.Match("matched.example")
	if hit.Value("rule_id") != "set" || hit.Value("policy") != "PROXY" || jsonx.Str(hit.Value("priority")) != "2" {
		t.Fatalf("hit = %s", jsonx.Repr(hit))
	}
}

func TestReplacedFileBetweenSignatureAndReadIsRetried(t *testing.T) {
	// Python replaced the file while the first read was running. Here the file
	// changes after the stamps were taken and before the lazy read; either way
	// the stale stamp must schedule a bounded retry.
	f := newFixture(t, nil)
	path := f.write("domains.list", "DOMAIN,old.example")
	matcher := f.matcher(f.config, "default")
	replacement := f.write("replacement", "DOMAIN,new.example")
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if matcher.Match("new.example") == nil {
		t.Fatal("the lazy read did not see the file on disk")
	}
	if !matcher.RetryPending() {
		t.Fatal("stale signature stamp did not schedule a retry")
	}
	f.clock.advance(DefaultReadRetry)
	if matcher.Match("new.example") == nil || matcher.Match("old.example") != nil {
		t.Fatal("content after retry is wrong")
	}
}

func TestMissingExpectedFileRetriesInlineFallbackOnExistingHandle(t *testing.T) {
	f := newFixture(t, nil)
	path := f.write("domains.list", "DOMAIN,file.example")
	matcher := f.matcher(f.config, "default")
	os.Remove(path)
	if matcher.Match("matched.example") == nil {
		t.Fatal("inline fallback not used")
	}
	if matcher.Match("file.example") != nil {
		t.Fatal("deleted file still matched")
	}
	if !matcher.RetryPending() {
		t.Fatal("no retry scheduled")
	}
	f.write("domains.list", "DOMAIN,file.example")
	f.clock.advance(DefaultReadRetry)
	if matcher.Match("file.example") == nil || matcher.Match("matched.example") != nil {
		t.Fatal("recovered content wrong")
	}
	// One further stable read acknowledges the changed stamp; it must not make
	// every later request re-read a healthy source.
	f.clock.advance(DefaultReadRetry)
	if matcher.Match("file.example") == nil {
		t.Fatal("no match")
	}
	index := matcherIndexFor(matcher, "lib")
	if matcher.RetryPending() {
		t.Fatal("retry still pending after a stable read")
	}
	f.clock.advance(DefaultReadRetry)
	if matcher.Match("another.example") != nil || matcherIndexFor(matcher, "lib") != index {
		t.Fatal("healthy index was rebuilt")
	}
}

// test_replaced_file_during_lazy_read_is_retried_on_same_handle: the path is
// replaced atomically after the handle's fstat-before/read/fstat-after; the
// old content is served once and the stat of the path schedules the retry.
func TestReplacedFileDuringLazyReadIsRetriedOnSameHandle(t *testing.T) {
	f := newFixture(t, nil)
	path := f.write("domains.list", "DOMAIN,old.example")
	matcher := f.matcher(f.config, "default")
	replaced := false
	testHookAfterRead = func(string) {
		if !replaced {
			replaced = true
			replacement := f.write("replacement", "DOMAIN,new.example")
			if err := os.Rename(replacement, path); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { testHookAfterRead = nil })
	if matcher.Match("old.example") == nil {
		t.Fatal("the content read through the old handle was not used")
	}
	if matcher.Match("new.example") != nil {
		t.Fatal("new content matched before the retry")
	}
	if !matcher.RetryPending() {
		t.Fatal("no retry scheduled")
	}
	f.clock.advance(DefaultReadRetry)
	if matcher.Match("new.example") == nil || matcher.Match("old.example") != nil {
		t.Fatal("content after retry is wrong")
	}
}

// Review C09: concurrent handles, config edits, file replacement, eviction
// and Clear (run with -race).
func TestConcurrentCacheAccess(t *testing.T) {
	f := newFixture(t, func(o *MatcherCacheOptions) { o.MaxMatchers = 2; o.CleanupInterval = 0.001 })
	f.write("domains.list", "DOMAIN,file.example")
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				config := clone(f.config)
				if i%3 == 0 {
					config.GetList("rule_configs")[0].(*jsonx.Object).Set("value", "edit.example")
				}
				matcher := f.matcher(config, []string{"a", "b", "c"}[(worker+i)%3])
				if matcher.Match("matched.example") == nil && matcher.Match("file.example") == nil {
					t.Error("neither the inline nor the file content matched")
					return
				}
				switch i % 50 {
				case 7:
					f.cache.EvictIdle(f.clock.get() + DefaultIdleSeconds)
				case 13:
					f.clock.advance(DefaultReadRetry)
				case 29:
					if worker == 0 {
						f.cache.Clear()
					}
				case 41:
					replacement := f.write(fmt.Sprintf("replacement-%d", worker), "DOMAIN,file.example\n")
					_ = os.Rename(replacement, filepath.Join(f.root, "domains.list"))
				}
			}
		}(worker)
	}
	wg.Wait()
	if size := f.cache.Size(); size > 2 {
		t.Fatalf("cache size %d beyond MaxMatchers", size)
	}
}
