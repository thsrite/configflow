package trafficstore

// Ports of backend/test_domain_status_cache.py (traffic store status cache)
// and of the traffic-store cases of backend/test_domain_discovery.py
// (clean_report, merging, host cap), which C37 owns but deferred to A05.
//
// Not ported here: test_other_json_stores_keep_close_before_replace_without_digest
// (internal/jsonstore.TestOtherStoresKeepCloseBeforeReplaceWithoutSignature),
// test_windows_writer_closes_file_before_replacement_and_returns_no_signature
// and test_windows_disables_stat_cache (Windows only; the server runs on
// Linux). The two "during atomic write" cases monkeypatched os.replace
// inside json_store; jsonstore covers the detection itself
// (TestExternalReplacementDuringVerifiedWriteDoesNotChangeWrittenIdentity,
// TestExternalInPlaceEditDuringVerifiedWriteIsNotSigned), and the tests
// below check the store's side: the edit happens and no signature comes
// back, so nothing is cached.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"configflow/internal/jsonstore"
	"configflow/internal/testjson"
)

func isolated(t *testing.T) {
	t.Helper()
	ResetCache()
	t.Cleanup(ResetCache)
}

func defaultStatus() *testjson.Object {
	return testjson.Obj(
		testjson.Field{Key: "value", Value: "running"},
		testjson.Field{Key: "at", Value: "now"},
		testjson.Field{Key: "mihomo_version", Value: "1"},
	)
}

func writeData(t *testing.T, store *NativeStore, agent string, status any) map[string]any {
	t.Helper()
	if status == nil {
		status = defaultStatus()
	}
	large := make([]int, 100)
	for i := range large {
		large[i] = i
	}
	data := map[string]any{
		"status": status,
		"days": map[string]any{
			"today": map[string]any{
				"hosts": map[string]any{
					"synthetic.example": map[string]any{"large": large},
				},
			},
		},
	}
	if _, err := store.files.Write(t.Context(), agent, data, true); err != nil {
		t.Fatal(err)
	}
	return data
}

func trackReads(store *NativeStore) *[]string {
	calls := &[]string{}
	original := store.readJSON
	store.readJSON = func(agent string) (json.RawMessage, error) {
		*calls = append(*calls, agent)
		return original(agent)
	}
	return calls
}

func statusNil(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

func statusValue(t *testing.T, store *NativeStore, agent string) any {
	t.Helper()
	raw := store.StatusJSON(agent)
	if statusNil(raw) {
		return nil
	}
	parsed, err := testjson.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	val, _ := testjson.Path(parsed, "value")
	return val
}

func cachedStatusRaw(path string) json.RawMessage {
	mu.Lock()
	defer mu.Unlock()
	if e, ok := statusIndex[path]; ok {
		return bytes.Clone(e.Value.(*cacheEntry).status)
	}
	return nil
}

func stat(t *testing.T, path string) (os.FileInfo, int64, int64) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info, info.ModTime().UnixNano(), info.Size()
}

