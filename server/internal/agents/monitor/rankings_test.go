package monitor

// Port of backend/test_oxidns_ranking_history.py: complete OxiDNS rankings
// from stored 15-minute periods. The fake Agent below behaves as the core
// does: one request ranks at most the newest 10,000 records of its window
// and returns at most `rows` entries. Python's tests patch
// oxidns_monitor._request; here requestNative. The random records come from Go's
// generator (the expectations are exact counts of the same records, so the
// sequence itself does not matter).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/model"
)

const (
	rDay  = HistoryDay
	rHour = HistoryHour
	rSlot = HistorySlot
	t0    = int64(1_793_000_000 - 1_793_000_000%HistoryDay) // a UTC midnight
)

type rankRecord struct {
	at      int64
	client  string
	domain  string
	blocked bool
}

type fakeAgent struct {
	mu      sync.Mutex
	records []rankRecord
	blocked bool
	cap     int
	calls   [][2]int64
	failAt  int // 0 = never
}

func newFakeAgent(records []rankRecord) *fakeAgent {
	sorted := append([]rankRecord(nil), records...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.at != b.at {
			return a.at < b.at
		}
		if a.client != b.client {
			return a.client < b.client
		}
		if a.domain != b.domain {
			return a.domain < b.domain
		}
		return !a.blocked && b.blocked
	})
	return &fakeAgent{records: sorted, blocked: true, cap: 10000}
}

func (f *fakeAgent) window(since, until int64, onlyBlocked bool) []rankRecord {
	var inside []rankRecord
	for _, r := range f.records {
		if since <= r.at && r.at < until && (!onlyBlocked || r.blocked) {
			inside = append(inside, r)
		}
	}
	if f.cap > 0 && len(inside) > f.cap {
		inside = inside[len(inside)-f.cap:] // the newest, like the core
	}
	return inside
}

func topCounts(records []rankRecord, pick func(rankRecord) string, rows int) []HistoryRow {
	counts := map[string]int64{}
	for _, r := range records {
		counts[pick(r)]++
	}
	out := make([]HistoryRow, 0, len(counts))
	for k, v := range counts {
		out = append(out, HistoryRow{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > rows {
		out = out[:rows]
	}
	return out
}

func (f *fakeAgent) section(records []rankRecord, pick func(rankRecord) string, rows int) map[string]any {
	list := []any{}
	for _, row := range topCounts(records, pick, rows) {
		list = append(list, map[string]any{"key": row.Key, "count": row.Count, "share": 0})
	}
	return map[string]any{"sample_size": len(records), "at_limit": len(records) == f.cap, "rows": list}
}

func byClient(r rankRecord) string { return r.client }
func byDomain(r rankRecord) string { return r.domain }

func (f *fakeAgent) answerMap(endpoint string, params query, minVersion string) (map[string]any, error) {
	if endpoint != "rankings" || minVersion != HistoryAgentVersion {
		panic("unexpected request " + endpoint + " " + minVersion)
	}
	sinceMS, _ := params.get("since_ms")
	untilMS, _ := params.get("until_ms")
	rowsText, _ := params.get("rows")
	recorder, _ := params.get("recorder")
	since, _ := strconv.ParseInt(sinceMS, 10, 64)
	until, _ := strconv.ParseInt(untilMS, 10, 64)
	since, until = since/1000, until/1000
	rows, _ := strconv.Atoi(rowsText)
	f.mu.Lock()
	f.calls = append(f.calls, [2]int64{since, until})
	failing := f.failAt != 0 && len(f.calls) >= f.failAt
	f.mu.Unlock()
	if failing {
		return nil, NewError("CORE_UNREACHABLE", 0, "")
	}
	sample := f.window(since, until, false)
	var blocked map[string]any
	if f.blocked {
		blocked = map[string]any{"available": true}
		updateMap(blocked, f.section(f.window(since, until, true), byDomain, rows))
	} else {
		blocked = map[string]any{"available": false, "reason": "unsupported_configuration"}
	}
	return map[string]any{
		"success": true, "recorder": recorder, "retention_days": 7,
		"clients": f.section(sample, byClient, rows),
		"domains": f.section(sample, byDomain, rows),
		"blocked": blocked,
	}, nil
}

func (f *fakeAgent) do(_ context.Context, _ Registry, _ *model.Agent, endpoint string, params query, minVersion string) (model.Fields, error) {
	answer, err := f.answerMap(endpoint, params, minVersion)
	if err != nil {
		return model.Fields{}, err
	}
	return fieldsFromValue(answer)
}

func (f *fakeAgent) callList() [][2]int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]int64(nil), f.calls...)
}

func (f *fakeAgent) clear() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

