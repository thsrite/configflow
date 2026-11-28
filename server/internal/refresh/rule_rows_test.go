package refresh

import (
	"configflow/internal/model"
	"configflow/internal/repo"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/rules"
	"configflow/internal/taskstatus"
)

func TestRuleRowsObserveFileReceiptClockAndJob(t *testing.T) {
	f := setup(t, 86400)
	rule := ruleDecl(t)
	key := RuleTaskKey(rule.ID)
	statuses := taskstatus.Snapshot{}
	check := func(want *string, running bool) {
		t.Helper()
		rows, err := f.svc.RuleTasks(f.ctx, statuses)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.Key != key {
				continue
			}
			if (row.CacheUpdatedAt == nil) != (want == nil) || textOr(row.CacheUpdatedAt, "") != textOr(want, "") || row.Running != running {
				t.Fatalf("cache=%v running=%v want=%v %v", row.CacheUpdatedAt, row.Running, want, running)
			}
			if row.CacheUpdatedAt != nil {
				*row.CacheUpdatedAt = "changed by caller"
			}
			return
		}
		t.Fatal("no row", key)
	}
	check(nil, false)
	dir, err := f.repo.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, rules.CacheFilename(rules.LibraryRule(rule)))
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("example.com"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err = os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	check(new(stamp.Format(time.RFC3339)), false)
	stamp = stamp.Add(time.Hour)
	if err = os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	check(new(stamp.Format(time.RFC3339)), false)
	if err = os.WriteFile(path+".invalid", nil, 0600); err != nil {
		t.Fatal(err)
	}
	check(nil, false)
	statuses[key] = []byte(`{"cache_updated_at":"receipt"}`)
	_, release, ok := running.TryLock(f.ctx, key)
	if !ok {
		t.Fatal("job claim")
	}
	defer release()
	check(new("receipt"), true)
	check(new("receipt"), true)
}

func BenchmarkPollingRuleRows(b *testing.B) {
	r, err := repo.Open(b.TempDir(), repo.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(r.Close)
	r.SetViewGuard(nil)
	err = r.UpdateShared(b.Context(), func(_ context.Context, shared *model.Shared) error {
		for i := range 31 {
			id := fmt.Sprintf("poll-%d", i)
			shared.RuleLibrary = append(shared.RuleLibrary, model.RuleLibraryItem{ID: id, Name: id, URL: new("https://example.com/" + id)})
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	s := New(Options{Repo: r})
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.RuleTasks(b.Context(), nil); err != nil {
			b.Fatal(err)
		}
	}
}
