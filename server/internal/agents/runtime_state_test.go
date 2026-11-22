package agents

// Port of backend/test_agent_runtime_state.py (manager and legacy import
// parts; the HTTP case is in internal/api/agents).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
)

func testMetricsMap() map[string]any {
	return map[string]any{
		"cpu":     map[string]any{"usage_percent": 12.5, "core_count": 4},
		"memory":  map[string]any{"total": 1024, "used": 512, "available": 512, "used_percent": 50.0},
		"disk":    map[string]any{"total": 2048, "used": 1024, "free": 1024, "used_percent": 50.0},
		"network": map[string]any{"bytes_sent": 10, "bytes_recv": 20, "speed_sent": 1, "speed_recv": 2},
	}
}

func decodeHeartbeatAgent(t *testing.T, payload map[string]any) *model.Agent {
	t.Helper()
	var a model.Agent
	if err := json.Unmarshal(mustJSON(t, payload), &a); err != nil {
		t.Fatal(err)
	}
	return &a
}

// setupRuntime is setup(tmp_path): edge @ 192.0.2.10, version 1.0.0.
func setupRuntime(t *testing.T) (*env, string) {
	t.Helper()
	ResetRuntime()
	ResetHooks()
	t.Cleanup(func() { ResetRuntime(); ResetHooks() })
	dir := t.TempDir()
	r := openRepo(t, dir)
	e := &env{t: t, ctx: context.Background(), repo: r, m: newManager(t, r), dir: dir}
	registered := e.register(map[string]any{"name": "edge", "host": "192.0.2.10", "version": "1.0.0"}, "")
	e.agent = e.get(registered.ID)
	return e, registered.ID
}

func (e *env) beat(id string, payload map[string]any) bool {
	e.t.Helper()
	return must[bool](e.t)(e.m.ObserveHeartbeat(e.ctx, id, decodeHeartbeatAgent(e.t, payload)))
}

func (e *env) ageRegistration(minutes int) string {
	old := isoformat(time.Now().Add(-time.Duration(minutes)*time.Minute), false)
	e.editSystem(func(system *model.System) {
		for i := range system.Agents {
			system.Agents[i].LastHeartbeat = old
		}
	})
	return old
}

func (e *env) restart() *Manager {
	ResetRuntime()
	return newManager(e.t, openRepo(e.t, e.dir))
}

func TestHeartbeatWithKnownVersionWritesNothingAndIsVisibleThroughTheManager(t *testing.T) {
	e, id := setupRuntime(t)
	head, _ := e.repo.Head(e.ctx)
	revision := e.revision()
	for i := 0; i < 3; i++ {
		if !e.beat(id, map[string]any{"version": "1.0.0", "service_status": "running", "config_version": "0", "system_metrics": testMetricsMap()}) {
			t.Fatal("heartbeat refused")
		}
	}
	if after, _ := e.repo.Head(e.ctx); after != head || e.revision() != revision {
		t.Fatal("heartbeat wrote the configuration")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "runtime.sqlite3")); err == nil {
		t.Fatal("runtime.sqlite3 created")
	}
	all := must[[]model.Agent](t)(e.m.ListAgents(e.ctx, true))
	wantMetrics := testMetricsMap()
	for _, current := range []*model.Agent{e.get(id), &all[0]} {
		if current.Status != "online" || current.ServiceStatus == nil || *current.ServiceStatus != "running" ||
			diffJSONValues(t, wantMetrics, current.SystemMetrics) != "" {
			t.Fatalf("current %+v", current)
		}
		for _, key := range metricFields {
			raw, present, _ := model.EncodeMember(&current, key)
			if !present || diffJSONValues(t, wantMetrics[key], parseJSONVal(t, raw)) != "" {
				t.Fatalf("%s %s", key, raw)
			}
		}
	}
}