func generate(seed uint64, start int64, days float64, perHour float64) []rankRecord {
	const clients, domains = 30, 3000
	rng := rand.New(rand.NewPCG(seed, seed))
	weights := make([]float64, domains)
	total := 0.0
	for i := range weights {
		total += 1 / math.Pow(float64(i+1), 1.05)
		weights[i] = total
	}
	hostWeights := make([]float64, clients)
	hostTotal := 0.0
	for i := range hostWeights {
		hostTotal += 1 / float64(i+1)
		hostWeights[i] = hostTotal
	}
	pick := func(cumulative []float64, sum float64) int {
		x := rng.Float64() * sum
		return sort.SearchFloat64s(cumulative, x)
	}
	n := int(days * 24 * perHour)
	out := make([]rankRecord, n)
	for i := range out {
		name := fmt.Sprintf("n%d.example.test.", min(pick(weights, total), domains-1))
		out[i] = rankRecord{at: int64(float64(start) + rng.Float64()*days*float64(rDay)),
			client: fmt.Sprintf("192.0.2.%d", min(pick(hostWeights, hostTotal), clients-1)+1),
			domain: name, blocked: strings.HasPrefix(name, "n7")}
	}
	return out
}

type exactCounts struct {
	records, blockedRecords   int64
	clients, domains, blocked []HistoryRow
}

func exact(records []rankRecord, since, until int64) exactCounts {
	var inside, blocked []rankRecord
	for _, r := range records {
		if since <= r.at && r.at < until {
			inside = append(inside, r)
			if r.blocked {
				blocked = append(blocked, r)
			}
		}
	}
	return exactCounts{records: int64(len(inside)), blockedRecords: int64(len(blocked)),
		clients: topCounts(inside, byClient, 10), domains: topCounts(inside, byDomain, 10), blocked: topCounts(blocked, byDomain, 10)}
}

func rowsEqual(t *testing.T, got, want []HistoryRow, label string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s: got %v want %v", label, got, want)
	}
}

func useAgent(t *testing.T, do func(context.Context, Registry, *model.Agent, string, query, string) (model.Fields, error)) {
	previous := requestNative
	requestNative = do
	t.Cleanup(func() { requestNative = previous })
}

var historyAgent = mustAgentMap(map[string]any{"id": "dns", "name": "DNS", "version": HistoryAgentVersion})

func i64(v int64) *int64 { return &v }

func collect(t *testing.T, store *RankingStore, agent *model.Agent, now int64, budget, retention int) int {
	t.Helper()
	n, err := CollectAgentHistory(ctx, nil, store, agent, []string{"records"}, i64(now), budget, retention, 0)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func collectAll(t *testing.T, store *RankingStore, agent *model.Agent, fake *fakeAgent, now int64) {
	t.Helper()
	useAgent(t, fake.do)
	for collect(t, store, agent, now, 10_000, 0) > 0 {
	}
}

func query1(t *testing.T, store *RankingStore, agentID, recorder string, since, until int64) *HistoryAnswer {
	t.Helper()
	answer, err := store.Query(t.Context(), agentID, recorder, since, until)
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

// installFakeAgents owns the hook in the parent; children only read its map.
func installFakeAgents(t *testing.T, fakes map[string]*fakeAgent) {
	t.Helper()
	useAgent(t, func(ctx context.Context, m Registry, agent *model.Agent, endpoint string, params query, minVersion string) (model.Fields, error) {
		id := agent.ID
		fake := fakes[id]
		if fake == nil {
			return model.Fields{}, fmt.Errorf("unexpected test Agent %q", id)
		}
		return fake.do(ctx, m, agent, endpoint, params, minVersion)
	})
}

func TestParallelSeedFixturesAreIsolated(t *testing.T) {
	fakes := make(map[string]*fakeAgent)
	for seed := 1; seed <= 3; seed++ {
		id := strconv.Itoa(seed)
		rows := make([]rankRecord, seed)
		for i := range rows {
			rows[i] = rankRecord{t0 + int64(i), "192.0.2." + id, "seed" + id + ".example", false}
		}
		fakes[id] = newFakeAgent(rows)
	}
	installFakeAgents(t, fakes)
	for seed := 1; seed <= 3; seed++ {
		id := strconv.Itoa(seed)
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			store := NewRankingStore(t.TempDir())
			agent := cloneAgent(historyAgent)
			setAgentField(agent, "id", id)
			for collect(t, store, agent, t0+rSlot+120, 10_000, 0) > 0 {
			}
			got := query1(t, store, id, "records", t0, t0+rHour)
			if got.Records != int64(seed) || len(got.Rows["domains"]) != 1 || got.Rows["domains"][0].Key != "seed"+id+".example" {
				t.Fatal("parallel seed received another fixture", got)
			}
		})
	}
}

// --- the requirement: what the panel shows is every record --------------------

