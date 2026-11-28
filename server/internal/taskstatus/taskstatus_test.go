package taskstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func receiptJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func strPtrVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func TestRecordKeepsRetainedCacheAndCounts(t *testing.T) {
	store, _ := fixedStore(t)
	mustRecord := func(r Result) {
		t.Helper()
		if _, err := store.SaveResult(context.Background(), "x", r); err != nil {
			t.Fatal(err)
		}
	}
	mustRecord(Result{Error: "boom", CacheRetained: yes(), CacheUpdatedAt: "2026-01-01T00:00:00Z"})
	mustRecord(Result{Error: "boom again", CacheRetained: yes()})
	mustRecord(Result{Skipped: true, Error: "nothing to do"})
	snap, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	entry := snap.Entry("x")
	if strPtrVal(entry.LastStatus) != "skipped" {
		t.Errorf("status %q", strPtrVal(entry.LastStatus))
	}
	if string(entry.FailureCount) != "2" || string(entry.RunCount) != "2" || string(entry.ConsecutiveFailures) != "2" {
		t.Errorf("a skipped run changed the counts: %s", receiptJSON(t, entry))
	}
	if entry.CacheRetained != nil {
		t.Error("a skipped run keeps no cache_retained")
	}
	if strPtrVal(entry.CacheUpdatedAt) != "2026-01-01T00:00:00Z" {
		t.Error("cache_updated_at must survive")
	}
	mustRecord(Result{OK: true, DurationMs: 12.6})
	snap, err = store.Read()
	if err != nil {
		t.Fatal(err)
	}
	entry = snap.Entry("x")
	if strPtrVal(entry.LastStatus) != "success" || entry.LastDurationMs == nil || int64(*entry.LastDurationMs) != 12 || string(entry.ConsecutiveFailures) != "0" ||
		string(entry.RunCount) != "3" || strPtrVal(entry.LastSuccessAt) != "2026-11-14T12:00:00Z" || strPtrVal(entry.LastError) != "" {
		t.Errorf("after success: %s", receiptJSON(t, entry))
	}
	if err := store.Forget(t.Context(), "x", "missing"); err != nil {
		t.Fatal(err)
	}
	snap, err = store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := snap["x"]; exists {
		t.Errorf("forgotten entry: %s", string(snap["x"]))
	}
}