func TestAChangedVersionIsWrittenOnceTogetherWithConfigVersion(t *testing.T) {
	e, id := setupRuntime(t)
	revision, registered := e.revision(), e.stored(id).LastHeartbeat
	e.beat(id, map[string]any{"version": "2.0.0", "config_version": "cfg-2"})
	stored := e.stored(id)
	if e.revision() != revision+1 || stored.Version != "2.0.0" || stored.ConfigVersion != "cfg-2" || stored.LastHeartbeat != registered {
		t.Fatalf("stored %+v", stored)
	}
	e.beat(id, map[string]any{"version": "2.0.0", "config_version": "cfg-2"})
	e.beat(id, map[string]any{})
	if e.revision() != revision+1 {
		t.Fatal("unchanged heartbeat wrote")
	}
	e.beat(id, map[string]any{"config_version": "cfg-3"})
	if e.revision() != revision+2 || e.stored(id).Version != "2.0.0" {
		t.Fatal("config_version change not written")
	}
}

func TestReturnedAgentsAreCopiesOfTheRememberedState(t *testing.T) {
	e, id := setupRuntime(t)
	e.beat(id, map[string]any{"system_metrics": testMetricsMap()})
	copied := e.get(id)
	copied.SystemMetrics.CPU.UsagePercent = "99"
	if diffJSONValues(t, testMetricsMap(), e.get(id).SystemMetrics) != "" {
		t.Fatal("shared state")
	}
}

func TestPartialHeartbeatsKeepEarlierValuesAndInheritRegisteredState(t *testing.T) {
	e, id := setupRuntime(t)
	e.editSystem(func(system *model.System) { e.firstAgent(system).ServiceStatus = new("running") })
	stamp, _, _ := fromISO(e.get(id).LastHeartbeat)
	e.beat(id, map[string]any{})
	first := e.get(id)
	beat, _, _ := fromISO(first.LastHeartbeat)
	if first.Version != "1.0.0" || first.ServiceStatus == nil || *first.ServiceStatus != "running" || beat.Before(stamp) {
		t.Fatalf("first %+v", first)
	}
	e.beat(id, map[string]any{"system_metrics": testMetricsMap()})
	e.beat(id, map[string]any{"version": "2.0.0"})
	current := e.get(id)
	if current.Version != "2.0.0" || diffJSONValues(t, testMetricsMap(), current.SystemMetrics) != "" {
		t.Fatalf("current %+v", current)
	}
}

func TestReRegistrationWinsUntilTheNextHeartbeat(t *testing.T) {
	e, id := setupRuntime(t)
	e.beat(id, map[string]any{"version": "2.0.0", "system_metrics": testMetricsMap()})
	time.Sleep(2 * time.Millisecond)
	e.register(map[string]any{"name": "edge", "host": "192.0.2.10", "version": "3.0.0"}, e.agent.Token)
	current := e.get(id)
	if current.Version != "3.0.0" || memberPresent(current, "system_metrics") {
		t.Fatalf("current %+v", current)
	}
	e.beat(id, map[string]any{})
	current = e.get(id)
	if current.Version != "3.0.0" || memberPresent(current, "system_metrics") {
		t.Fatalf("after heartbeat %+v", current)
	}
}

func TestUnknownAgentWritesAndRemembersNothing(t *testing.T) {
	e, _ := setupRuntime(t)
	head, _ := e.repo.Head(e.ctx)
	if e.beat("missing", map[string]any{"version": "2.0.0"}) {
		t.Fatal("unknown accepted")
	}
	if after, _ := e.repo.Head(e.ctx); after != head || runtimeEntriesCount(e.m.Runtime()) != 0 {
		t.Fatal("something written")
	}
}

func TestDeletingAnAgentForgetsItsHeartbeat(t *testing.T) {
	e, id := setupRuntime(t)
	e.beat(id, map[string]any{"system_metrics": testMetricsMap()})
	if ok, err := e.m.Runtime().recordModel(e.ctx, &model.Agent{ID: "stray"}, &model.Agent{Version: "9"}, e.m.Runtime().Mark()); err != nil || !ok {
		t.Fatal("stray refused", ok, err)
	}
	if all := must[[]model.Agent](t)(e.m.ListAgents(e.ctx, true)); len(all) != 1 || all[0].ID != id {
		t.Fatal("only registered Agents are listed")
	}
	if !must[bool](t)(e.m.DeleteAgent(e.ctx, id)) {
		t.Fatal("delete failed")
	}
	_, hasStray := runtimeEntryCopy(e.m.Runtime(), "stray")
	if runtimeEntriesCount(e.m.Runtime()) != 1 || !hasStray {
		t.Fatal("stray entry missing or extra entries remain")
	}
	if len(must[[]model.Agent](t)(e.m.ListAgents(e.ctx, true))) != 0 {
		t.Fatal("deleted Agent listed")
	}
}