func TestStoredPeriodsAddUpToTheExactCountsOfEveryRecord(t *testing.T) {
	// The parent owns one global request hook until all parallel children finish.
	// Dispatch by Agent ID so each seed retains its own core, store and call log.
	fakes := make(map[string]*fakeAgent)
	for _, seed := range []uint64{1, 2, 3} {
		fakes[strconv.FormatUint(seed, 10)] = newFakeAgent(generate(seed, t0, 3, 16_500/8))
	}
	installFakeAgents(t, fakes)
	for _, seed := range []uint64{1, 2, 3} {
		id := strconv.FormatUint(seed, 10)
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			store := NewRankingStore(t.TempDir())
			fake := fakes[id]
			records := fake.records
			agent := cloneAgent(historyAgent)
			setAgentField(agent, "id", id)
			now := t0 + 3*rDay + 2*rSlot
			for collect(t, store, agent, now, 10_000, 0) > 0 {
			}
			until := LastCompleteSlotEnd(now)
			var want exactCounts
			for _, since := range []int64{t0, t0 + rDay, t0 + 2*rDay + 5*rHour} {
				got := query1(t, store, id, "records", since, until+rHour)
				want = exact(records, since, until)
				if got.Records != want.records || got.BlockedRecords != want.blockedRecords {
					t.Fatal(seed, since, got, want)
				}
				rowsEqual(t, got.Rows["clients"], want.clients, "clients")
				rowsEqual(t, got.Rows["domains"], want.domains, "domains")
				rowsEqual(t, got.Rows["blocked"], want.blocked, "blocked")
			}
			// A single request over the same three days sees the newest 10,000 only.
			if n := len(fake.window(t0, until, false)); n != 10_000 || int64(n) >= want.records {
				t.Fatal(n, want.records)
			}
		})
	}
}

func TestASlotOverTheCoreCapIsSplitUntilItFits(t *testing.T) {
	store := NewRankingStore(t.TempDir())
	records := make([]rankRecord, 25_000)
	for i := range records {
		records[i] = rankRecord{t0 + int64(i)*rSlot/25_000, fmt.Sprintf("192.0.2.%d", i%7+1), fmt.Sprintf("d%d.example.test.", i%40), false}
	}
	fake := newFakeAgent(records)
	useAgent(t, fake.do)
	if n := collect(t, store, historyAgent, t0+rSlot+120, 1, 0); n != 1 {
		t.Fatal(n)
	}
	got := query1(t, store, "dns", "records", t0, t0+rHour)
	if got.Records != 25_000 || got.PartialSlots != 0 {
		t.Fatal(got)
	}
	rowsEqual(t, got.Rows["domains"], exact(records, t0, t0+rSlot).domains, "domains")
	calls := fake.callList()
	if len(calls) <= 3 {
		t.Fatal(calls)
	}
	for _, c := range calls {
		if c[1]-c[0] < RankingMinSlot {
			t.Fatal(c)
		}
	}
}

func TestAMinuteStillOverTheCapIsKeptAndMarkedPartial(t *testing.T) {
	store := NewRankingStore(t.TempDir())
	records := make([]rankRecord, 12_000)
	for i := range records {
		records[i] = rankRecord{t0 + int64(i)*60/12_000, "192.0.2.1", "busy.example.test.", false}
	}
	useAgent(t, newFakeAgent(records).do)
	collect(t, store, historyAgent, t0+rSlot+120, 1, 0)
	got := query1(t, store, "dns", "records", t0, t0+rHour)
	if got.PartialSlots != 1 || got.Records != 10_000 {
		t.Fatal(got)
	}
}

// --- collecting -----------------------------------------------------------------

func TestCollectionTakesTheNewestMissingSlotsFirstAndNeverTwice(t *testing.T) {
	store := NewRankingStore(t.TempDir())
	fake := newFakeAgent(generate(4, t0, 1, 400))
	useAgent(t, fake.do)
	now := t0 + rDay + 120
	end := LastCompleteSlotEnd(now)
	starts := func() []int64 {
		var out []int64
		for _, c := range fake.callList() {
			out = append(out, c[0])
		}
		return out
	}
	if n := collect(t, store, historyAgent, now, 3, 1); n != 3 || fmt.Sprint(starts()) != fmt.Sprint([]int64{end - rSlot, end - 2*rSlot, end - 3*rSlot}) {
		t.Fatal(n, starts())
	}
	fake.clear()
	if n := collect(t, store, historyAgent, now, 2, 1); n != 2 || fmt.Sprint(starts()) != fmt.Sprint([]int64{end - 4*rSlot, end - 5*rSlot}) {
		t.Fatal(n, starts())
	}
	for collect(t, store, historyAgent, now, 50, 1) > 0 {
	}
	if got := query1(t, store, "dns", "records", 0, end+rHour); got.Slots != rDay/rSlot {
		t.Fatal(got.Slots)
	}
	fake.clear()
	if n := collect(t, store, historyAgent, now, 50, 1); n != 0 || len(fake.callList()) != 0 {
		t.Fatal(n, fake.callList())
	}
	// A slot is asked for only once it ended and the core had a minute to write it.
	collect(t, store, historyAgent, end+rSlot+RankingSettleSeconds-1, RankingSlotsPerRound, 1)
	if len(fake.callList()) != 0 {
		t.Fatal(fake.callList())
	}
	collect(t, store, historyAgent, end+rSlot+RankingSettleSeconds, RankingSlotsPerRound, 1)
	if calls := fake.callList(); len(calls) != 1 || calls[0] != [2]int64{end, end + rSlot} {
		t.Fatal(calls)
	}
}

