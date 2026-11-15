package trafficstore_test

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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents/trafficstore"
	"configflow/internal/jsonstore"
	"configflow/internal/jsonx"
)

func isolated(t *testing.T) {
	t.Helper()
	trafficstore.ResetCache()
	t.Cleanup(trafficstore.ResetCache)
}

func defaultStatus() *jsonx.Object {
	return jsonx.Obj("value", "running", "at", "now", "mihomo_version", "1")
}

func writeData(t *testing.T, store *trafficstore.Store, agent string, status any) *jsonx.Object {
	t.Helper()
	if status == nil {
		status = defaultStatus()
	}
	large := make([]any, 100)
	for i := range large {
		large[i] = i
	}
	data := jsonx.Obj("status", status, "days", jsonx.Obj("today", jsonx.Obj("hosts",
		jsonx.Obj("synthetic.example", jsonx.Obj("large", large)))))
	if _, err := store.WriteJSON(agent, data); err != nil {
		t.Fatal(err)
	}
	return data
}

func trackReads(store *trafficstore.Store) *[]string {
	calls := &[]string{}
	original := store.ReadJSON
	store.ReadJSON = func(agent string) *jsonx.Object {
		*calls = append(*calls, agent)
		return original(agent)
	}
	return calls
}

func statusValue(t *testing.T, store *trafficstore.Store, agent string) any {
	t.Helper()
	status, _ := store.LoadStatus(agent).(*jsonx.Object)
	return status.Value("value")
}

func stat(t *testing.T, path string) (os.FileInfo, int64, int64) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info, info.ModTime().UnixNano(), info.Size()
}