func TestRestartKeepsTheReportedVersionAndWaitsForTheNextHeartbeat(t *testing.T) {
	e, id := setupRuntime(t)
	registered := e.ageRegistration(10)
	e.beat(id, map[string]any{"version": "2.0.0", "service_status": "running", "system_metrics": testMetricsMap()})
	if must[[]model.Agent](t)(e.m.ListAgents(e.ctx, true))[0].Status != "online" {
		t.Fatal("not online")
	}
	restarted := e.restart()
	listed := must[[]model.Agent](t)(restarted.ListAgents(e.ctx, true))[0]
	if listed.Version != "2.0.0" || listed.Status != "offline" || listed.LastHeartbeat != registered ||
		memberPresent(&listed, "system_metrics") || memberPresent(&listed, "service_status") {
		t.Fatalf("listed %+v", listed)
	}
	if must[*model.Agent](t)(restarted.ReadAgent(e.ctx, id, true)).Status != "online" {
		t.Fatal("get_agent_by_id derived offline")
	}
	must[bool](t)(restarted.ObserveHeartbeat(e.ctx, id, &model.Agent{Version: "2.0.0", ServiceStatus: new("running")}))
	listed = must[[]model.Agent](t)(restarted.ListAgents(e.ctx, true))[0]
	if listed.Status != "online" || listed.ServiceStatus == nil || *listed.ServiceStatus != "running" {
		t.Fatalf("after heartbeat %+v", listed)
	}
}

func TestEveryManagerForADataDirectorySharesTheState(t *testing.T) {
	e, id := setupRuntime(t)
	second := newManager(t, openRepo(t, e.dir))
	must[bool](t)(second.ObserveHeartbeat(e.ctx, id, &model.Agent{ServiceStatus: new("running")}))
	if got := e.get(id).ServiceStatus; got == nil || *got != "running" {
		t.Fatal("state not shared")
	}
	other := newManager(t, openRepo(t, filepath.Join(e.dir, "other")))
	if other.Runtime() == e.m.Runtime() || second.Runtime() != e.m.Runtime() {
		t.Fatal("wrong sharing")
	}
}

func TestDomainDiscoveryToggleReturnsTheLiveAgent(t *testing.T) {
	e, id := setupRuntime(t)
	e.beat(id, map[string]any{"version": "2.0.0", "system_metrics": testMetricsMap()})
	updated := must[*model.Agent](t)(e.m.SetDomainDiscoveryModel(e.ctx, id, true))
	if updated.Version != "2.0.0" || diffJSONValues(t, testMetricsMap(), updated.SystemMetrics) != "" || updated.Status != "online" {
		t.Fatalf("updated %+v", updated)
	}
}

func TestANewerHeartbeatInTheDocumentWins(t *testing.T) {
	e, id := setupRuntime(t)
	e.beat(id, map[string]any{"service_status": "running"})
	entry, _ := runtimeEntryCopy(e.m.Runtime(), id)
	entry.HeartbeatAt -= 5
	setRuntimeEntry(e.m.Runtime(), id, entry)
	later := nowISO()
	e.editSystem(func(system *model.System) {
		e.firstAgent(system).LastHeartbeat = later
		e.firstAgent(system).ServiceStatus = new("stopped")
	})
	current := e.get(id)
	if current.ServiceStatus == nil || *current.ServiceStatus != "stopped" || current.LastHeartbeat != later {
		t.Fatalf("current %+v", current)
	}
}

