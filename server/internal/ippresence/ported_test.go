package ippresence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

// Behaviour the replay cannot observe: the memo (keyed by content hash, or
// by cache path + mtime_ns + size, cleared when 1024 entries are reached)
// and the hook registration of Wire.

func listRepo(t *testing.T) (context.Context, *repo.Repository, string, model.RuleConfig, []model.RuleLibraryItem) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	var entry model.RuleLibraryItem
	if err := json.Unmarshal([]byte(`{"id":"lib-tiktok","name":"TikTok","source_type":"url","url":"https://rules.test/TikTok.list","behavior":"classical","format":"text"}`), &entry); err != nil {
		t.Fatal(err)
	}
	root, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	var item model.RuleConfig
	if err := json.Unmarshal([]byte(`{"id":"tiktok","itemType":"ruleset","library_rule_id":"lib-tiktok","behavior":"classical"}`), &item); err != nil {
		t.Fatal(err)
	}
	return ctx, r, filepath.Join(root, rules.CacheFilename(rules.LibraryRule(entry))), item, []model.RuleLibraryItem{entry}
}

// A cache file rewritten with the same size and modification time keeps the
// memoized answer (keyed by path, mtime_ns, size); a new size or mtime is read again.
func TestCacheMemoIsKeyedByPathMtimeAndSize(t *testing.T) {
	ClearMemo()
	ctx, r, path, item, library := listRepo(t)
	stamp := time.Unix(1700000000, 123456789)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(text string, when time.Time) {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want bool) {
		t.Helper()
		got, err := TypedClassicalIPLines(ctx, r, item, library)
		if err != nil || got == nil || *got != want {
			t.Fatalf("got %v %v, want %v", got, err, want)
		}
	}
	write("DOMAIN,a.test\n", stamp) // 14 bytes, no IP lines
	check(false)
	write("192.0.2.0/24\n\n", stamp) // same size and mtime: memo answers
	check(false)
	write("192.0.2.0/24\n\n", stamp.Add(time.Nanosecond)) // new mtime
	check(true)
	write("DOMAIN,a.test\nDOMAIN,b.test\n", stamp.Add(time.Nanosecond)) // new size
	check(false)
}

// Inline content is memoized by its SHA-256; the memo is cleared once it
// holds 1024 entries.
func TestContentMemoLimit(t *testing.T) {
	ClearMemo()
	ctx := context.Background()
	srcType := "content"
	for i := 0; i < memoLimit; i++ {
		content := fmt.Sprintf("DOMAIN,%c%d.test", rune('a'+i%26), i)
		item := model.RuleConfig{SourceType: &srcType, Content: &content}
		if got, err := TypedClassicalIPLines(ctx, nil, item, nil); err != nil || got == nil || *got {
			t.Fatalf("%d: %v %v", i, got, err)
		}
	}
	memoMu.Lock()
	full := len(memo)
	memoMu.Unlock()
	if full != memoLimit {
		t.Fatalf("memo holds %d", full)
	}
	ipContent := "1.1.1.1"
	if _, err := TypedClassicalIPLines(ctx, nil, model.RuleConfig{SourceType: &srcType, Content: &ipContent}, nil); err != nil {
		t.Fatal(err)
	}
	memoMu.Lock()
	after := len(memo)
	memoMu.Unlock()
	if after != 1 {
		t.Fatalf("memo not cleared at the limit: %d", after)
	}
}
