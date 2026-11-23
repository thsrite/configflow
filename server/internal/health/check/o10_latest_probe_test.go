package check

import (
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"configflow/internal/model"
)

// latestProbeEntriesSequential is the pre-O10 algorithm (one Set per host),
// kept as the reference for ordering and values.
func latestProbeEntriesSequential(t *testing.T, agentIDs []string, store *ProbeStore) model.OrderedMap[TimedRecord] {
	t.Helper()
	var merged model.OrderedMap[TimedRecord]
	for _, agentID := range agentIDs {
		hosts := store.LoadEntries(agentID, time.Time{})
		for host, entry := range hosts.All() {
			if previous, seen := merged.Get(host); seen && !(entry.CheckedAt() > previous.CheckedAt()) {
				continue
			}
			copied := entry
			if err := copied.Fields.Set("agent_id", []byte(strconv.Quote(agentID))); err != nil {
				t.Fatal(err)
			}
			merged.Set(host, copied)
		}
	}
	return merged
}

func seedProbeHosts(t *testing.T, store *ProbeStore, agentID string, hosts []string, checked time.Time) {
	t.Helper()
	var entries model.OrderedMap[TimedRecord]
	for _, host := range hosts {
		entries.Set(host, mustTimedRecord(t, `{"checked_at":"`+NaiveISOSeconds(checked)+`","verdict":"both_ok"}`))
	}
	if err := store.SaveEntries(agentID, entries, checked); err != nil {
		t.Fatal(err)
	}
}

func TestLatestProbeEntriesMatchesSequentialMerge(t *testing.T) {
	store := NewProbeStore(t.TempDir())
	now := time.Now().Add(-time.Hour)
	seedProbeHosts(t, store, "a1", []string{"b.example.com", "a.example.com", "c.example.com"}, now.Add(-time.Minute))
	// a2 is newer for a.example.com, adds d; a3 is older for b.
	seedProbeHosts(t, store, "a2", []string{"d.example.com", "a.example.com"}, now)
	seedProbeHosts(t, store, "a3", []string{"b.example.com"}, now.Add(-2*time.Minute))
	ids := []string{"a1", "a2", "a3", "missing"}
	got, err := LatestProbeEntries(ids, store)
	if err != nil {
		t.Fatal(err)
	}
	want := latestProbeEntriesSequential(t, ids, store)
	gotJSON, err := model.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := model.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) || !slices.Equal(got.Keys(), want.Keys()) {
		t.Fatalf("got %s, want %s", gotJSON, wantJSON)
	}
	if entry, _ := got.Get("a.example.com"); string(mustMember(t, entry, "agent_id")) != `"a2"` {
		t.Fatalf("a.example.com should come from the newer a2 probe: %+v", entry)
	}
	empty, err := LatestProbeEntries([]string{"missing"}, store)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := model.Marshal(empty); string(data) != "{}" {
		t.Fatalf("no probes = %s, want {}", data)
	}
}

func mustMember(t *testing.T, entry TimedRecord, name string) []byte {
	t.Helper()
	raw, ok := entry.Fields.Get(name)
	if !ok {
		t.Fatalf("missing member %s", name)
	}
	return raw
}

// TestLatestProbeEntriesAllocationIsLinear guards the O10 fix: building the
// merged Map with one Set per host copied the whole Map each time.
func TestLatestProbeEntriesAllocationIsLinear(t *testing.T) {
	now := time.Now().Add(-time.Hour)
	build := func(n int) *ProbeStore {
		store := NewProbeStore(t.TempDir())
		hosts := make([]string, n)
		for i := range hosts {
			hosts[i] = "h" + strconv.Itoa(i) + ".example.com"
		}
		seedProbeHosts(t, store, "a1", hosts, now)
		return store
	}
	measure := func(fn func()) int64 {
		fn()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for range 3 {
			fn()
		}
		runtime.ReadMemStats(&after)
		return int64(after.TotalAlloc-before.TotalAlloc) / 3
	}
	small, large := build(250), build(2500)
	merge := func(store *ProbeStore) func() {
		return func() {
			if _, err := LatestProbeEntries([]string{"a1"}, store); err != nil {
				t.Fatal(err)
			}
		}
	}
	// LoadEntries decodes the file (linear); only the merge on top is compared.
	load := func(store *ProbeStore) func() { return func() { store.LoadEntries("a1", time.Time{}) } }
	loadSmall, loadLarge := measure(load(small)), measure(load(large))
	mergeSmall := max(measure(merge(small))-loadSmall, 0)
	mergeLarge := max(measure(merge(large))-loadLarge, 0)
	if mergeLarge > 20*mergeSmall+64*1024 {
		t.Fatalf("merge allocations grew superlinearly: 250 hosts=%d B, 2500 hosts=%d B", mergeSmall, mergeLarge)
	}
	// Negative control: the per-Set reference grows quadratically on the same data.
	ctrlSmall := max(measure(func() { latestProbeEntriesSequential(t, []string{"a1"}, small) })-loadSmall, 0)
	ctrlLarge := max(measure(func() { latestProbeEntriesSequential(t, []string{"a1"}, large) })-loadLarge, 0)
	if ctrlLarge <= 20*ctrlSmall+64*1024 {
		t.Fatalf("control: sequential Set did not grow superlinearly: %d B vs %d B", ctrlSmall, ctrlLarge)
	}
}