func TestStaleHeartbeatReadsAsOfflineOnlyInTheList(t *testing.T) {
	e, id := setupRuntime(t)
	e.ageRegistration(10)
	e.beat(id, map[string]any{})
	entry, _ := runtimeEntryCopy(e.m.Runtime(), id)
	entry.LastHeartbeat = isoformat(time.Now().Add(-5*time.Minute), false)
	setRuntimeEntry(e.m.Runtime(), id, entry)
	if must[[]model.Agent](t)(e.m.ListAgents(e.ctx, true))[0].Status != "offline" || e.get(id).Status != "online" {
		t.Fatal("freshness")
	}
}

func TestOneFreshnessRuleTwoMinutesInclusive(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.Local)
	stamp := func(seconds float64) string {
		at := now.Add(-time.Duration(seconds * float64(time.Second)))
		return isoformat(at, false)
	}
	if HeartbeatFreshSeconds != 120 || !FreshHeartbeat(stamp(0), &now) || !FreshHeartbeat(stamp(120), &now) ||
		FreshHeartbeat(stamp(120.001), &now) {
		t.Fatal("freshness bounds")
	}
	for _, broken := range []string{"", "yesterday"} {
		if FreshHeartbeat(broken, &now) {
			t.Fatalf("broken %q", broken)
		}
	}
	aware := time.Now()
	if !FreshHeartbeat(isoformat(aware.Add(-30*time.Second), true), nil) ||
		FreshHeartbeat(isoformat(aware.Add(-3*time.Minute), true), nil) {
		t.Fatal("aware")
	}
	tr, fa := true, false
	if !AgentEnabledModel(&model.Agent{}) || !AgentEnabledModel(&model.Agent{Enabled: &tr}) || AgentEnabledModel(&model.Agent{Enabled: &fa}) {
		t.Fatal("enabled")
	}
}

func TestTheListDerivesStatusWithTheSharedRule(t *testing.T) {
	e, id := setupRuntime(t)
	if must[[]model.Agent](t)(e.m.ListAgents(e.ctx, true))[0].Status != "online" {
		t.Fatal("fresh registration offline")
	}
	e.ageRegistration(3)
	restarted := e.restart()
	listed := must[[]model.Agent](t)(restarted.ListAgents(e.ctx, true))[0]
	single := must[*model.Agent](t)(restarted.ReadAgent(e.ctx, id, true))
	if listed.Status != "offline" || FreshHeartbeat(single.LastHeartbeat, nil) {
		t.Fatal("derived status")
	}
}

func TestARegistrationTimeAheadOfTheClockDoesNotHideHeartbeats(t *testing.T) {
	e, id := setupRuntime(t)
	ahead := isoformat(time.Now().Add(8*time.Hour), false)
	e.editSystem(func(system *model.System) { e.firstAgent(system).LastHeartbeat = ahead })
	e.beat(id, map[string]any{"service_status": "running"})
	current := e.get(id)
	beat, _, _ := fromISO(current.LastHeartbeat)
	if current.ServiceStatus == nil || *current.ServiceStatus != "running" || beat.After(time.Now()) {
		t.Fatalf("current %+v", current)
	}
}

func TestAFailedVersionWriteStillRecordsTheHeartbeatAndRetries(t *testing.T) {
	e, id := setupRuntime(t)
	testFailUpdate = func() error { return errors.New("disk full") }
	if _, err := e.m.ObserveHeartbeat(e.ctx, id, &model.Agent{Version: "2.0.0"}); err == nil || err.Error() != "disk full" {
		t.Fatalf("err %v", err)
	}
	testFailUpdate = nil
	if e.get(id).Version != "2.0.0" || e.stored(id).Version != "1.0.0" {
		t.Fatal("state")
	}
	e.beat(id, map[string]any{"version": "2.0.0"})
	if e.stored(id).Version != "2.0.0" {
		t.Fatal("retry did not write")
	}
}

