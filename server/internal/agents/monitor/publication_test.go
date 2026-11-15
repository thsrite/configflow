package monitor

// Port of backend/test_oxidns_publication_stats.py: publication
// checkpoints preserve counters without a background collector. The
// Python tests patch _request / _overview_snapshot; here doRequest
// (fakeTransport, whose hook runs while the snapshot is taken).

import (
	"bytes"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// task is the Python test helper: a deployment record of the Agent and its
// latest_deployment copy.
func task(m *fakeManager, ident, status, revision string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	agent := m.agents["one"]
	profile := agent.Value("profile_id")
	if !jsonx.Truthy(profile) {
		profile = "default"
	}
	deployments, _ := agent.Value("deployments").(*jsonx.Object)
	if deployments == nil {
		deployments = jsonx.NewObject()
		agent.Set("deployments", deployments)
	}
	record := jsonx.Obj("deployment_id", ident, "status", status, "service_type", "oxidns",
		"profile_id", profile, "config_version", revision)
	deployments.Set(ident, record)
	agent.Set("latest_deployment", record.DeepCopy())
}

var (
	revB = strings.Repeat("b", 64)
	revC = strings.Repeat("c", 64)
	revE = strings.Repeat("e", 64)
	revF = strings.Repeat("f", 64)
)

// change is the Python helper: a new snapshot and rankings at now.
func change(f *fakeTransport, now int64, count, elapsed int, revision, runtime string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cur().Update(snapshotData(now, count, elapsed))
	f.cur().Set("source_revision", revision)
	f.cur().Set("runtime_revision", runtime)
	f.rk().Update(rankingData(now))
	f.rk().Set("source_revision", revision)
	f.rk().Set("runtime_revision", runtime)
}

type pubEnv struct {
	m       *fakeManager
	f       *fakeTransport
	initial *jsonx.Object
	store   *StateStore
}

func newPubEnv(t *testing.T) *pubEnv {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, nil, nil)
	initial := overview(t, m, "one")
	f.cur().Update(snapshotData(2000, 40, 900))
	f.rk().Set("collected_at_ms", 2000)
	task(m, "publish-one", "uploading", revB)
	return &pubEnv{m, f, initial, NewStateStore(dir)}
}

func (e *pubEnv) checkpoint(t *testing.T, ident, revision string) {
	t.Helper()
	if err := Checkpoint(ctx, e.m, "one", ident, revision); err != nil {
		t.Fatal(err)
	}
}

func (e *pubEnv) complete(t *testing.T, ident, status string) {
	t.Helper()
	if err := Complete(ctx, e.m, "one", ident, status); err != nil {
		t.Fatal(err)
	}
}