func TestFileBytesAndKeyOrderMatchPython(t *testing.T) {
	store, _ := fixedStore(t)
	if _, err := store.SaveResult(context.Background(), "subscription:s", Result{Error: "例子失败", StartedAt: "2026-11-14T11:59:00Z", Trigger: "pull", DurationMs: -5}); err != nil {
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
	entry, err := store.SaveResult(context.Background(), "k", Result{Error: strings.Repeat("错", 3000)})
	if err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(strPtrVal(entry.LastError))); got != MaxError {
		t.Errorf("last_error has %d characters", got)
	}
	all, _ := store.Read()
	if len(all) != 1 {
		t.Errorf("load: %s", receiptJSON(t, all))
	}
	if err := os.WriteFile(filepath.Join(store.DataDir, Filename), []byte("[1]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if all, _ = store.Read(); len(all) != 0 {
		t.Error("a non-object file must read as empty")
	}
}

func TestOnRecordRunsAfterTheWrite(t *testing.T) {
	store, _ := fixedStore(t)
	var seen []string
	store.OnRecord = func(key string) {
		// the lock is released: reading must not deadlock and sees the entry
		snap, err := store.Read()
		entry := snap.Entry(key)
		if err != nil || strPtrVal(entry.LastStatus) != "success" {
			t.Errorf("hook saw %v %v", entry, err)
		}
		seen = append(seen, key)
	}
	if _, err := store.SaveResult(context.Background(), "a", Result{OK: true}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "a" {
		t.Errorf("hook calls: %v", seen)
	}
}

func TestUpdatingExistingEntryKeepsKeyOrder(t *testing.T) {
	store, clock := fixedStore(t)
	_, _ = store.SaveResult(context.Background(), "a", Result{Error: "x", CacheRetained: yes()})
	*clock = clock.Add(time.Minute)
	_, _ = store.SaveResult(context.Background(), "a", Result{OK: true})
	data, _ := os.ReadFile(filepath.Join(store.DataDir, Filename))
	want := `{"a":{"last_run_at":"2026-11-14T12:01:00Z","last_finished_at":"2026-11-14T12:01:00Z","last_status":"success","last_duration_ms":0,"last_trigger":"schedule","last_error":"","run_count":2,"failure_count":1,"consecutive_failures":0,"last_success_at":"2026-11-14T12:01:00Z"}}`
	if string(data) != want {
		t.Errorf("got  %s\nwant %s", data, want)
	}
}

func TestParseTime(t *testing.T) {
	got, ok := ParseTimestamp("2026-11-14T08:00:00+08:00")
	if !ok || !got.Equal(time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("%v %v", got, ok)
	}
	if got, ok = ParseTimestamp("2026-11-14T12:00:00Z"); !ok || !got.Equal(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("%v %v", got, ok)
	}
	if got, ok = ParseTimestamp("2026-11-14T12:00:00"); !ok || got.Location() != time.UTC || got.Hour() != 12 {
		t.Errorf("naive must be UTC: %v %v", got, ok)
	}
	for _, bad := range []string{"", "garbage"} {
		if _, ok := ParseTimestamp(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
	if !strings.HasSuffix(NowISO(), "Z") || len(NowISO()) != 20 {
		t.Errorf("now_iso: %q", NowISO())
	}
}

func TestParseISO(t *testing.T) {
	plus8 := time.FixedZone("", 8*3600)
	tests := []struct {
		in    string
		want  time.Time
		naive bool
	}{
		{"2026-11-14T12:34:56", time.Date(2026, 11, 14, 12, 34, 56, 0, time.UTC), true},
		{"2026-11-14T12:34:56.123456", time.Date(2026, 11, 14, 12, 34, 56, 123456000, time.UTC), true},
		{"2026-11-14T12:34:56.123456Z", time.Date(2026, 11, 14, 12, 34, 56, 123456000, time.UTC), false},
		{"2026-11-14T12:34:56Z", time.Date(2026, 11, 14, 12, 34, 56, 0, time.UTC), false},
		{"2026-11-14T12:34:56.5+08:00", time.Date(2026, 11, 14, 12, 34, 56, 500000000, plus8), false},
		{"2026-11-14T12:34:56+00:00", time.Date(2026, 11, 14, 12, 34, 56, 0, time.UTC), false},
		{"2026-11-14T12:34:56-05:30", time.Date(2026, 11, 14, 12, 34, 56, 0, time.FixedZone("", -19800)), false},
		{"2026-11-14T12:34:56.123456789Z", time.Date(2026, 11, 14, 12, 34, 56, 123456789, time.UTC), false},
		{"2026-11-14T12:34:56+08:05:43", time.Date(2026, 11, 14, 12, 34, 56, 0, time.FixedZone("", 8*3600+5*60+43)), false},
		{"2026-11-14", time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, naive, ok := ParseISO(tt.in)
			if !ok || !got.Equal(tt.want) || naive != tt.naive {
				t.Fatalf("got %v naive=%v ok=%v, want %v naive=%v", got, naive, ok, tt.want, tt.naive)
			}
			_, gotOffset := got.Zone()
			_, wantOffset := tt.want.Zone()
			if gotOffset != wantOffset || (wantOffset == 0 && got.Location() != time.UTC) {
				t.Fatalf("zone %v, want %v", got.Location(), tt.want.Location())
			}
		})
	}
	for _, bad := range []string{"", "garbage", "2026-11-14T12:34", "2026-11-14 12:34:56", "20261114T123456Z",
		"2026-W46-5", "2026-11-31T00:00:00", "2026-11-14T24:00:00", "2026-11-14T12:34:56+24:00",
		"2026-11-14T12:34:56ZZ", "2026-11-14T12:34:56Z\n", "2026-11-14T12:34:56 +08:00"} {
		if _, _, ok := ParseISO(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestSharedSnapshotOwnershipAndReplacement(t *testing.T) {
	store, _ := fixedStore(t)
	read := func() Snapshot {
		t.Helper()
		data, err := New(store.DataDir).Read()
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	path := store.path()
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, `{"x":{"old":1}}`)
	first := read()
	first["x"][2] = 'X'
	delete(first, "x")
	if got := string(read()["x"]); got != `{"old":1}` {
		t.Fatal("shared mutable result", got)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	write(path+".new", `{"x":{"new":1}}`)
	if err := os.Chtimes(path+".new", before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
	if got := string(read()["x"]); got != `{"new":1}` {
		t.Fatal(got)
	}
	if _, err := store.SaveResult(t.Context(), "y", Result{OK: true}); err != nil {
		t.Fatal(err)
	}
	if got := read(); got["y"] == nil || string(got["x"]) != `{"new":1}` {
		t.Fatal(got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if len(read()) != 0 {
		t.Fatal("stale deleted receipt")
	}
}

func BenchmarkPollingSnapshot(b *testing.B) {
	s := New(b.TempDir())
	data := Snapshot{}
	for i := range 40 {
		data[fmt.Sprintf("rule_source:%d", i)] = json.RawMessage(`{"last_status":"success","last_run_at":"2026-01-01T00:00:00Z","run_count":11}`)
	}
	if err := writeSnapshot(b.Context(), s.path(), data); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if snapshot, err := s.Read(); err != nil || len(snapshot) != len(data) {
			b.Fatal(err)
		}
	}
}