func TestConcurrentHeartbeatsAreAllRecorded(t *testing.T) {
	e, id := setupRuntime(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			for j := 0; j < 20; j++ {
				payload := decodeHeartbeatAgent(t, map[string]any{"service_status": "s" + string(rune('0'+index)), "system_metrics": testMetricsMap()})
				if _, err := e.m.ObserveHeartbeat(e.ctx, id, payload); err != nil {
					errs <- err
					return
				}
				if _, err := e.m.ListAgents(e.ctx, true); err != nil {
					errs <- err
					return
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if got := e.get(id).ServiceStatus; got == nil || !strings.HasPrefix(*got, "s") {
		t.Fatal("status")
	}
}

func TestAnAgentRestoredAfterItsDeletionIsHeardAgain(t *testing.T) {
	e, id := setupRuntime(t)
	saved := *e.stored(id)
	must[bool](t)(e.m.DeleteAgent(e.ctx, id))
	e.editSystem(func(system *model.System) { system.Agents = append(system.Agents, saved) })
	e.beat(id, map[string]any{"service_status": "running"})
	if got := e.get(id).ServiceStatus; got == nil || *got != "running" {
		t.Fatal("not heard")
	}
}

// --- one-time import of #369's runtime.sqlite3 ---

func legacyDatabase(t *testing.T, path string, rows [][4]any) {
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE agent_runtime (agent_id TEXT PRIMARY KEY, heartbeat_at REAL NOT NULL, last_heartbeat TEXT NOT NULL, fields TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if _, err := db.Exec("INSERT INTO agent_runtime VALUES (?, ?, ?, ?)", row[0], row[1], row[2], row[3]); err != nil {
			t.Fatal(err)
		}
	}
}

func legacyRowOf(id string, moment time.Time, fields map[string]any) [4]any {
	text, _ := json.Marshal(fields)
	return [4]any{id, pyTimestamp(moment), isoformat(moment, false), string(text)}
}

func twoAgents(t *testing.T) (*env, string, string) {
	e, _ := setupRuntime(t)
	e.m.DeleteAgent(e.ctx, e.agent.ID)
	fresh := e.register(map[string]any{"name": "fresh", "host": "192.0.2.10", "version": "1.0.0"}, "").ID
	stale := e.register(map[string]any{"name": "stale", "host": "192.0.2.11", "version": "1.0.0"}, "").ID
	e.ageRegistration(10)
	return e, fresh, stale
}

// captureLogs records the levels logged by backend.agents.runtime_state.
func captureLogs(t *testing.T) func() []string {
	dir := t.TempDir()
	root, err := logx.Setup(logx.Config{Level: logx.Info, File: filepath.Join(dir, "app.log")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return func() []string {
		data, _ := os.ReadFile(filepath.Join(dir, "app.log"))
		var levels []string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, " - backend.agents.runtime_state - ") {
				levels = append(levels, strings.SplitN(strings.SplitN(line, " - backend.agents.runtime_state - ", 2)[1], " - ", 2)[0])
			}
		}
		return levels
	}
}

func TestUpgradeFromRuntimeSqliteKeepsVersionsAndLiveAgentsThenRemovesIt(t *testing.T) {
	e, fresh, stale := twoAgents(t)
	now, older := time.Now(), time.Now().Add(-5*time.Minute)
	legacyDatabase(t, filepath.Join(e.dir, "runtime.sqlite3"), [][4]any{
		legacyRowOf(fresh, now, map[string]any{"version": "2.0.0", "service_status": "running", "system_metrics": testMetricsMap()}),
		legacyRowOf(stale, older, map[string]any{"version": "1.5.0"}),
		legacyRowOf("deleted", now, map[string]any{"version": "9.9.9"}),
	})
	os.WriteFile(filepath.Join(e.dir, "runtime.sqlite3-journal"), nil, 0o644)
	os.WriteFile(filepath.Join(e.dir, "runtime.sqlite3.corrupt-1"), []byte("x"), 0o644)
	e.editSystem(func(system *model.System) {
		registryAgent(system.Agents, stale).LastHeartbeat = nowISO()
	})
	revision := e.revision()
	logs := captureLogs(t)
	ResetRuntime()
	manager := newManager(t, openRepo(t, e.dir))
	newManager(t, openRepo(t, e.dir))
	if got := logs(); strings.Join(got, ",") != "INFO" {
		t.Fatalf("logs %v", got)
	}
	if e.revision() != revision+1 || e.stored(fresh).Version != "2.0.0" || e.stored(stale).Version != "1.0.0" {
		t.Fatal("versions")
	}
	byID := map[string]model.Agent{}
	for _, agent := range must[[]model.Agent](t)(manager.ListAgents(e.ctx, true)) {
		byID[agent.ID] = agent
	}
	if byID[fresh].Status != "online" || diffJSONValues(t, testMetricsMap(), byID[fresh].SystemMetrics) != "" ||
		byID[stale].Version != "1.0.0" || byID[stale].Status != "online" {
		t.Fatal("agents")
	}
	if n := runtimeEntriesCount(manager.Runtime()); n != 1 {
		t.Fatalf("entries %d", n)
	}
	matches, _ := filepath.Glob(filepath.Join(e.dir, "runtime.sqlite3*"))
	if len(matches) != 0 {
		t.Fatalf("left %v", matches)
	}
}

func TestUpgradeWithUnchangedVersionsDoesNotWriteTheConfiguration(t *testing.T) {
	e, fresh, _ := twoAgents(t)
	legacyDatabase(t, filepath.Join(e.dir, "runtime.sqlite3"), [][4]any{legacyRowOf(fresh, time.Now(), map[string]any{"version": "1.0.0"})})
	revision := e.revision()
	manager := e.restart()
	if e.revision() != revision || must[*model.Agent](t)(manager.ReadAgent(e.ctx, fresh, true)).Status != "online" {
		t.Fatal("unexpected write")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "runtime.sqlite3")); err == nil {
		t.Fatal("not removed")
	}
}

func TestADamagedRuntimeSqliteIsIgnoredAndRemoved(t *testing.T) {
	e, fresh, _ := twoAgents(t)
	os.WriteFile(filepath.Join(e.dir, "runtime.sqlite3"), []byte(strings.Repeat("not a database", 100)), 0o644)
	logs := captureLogs(t)
	manager := e.restart()
	if must[*model.Agent](t)(manager.ReadAgent(e.ctx, fresh, true)).Version != "1.0.0" || runtimeEntriesCount(manager.Runtime()) != 0 {
		t.Fatal("imported garbage")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "runtime.sqlite3")); err == nil {
		t.Fatal("not removed")
	}
	if got := logs(); strings.Join(got, ",") != "WARNING,INFO" {
		t.Fatalf("logs %v", got)
	}
}

func TestUndecodableLegacyRowsAreSkipped(t *testing.T) {
	e, fresh, stale := twoAgents(t)
	legacyDatabase(t, filepath.Join(e.dir, "runtime.sqlite3"), [][4]any{
		{fresh, pyTimestamp(time.Now()), nowISO(), "{bad"},
		legacyRowOf(stale, time.Now(), map[string]any{"version": "2.0.0"}),
	})
	manager := e.restart()
	_, ok := runtimeEntryCopy(manager.Runtime(), stale)
	if runtimeEntriesCount(manager.Runtime()) != 1 || !ok || e.stored(stale).Version != "2.0.0" {
		t.Fatal("entries mismatch")
	}
}

func TestAFailedVersionImportIsNotRetriedOverNewerVersions(t *testing.T) {
	e, fresh, _ := twoAgents(t)
	legacyDatabase(t, filepath.Join(e.dir, "runtime.sqlite3"), [][4]any{legacyRowOf(fresh, time.Now(), map[string]any{"version": "2.0.0"})})
	ResetRuntime()
	logs := captureLogs(t)
	r := openRepo(t, e.dir)
	state := RuntimeFor(r.DataDir())
	if err := importLegacyDatabase(e.ctx, r, state, func(context.Context, map[string]model.Fields) (map[string]bool, error) {
		return nil, errors.New("disk full")
	}); err != nil {
		t.Fatal(err)
	}
	manager := newManager(t, r)
	if got := logs(); strings.Join(got, ",") != "WARNING,INFO" {
		t.Fatalf("logs %v", got)
	}
	if must[*model.Agent](t)(manager.ReadAgent(e.ctx, fresh, true)).Version != "2.0.0" {
		t.Fatal("not shown live")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "runtime.sqlite3")); err == nil {
		t.Fatal("not removed")
	}
	must[bool](t)(manager.ObserveHeartbeat(e.ctx, fresh, &model.Agent{Version: "3.0.0"}))
	restarted := e.restart()
	if e.stored(fresh).Version != "3.0.0" || must[*model.Agent](t)(restarted.ReadAgent(e.ctx, fresh, true)).Version != "3.0.0" {
		t.Fatal("next heartbeat did not write")
	}
}