func (e *pubEnv) read(t *testing.T) *jsonx.Object {
	t.Helper()
	state, err := e.store.Read("one")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestTwoPublicationsAccumulateWeightedCountersKeepRankingsAndLiveGauges(t *testing.T) {
	e := newPubEnv(t)
	e.checkpoint(t, "publish-one", revB)
	if e.read(t).GetInt("schema") != 2 {
		t.Fatal(e.read(t))
	}
	if eps := e.f.endpoints(); eps[len(eps)-1] != "overview" {
		t.Fatal("history scan at publication time", eps)
	}
	change(e.f, 3000, 6, 60, revB, revE)
	task(e.m, "publish-one", "succeeded", revB)
	e.complete(t, "publish-one", "succeeded")
	result := overview(t, e.m, "one")
	metrics := result.GetObject("metrics")
	if metrics.GetInt("requests_total") != 36 {
		t.Fatal(metrics)
	}
	approx(t, metrics.Value("avg_latency_ms"), 860.0/36)
	approx(t, metrics.Value("cache_hit_rate"), 50)
	approx(t, metrics.Value("inflight"), 2)
	if result.GetList("caches")[0].(*jsonx.Object).GetInt("entries") != 30 ||
		result.GetList("upstreams")[0].(*jsonx.Object).GetInt("requests") != 36 {
		t.Fatal(result)
	}
	statistics := result.GetObject("statistics")
	if statistics.GetInt("started_at_ms") != e.initial.GetObject("statistics").GetInt("started_at_ms") ||
		statistics.GetInt("publication_count") != 1 || statistics.Value("reason") != "publication" {
		t.Fatal(statistics)
	}
	calls := e.f.rankingCalls()
	if since, _ := calls[len(calls)-1].get("since_ms"); since != "1000" {
		t.Fatal(calls)
	}
	saved := readFile(t, e.store.Path("one"))
	e.complete(t, "publish-one", "succeeded")
	overview(t, e.m, "one")
	if !bytes.Equal(readFile(t, e.store.Path("one")), saved) {
		t.Fatal("state rewritten")
	}

	// Simulate a ConfigFlow process restart between publications.
	restarted := dashboardManager(t, e.m.dataDir)
	for id, agent := range e.m.agents {
		restarted.agents[id] = agent.DeepCopy()
	}
	e.m = restarted
	change(e.f, 4000, 16, 160, revB, revE)
	task(restarted, "publish-two", "uploading", revC)
	e.checkpoint(t, "publish-two", revC)
	change(e.f, 5000, 5, 100, revC, revF)
	task(restarted, "publish-two", "succeeded", revC)
	// Overview also reconciles a terminal result if the server died before complete().
	result = overview(t, restarted, "one")
	metrics = result.GetObject("metrics")
	if metrics.GetInt("requests_total") != 51 {
		t.Fatal(metrics)
	}
	approx(t, metrics.Value("avg_latency_ms"), 1060.0/51)
	approx(t, metrics.Value("cache_hit_rate"), 25.0/51*100)
	if s := result.GetObject("statistics"); s.GetInt("publication_count") != 2 || s.GetInt("started_at_ms") != 1000 {
		t.Fatal(s)
	}
}

func TestOverviewDoesNotDiscardCheckpointBeforeTransactionConfirmation(t *testing.T) {
	for _, status := range []string{"uploading", "reloading", "unknown"} {
		e := newPubEnv(t)
		e.checkpoint(t, "publish-one", revB)
		saved := readFile(t, e.store.Path("one"))
		change(e.f, 3000, 6, 60, revB, revE)
		task(e.m, "publish-one", status, revB)
		_, err := Overview(ctx, e.m, "one", nil)
		wantMonitorError(t, err, "SNAPSHOT_CHANGED", 409)
		if !bytes.Equal(readFile(t, e.store.Path("one")), saved) {
			t.Fatal(status)
		}
	}
}

func TestNoRuntimeChangeNeverAddsCheckpointTwice(t *testing.T) {
	for _, status := range []string{"failed", "succeeded"} {
		e := newPubEnv(t)
		e.checkpoint(t, "publish-one", revB)
		e.f.cur().Update(snapshotData(3000, 46, 960))
		e.f.rk().Set("collected_at_ms", 3000)
		task(e.m, "publish-one", status, revB)
		e.complete(t, "publish-one", status)
		result := overview(t, e.m, "one")
		if result.GetObject("metrics").GetInt("requests_total") != 36 || result.GetObject("statistics").Value("reason") != "initial" {
			t.Fatal(status, result)
		}
		if pending, ok := e.read(t).Get("pending"); !ok || pending != nil {
			t.Fatal(status, e.read(t))
		}
	}
}

func TestAutomaticRollbackPreservesPrepublicationCounts(t *testing.T) {
	e := newPubEnv(t)
	e.checkpoint(t, "publish-one", revB)
	change(e.f, 3000, 4, 20, strings.Repeat("a", 64), revE)
	task(e.m, "publish-one", "rolled_back", revB)
	e.complete(t, "publish-one", "rolled_back")
	if n := overview(t, e.m, "one").GetObject("metrics").GetInt("requests_total"); n != 34 {
		t.Fatal(n)
	}
}

func TestUnrelatedChangesNeverInheritThePublicationCheckpoint(t *testing.T) {
	for _, cause := range []string{"wrong_revision", "wrong_task", "wrong_target", "rollback_failed", "manual_reset"} {
		e := newPubEnv(t)
		e.checkpoint(t, "publish-one", revB)
		if cause == "manual_reset" {
			if _, err := ResetDashboard(ctx, e.m, "one", jsonx.NewObject()); err != nil {
				t.Fatal(err)
			}
		}
		change(e.f, 3000, 6, 60, revB, revE)
		status := "succeeded"
		if cause == "rollback_failed" {
			status = "rollback_failed"
		}
		task(e.m, "publish-one", status, revB)
		switch cause {
		case "wrong_revision":
			e.f.cur().Set("source_revision", revC)
			e.f.rk().Set("source_revision", revC)
		case "wrong_task":
			task(e.m, "another", "succeeded", revB)
		case "wrong_target":
			e.m.agents["one"].Set("host", "192.0.2.99")
		}
		result := overview(t, e.m, "one")
		if result.GetObject("metrics").GetInt("requests_total") != 0 || result.GetObject("statistics").GetInt("started_at_ms") != 3000 {
			t.Fatal(cause, result)
		}
		if _, ok := e.read(t).Get("pending"); ok {
			t.Fatal(cause, e.read(t))
		}
	}
}

func TestExternalReloadAfterCompletedPublicationIsNotCountedAgain(t *testing.T) {
	e := newPubEnv(t)
	e.checkpoint(t, "publish-one", revB)
	change(e.f, 3000, 6, 60, revB, revE)
	task(e.m, "publish-one", "succeeded", revB)
	e.complete(t, "publish-one", "succeeded")
	change(e.f, 4000, 2, 20, revB, revF)
	result := overview(t, e.m, "one")
	if result.GetObject("statistics").Value("reason") != "core_changed" || result.GetObject("metrics").GetInt("requests_total") != 0 {
		t.Fatal(result)
	}
}

func TestNextPublicationRecoversPriorPendingSuccessBeforeCheckpoint(t *testing.T) {
	e := newPubEnv(t)
	e.checkpoint(t, "publish-one", revB)
	change(e.f, 3000, 6, 60, revB, revE)
	task(e.m, "publish-one", "succeeded", revB)
	// Complete() could not reach the core; no overview was opened in between.
	task(e.m, "publish-two", "uploading", revC)
	e.checkpoint(t, "publish-two", revC)
	change(e.f, 4000, 4, 20, revC, revF)
	task(e.m, "publish-two", "succeeded", revC)
	result := overview(t, e.m, "one")
	if result.GetObject("metrics").GetInt("requests_total") != 40 || result.GetObject("statistics").GetInt("publication_count") != 2 {
		t.Fatal(result)
	}
}

func TestRefreshReadyCandidateCheckpointAfterRejectedActivation(t *testing.T) {
	e := newPubEnv(t)
	task(e.m, "publish-one", "ready", revB)
	e.checkpoint(t, "publish-one", revB)
	e.f.cur().Update(snapshotData(3000, 60, 1000))
	e.checkpoint(t, "publish-one", revB)
	servers := e.read(t).GetObject("pending").GetObject("totals").GetList("servers")
	if servers[0].(*jsonx.Object).GetInt("requests") != 50 {
		t.Fatal(servers)
	}
	change(e.f, 4000, 2, 20, revB, revE)
	task(e.m, "publish-one", "succeeded", revB)
	if n := overview(t, e.m, "one").GetObject("metrics").GetInt("requests_total"); n != 52 {
		t.Fatal(n)
	}
}

func TestCheckpointFailureDoesNotModifyPreviousState(t *testing.T) {
	e := newPubEnv(t)
	saved := readFile(t, e.store.Path("one"))
	e.f.mu.Lock()
	e.f.current = NewError("CORE_UNREACHABLE", 0, "")
	e.f.mu.Unlock()
	e.checkpoint(t, "publish-one", revB)
	if !bytes.Equal(readFile(t, e.store.Path("one")), saved) {
		t.Fatal("state changed")
	}
}

func TestNeverOpenedDashboardDoesNotStartMonitoringOnPublication(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, nil, nil)
	task(m, "publish-one", "uploading", revB)
	if err := Checkpoint(ctx, m, "one", "publish-one", revB); err != nil {
		t.Fatal(err)
	}
	task(m, "publish-one", "succeeded", revB)
	if err := Complete(ctx, m, "one", "publish-one", "succeeded"); err != nil {
		t.Fatal(err)
	}
	if len(f.endpoints()) != 0 {
		t.Fatal(f.endpoints())
	}
	if state, _ := NewStateStore(dir).Read("one"); state != nil {
		t.Fatal(state)
	}
}

