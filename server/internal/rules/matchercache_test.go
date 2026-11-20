package rules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"configflow/internal/clock"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/model"
)

// Ported from backend/test_domain_matcher_cache.py. Tests that watched Python
// object lifetimes (weakref/GC) check the registry instead; tests that
// monkeypatched open() or _file_stamp use real files (chmod, replacing a file
// between the signature and the first read).

type testClock struct {
	*lifecycletest.Clock
	origin time.Time
}

func newCacheClock() *testClock {
	origin := time.Unix(0, 0)
	return &testClock{Clock: lifecycletest.NewClock(origin), origin: origin}
}
func (c *testClock) get() float64 { return c.Now().Sub(c.origin).Seconds() }
func (c *testClock) advance(seconds float64) {
	c.Advance(time.Duration(seconds * float64(time.Second)))
}

type cacheTestSpawner struct{ wg sync.WaitGroup }

func (s *cacheTestSpawner) Spawn(ctx context.Context, _ string, fn func(context.Context)) error {
	s.wg.Go(func() { fn(ctx) })
	return nil
}

type cacheFixture struct {
	t      *testing.T
	cache  *Cache
	root   string
	clock  *testClock
	config MatcherInput
	opt    *CacheOptions
}

func newFixture(t *testing.T, tune func(*CacheOptions)) *cacheFixture {
	t.Helper()
	f := &cacheFixture{t: t, root: t.TempDir(), clock: newCacheClock()}
	dir := f.root
	spawner := &cacheTestSpawner{}
	t.Cleanup(spawner.wg.Wait)
	opt := CacheOptions{RulesDir: func() string { return dir }, Clock: f.clock, Spawner: spawner}
	if tune != nil {
		tune(&opt)
	}
	f.cache = NewCache(opt)
	f.opt = &opt
	t.Cleanup(f.cache.Clear)
	f.config = MatcherInput{
		Rules: []model.RuleConfig{
			{ID: "first", ItemType: "rule", RuleType: "DOMAIN-SUFFIX", Value: "first.example", Policy: strp("DIRECT")},
			{ID: "set", ItemType: "ruleset", LibraryRuleID: "lib", Name: "domains", Policy: strp("PROXY")},
		},
		Library: []model.RuleLibraryItem{
			{ID: "lib", Name: "domains", SourceType: strp("content"), Behavior: strp("classical"), Content: strp("DOMAIN-SUFFIX,matched.example\n")},
		},
		CacheLibrary: []model.RuleLibraryItem{
			{ID: "lib", Name: "domains"},
		},
	}
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

func (f *cacheFixture) matcher(input MatcherInput, profile string) *Cached {
	return f.cache.LocalMatcher(input, profile)
}

func policyOf(t *testing.T, result *Hit) string {
	t.Helper()
	if result == nil {
		return "<nil>"
	}
	return result.Policy
}

func cloneInput(in MatcherInput) MatcherInput {
	out := MatcherInput{
		Rules:        append([]model.RuleConfig(nil), in.Rules...),
		Library:      append([]model.RuleLibraryItem(nil), in.Library...),
		CacheLibrary: append([]model.RuleLibraryItem(nil), in.CacheLibrary...),
	}
	for i := range out.Rules {
		if out.Rules[i].Enabled != nil {
			v := *out.Rules[i].Enabled
			out.Rules[i].Enabled = &v
		}
		if out.Rules[i].Policy != nil {
			v := *out.Rules[i].Policy
			out.Rules[i].Policy = &v
		}
	}
	for i := range out.Library {
		if out.Library[i].Content != nil {
			v := *out.Library[i].Content
			out.Library[i].Content = &v
		}
		if out.Library[i].SourceType != nil {
			v := *out.Library[i].SourceType
			out.Library[i].SourceType = &v
		}
		if out.Library[i].URL != nil {
			v := *out.Library[i].URL
			out.Library[i].URL = &v
		}
	}
	return out
}

func TestReusesPositiveAndNegativeMatchesWithoutCachingTraffic(t *testing.T) {
	f := newFixture(t, nil)
	first := f.matcher(f.config, "default")
	result := first.Match("www.matched.example")
	result.Policy = "corrupted"
	if first.Match("missing.example") != nil {
		t.Fatal("missing.example matched")
	}
	changed := cloneInput(f.config)
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
		f.config.Library[0].Content = strp("DOMAIN-SUFFIX,matched.example\nDOMAIN-SUFFIX,first.example\n")
		old := f.matcher(f.config, "default")
		if old.Match("new.example") != nil {
			t.Fatalf("%s: new.example matched", edit)
		}
		if got := policyOf(t, old.Match("first.example")); got != "DIRECT" {
			t.Fatalf("%s: policy = %q", edit, got)
		}
		changed := cloneInput(f.config)
		switch edit {
		case "inline":
			changed.Library[0].Content = strp(*changed.Library[0].Content + "DOMAIN,new.example\n")
		case "policy":
			changed.Rules[0].Policy = strp("PROXY")
		case "enabled":
			changed.Rules[0].Enabled = boolp(false)
		default:
			changed.Rules[0], changed.Rules[1] = changed.Rules[1], changed.Rules[0]
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
	matchers := make([]*Cached, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := f.matcher(cloneInput(f.config), "default")
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
	f := newFixture(t, func(o *CacheOptions) { o.MaxMatchers, o.MaxHostResults = 2, 2 })
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
	f := newFixture(t, func(o *CacheOptions) {
		o.Clock = clock.Real{}
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
	f := newFixture(t, func(o *CacheOptions) {
		o.ValidateRemote = func(string, model.RuleConfig) error {
			once.Do(func() { close(entered) })
			<-release
			return nil
		}
	})
	// A url source with an existing cache file makes the lazy load block in
	// the validation hook.
	f.config.Library[0].SourceType = strp("url")
	f.config.Library[0].URL = strp("https://rules.test/domains")
	f.write(CacheFilename(LibraryRule(f.config.Library[0])), "DOMAIN-SUFFIX,matched.example\n")
	matcher := f.matcher(f.config, "default")
	f.clock.advance(DefaultIdleSeconds - 1)
	done := make(chan *Hit, 1)
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
	f.config.Rules = []model.RuleConfig{
		{ID: "broken", ItemType: "ruleset", LibraryRuleID: "broken", Policy: strp("FIRST")},
		f.config.Rules[1],
	}
	f.config.Library = append(f.config.Library, model.RuleLibraryItem{
		ID: "broken", Name: "broken", SourceType: strp("content"), Behavior: strp("classical"),
		Content: strp("DOMAIN,fallback.example"),
	})
	f.config.CacheLibrary = append(f.config.CacheLibrary, model.RuleLibraryItem{ID: "broken", Name: "broken"})
	broken := f.write("broken.list", "DOMAIN,matched.example\nDOMAIN,recovered.example")
	f.write("domains.list", "DOMAIN,matched.example")
	if err := os.Chmod(broken, 0); err != nil {
		t.Fatal(err)
	}
	if file, err := os.Open(broken); err == nil {
		file.Close()
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
func matcherIndexFor(m *Cached, libraryID string) *ruleIndex {
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
	f.config.Library[0].SourceType = strp("url")
	f.config.Library[0].URL = strp("https://must-not-fetch.invalid/rules")
	old := f.matcher(f.config, "default")
	if old.Match("matched.example") != nil {
		t.Fatal("matched without a cache file")
	}
	if !old.RetryPending() {
		t.Fatal("missing source did not schedule a retry")
	}
	f.write(CacheFilename(LibraryRule(f.config.Library[0])), "DOMAIN,matched.example")
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
	f.config.Rules[1].Policy = strp("CHANGED")
	f.config.Library[0].Content = strp("DOMAIN,changed.example")
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
	later := f.config.Rules[1]
	later.ID = "later"
	later.Policy = strp("LATER")
	f.config.Rules = append(f.config.Rules, later)
	matcher := f.matcher(f.config, "default")
	if matcher.Match("missing.example") != nil { // visits both references
		t.Fatal("matched")
	}
	if matcher.Matcher().IndexCount() != 1 {
		t.Fatalf("index count = %d", matcher.Matcher().IndexCount())
	}
	hit := matcher.Match("matched.example")
	if hitRuleID(hit) != "set" || hit.Policy != "PROXY" || hit.Priority != 2 {
		t.Fatalf("hit = %+v", hit)
	}
}

func TestReplacedFileBetweenSignatureAndReadIsRetried(t *testing.T) {
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

func TestConcurrentCacheAccess(t *testing.T) {
	f := newFixture(t, func(o *CacheOptions) { o.MaxMatchers = 2; o.CleanupInterval = 0.001 })
	f.write("domains.list", "DOMAIN,file.example")
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				config := cloneInput(f.config)
				if i%3 == 0 {
					config.Rules[0].Value = "edit.example"
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

func TestReviewCleanupHonorsIdleDeadlineWithLongSweepInterval(t *testing.T) {
	f := newFixture(t, func(o *CacheOptions) {
		o.Clock = clock.Real{}
		o.IdleSeconds = 0.025
		o.CleanupInterval = 60
	})
	f.matcher(f.config, "default").Match("matched.example")
	deadline := time.Now().Add(2 * time.Second)
	for f.cache.Size() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.cache.Size() != 0 {
		t.Fatal("idle expiry waited for the next full cleanup interval")
	}
}