func TestARuntimeSqliteThatCannotBeRemovedIsNotImported(t *testing.T) {
	e, fresh, _ := twoAgents(t)
	database := filepath.Join(e.dir, "runtime.sqlite3")
	legacyDatabase(t, database, [][4]any{legacyRowOf(fresh, time.Now(), map[string]any{"version": "2.0.0"})})
	revision := e.revision()
	removeLegacyFile = func(paths []string) {
		var others []string
		for _, path := range paths {
			if filepath.Base(path) == "runtime.sqlite3" {
				runtimeLogger.Warningf("旧版 Agent 心跳状态文件 %s 删除失败：%s", "runtime.sqlite3", "read-only")
				continue
			}
			others = append(others, path)
		}
		removeFiles(others)
	}
	t.Cleanup(func() { removeLegacyFile = removeFiles })
	logs := captureLogs(t)
	manager := e.restart()
	if got := logs(); strings.Join(got, ",") != "WARNING,WARNING" {
		t.Fatalf("logs %v", got)
	}
	if e.revision() != revision || runtimeEntriesCount(manager.Runtime()) != 0 {
		t.Fatal("imported")
	}
	if _, err := os.Stat(database); err != nil {
		t.Fatal("removed")
	}
}

// --- presence logging across a restart ---

func observePresenceModels(records []model.Agent) {
	observations := make([]PresenceObservation, 0, len(records))
	tokens := make([]string, 0, len(records))
	for i := range records {
		a := &records[i]
		tokens = append(tokens, a.Token)
		observations = append(observations, PresenceObservation{
			ID: a.ID, Status: a.Status, Name: a.Name, LastHeartbeat: a.LastHeartbeat,
		})
	}
	ObserveAgentPresence(observations, redact.NewSecrets(tokens))
}

