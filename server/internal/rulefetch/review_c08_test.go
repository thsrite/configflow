package rulefetch

import (
	"os"
	"path/filepath"
	"testing"
)

// Review C08: rule_cache_read_path returns an existing current cache before
// it reads the shared document, so a failing repository read does not turn
// a usable cache into an error.
func TestReviewC08ReadPathPrefersExistingCacheWithoutRepositoryRead(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, &rule)
	path := cachePath(t, r, rule)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.DatabasePath(), []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Shared(ctx); err == nil {
		t.Skip("repository still readable; nothing to prove")
	}
	got, err := CacheReadPath(ctx, rule, nil)
	if err != nil || got != path {
		t.Fatalf("CacheReadPath = %q, %v; want %q", got, err, path)
	}
	other := remoteSource()
	other.URL = strp("https://rules.test/other.txt")
	if _, err := CacheReadPath(ctx, other, nil); err == nil {
		t.Fatal("a missing cache must still read the repository (and fail here)")
	}
}