// rewrite writes encoded compactly to path (destination for atomic) keeping
// the old mtime, then renames over path when atomic; in place it waits
// until the ctime moved.
func rewriteBytes(t *testing.T, path string, encoded []byte, atomic bool) {
	t.Helper()
	before, mtime, size := stat(t, path)
	destination := path
	if atomic {
		destination = strings.TrimSuffix(path, ".json") + ".new"
	}
	if err := os.WriteFile(destination, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	keep := time.Unix(0, mtime)
	if err := os.Chtimes(destination, keep, keep); err != nil {
		t.Fatal(err)
	}
	if atomic {
		if err := os.Rename(destination, path); err != nil {
			t.Fatal(err)
		}
	} else {
		waitForChangedCtime(t, path, before)
	}
	_, mtimeAfter, sizeAfter := stat(t, path)
	if sizeAfter != size || mtimeAfter != mtime {
		t.Fatalf("size/mtime changed: %d/%d vs %d/%d", sizeAfter, mtimeAfter, size, mtime)
	}
}

func rewrite(t *testing.T, path string, data any, atomic bool) {
	t.Helper()
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	rewriteBytes(t, path, encoded, atomic)
}

func ctimeOf(t *testing.T, path string) int64 {
	return CtimeNs(mustStat(t, path))
}

func mustStat(t *testing.T, path string) os.FileInfo {
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func waitForChangedCtime(t *testing.T, path string, previous os.FileInfo) {
	t.Helper()
	old := CtimeNs(previous)
	deadline := time.Now().Add(2 * time.Second)
	for ctimeOf(t, path) == old && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		keep := previous.ModTime()
		os.Chtimes(path, keep, keep)
	}
	if ctimeOf(t, path) == old {
		t.Fatal("ctime did not change")
	}
}

func TestStatusHitReusesOnlySmallIndependentMetadata(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	writeData(t, store, "a", nil)
	calls := trackReads(store)
	first := store.StatusJSON("a")
	first[0] = 'X'
	if statusValue(t, store, "a") != "running" {
		t.Fatal("cached status was mutated")
	}
	if strings.Join(*calls, ",") != "a" {
		t.Fatalf("reads %v", *calls)
	}
	paths := CachedPaths()
	if len(paths) != 1 || !testjson.Equal(defaultStatus(), cachedStatusRaw(paths[0])) {
		t.Fatalf("cache %v", paths)
	}
}

func TestExternalSameSizeSameMtimeChangeInvalidates(t *testing.T) {
	for _, atomic := range []bool{false, true} {
		isolated(t)
		store := NewNative(t.TempDir())
		data := writeData(t, store, "a", nil)
		if statusValue(t, store, "a") != "running" {
			t.Fatal("first read")
		}
		data["status"] = testjson.Obj(
			testjson.Field{Key: "value", Value: "unknown"},
			testjson.Field{Key: "at", Value: "now"},
			testjson.Field{Key: "mihomo_version", Value: "1"},
		)
		rewrite(t, store.Path("a"), data, atomic)
		if got := statusValue(t, store, "a"); got != "unknown" {
			t.Fatalf("atomic=%v: %v", atomic, got)
		}
	}
}

func TestReplacementDuringReadDoesNotLabelOldStatusWithNewSignature(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	writeData(t, store, "a", nil)
	original := store.readJSON
	replaced := false
	store.readJSON = func(agent string) (json.RawMessage, error) {
		data, err := original(agent)
		if !replaced {
			replaced = true
			writeData(t, store, "a", testjson.Obj(testjson.Field{Key: "value", Value: "unreachable"}))
		}
		return data, err
	}
	if statusValue(t, store, "a") != "running" {
		t.Fatal("first read")
	}
	if len(CachedPaths()) != 0 {
		t.Fatal("old status cached with the new signature")
	}
	if statusValue(t, store, "a") != "unreachable" {
		t.Fatal("second read")
	}
}

func report(t *testing.T, status string) *Report {
	t.Helper()
	return mustDecodeReport(t, map[string]any{"schema": 1, "status": status, "items": []any{}})
}

func mustDecodeReport(t *testing.T, payload any) *Report {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r, err := DecodeReport(raw)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReportPopulatesStatusWithoutRedecodingHistoryAndDeleteInvalidates(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	calls := trackReads(store)
	if _, err := store.Add(context.Background(), "a", report(t, "running"), nil); err != nil {
		t.Fatal(err)
	}
	if statusValue(t, store, "a") != "running" {
		t.Fatal("status after report")
	}
	returned, err := store.Add(context.Background(), "a", report(t, "unreachable"), nil)
	if err != nil {
		t.Fatal(err)
	}
	returned.Value = "caller mutation"
	if statusValue(t, store, "a") != "unreachable" {
		t.Fatal("caller mutation leaked")
	}
	if strings.Join(*calls, ",") != "a" {
		t.Fatalf("only the cold merge needs a history read: %v", *calls)
	}
	if err := store.Delete(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if len(CachedPaths()) != 0 || !statusNil(store.StatusJSON("a")) {
		t.Fatal("delete did not invalidate")
	}
}

func TestFullHistoryReadPopulatesIndependentStatusForOtherStoreInstances(t *testing.T) {
	isolated(t)
	dir := t.TempDir()
	first, second := NewNative(dir), NewNative(dir)
	writeData(t, first, "a", nil)
	loaded := first.LoadDocument("a", nil)
	loaded.fields.setEncoded("status", json.RawMessage(`{"value":"caller mutation"}`))
	calls := trackReads(second)
	if statusValue(t, second, "a") != "running" || len(*calls) != 0 {
		t.Fatalf("calls %v", *calls)
	}
}

func TestFailedReportWriteNeverCachesUncommittedStatus(t *testing.T) {
	for _, afterReplace := range []bool{false, true} {
		isolated(t)
		store := NewNative(t.TempDir())
		if _, err := store.Add(context.Background(), "a", report(t, "running"), nil); err != nil {
			t.Fatal(err)
		}
		original := store.writeJSON
		store.writeJSON = func(ctx context.Context, agent string, data json.RawMessage) (*jsonstore.Signature, error) {
			if afterReplace {
				original(ctx, agent, data)
			}
			return nil, errors.New("write failed")
		}
		if _, err := store.Add(context.Background(), "a", report(t, "unreachable"), nil); err == nil || err.Error() != "write failed" {
			t.Fatalf("err %v", err)
		}
		if len(CachedPaths()) != 0 {
			t.Fatal("uncommitted status cached")
		}
		calls := trackReads(store)
		want := "running"
		if afterReplace {
			want = "unreachable"
		}
		if got := statusValue(t, store, "a"); got != want || len(*calls) != 1 {
			t.Fatalf("afterReplace=%v: %v %v", afterReplace, got, *calls)
		}
	}
}

func TestExternalChangeAfterReportWriteCannotAcquireWrittenStatus(t *testing.T) {
	for _, atomic := range []bool{false, true} {
		isolated(t)
		store := NewNative(t.TempDir())
		original := store.writeJSON
		store.writeJSON = func(ctx context.Context, agent string, data json.RawMessage) (*jsonstore.Signature, error) {
			written, err := original(ctx, agent, data)
			if err != nil {
				return nil, err
			}
			changed := bytes.Replace(data, []byte(`"running"`), []byte(`"unknown"`), 1)
			rewriteBytes(t, store.Path(agent), changed, atomic)
			return written, nil
		}
		if _, err := store.Add(context.Background(), "a", report(t, "running"), nil); err != nil {
			t.Fatal(err)
		}
		if len(CachedPaths()) != 0 {
			t.Fatalf("atomic=%v: written status cached for a changed file", atomic)
		}
		if got := statusValue(t, store, "a"); got != "unknown" {
			t.Fatalf("atomic=%v: %v", atomic, got)
		}
	}
}

// The writer detected an external change during the rename (jsonstore
// returns no signature): nothing is cached and the file wins.
func TestExternalReplacementDuringAtomicWriteDoesNotChangeWrittenIdentity(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	original := store.writeJSON
	store.writeJSON = func(ctx context.Context, agent string, data json.RawMessage) (*jsonstore.Signature, error) {
		if _, err := original(ctx, agent, data); err != nil {
			return nil, err
		}
		external := strings.TrimSuffix(store.Path(agent), ".json") + ".external"
		os.WriteFile(external, []byte(`{"status":{"value":"unreachable"},"days":{}}`), 0o644)
		os.Rename(external, store.Path(agent))
		return nil, nil
	}
	if _, err := store.Add(context.Background(), "a", report(t, "running"), nil); err != nil {
		t.Fatal(err)
	}
	if len(CachedPaths()) != 0 || statusValue(t, store, "a") != "unreachable" {
		t.Fatal("external replacement not honoured")
	}
}

func TestExternalInPlaceEditDuringAtomicWriteIsNotCached(t *testing.T) {
	for _, preserveMtime := range []bool{false, true} {
		isolated(t)
		store := NewNative(t.TempDir())
		original := store.writeJSON
		store.writeJSON = func(ctx context.Context, agent string, data json.RawMessage) (*jsonstore.Signature, error) {
			if _, err := original(ctx, agent, data); err != nil {
				return nil, err
			}
			path := store.Path(agent)
			previous := mustStat(t, path)
			raw, _ := os.ReadFile(path)
			encoded := bytes.Replace(raw, []byte(`"running"`), []byte(`"unknown"`), 1)
			os.WriteFile(path, encoded, 0o644)
			stamp := previous.ModTime()
			if !preserveMtime {
				stamp = mustStat(t, path).ModTime().Add(time.Second)
			}
			os.Chtimes(path, stamp, stamp)
			if mustStat(t, path).Size() != previous.Size() {
				t.Fatal("size changed")
			}
			return nil, nil
		}
		if _, err := store.Add(context.Background(), "a", report(t, "running"), nil); err != nil {
			t.Fatal(err)
		}
		if len(CachedPaths()) != 0 || statusValue(t, store, "a") != "unknown" {
			t.Fatalf("preserveMtime=%v: edit not honoured", preserveMtime)
		}
	}
}

func naive(y int, m time.Month, d, h int) *time.Time {
	at := time.Date(y, m, d, h, 0, 0, 0, time.Local)
	return &at
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestReportMergesExternalHistoryAndKeepsVersionAndRetention(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	if _, err := store.Add(context.Background(), "a", mustDecodeReport(t, map[string]any{
		"schema": 1, "status": "running", "mihomo_version": "v1", "items": []any{},
	}), naive(2026, 10, 1, 0)); err != nil {
		t.Fatal(err)
	}
	data := store.LoadDocument("a", naive(2026, 10, 1, 0))
	data.days.setEncoded("2026-10-10", json.RawMessage(`{"ip_only_conns":12,"dropped":3,"hosts":{}}`))
	if _, err := store.files.Write(t.Context(), "a", data, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(context.Background(), "a", mustDecodeReport(t, map[string]any{
		"schema": 1, "status": "unreachable", "ip_only_conns": 5, "items": []any{},
	}), naive(2026, 10, 10, 0)); err != nil {
		t.Fatal(err)
	}
	saved := store.LoadDocument("a", naive(2026, 10, 10, 0))
	days := saved.Days()
	if strings.Join(sortedKeys(days), ",") != "2026-10-10" {
		t.Fatalf("days %v", sortedKeys(days))
	}
	wantDay := map[string]any{"ip_only_conns": 17, "dropped": 3, "hosts": map[string]any{}}
	if diff := testjson.Diff(wantDay, days["2026-10-10"]); diff != "" {
		t.Fatalf("day diff: %s", diff)
	}
	wantStatus := map[string]any{"value": "unreachable", "at": "2026-10-10T00:00:00", "mihomo_version": "v1"}
	if diff := testjson.Diff(wantStatus, store.StatusJSON("a")); diff != "" {
		t.Fatalf("status diff: %s", diff)
	}
}

func TestReportRefusesInvalidHistoryWithoutCachingOldStatus(t *testing.T) {
	for _, content := range []string{"{", "[]", `{"status":{"value":"running"},"days":[]}`} {
		t.Run(content, func(t *testing.T) {
			isolated(t)
			store := NewNative(t.TempDir())
			if _, err := store.Add(context.Background(), "a", report(t, "running"), nil); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store.Path("a"), []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Add(context.Background(), "a", report(t, "unreachable"), nil); err == nil {
				t.Fatal("corrupt file written")
			}
			raw, err := os.ReadFile(store.Path("a"))
			if err != nil || string(raw) != content {
				t.Fatal("refused file changed", err)
			}
			if !statusNil(store.StatusJSON("a")) || len(CachedPaths()) != 0 {
				t.Fatal("old status cached")
			}
		})
	}
}

func TestReadErrorsAndInvalidDocumentsAreNotCached(t *testing.T) {
	for _, content := range []*string{nil, ptr("{"), ptr("[]"), ptr(`{"status":{"value":"running"},"days":[]}`)} {
		isolated(t)
		store := NewNative(t.TempDir())
		if content != nil {
			os.WriteFile(store.Path("a"), []byte(*content), 0o644)
		}
		calls := trackReads(store)
		if !statusNil(store.StatusJSON("a")) || !statusNil(store.StatusJSON("a")) {
			t.Fatal("status not None")
		}
		if len(*calls) != 2 || len(CachedPaths()) != 0 {
			t.Fatalf("calls %v cache %v", *calls, CachedPaths())
		}
	}
}

func ptr(s string) *string { return &s }

func TestOversizedOrUnexpectedStatusIsReturnedWithoutCaching(t *testing.T) {
	for _, status := range []map[string]any{
		{"value": strings.Repeat("x", 1025)},
		{"value": strings.Repeat("界", 400)},
		{"value": map[string]any{"nested": []any{}}},
		{"value": "running", "unexpected": "extra"},
	} {
		isolated(t)
		store := NewNative(t.TempDir())
		writeData(t, store, "a", status)
		calls := trackReads(store)
		if !testjson.Equal(status, store.StatusJSON("a")) || !testjson.Equal(status, store.StatusJSON("a")) {
			t.Fatal("status not returned")
		}
		if len(*calls) != 2 || len(CachedPaths()) != 0 {
			t.Fatalf("%v cached", status)
		}
	}
}

func TestStatusCacheIsLRUBoundedAndScopedByPath(t *testing.T) {
	isolated(t)
	old := SetCacheLimit(2)
	t.Cleanup(func() { SetCacheLimit(old) })
	dir := t.TempDir()
	first, second := NewNative(filepath.Join(dir, "one")), NewNative(filepath.Join(dir, "two"))
	writeData(t, first, "a", map[string]any{"value": "running"})
	writeData(t, second, "a", map[string]any{"value": "unreachable"})
	writeData(t, first, "b", map[string]any{"value": "no_controller"})
	if statusValue(t, first, "a") != "running" || statusValue(t, second, "a") != "unreachable" {
		t.Fatal("scoped by path")
	}
	first.StatusJSON("a")
	first.StatusJSON("b")
	paths := CachedPaths()
	secondPath, _ := filepath.Abs(second.Path("a"))
	if len(paths) != 2 || strings.Contains(strings.Join(paths, "\n"), secondPath) {
		t.Fatalf("cache %v", paths)
	}
}

// ---------- test_domain_discovery.py: clean_report and storage ----------

func item(host string, overrides ...testjson.Field) map[string]any {
	out := map[string]any{
		"host": host, "port": 443, "network": "tcp", "rule": "Match", "rule_payload": "",
		"policy": "漏网之鱼", "outlet": "direct", "conns": 1, "fails": 0, "fail_kinds": map[string]any{},
		"zero_dl": 0, "up": 10, "down": 20,
	}
	for _, f := range overrides {
		out[f.Key] = f.Value
	}
	return out
}

func discoveryReport(items ...any) map[string]any {
	if items == nil {
		items = []any{}
	}
	return map[string]any{
		"schema": 1, "status": "running", "mihomo_version": "v1.19.32",
		"window_start": "2026-10-10T10:00:00Z", "window_end": "2026-10-10T10:05:00Z",
		"ip_only_conns": 0, "dropped": 0, "items": items,
	}
}

func TestCleanReportAcceptsWellFormedReport(t *testing.T) {
	r := discoveryReport(item("www.example.com",
		testjson.Field{Key: "fails", Value: 3},
		testjson.Field{Key: "fail_kinds", Value: map[string]any{"timeout": 3}},
	))
	got := mustDecodeReport(t, r)
	if got == nil {
		t.Fatal("nil report")
	}
	gotItems, ok := testjson.Path(got, "items")
	if !ok || !testjson.Equal(r["items"], gotItems) {
		t.Fatalf("items diff: %s", testjson.Diff(r["items"], gotItems))
	}
}

func TestCleanReportAcceptsEmptyRule(t *testing.T) {
	got := mustDecodeReport(t, discoveryReport(item("www.example.com", testjson.Field{Key: "rule", Value: ""})))
	if got == nil || len(got.Items) != 1 {
		t.Fatalf("unexpected report: %+v", got)
	}
}

func TestCleanReportRejectsBadTopLevel(t *testing.T) {
	many := make([]any, ReportMaxItems+1)
	for i := range many {
		many[i] = item("a.com")
	}
	for name, mutate := range map[string]func(map[string]any){
		"extra-field": func(r map[string]any) { r["extra"] = 1 },
		"schema":      func(r map[string]any) { r["schema"] = 2 },
		"status":      func(r map[string]any) { r["status"] = "hacked" },
		"dropped":     func(r map[string]any) { r["dropped"] = -1 },
		"items-type":  func(r map[string]any) { r["items"] = "nope" },
		"too-many":    func(r map[string]any) { r["items"] = many },
	} {
		r := discoveryReport(item("www.example.com"))
		mutate(r)
		if mustDecodeReport(t, r) != nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestCleanReportDropsOnlyTheBadItem(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"ip-host":      func(i map[string]any) { i["host"] = "1.2.3.4" },
		"upper-host":   func(i map[string]any) { i["host"] = "Example.COM" },
		"space-host":   func(i map[string]any) { i["host"] = "bad host" },
		"outlet":       func(i map[string]any) { i["outlet"] = "tunnel" },
		"negative":     func(i map[string]any) { i["conns"] = -1 },
		"bool-count":   func(i map[string]any) { i["conns"] = true },
		"control-char": func(i map[string]any) { i["policy"] = "a\nb" },
		"long-policy":  func(i map[string]any) { i["policy"] = strings.Repeat("x", 129) },
		"long-payload": func(i map[string]any) { i["rule_payload"] = strings.Repeat("x", 257) },
		"fail-kind":    func(i map[string]any) { i["fail_kinds"] = map[string]any{"weird": 1} },
		"item-field":   func(i map[string]any) { i["path"] = "/secret" },
	} {
		bad := item("bad.example.com")
		mutate(bad)
		cleaned := mustDecodeReport(t, discoveryReport(item("good.example.com"), bad))
		if cleaned == nil || len(cleaned.Items) != 1 || cleaned.Items[0].Host != "good.example.com" || cleaned.Dropped.Int64() != 1 {
			t.Errorf("%s: %+v", name, cleaned)
		}
	}
}

// An array or object where a status or outlet choice belongs is invalid
// data (the route answers 500).
func TestCleanReportUnhashableStatusOrOutletRaise(t *testing.T) {
	r := discoveryReport()
	r["status"] = []any{"running"}
	raw, _ := json.Marshal(r)
	if _, err := DecodeReport(raw); !errors.Is(err, ErrInvalid) {
		t.Fatalf("status: %v", err)
	}
	raw, _ = json.Marshal(discoveryReport(item("a.com", testjson.Field{Key: "outlet", Value: map[string]any{}})))
	if _, err := DecodeReport(raw); !errors.Is(err, ErrInvalid) {
		t.Fatalf("outlet: %v", err)
	}
	// schema True == 1 and 1.0 == 1 pass, as in Python.
	for _, schema := range []any{true, 1.0} {
		r := discoveryReport()
		r["schema"] = schema
		if mustDecodeReport(t, r) == nil {
			t.Errorf("schema %v refused", schema)
		}
	}
}

func TestStoreMergesReportsAndExpiresOldDays(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	if _, err := store.Add(context.Background(), "a1", mustDecodeReport(t, discoveryReport(item("old.com"))), naive(2026, 10, 1, 12)); err != nil {
		t.Fatal(err)
	}
	now := naive(2026, 10, 10, 12)
	_, _ = store.Add(context.Background(), "a1", mustDecodeReport(t, discoveryReport(item("x.com",
		testjson.Field{Key: "conns", Value: 2},
		testjson.Field{Key: "fails", Value: 1},
		testjson.Field{Key: "fail_kinds", Value: map[string]any{"reset": 1}},
	))), now)
	_, _ = store.Add(context.Background(), "a1", mustDecodeReport(t, discoveryReport(item("x.com",
		testjson.Field{Key: "conns", Value: 3},
		testjson.Field{Key: "fails", Value: 2},
		testjson.Field{Key: "fail_kinds", Value: map[string]any{"reset": 2}},
	))), now)
	data := store.LoadDocument("a1", now)
	days := data.Days()
	if strings.Join(sortedKeys(days), ",") != "2026-10-10" {
		t.Fatal(sortedKeys(days))
	}
	stats := days["2026-10-10"].Hosts["x.com"].ByRoute[RouteKey("Match", "", "direct", "漏网之鱼")]
	if !testjson.Equal(5, stats.Conns) || !testjson.Equal(3, stats.Fails) || !testjson.Equal(map[string]any{"reset": 3}, stats.FailKinds) {
		t.Fatalf("stats: %+v", stats)
	}
	if val, _ := testjson.Path(data.StatusJSON(), "value"); val != "running" {
		t.Fatalf("status: %s", data.StatusJSON())
	}
}

func TestStoreCapsHostsPerDayKeepingHeaviest(t *testing.T) {
	isolated(t)
	old := MaxHostsPerDay
	MaxHostsPerDay = 2
	t.Cleanup(func() { MaxHostsPerDay = old })
	store := NewNative(t.TempDir())
	now := naive(2026, 10, 10, 12)
	if _, err := store.Add(context.Background(), "a1", mustDecodeReport(t, discoveryReport(
		item("a.com", testjson.Field{Key: "conns", Value: 1}),
		item("b.com", testjson.Field{Key: "conns", Value: 9}),
		item("c.com", testjson.Field{Key: "conns", Value: 5}),
	)), now); err != nil {
		t.Fatal(err)
	}
	day := store.LoadDocument("a1", now).Days()["2026-10-10"]
	if strings.Join(sortedKeys(day.Hosts), ",") != "b.com,c.com" || !testjson.Equal(1, day.Dropped) {
		t.Fatalf("day: %+v", day)
	}
}

// Merging needs item rule text: an item without "rule" (valid for
// DecodeReport) is a missing member, a null rule invalid; nothing is written.
func TestReportItemWithoutRuleFailsLikePython(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	withoutRule := item("x.com")
	delete(withoutRule, "rule")
	if _, err := store.Add(context.Background(), "a1", mustDecodeReport(t, discoveryReport(withoutRule)), nil); !errors.Is(err, ErrMissingMember) {
		t.Fatalf("missing rule: %v", err)
	}
	if _, err := store.Add(context.Background(), "a1", mustDecodeReport(t, discoveryReport(item("x.com", testjson.Field{Key: "rule", Value: nil}))), nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("None rule: %v", err)
	}
	if _, err := os.Stat(store.Path("a1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("written")
	}
}

func TestSplitRouteKey(t *testing.T) {
	route, ok := ParseRouteKey(RouteKey("Match", "", "direct", "a\tb"))
	want := Route{Rule: "Match", RulePayload: "", Outlet: "direct", Policy: "a\tb"}
	if !ok || route != want {
		t.Fatalf("route=%+v ok=%v", route, ok)
	}
	if _, ok := ParseRouteKey("a\tb"); ok {
		t.Fatal("short key accepted")
	}
}

// Delete never removes a directory (os.Remove is unlink) and reports EISDIR.
func TestDeleteRefusesDirectoryWithPythonText(t *testing.T) {
	isolated(t)
	dir := t.TempDir()
	store := NewNative(dir)
	os.MkdirAll(store.Path("a"), 0o755)
	err := store.Delete(context.Background(), "a")
	if !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("err %v", err)
	}
	if store.Delete(context.Background(), "missing") != nil {
		t.Fatal("missing file is fine")
	}
}
