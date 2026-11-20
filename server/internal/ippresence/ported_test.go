package ippresence

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/testjson"
)

// Behaviour the replay cannot observe: the memo (keyed by content hash, or
// by cache path + mtime_ns + size, cleared when 1024 entries are reached)
// and the hook registration of Wire.

func listRepo(t *testing.T) (context.Context, string, *jsonx.Object, *jsonx.Object) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	entry := jsonx.Obj("id", "lib-tiktok", "name", "TikTok", "source_type", "url", "url", "https://rules.test/TikTok.list",
		"behavior", "classical", "format", "text")
	root, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	config := jsonx.Obj("rule_library", []any{entry})
	item := jsonx.Obj("id", "tiktok", "itemType", "ruleset", "library_rule_id", "lib-tiktok", "behavior", "classical")
	return ctx, filepath.Join(root, rules.CacheFilename(testjson.Decode[model.RuleConfig](t, entry))), item, config
}

// A cache file rewritten with the same size and modification time keeps the
// memoized answer (Python keys the memo by path, st_mtime_ns, st_size); a
// new size or mtime is read again.
func TestCacheMemoIsKeyedByPathMtimeAndSize(t *testing.T) {
	ClearMemo()
	ctx, path, item, config := listRepo(t)
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
	check := func(want any) {
		t.Helper()
		got, err := ClassicalIPLines(ctx, item, config)
		if err != nil || got != want {
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
	for i := 0; i < memoLimit; i++ {
		item := jsonx.Obj("source_type", "content", "content", "DOMAIN,"+string(rune('a'+i%26))+jsonx.Int(int64(i)).String()+".test")
		if got, err := ClassicalIPLines(ctx, item, nil); err != nil || got != false {
			t.Fatalf("%d: %v %v", i, got, err)
		}
	}
	memoMu.Lock()
	full := len(memo)
	memoMu.Unlock()
	if full != memoLimit {
		t.Fatalf("memo holds %d", full)
	}
	if _, err := ClassicalIPLines(ctx, jsonx.Obj("source_type", "content", "content", "1.1.1.1"), nil); err != nil {
		t.Fatal(err)
	}
	memoMu.Lock()
	after := len(memo)
	memoMu.Unlock()
	if after != 1 {
		t.Fatalf("memo not cleared at the limit: %d", after)
	}
}

// Wire registers convert.MihomoHooks.IsFullTun (takeover_mode now answers
// for a marked profile instead of failing closed).
func TestWireRegistersIsFullTun(t *testing.T) {
	marked := jsonx.Obj("mihomo", jsonx.Obj("router_split", jsonx.Obj("version", jsonx.Int(1), "takeover", "full_tun",
		"route_rule_set_ids", []any{}, "origin", "setup")))
	restore := convert.RegisterMihomoHooks(convert.MihomoHooks{})
	defer restore()
	Wire()
	mode, err := convert.TakeoverMode(marked)
	if err != nil || mode != "full_tun" {
		t.Fatalf("TakeoverMode %q %v", mode, err)
	}
	invalid := jsonx.Obj("mihomo", jsonx.Obj("router_split", jsonx.Obj("version", jsonx.Int(2))))
	if mode, err := convert.TakeoverMode(invalid); err != nil || mode != "" {
		t.Fatalf("TakeoverMode invalid marker %q %v", mode, err)
	}
}