func TestConcurrentResetWinsOverPrepublicationNetworkSnapshot(t *testing.T) {
	e := newPubEnv(t)
	saved := e.read(t)
	replacement := saved.Clone()
	replacement.Set("generation", strings.Repeat("f", 32))
	replacement.Set("reason", "manual")
	e.f.hook = func(_ *jsonx.Object, endpoint string, _ query) {
		if endpoint == "overview" {
			if _, err := e.store.CompareAndSet("one", saved.GetString("generation"), replacement, nil); err != nil {
				t.Error(err)
			}
		}
	}
	e.checkpoint(t, "publish-one", revB)
	wantJSON(t, e.read(t), replacement)
}

func TestRetiredSeriesKeepCountersAndAddedSeriesStartAtZero(t *testing.T) {
	old, err := ParseMetrics(snapshotData(1000, 40, 100).GetString("metrics"))
	if err != nil {
		t.Fatal(err)
	}
	newer, err := ParseMetrics(strings.ReplaceAll(snapshotData(1000, 6, 100).GetString("metrics"), `dns"`, `new-dns"`))
	if err != nil {
		t.Fatal(err)
	}
	result := SummarizeObservations(AddObservations(newer, old))
	metrics := result.GetObject("metrics")
	if metrics.GetInt("requests_total") != 46 {
		t.Fatal(metrics)
	}
	approx(t, metrics.Value("inflight"), 2)
	if result.GetList("caches")[0].(*jsonx.Object).GetInt("entries") != 30 {
		t.Fatal(result)
	}
	if _, partial, err := ApplyMetricsBaseline(newer, ZeroBaseline(newer)); err != nil || partial {
		t.Fatal(partial, err)
	}
}