func TestAFailingDeviceStopsTheRoundAndIsAskedAgainLater(t *testing.T) {
	store := NewRankingStore(t.TempDir())
	fake := newFakeAgent(generate(5, t0, 1, 200))
	fake.failAt = 3
	useAgent(t, fake.do)
	now := t0 + rDay + 120
	if n := collect(t, store, historyAgent, now, 10, 1); n != 2 || len(fake.callList()) != 3 {
		t.Fatal(n, fake.callList())
	}
	fake.failAt = 0
	if n := collect(t, store, historyAgent, now, 10, 1); n != 10 {
		t.Fatal(n)
	}
}

func testSlot(records int64, blocked *int64, rows map[string]*Counts) *Slot {
	return &Slot{Records: records, Blocked: blocked, Rows: rows}
}

func addSlot(t *testing.T, store *RankingStore, agentID, recorder string, start int64, slot *Slot) bool {
	t.Helper()
	added, err := store.StoreSlot(t.Context(), agentID, recorder, start, slot)
	if err != nil {
		t.Fatal(err)
	}
	return added
}

func TestAddingTheSameSlotTwiceCountsItOnce(t *testing.T) {
	store := NewRankingStore(t.TempDir())
	slot := func() *Slot {
		return testSlot(3, i64(1), map[string]*Counts{"clients": NewCounts("192.0.2.1", 3),
			"domains": NewCounts("a.test.", 2, "b.test.", 1), "blocked": NewCounts("b.test.", 1)})
	}
	if !addSlot(t, store, "dns", "records", t0, slot()) || addSlot(t, store, "dns", "records", t0, slot()) {
		t.Fatal("added twice")
	}
	addSlot(t, store, "dns", "records", t0+rSlot, slot())
	got := query1(t, store, "dns", "records", t0, t0+rHour)
	if got.Records != 6 || got.Slots != 2 || got.BlockedRecords != 2 {
		t.Fatal(got)
	}
	rowsEqual(t, got.Rows["domains"], []HistoryRow{{"a.test.", 4}, {"b.test.", 2}}, "domains")
	if got.First != t0 || got.Last != t0+2*rSlot {
		t.Fatal(got.First, got.Last)
	}
}

func TestRecordersAndDevicesAreKeptApart(t *testing.T) {
	store := NewRankingStore(t.TempDir())
	slot := func(key string) *Slot {
		return testSlot(1, nil, map[string]*Counts{"clients": NewCounts("192.0.2.1", 1), "domains": NewCounts(key, 1)})
	}
	addSlot(t, store, "dns", "records", t0, slot("a.test."))
	addSlot(t, store, "dns", "other", t0, slot("b.test."))
	addSlot(t, store, "dns2", "records", t0, slot("c.test."))
	rowsEqual(t, query1(t, store, "dns", "records", t0, t0+rHour).Rows["domains"], []HistoryRow{{"a.test.", 1}}, "records")
	rowsEqual(t, query1(t, store, "dns", "other", t0, t0+rHour).Rows["domains"], []HistoryRow{{"b.test.", 1}}, "other")
	if query1(t, store, "dns", "records", t0, t0+rHour).BlockedSlots != 0 {
		t.Fatal("blocked slots")
	}
}

// --- keeping ----------------------------------------------------------------------

func withConn(t *testing.T, store *RankingStore, body func(c *rankConn) error) {
	t.Helper()
	if err := store.with(t.Context(), body); err != nil {
		t.Fatal(err)
	}
}

func TestOldHoursBecomeDaysWithTheSameTotalsAndExpiredDaysGo(t *testing.T) {
	store := NewRankingStore(t.TempDir())
	records := generate(6, t0, 2, 600)
	collectAll(t, store, historyAgent, newFakeAgent(records), t0+2*rDay+120)
	before := *query1(t, store, "dns", "records", t0, t0+2*rDay)
	// Ten days later the two days are older than the hourly window.
	if err := store.Maintain(t.Context(), []string{"dns"}, i64(t0+12*rDay)); err != nil {
		t.Fatal(err)
	}
	var kinds []int64
	withConn(t, store, func(c *rankConn) error {
		return c.query("SELECT DISTINCT seconds FROM ranking_periods", nil, func(r *sql.Rows) error {
			var s int64
			if err := r.Scan(&s); err != nil {
				return err
			}
			kinds = append(kinds, s)
			return nil
		})
	})
	if fmt.Sprint(kinds) != fmt.Sprint([]int64{rDay}) {
		t.Fatal(kinds)
	}
	after := *query1(t, store, "dns", "records", t0, t0+2*rDay)
	if after.Records != before.Records || after.BlockedRecords != before.BlockedRecords || after.Slots != before.Slots {
		t.Fatal(before, after)
	}
	for _, kind := range RankingKinds {
		rowsEqual(t, after.Rows[kind], before.Rows[kind], kind)
	}
	// Merging again changes nothing (a new hour: maintenance runs again).
	store.maintainedHour = nil
	if err := store.Maintain(t.Context(), []string{"dns"}, i64(t0+12*rDay)); err != nil {
		t.Fatal(err)
	}
	again := query1(t, store, "dns", "records", t0, t0+2*rDay)
	if fmt.Sprint(*again) != fmt.Sprint(after) {
		t.Fatal(again, after)
	}
	// Past the retention period everything of that time is gone.
	if err := store.Maintain(t.Context(), []string{"dns"}, i64(t0+(HistoryRetentionDay+3)*rDay)); err != nil {
		t.Fatal(err)
	}
	if query1(t, store, "dns", "records", 0, t0+4*rDay).Slots != 0 {
		t.Fatal("not expired")
	}
}