// rewrite writes data compactly to path (destination for atomic) keeping
// the old mtime, then renames over path when atomic; in place it waits
// until the ctime moved.
func rewrite(t *testing.T, path string, data *jsonx.Object, atomic bool) {
	t.Helper()
	before, mtime, size := stat(t, path)
	destination := path
	if atomic {
		destination = strings.TrimSuffix(path, ".json") + ".new"
	}
	encoded, err := jsonx.Compact(data)
	if err != nil {
		t.Fatal(err)
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

func ctimeOf(t *testing.T, path string) int64 {
	return trafficstore.CtimeNs(mustStat(t, path))
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
	old := trafficstore.CtimeNs(previous)
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
	store := trafficstore.New(t.TempDir())
	writeData(t, store, "a", nil)
	calls := trackReads(store)
	first := store.LoadStatus("a").(*jsonx.Object)
	first.Set("value", "mutated")
	if statusValue(t, store, "a") != "running" {
		t.Fatal("cached status was mutated")
	}
	if strings.Join(*calls, ",") != "a" {
		t.Fatalf("reads %v", *calls)
	}
	paths := trafficstore.CachedPaths()
	if len(paths) != 1 || !jsonx.Equal(trafficstore.CachedStatus(paths[0]), defaultStatus()) {
		t.Fatalf("cache %v", paths)
	}
}

func TestExternalSameSizeSameMtimeChangeInvalidates(t *testing.T) {
	for _, atomic := range []bool{false, true} {
		isolated(t)
		store := trafficstore.New(t.TempDir())
		data := writeData(t, store, "a", nil)
		if statusValue(t, store, "a") != "running" {
			t.Fatal("first read")
		}
		data.GetObject("status").Set("value", "unknown")
		rewrite(t, store.Path("a"), data, atomic)
		if got := statusValue(t, store, "a"); got != "unknown" {
			t.Fatalf("atomic=%v: %v", atomic, got)
		}
	}
}

func TestReplacementDuringReadDoesNotLabelOldStatusWithNewSignature(t *testing.T) {
	isolated(t)
	store := trafficstore.New(t.TempDir())
	writeData(t, store, "a", nil)
	original := store.ReadJSON
	replaced := false
	store.ReadJSON = func(agent string) *jsonx.Object {
		data := original(agent)
		if !replaced {
			replaced = true
			writeData(t, store, "a", jsonx.Obj("value", "unreachable"))
		}
		return data
	}
	if statusValue(t, store, "a") != "running" {
		t.Fatal("first read")
	}
	if len(trafficstore.CachedPaths()) != 0 {
		t.Fatal("old status cached with the new signature")
	}
	if statusValue(t, store, "a") != "unreachable" {
		t.Fatal("second read")
	}
}

func report(status string) *jsonx.Object {
	return jsonx.Obj("status", status, "items", []any{})
}

func TestReportPopulatesStatusWithoutRedecodingHistoryAndDeleteInvalidates(t *testing.T) {
	isolated(t)
	store := trafficstore.New(t.TempDir())
	calls := trackReads(store)
	if _, err := store.AddReport("a", report("running"), nil); err != nil {
		t.Fatal(err)
	}
	if statusValue(t, store, "a") != "running" {
		t.Fatal("status after report")
	}
	returned, err := store.AddReport("a", report("unreachable"), nil)
	if err != nil {
		t.Fatal(err)
	}
	returned.Set("value", "caller mutation")
	if statusValue(t, store, "a") != "unreachable" {
		t.Fatal("caller mutation leaked")
	}
	if strings.Join(*calls, ",") != "a,a" {
		t.Fatalf("only the two report merges need a history decode: %v", *calls)
	}
	if err := store.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if len(trafficstore.CachedPaths()) != 0 || store.LoadStatus("a") != nil {
		t.Fatal("delete did not invalidate")
	}
}

func TestFullHistoryReadPopulatesIndependentStatusForOtherStoreInstances(t *testing.T) {
	isolated(t)
	dir := t.TempDir()
	first, second := trafficstore.New(dir), trafficstore.New(dir)
	writeData(t, first, "a", nil)
	loaded := first.Load("a", nil)
	loaded.GetObject("status").Set("value", "caller mutation")
	calls := trackReads(second)
	if statusValue(t, second, "a") != "running" || len(*calls) != 0 {
		t.Fatalf("calls %v", *calls)
	}
}

func TestFailedReportWriteNeverCachesUncommittedStatus(t *testing.T) {
	for _, afterReplace := range []bool{false, true} {
		isolated(t)
		store := trafficstore.New(t.TempDir())
		if _, err := store.AddReport("a", report("running"), nil); err != nil {
			t.Fatal(err)
		}
		original := store.WriteJSON
		store.WriteJSON = func(agent string, data *jsonx.Object) (*jsonstore.Signature, error) {
			if afterReplace {
				original(agent, data)
			}
			return nil, errors.New("write failed")
		}
		if _, err := store.AddReport("a", report("unreachable"), nil); err == nil || err.Error() != "write failed" {
			t.Fatalf("err %v", err)
		}
		if len(trafficstore.CachedPaths()) != 0 {
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
		store := trafficstore.New(t.TempDir())
		original := store.WriteJSON
		store.WriteJSON = func(agent string, data *jsonx.Object) (*jsonstore.Signature, error) {
			written, err := original(agent, data)
			if err != nil {
				return nil, err
			}
			changed := data.DeepCopy()
			changed.GetObject("status").Set("value", "unknown")
			rewrite(t, store.Path(agent), changed, atomic)
			return written, nil
		}
		if _, err := store.AddReport("a", report("running"), nil); err != nil {
			t.Fatal(err)
		}
		if len(trafficstore.CachedPaths()) != 0 {
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
	store := trafficstore.New(t.TempDir())
	original := store.WriteJSON
	store.WriteJSON = func(agent string, data *jsonx.Object) (*jsonstore.Signature, error) {
		if _, err := original(agent, data); err != nil {
			return nil, err
		}
		external := strings.TrimSuffix(store.Path(agent), ".json") + ".external"
		os.WriteFile(external, []byte(`{"status":{"value":"unreachable"},"days":{}}`), 0o644)
		os.Rename(external, store.Path(agent))
		return nil, nil
	}
	if _, err := store.AddReport("a", report("running"), nil); err != nil {
		t.Fatal(err)
	}
	if len(trafficstore.CachedPaths()) != 0 || statusValue(t, store, "a") != "unreachable" {
		t.Fatal("external replacement not honoured")
	}
}

func TestExternalInPlaceEditDuringAtomicWriteIsNotCached(t *testing.T) {
	for _, preserveMtime := range []bool{false, true} {
		isolated(t)
		store := trafficstore.New(t.TempDir())
		original := store.WriteJSON
		store.WriteJSON = func(agent string, data *jsonx.Object) (*jsonstore.Signature, error) {
			if _, err := original(agent, data); err != nil {
				return nil, err
			}
			path := store.Path(agent)
			previous := mustStat(t, path)
			raw, _ := os.ReadFile(path)
			value, _ := jsonx.Parse(raw)
			value.(*jsonx.Object).GetObject("status").Set("value", "unknown")
			encoded, _ := jsonx.Compact(value)
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
		if _, err := store.AddReport("a", report("running"), nil); err != nil {
			t.Fatal(err)
		}
		if len(trafficstore.CachedPaths()) != 0 || statusValue(t, store, "a") != "unknown" {
			t.Fatalf("preserveMtime=%v: edit not honoured", preserveMtime)
		}
	}
}

func naive(y int, m time.Month, d, h int) *time.Time {
	at := time.Date(y, m, d, h, 0, 0, 0, time.Local)
	return &at
}

func TestReportMergesExternalHistoryAndKeepsVersionAndRetention(t *testing.T) {
	isolated(t)
	store := trafficstore.New(t.TempDir())
	if _, err := store.AddReport("a", jsonx.Obj("status", "running", "mihomo_version", "v1", "items", []any{}), naive(2026, 10, 1, 0)); err != nil {
		t.Fatal(err)
	}
	data := store.Load("a", naive(2026, 10, 1, 0))
	data.GetObject("days").Set("2026-10-10", jsonx.Obj("ip_only_conns", 12, "dropped", 3, "hosts", jsonx.NewObject()))
	if _, err := store.WriteJSON("a", data); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddReport("a", jsonx.Obj("status", "unreachable", "ip_only_conns", 5, "items", []any{}), naive(2026, 10, 10, 0)); err != nil {
		t.Fatal(err)
	}
	saved := store.Load("a", naive(2026, 10, 10, 0))
	if strings.Join(saved.GetObject("days").Keys(), ",") != "2026-10-10" {
		t.Fatalf("days %v", saved.GetObject("days").Keys())
	}
	if !jsonx.Equal(saved.GetObject("days").Value("2026-10-10"), jsonx.Obj("ip_only_conns", 17, "dropped", 3, "hosts", jsonx.NewObject())) {
		t.Fatalf("day %s", jsonx.Repr(saved.GetObject("days").Value("2026-10-10")))
	}
	if !jsonx.Equal(store.LoadStatus("a"), jsonx.Obj("value", "unreachable", "at", "2026-10-10T00:00:00", "mihomo_version", "v1")) {
		t.Fatalf("status %s", jsonx.Repr(store.LoadStatus("a")))
	}
}

func TestReportRecoversInvalidHistoryWithoutCachingOldStatus(t *testing.T) {
	for _, content := range []string{"{", "[]", `{"status":{"value":"running"},"days":[]}`} {
		isolated(t)
		store := trafficstore.New(t.TempDir())
		if _, err := store.AddReport("a", report("running"), nil); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(store.Path("a"), []byte(content), 0o644)
		if _, err := store.AddReport("a", report("unreachable"), nil); err != nil {
			t.Fatal(err)
		}
		if statusValue(t, store, "a") != "unreachable" || store.Load("a", nil).GetObject("days").Len() != 1 {
			t.Fatalf("%q not recovered", content)
		}
	}
}

func TestReadErrorsAndInvalidDocumentsAreNotCached(t *testing.T) {
	for _, content := range []*string{nil, ptr("{"), ptr("[]"), ptr(`{"status":{"value":"running"},"days":[]}`)} {
		isolated(t)
		store := trafficstore.New(t.TempDir())
		if content != nil {
			os.WriteFile(store.Path("a"), []byte(*content), 0o644)
		}
		calls := trackReads(store)
		if store.LoadStatus("a") != nil || store.LoadStatus("a") != nil {
			t.Fatal("status not None")
		}
		if len(*calls) != 2 || len(trafficstore.CachedPaths()) != 0 {
			t.Fatalf("calls %v cache %v", *calls, trafficstore.CachedPaths())
		}
	}
}

func ptr(s string) *string { return &s }

func TestOversizedOrUnexpectedStatusIsReturnedWithoutCaching(t *testing.T) {
	for _, status := range []*jsonx.Object{
		jsonx.Obj("value", strings.Repeat("x", 1025)),
		jsonx.Obj("value", strings.Repeat("界", 400)),
		jsonx.Obj("value", jsonx.Obj("nested", []any{})),
		jsonx.Obj("value", "running", "unexpected", "extra"),
	} {
		isolated(t)
		store := trafficstore.New(t.TempDir())
		writeData(t, store, "a", status)
		calls := trackReads(store)
		if !jsonx.Equal(store.LoadStatus("a"), status) || !jsonx.Equal(store.LoadStatus("a"), status) {
			t.Fatal("status not returned")
		}
		if len(*calls) != 2 || len(trafficstore.CachedPaths()) != 0 {
			t.Fatalf("%s cached", jsonx.Repr(status))
		}
	}
}

func TestStatusCacheIsLRUBoundedAndScopedByPath(t *testing.T) {
	isolated(t)
	old := trafficstore.SetCacheLimit(2)
	t.Cleanup(func() { trafficstore.SetCacheLimit(old) })
	dir := t.TempDir()
	first, second := trafficstore.New(filepath.Join(dir, "one")), trafficstore.New(filepath.Join(dir, "two"))
	writeData(t, first, "a", jsonx.Obj("value", "running"))
	writeData(t, second, "a", jsonx.Obj("value", "unreachable"))
	writeData(t, first, "b", jsonx.Obj("value", "no_controller"))
	if statusValue(t, first, "a") != "running" || statusValue(t, second, "a") != "unreachable" {
		t.Fatal("scoped by path")
	}
	first.LoadStatus("a")
	first.LoadStatus("b")
	paths := trafficstore.CachedPaths()
	secondPath, _ := filepath.Abs(second.Path("a"))
	if len(paths) != 2 || strings.Contains(strings.Join(paths, "\n"), secondPath) {
		t.Fatalf("cache %v", paths)
	}
}

// ---------- test_domain_discovery.py: clean_report and storage ----------

func item(host string, pairs ...any) *jsonx.Object {
	out := jsonx.Obj("host", host, "port", 443, "network", "tcp", "rule", "Match", "rule_payload", "",
		"policy", "漏网之鱼", "outlet", "direct", "conns", 1, "fails", 0, "fail_kinds", jsonx.NewObject(),
		"zero_dl", 0, "up", 10, "down", 20)
	out.Update(jsonx.Obj(pairs...))
	return out
}

func discoveryReport(items ...any) *jsonx.Object {
	if items == nil {
		items = []any{}
	}
	return jsonx.Obj("schema", 1, "status", "running", "mihomo_version", "v1.19.32",
		"window_start", "2026-10-10T10:00:00Z", "window_end", "2026-10-10T10:05:00Z",
		"ip_only_conns", 0, "dropped", 0, "items", items)
}

func clean(t *testing.T, payload any) *jsonx.Object {
	t.Helper()
	cleaned, err := trafficstore.CleanReport(payload)
	if err != nil {
		t.Fatal(err)
	}
	return cleaned
}

func TestCleanReportAcceptsWellFormedReport(t *testing.T) {
	r := discoveryReport(item("www.example.com", "fails", 3, "fail_kinds", jsonx.Obj("timeout", 3)))
	if got := clean(t, r); !jsonx.Equal(got.Value("items"), r.Value("items")) {
		t.Fatal(jsonx.Repr(got))
	}
}

func TestCleanReportAcceptsEmptyRule(t *testing.T) {
	if got := clean(t, discoveryReport(item("www.example.com", "rule", ""))); len(got.GetList("items")) != 1 {
		t.Fatal(jsonx.Repr(got))
	}
}

func TestCleanReportRejectsBadTopLevel(t *testing.T) {
	many := make([]any, trafficstore.ReportMaxItems+1)
	for i := range many {
		many[i] = item("a.com")
	}
	for name, mutate := range map[string]func(*jsonx.Object){
		"extra-field": func(r *jsonx.Object) { r.Set("extra", 1) },
		"schema":      func(r *jsonx.Object) { r.Set("schema", 2) },
		"status":      func(r *jsonx.Object) { r.Set("status", "hacked") },
		"dropped":     func(r *jsonx.Object) { r.Set("dropped", -1) },
		"items-type":  func(r *jsonx.Object) { r.Set("items", "nope") },
		"too-many":    func(r *jsonx.Object) { r.Set("items", many) },
	} {
		r := discoveryReport(item("www.example.com"))
		mutate(r)
		if clean(t, r) != nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestCleanReportDropsOnlyTheBadItem(t *testing.T) {
	for name, mutate := range map[string]func(*jsonx.Object){
		"ip-host":      func(i *jsonx.Object) { i.Set("host", "1.2.3.4") },
		"upper-host":   func(i *jsonx.Object) { i.Set("host", "Example.COM") },
		"space-host":   func(i *jsonx.Object) { i.Set("host", "bad host") },
		"outlet":       func(i *jsonx.Object) { i.Set("outlet", "tunnel") },
		"negative":     func(i *jsonx.Object) { i.Set("conns", -1) },
		"bool-count":   func(i *jsonx.Object) { i.Set("conns", true) },
		"control-char": func(i *jsonx.Object) { i.Set("policy", "a\nb") },
		"long-policy":  func(i *jsonx.Object) { i.Set("policy", strings.Repeat("x", 129)) },
		"long-payload": func(i *jsonx.Object) { i.Set("rule_payload", strings.Repeat("x", 257)) },
		"fail-kind":    func(i *jsonx.Object) { i.Set("fail_kinds", jsonx.Obj("weird", 1)) },
		"item-field":   func(i *jsonx.Object) { i.Set("path", "/secret") },
	} {
		bad := item("bad.example.com")
		mutate(bad)
		cleaned := clean(t, discoveryReport(item("good.example.com"), bad))
		items := cleaned.GetList("items")
		if len(items) != 1 || items[0].(*jsonx.Object).Value("host") != "good.example.com" || !jsonx.Equal(cleaned.Value("dropped"), 1) {
			t.Errorf("%s: %s", name, jsonx.Repr(cleaned))
		}
	}
}

// Unhashable values tested against a frozenset raise TypeError (the route
// answers 500), as in Python.
func TestCleanReportUnhashableStatusOrOutletRaise(t *testing.T) {
	r := discoveryReport()
	r.Set("status", []any{"running"})
	if _, err := trafficstore.CleanReport(r); err == nil || err.Error() != "unhashable type: 'list'" {
		t.Fatalf("status: %v", err)
	}
	if _, err := trafficstore.CleanReport(discoveryReport(item("a.com", "outlet", jsonx.NewObject()))); err == nil ||
		err.Error() != "unhashable type: 'dict'" {
		t.Fatalf("outlet: %v", err)
	}
	// schema True == 1 and 1.0 == 1 pass, as in Python.
	for _, schema := range []any{true, jsonx.Float(1)} {
		r := discoveryReport()
		r.Set("schema", schema)
		if clean(t, r) == nil {
			t.Errorf("schema %v refused", schema)
		}
	}
}

func TestStoreMergesReportsAndExpiresOldDays(t *testing.T) {
	isolated(t)
	store := trafficstore.New(t.TempDir())
	if _, err := store.AddReport("a1", clean(t, discoveryReport(item("old.com"))), naive(2026, 10, 1, 12)); err != nil {
		t.Fatal(err)
	}
	now := naive(2026, 10, 10, 12)
	store.AddReport("a1", clean(t, discoveryReport(item("x.com", "conns", 2, "fails", 1, "fail_kinds", jsonx.Obj("reset", 1)))), now)
	store.AddReport("a1", clean(t, discoveryReport(item("x.com", "conns", 3, "fails", 2, "fail_kinds", jsonx.Obj("reset", 2)))), now)
	data := store.Load("a1", now)
	if strings.Join(data.GetObject("days").Keys(), ",") != "2026-10-10" {
		t.Fatal(data.GetObject("days").Keys())
	}
	stats := data.GetObject("days").GetObject("2026-10-10").GetObject("hosts").GetObject("x.com").GetObject("by_route").
		GetObject(trafficstore.RouteKey("Match", "", "direct", "漏网之鱼"))
	if !jsonx.Equal(stats.Value("conns"), 5) || !jsonx.Equal(stats.Value("fails"), 3) || !jsonx.Equal(stats.Value("fail_kinds"), jsonx.Obj("reset", 3)) {
		t.Fatal(jsonx.Repr(stats))
	}
	if data.GetObject("status").Value("value") != "running" {
		t.Fatal("status")
	}
}

func TestStoreCapsHostsPerDayKeepingHeaviest(t *testing.T) {
	isolated(t)
	old := trafficstore.MaxHostsPerDay
	trafficstore.MaxHostsPerDay = 2
	t.Cleanup(func() { trafficstore.MaxHostsPerDay = old })
	store := trafficstore.New(t.TempDir())
	now := naive(2026, 10, 10, 12)
	if _, err := store.AddReport("a1", clean(t, discoveryReport(item("a.com", "conns", 1), item("b.com", "conns", 9), item("c.com", "conns", 5))), now); err != nil {
		t.Fatal(err)
	}
	day := store.Load("a1", now).GetObject("days").GetObject("2026-10-10")
	if strings.Join(day.GetObject("hosts").Keys(), ",") != "b.com,c.com" || !jsonx.Equal(day.Value("dropped"), 1) {
		t.Fatal(jsonx.Repr(day))
	}
}

// Python indexes item['rule'] when merging: an item without "rule" (valid
// for clean_report) is a KeyError, None a TypeError; nothing is written.
func TestReportItemWithoutRuleFailsLikePython(t *testing.T) {
	isolated(t)
	store := trafficstore.New(t.TempDir())
	withoutRule := item("x.com")
	withoutRule.Delete("rule")
	if _, err := store.AddReport("a1", clean(t, discoveryReport(withoutRule)), nil); err == nil || err.Error() != "'rule'" {
		t.Fatalf("missing rule: %v", err)
	}
	if _, err := store.AddReport("a1", clean(t, discoveryReport(item("x.com", "rule", nil))), nil); err == nil ||
		err.Error() != "sequence item 0: expected str instance, NoneType found" {
		t.Fatalf("None rule: %v", err)
	}
	if _, err := os.Stat(store.Path("a1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("written")
	}
}

func TestSplitRouteKey(t *testing.T) {
	route, ok := trafficstore.SplitRouteKey(trafficstore.RouteKey("Match", "", "direct", "a\tb"))
	if !ok || !jsonx.Equal(route, jsonx.Obj("rule", "Match", "rule_payload", "", "outlet", "direct", "policy", "a\tb")) {
		t.Fatal(jsonx.Repr(route))
	}
	if _, ok := trafficstore.SplitRouteKey("a\tb"); ok {
		t.Fatal("short key accepted")
	}
}

// Delete never removes a directory (os.remove is unlink) and reports the
// failure with Python's OSError text.
func TestDeleteRefusesDirectoryWithPythonText(t *testing.T) {
	isolated(t)
	dir := t.TempDir()
	store := trafficstore.New(dir)
	os.MkdirAll(store.Path("a"), 0o755)
	err := store.Delete("a")
	if err == nil || err.Error() != "[Errno 21] Is a directory: '"+store.Path("a")+"'" {
		t.Fatalf("err %v", err)
	}
	if store.Delete("missing") != nil {
		t.Fatal("missing file is fine")
	}
}