func TestMissingPrepublicationCounterRemainsUnknown(t *testing.T) {
	e := newPubEnv(t)
	e.f.cur().Set("metrics", strings.ReplaceAll(e.f.cur().GetString("metrics"), "cache_hit_total", "unavailable_hit_total"))
	e.checkpoint(t, "publish-one", revB)
	change(e.f, 3000, 6, 60, revB, revE)
	task(e.m, "publish-one", "succeeded", revB)
	result := overview(t, e.m, "one")
	if result.GetObject("metrics").Value("cache_hit_rate") != nil || result.GetObject("statistics").Value("partial") != true ||
		result.GetObject("metrics").GetInt("requests_total") != 36 {
		t.Fatal(result)
	}
}

func TestInvalidExtendedStateIsRejected(t *testing.T) {
	for _, tc := range []struct {
		field string
		value any
	}{{"carried", jsonx.NewObject()}, {"carried_partial", 1}, {"publication_count", -1},
		{"pending", jsonx.Obj("deployment_id", "x")}} {
		e := newPubEnv(t)
		e.checkpoint(t, "publish-one", revB)
		state := e.read(t)
		if !ValidateState(state) {
			t.Fatal(state)
		}
		state.Set(tc.field, tc.value)
		if ValidateState(state) {
			t.Fatal(tc.field)
		}
	}
}

// Beyond the Python cases: complete ignores non-terminal statuses and a
// list status is a TypeError; checkpoint ignores a malformed revision.
func TestCompleteAndCheckpointGuards(t *testing.T) {
	e := newPubEnv(t)
	before := len(e.f.endpoints())
	for _, status := range []any{"uploading", nil, jsonx.Int(1)} {
		if err := Complete(ctx, e.m, "one", "publish-one", status); err != nil {
			t.Fatal(err)
		}
	}
	if err := Complete(ctx, e.m, "one", "publish-one", []any{"succeeded"}); err == nil {
		t.Fatal("list status")
	}
	for _, revision := range []any{nil, strings.Repeat("B", 64), revB + "\n", jsonx.Int(5)} {
		if err := Checkpoint(ctx, e.m, "one", "publish-one", revision); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.f.endpoints()) != before {
		t.Fatal(e.f.endpoints())
	}
	if pending := e.read(t).Value("pending"); pending != nil {
		t.Fatal(pending)
	}
}