func TestARemovedDeviceLosesItsHistory(t *testing.T) {
	store := NewRankingStore(t.TempDir())
	addSlot(t, store, "gone", "records", t0, testSlot(1, nil, map[string]*Counts{"clients": NewCounts("192.0.2.1", 1)}))
	addSlot(t, store, "kept", "records", t0, testSlot(1, nil, map[string]*Counts{"clients": NewCounts("192.0.2.1", 1)}))
	if err := store.Maintain(t.Context(), []string{"kept"}, i64(t0+rHour)); err != nil {
		t.Fatal(err)
	}
	if query1(t, store, "gone", "records", t0, t0+rHour).Slots != 0 || query1(t, store, "kept", "records", t0, t0+rHour).Slots != 1 {
		t.Fatal("removal")
	}
}

// --- the panel ------------------------------------------------------------------------

func useStores(t *testing.T) {
	ResetRankingStores()
	t.Cleanup(ResetRankingStores)
}

type panelEnv struct {
	m       *fakeManager
	current map[string]any
	live    []query
	mu      sync.Mutex
}

// panelAnswers answers overview with current and rankings with a live
// sample (recorded in live).
func (e *panelEnv) answer(nowMS int64) func(context.Context, Registry, *model.Agent, string, query, string) (model.Fields, error) {
	return func(_ context.Context, _ Registry, _ *model.Agent, endpoint string, params query, _ string) (model.Fields, error) {
		if endpoint == "overview" {
			return fieldsFromValue(cloneMap(e.current))
		}
		e.mu.Lock()
		e.live = append(e.live, params)
		e.mu.Unlock()
		return fieldsFromValue(rankingData(nowMS))
	}
}

