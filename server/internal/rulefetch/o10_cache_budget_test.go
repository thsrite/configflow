package rulefetch

import (
	"fmt"
	"strings"
	"testing"
)

func TestNormalizedCacheBudget(t *testing.T) {
	normalizedCache.Clear()
	t.Cleanup(normalizedCache.Clear)

	// An entry larger than normalizedCacheMaxItemLen is normalized normally
	// but not retained in the global cache.
	oversized := strings.Repeat("example.com\n", (normalizedCacheMaxItemLen/12)+10)
	got, _, err := NormalizeRuleset(oversized, "domain", "text")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) <= normalizedCacheMaxItemLen {
		t.Fatalf("expected oversized result > %d, got %d", normalizedCacheMaxItemLen, len(got))
	}
	normalizedCache.mu.Lock()
	entriesAfterOversized := len(normalizedCache.entries)
	normalizedCache.mu.Unlock()
	if entriesAfterOversized != 0 {
		t.Fatalf("oversized entry retained: %d entries", entriesAfterOversized)
	}

	// Repeated medium entries stay within normalizedCacheMaxBytes.
	unit := strings.Repeat("a.example.com\n", 15000) // ~210 KB
	for i := range 8 {
		body := fmt.Sprintf("# %d\n%s", i, unit)
		if _, _, err := NormalizeRuleset(body, "domain", "text"); err != nil {
			t.Fatal(err)
		}
	}
	normalizedCache.mu.Lock()
	retainedBytes := normalizedCache.size
	normalizedCache.mu.Unlock()
	if retainedBytes > normalizedCacheMaxBytes {
		t.Fatalf("normalizedCache retained %d bytes > limit %d", retainedBytes, normalizedCacheMaxBytes)
	}
}