func TestARestartDoesNotLogEveryAgentBackOnline(t *testing.T) {
	resetPresence()
	t.Cleanup(resetPresence)
	e, id := setupRuntime(t)
	e.ageRegistration(10)
	restarted := e.restart()
	dir := t.TempDir()
	root, err := logx.Setup(logx.Config{Level: logx.Info, File: filepath.Join(dir, "app.log")})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	list := func() []model.Agent { return must[[]model.Agent](t)(restarted.ListAgents(e.ctx, true)) }
	observePresenceModels(list())
	must[bool](t)(restarted.ObserveHeartbeat(e.ctx, id, &model.Agent{}))
	observePresenceModels(list())
	entry, _ := runtimeEntryCopy(restarted.Runtime(), id)
	entry.LastHeartbeat = isoformat(time.Now().Add(-5*time.Minute), false)
	setRuntimeEntry(restarted.Runtime(), id, entry)
	observePresenceModels(list())
	must[bool](t)(restarted.ObserveHeartbeat(e.ctx, id, &model.Agent{}))
	observePresenceModels(list())
	data, _ := os.ReadFile(filepath.Join(dir, "app.log"))
	var messages []string
	for _, line := range strings.Split(string(data), "\n") {
		if parts := strings.SplitN(line, " - backend.agents.presence_log - ", 2); len(parts) == 2 {
			messages = append(messages, strings.SplitN(parts[1], " - ", 2)[1])
		}
	}
	if len(messages) != 2 || !strings.HasPrefix(messages[0], "Agent edge 已离线") || messages[1] != "Agent edge 已恢复在线" {
		t.Fatalf("messages %q", messages)
	}
}

func TestTimestampOfRejectsFutureAndGarbage(t *testing.T) {
	if !math.IsInf(timestampOf("garbage"), -1) || !math.IsInf(timestampOf(5), -1) ||
		!math.IsInf(timestampOf(isoformat(time.Now().Add(time.Hour), false)), -1) {
		t.Fatal("timestamp")
	}
}