func newPanelEnv(t *testing.T, records []rankRecord, now int64) *panelEnv {
	freshRankings(t)
	useStores(t)
	m := dashboardManager(t, t.TempDir())
	setAgentField(m.agents["one"], "version", HistoryAgentVersion)
	store := GetRankingStore(m.DataDir())
	useAgent(t, newFakeAgent(records).do)
	for {
		n, err := CollectAgentHistory(ctx, m, store, m.agents["one"], []string{"records"}, i64(now), 10_000, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	e := &panelEnv{m: m, current: snapshotData(now*1000, 10, 100)}
	useAgent(t, e.answer(now*1000))
	return e
}

func rankingRows(section map[string]any) []HistoryRow {
	var out []HistoryRow
	for _, item := range asList(section["rows"]) {
		row := asMap(item)
		out = append(out, HistoryRow{asStr(row["key"]), asInt64(row["count"])})
	}
	return out
}

func TestThePanelRanksFromTheStoredPeriodsForEachRange(t *testing.T) {
	records := generate(7, t0, 3, 500)
	now := t0 + 3*rDay + 120
	e := newPanelEnv(t, records, now)
	until := LastCompleteSlotEnd(now)
	for _, tc := range []struct {
		key   string
		since int64
	}{{"24h", now - rDay}, {"7d", now - 7*rDay}} {
		result := asMap(overview(t, e.m, "one", "range", tc.key)["rankings"])
		since := tc.since - tc.since%rHour
		want := exact(records, max(since, t0), until)
		if result["source"] != "history" || result["range"] != tc.key {
			t.Fatal(result)
		}
		clients := asMap(result["clients"])
		if asInt64(clients["sample_size"]) != want.records || clients["at_limit"] != false {
			t.Fatal(clients)
		}
		rowsEqual(t, rankingRows(asMap(result["domains"])), want.domains, "domains")
		rowsEqual(t, rankingRows(asMap(result["blocked"])), want.blocked, "blocked")
		if asInt64(result["until_ms"]) != until*1000 || asInt64(result["missing_slots"]) != 0 {
			t.Fatal(result)
		}
	}
	if len(e.live) != 0 {
		t.Fatal("the core was asked to rank", e.live)
	}
}

func TestTheRoundRangeStartsAtTheStatisticsStart(t *testing.T) {
	freshRankings(t)
	useStores(t)
	records := generate(10, t0, 2, 400)
	m := dashboardManager(t, t.TempDir())
	setAgentField(m.agents["one"], "version", HistoryAgentVersion)
	start := t0 + 5*rHour + 20*60
	e := &panelEnv{m: m, current: snapshotData(start*1000, 10, 100)}
	useAgent(t, e.answer(start*1000))
	first := overview(t, m, "one")
	if asInt64(asMap(first["statistics"])["started_at_ms"]) != start*1000 {
		t.Fatal(first)
	}
	now := t0 + 2*rDay + 120
	store := GetRankingStore(m.DataDir())
	collectAll(t, store, m.agents["one"], newFakeAgent(records), now)
	e.current = snapshotData(now*1000, 10, 100)
	useAgent(t, e.answer(now*1000))
	result := asMap(overview(t, m, "one")["rankings"])
	since := start - start%rHour
	want := exact(records, since, LastCompleteSlotEnd(now))
	if result["range"] != "round" || asInt64(result["since_ms"]) != since*1000 || asInt64(result["first_ms"]) != since*1000 {
		t.Fatal(result)
	}
	if asInt64(asMap(result["clients"])["sample_size"]) != want.records {
		t.Fatal(result["clients"], want.records)
	}
	rowsEqual(t, rankingRows(asMap(result["domains"])), want.domains, "domains")
}

func TestThePanelFallsBackToTheLiveSampleUntilPeriodsExist(t *testing.T) {
	freshRankings(t)
	useStores(t)
	m := dashboardManager(t, t.TempDir())
	setAgentField(m.agents["one"], "version", HistoryAgentVersion)
	f := transport(t, nil, nil)
	result := asMap(overview(t, m, "one", "range", "7d")["rankings"])
	if result["source"] == "history" || result["success"] != true || result["history"] != "collecting" {
		t.Fatal(result)
	}
	if len(f.rankingCalls()) == 0 {
		t.Fatal(f.endpoints())
	}
}

func TestAnOlderAgentKeepsTheLiveSample(t *testing.T) {
	records := generate(8, t0, 1, 300)
	now := t0 + rDay + 120
	e := newPanelEnv(t, records, now)
	setAgentField(e.m.agents["one"], "version", "1.8.54-go")
	result := asMap(overview(t, e.m, "one")["rankings"])
	if result["source"] == "history" || len(e.live) == 0 {
		t.Fatal(result)
	}
	if result["history"] != "upgrade_required" || result["history_version"] != HistoryAgentVersion {
		t.Fatal(result)
	}
}

func TestThePanelSaysWhichSlotsAreMissing(t *testing.T) {
	records := generate(9, t0, 1, 300)
	now := t0 + rDay + 120
	e := newPanelEnv(t, records, now)
	store := GetRankingStore(e.m.DataDir())
	withConn(t, store, func(c *rankConn) error { // the device was away for two hours
		if _, err := c.write("DELETE FROM ranking_slots WHERE start >= ? AND start < ?", t0+5*rHour, t0+7*rHour); err != nil {
			return err
		}
		_, err := c.write("DELETE FROM ranking_periods WHERE start >= ? AND start < ?", t0+5*rHour, t0+7*rHour)
		return err
	})
	store.version++ // the Python test deletes behind the store's back; its answers were not cached yet
	result := asMap(overview(t, e.m, "one", "range", "24h")["rankings"])
	if asInt64(result["missing_slots"]) != 8 {
		t.Fatal(result)
	}
}

func TestUnknownRangesAreRefused(t *testing.T) {
	for _, value := range []string{"1h", "", "ALL", "30"} {
		freshRankings(t)
		m := dashboardManager(t, t.TempDir())
		transport(t, nil, nil)
		_, err := ReadOverview(ctx, m, "one", args("range", value))
		wantMonitorError(t, err, "INVALID_QUERY", 400)
	}
}

type roundManager struct {
	*fakeManager
	list      []*model.Agent
	recorders func(*model.Agent) ([]string, error)
	collectFn func(*RankingStore, *model.Agent, []string, *int64) error
	usableFn  func(*model.Agent) bool
}

func (m *roundManager) ReadAgents(_ context.Context, _ bool, inspect func(*model.Agent)) ([]model.Agent, error) {
	out := make([]model.Agent, len(m.list))
	for i, a := range m.list {
		cp := cloneAgent(a)
		if inspect != nil {
			inspect(cp)
		}
		out[i] = *cp
	}
	return out, nil
}

func (m *roundManager) historyUsable(a *model.Agent) bool {
	if m.usableFn != nil {
		return m.usableFn(a)
	}
	return agents.AgentEnabledModel(a) && a.Status == "online" && agents.FreshHeartbeat(a.LastHeartbeat, nil)
}

func (m *roundManager) historyRecorders(ctx context.Context, a *model.Agent) ([]string, error) {
	if m.recorders != nil {
		return m.recorders(a)
	}
	snapshot, err := readOverviewSnapshot(ctx, m, a, true)
	if err != nil {
		return nil, err
	}
	return snapshot.Recorders, nil
}

func (m *roundManager) historyCollect(ctx context.Context, store *RankingStore, a *model.Agent, recorders []string, now *int64) error {
	if m.collectFn != nil {
		return m.collectFn(store, a, recorders, now)
	}
	_, err := CollectAgentHistory(ctx, m, store, a, recorders, now, RankingSlotsPerRound, RankingCoreRetentionDays, 200*time.Millisecond)
	return err
}

func TestACollectionRoundCoversUsableNewOxidnsDevicesOnly(t *testing.T) {
	var seen []string
	m := &roundManager{
		fakeManager: &fakeManager{dataDir: t.TempDir()},
		list: []*model.Agent{
			mustAgentMap(map[string]any{"id": "new", "service_type": "oxidns", "version": HistoryAgentVersion}),
			mustAgentMap(map[string]any{"id": "old", "service_type": "oxidns", "version": "1.8.54-go"}),
			mustAgentMap(map[string]any{"id": "off", "service_type": "oxidns", "version": "1.8.60-go", "enabled": false}),
			mustAgentMap(map[string]any{"id": "mihomo", "service_type": "mihomo", "version": "1.9.0-go"}),
		},
		recorders: func(*model.Agent) ([]string, error) { return []string{"records"}, nil },
		collectFn: func(_ *RankingStore, agent *model.Agent, _ []string, _ *int64) error {
			seen = append(seen, agent.ID)
			return nil
		},
		usableFn: func(agent *model.Agent) bool { return agent.Enabled == nil || *agent.Enabled },
	}
	ResetRankingStores()
	t.Cleanup(ResetRankingStores)
	if err := CollectHistoryRound(ctx, m, nil, i64(t0)); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(seen) != "[new]" {
		t.Fatal(seen)
	}
}

func TestARoundWithoutAFinishedPeriodSaysOtherRangesAreComplete(t *testing.T) {
	records := generate(12, t0, 1, 300)
	now := t0 + rDay + 120
	e := newPanelEnv(t, records, now)
	later := now + 5*60
	e.current = snapshotData(later*1000, 10, 100)
	useAgent(t, e.answer(later*1000))
	if _, err := ResetStatistics(ctx, e.m, "one", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	result := asMap(overview(t, e.m, "one")["rankings"])
	if result["source"] == "history" || result["history"] != "pending_range" {
		t.Fatal(result)
	}
	if asMap(overview(t, e.m, "one", "range", "24h")["rankings"])["source"] != "history" {
		t.Fatal("24h not from history")
	}
}

// --- idle cost: nothing is added up or asked for again until the data changes ---

func TestAnswersAreReusedUntilASlotIsStored(t *testing.T) {
	store := NewRankingStore(t.TempDir())
	slot := func(key string) *Slot {
		return testSlot(1, nil, map[string]*Counts{"clients": NewCounts("192.0.2.1", 1), "domains": NewCounts(key, 1)})
	}
	addSlot(t, store, "dns", "records", t0, slot("a.test."))
	sums := 0
	store.queryHook = func(agentID, recorder string, since, until int64) (*HistoryAnswer, error) {
		sums++
		return store.query(t.Context(), agentID, recorder, since, until)
	}
	first := query1(t, store, "dns", "records", t0, t0+rHour)
	for range 5 { // the panel polls
		if query1(t, store, "dns", "records", t0, t0+rHour) != first {
			t.Fatal("not reused")
		}
	}
	if sums != 1 {
		t.Fatal(sums)
	}
	addSlot(t, store, "dns", "records", t0+rSlot, slot("b.test."))
	if query1(t, store, "dns", "records", t0, t0+rHour).Records != 2 || sums != 2 {
		t.Fatal(sums)
	}
	addSlot(t, store, "dns", "records", t0+rSlot, slot("b.test.")) // already stored: nothing changed
	query1(t, store, "dns", "records", t0, t0+rHour)
	if sums != 2 {
		t.Fatal(sums)
	}
	// Maintenance that removes data also makes answers stale.
	if err := store.Maintain(t.Context(), []string{"someone-else"}, i64(t0+rHour)); err != nil {
		t.Fatal(err)
	}
	if query1(t, store, "dns", "records", t0, t0+rHour).Slots != 0 || sums != 3 {
		t.Fatal(sums)
	}
}

func TestMaintenanceRunsOnceAnHour(t *testing.T) {
	store := NewRankingStore(t.TempDir())
	addSlot(t, store, "dns", "records", t0, testSlot(1, nil, map[string]*Counts{"clients": NewCounts("192.0.2.1", 1)}))
	if err := store.Maintain(t.Context(), []string{"dns"}, i64(t0+10)); err != nil {
		t.Fatal(err)
	}
	store.Maintain(t.Context(), nil, i64(t0+20)) // the same hour: skipped, the device's data stays
	if query1(t, store, "dns", "records", t0, t0+rHour).Slots != 1 {
		t.Fatal("removed in the same hour")
	}
	store.Maintain(t.Context(), nil, i64(t0+rHour))
	if query1(t, store, "dns", "records", t0, t0+rHour).Slots != 0 {
		t.Fatal("not removed")
	}
}

func TestACaughtUpDeviceIsNotContactedUntilTheNextSlotEnds(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	ask := func(what string) { mu.Lock(); asked = append(asked, what); mu.Unlock() }
	count := func(what string) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, a := range asked {
			if a == what {
				n++
			}
		}
		return n
	}
	fake := newFakeAgent(generate(13, t0, 1, 100))
	useAgent(t, func(c context.Context, m Registry, a *model.Agent, endpoint string, p query, v string) (model.Fields, error) {
		ask("rankings")
		return fake.do(c, m, a, endpoint, p, v)
	})
	previousSleep := rankingSleep
	rankingSleep = func(context.Context, time.Duration) bool { return true }
	t.Cleanup(func() { rankingSleep = previousSleep })
	m := &roundManager{
		fakeManager: &fakeManager{dataDir: t.TempDir()},
		list:        []*model.Agent{mustAgentMap(map[string]any{"id": "dns", "service_type": "oxidns", "version": HistoryAgentVersion})},
		recorders:   func(*model.Agent) ([]string, error) { ask("overview"); return []string{"records"}, nil },
		usableFn:    func(*model.Agent) bool { return true },
	}
	store := NewRankingStore(m.dataDir)
	now := t0 + rDay + 120
	rounds := 0
	for {
		if _, ok := store.CaughtUp("dns"); ok {
			break
		}
		if err := CollectHistoryRound(ctx, m, store, i64(now)); err != nil {
			t.Fatal(err)
		}
		rounds++
	}
	if count("overview") != rounds || count("rankings") != 7*rDay/rSlot {
		t.Fatal(rounds, count("overview"), count("rankings"))
	}
	if want := (7*rDay/rSlot + RankingSlotsPerRound - 1) / RankingSlotsPerRound; rounds != int(want) {
		t.Fatal(rounds, want)
	}
	mu.Lock()
	asked = nil
	mu.Unlock()
	for minute := int64(1); minute < 14; minute++ { // the rest of this quarter hour
		CollectHistoryRound(ctx, m, store, i64(now+minute*60))
	}
	if len(asked) != 0 {
		t.Fatal(asked)
	}
	CollectHistoryRound(ctx, m, store, i64(now+15*60)) // the next slot ended
	if fmt.Sprint(asked) != "[overview rankings]" {
		t.Fatal(asked)
	}
}

func TestADeviceCutShortIsNotCountedAsCaughtUp(t *testing.T) {
	fake := newFakeAgent(generate(14, t0, 1, 100))
	fake.failAt = 2
	useAgent(t, fake.do)
	store := NewRankingStore(t.TempDir())
	collect(t, store, historyAgent, t0+rDay+120, 5, 1)
	if _, ok := store.CaughtUp("dns"); ok {
		t.Fatal("caught up after a failure")
	}
	fake.failAt = 0
	collect(t, store, historyAgent, t0+rDay+120, 2, 1)
	if _, ok := store.CaughtUp("dns"); ok {
		t.Fatal("caught up out of budget") // more is due next round
	}
}

// --- Python details beyond the ported cases ----------------------------------------

func TestSlotRankingRejectsUnexpectedAnswers(t *testing.T) {
	store := NewRankingStore(t.TempDir())
	good, _ := newFakeAgent(nil).answerMap("rankings", query{{"recorder", "records"}, {"since_ms", "0"}, {"until_ms", "900000"}, {"rows", "2000"}}, HistoryAgentVersion)
	for _, mutate := range []func(map[string]any){
		func(o map[string]any) { o["success"] = 1 },
		func(o map[string]any) { o["recorder"] = "other" },
		func(o map[string]any) { o["blocked"] = nil },
		func(o map[string]any) { asMap(o["blocked"])["available"] = 1 },
		func(o map[string]any) { asMap(o["clients"])["at_limit"] = true },
		func(o map[string]any) { o["domains"] = "x" },
	} {
		answer := cloneMap(good)
		mutate(answer)
		useAgent(t, func(context.Context, Registry, *model.Agent, string, query, string) (model.Fields, error) {
			return fieldsFromValue(cloneMap(answer))
		})
		if n := collect(t, store, historyAgent, t0+rDay+120, 5, 1); n != 0 {
			t.Fatal(n)
		}
	}
	// An unavailable blocked section stores the slot without blocked counts.
	answer := cloneMap(good)
	answer["blocked"] = map[string]any{"available": false}
	useAgent(t, func(context.Context, Registry, *model.Agent, string, query, string) (model.Fields, error) {
		return fieldsFromValue(cloneMap(answer))
	})
	if n := collect(t, store, historyAgent, t0+rDay+120, 1, 1); n != 1 {
		t.Fatal(n)
	}
	if got := query1(t, store, "dns", "records", 0, t0+2*rDay); got.Slots != 1 || got.BlockedSlots != 0 {
		t.Fatal(got)
	}
}
