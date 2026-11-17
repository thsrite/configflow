package taskstatus

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

// Ported from the task_status parts of backend/test_scheduled_tasks.py.

func fixedStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	store := New(t.TempDir())
	clock := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	return store, &clock
}

func yes() *bool { v := true; return &v }

func TestRecordKeepsRetainedCacheAndCounts(t *testing.T) {
	store, _ := fixedStore(t)
	mustRecord := func(r Result) {
		t.Helper()
		if _, err := store.Record("x", r); err != nil {
			t.Fatal(err)
		}
	}
	mustRecord(Result{Error: "boom", CacheRetained: yes(), CacheUpdatedAt: "2026-01-01T00:00:00Z"})
	mustRecord(Result{Error: "boom again", CacheRetained: yes()})
	mustRecord(Result{Skipped: true, Error: "nothing to do"})
	entry, _ := store.Get("x")
	if entry.GetString("last_status") != "skipped" {
		t.Errorf("status %q", entry.GetString("last_status"))
	}
	if entry.GetInt("failure_count") != 2 || entry.GetInt("run_count") != 2 || entry.GetInt("consecutive_failures") != 2 {
		t.Errorf("a skipped run changed the counts: %s", jsonx.Repr(entry))
	}
	if entry.Has("cache_retained") {
		t.Error("a skipped run keeps no cache_retained")
	}
	if entry.GetString("cache_updated_at") != "2026-01-01T00:00:00Z" {
		t.Error("cache_updated_at must survive")
	}
	mustRecord(Result{OK: true, DurationMs: 12.6})
	entry, _ = store.Get("x")
	if entry.GetString("last_status") != "success" || entry.GetInt("last_duration_ms") != 12 || entry.GetInt("consecutive_failures") != 0 ||
		entry.GetInt("run_count") != 3 || entry.GetString("last_success_at") != "2026-11-14T12:00:00Z" || entry.GetString("last_error") != "" {
		t.Errorf("after success: %s", jsonx.Repr(entry))
	}
	if err := store.Forget("x", "missing"); err != nil {
		t.Fatal(err)
	}
	if entry, _ = store.Get("x"); entry.Len() != 0 {
		t.Errorf("forgotten entry: %s", jsonx.Repr(entry))
	}
}

func TestFileBytesAndKeyOrderMatchPython(t *testing.T) {
	store, _ := fixedStore(t)
	if _, err := store.Record("subscription:s", Result{Error: "例子失败", StartedAt: "2026-11-14T11:59:00Z", Trigger: "pull", DurationMs: -5}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(store.DataDir, Filename))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"subscription:s":{"last_run_at":"2026-11-14T11:59:00Z","last_finished_at":"2026-11-14T12:00:00Z","last_status":"failed","last_duration_ms":0,"last_trigger":"pull","last_error":"例子失败","run_count":1,"failure_count":1,"consecutive_failures":1}}`
	if string(data) != want {
		t.Errorf("got  %s\nwant %s", data, want)
	}
	info, _ := os.Stat(filepath.Join(store.DataDir, Filename))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, tempfile.mkstemp gives 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(store.DataDir, Filename+".lock")); !os.IsNotExist(err) {
		t.Errorf("unexpected per-resource lock file: %v", err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(store.DataDir, ".task-status-*"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left: %v", leftovers)
	}
}

func TestErrorIsCutAndCorruptFileStartsOver(t *testing.T) {
	store, _ := fixedStore(t)
	if err := os.WriteFile(filepath.Join(store.DataDir, Filename), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := store.Record("k", Result{Error: strings.Repeat("错", 3000)})
	if err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(entry.GetString("last_error"))); got != MaxError {
		t.Errorf("last_error has %d characters", got)
	}
	all, _ := store.Load()
	if all.Len() != 1 {
		t.Errorf("load: %s", jsonx.Repr(all))
	}
	if err := os.WriteFile(filepath.Join(store.DataDir, Filename), []byte("[1]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if all, _ = store.Load(); all.Len() != 0 {
		t.Error("a non-object file must read as empty")
	}
}

func TestOnRecordRunsAfterTheWrite(t *testing.T) {
	store, _ := fixedStore(t)
	var seen []string
	store.OnRecord = func(key string) {
		// the lock is released: reading must not deadlock and sees the entry
		entry, err := store.Get(key)
		if err != nil || entry.GetString("last_status") != "success" {
			t.Errorf("hook saw %v %v", entry, err)
		}
		seen = append(seen, key)
	}
	if _, err := store.Record("a", Result{OK: true}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "a" {
		t.Errorf("hook calls: %v", seen)
	}
}

func TestUpdatingExistingEntryKeepsKeyOrder(t *testing.T) {
	store, clock := fixedStore(t)
	_, _ = store.Record("a", Result{Error: "x", CacheRetained: yes()})
	*clock = clock.Add(time.Minute)
	_, _ = store.Record("a", Result{OK: true})
	data, _ := os.ReadFile(filepath.Join(store.DataDir, Filename))
	want := `{"a":{"last_run_at":"2026-11-14T12:01:00Z","last_finished_at":"2026-11-14T12:01:00Z","last_status":"success","last_duration_ms":0,"last_trigger":"schedule","last_error":"","run_count":2,"failure_count":1,"consecutive_failures":0,"last_success_at":"2026-11-14T12:01:00Z"}}`
	if string(data) != want {
		t.Errorf("got  %s\nwant %s", data, want)
	}
}

func TestParseTime(t *testing.T) {
	got, ok := ParseTime("2026-11-14T08:00:00+08:00")
	if !ok || !got.Equal(time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("%v %v", got, ok)
	}
	if got, ok = ParseTime("2026-11-14T12:00:00Z"); !ok || !got.Equal(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("%v %v", got, ok)
	}
	if got, ok = ParseTime("2026-11-14T12:00:00"); !ok || got.Location() != time.UTC || got.Hour() != 12 {
		t.Errorf("naive must be UTC: %v %v", got, ok)
	}
	for _, bad := range []any{"", nil, 5, "garbage"} {
		if _, ok := ParseTime(bad); ok {
			t.Errorf("%v parsed", bad)
		}
	}
	if !strings.HasSuffix(NowISO(), "Z") || len(NowISO()) != 20 {
		t.Errorf("now_iso: %q", NowISO())
	}
}

func TestFromISOFormatMatchesPython(t *testing.T) {
	file, err := os.Open("testdata/corpus.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	accepted, rejected := 0, 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line struct {
			In string `json:"in"`
			Us *int64 `json:"us"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		got, ok := ParseTime(line.In)
		switch {
		case line.Us == nil && ok:
			t.Errorf("%q: Python rejects it, Go gave %v", line.In, got)
		case line.Us != nil && !ok:
			t.Errorf("%q: Python gives %d µs, Go rejects it", line.In, *line.Us)
		case line.Us != nil && got.UnixMicro() != *line.Us:
			t.Errorf("%q: got %d µs, Python %d", line.In, got.UnixMicro(), *line.Us)
		case line.Us == nil:
			rejected++
		default:
			accepted++
		}
	}
	t.Logf("identical to Python: %d accepted, %d rejected", accepted, rejected)
	if accepted < 100 || rejected < 100 {
		t.Fatalf("corpus too one-sided: %d/%d", accepted, rejected)
	}
}
