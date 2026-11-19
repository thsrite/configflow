package agents

// Port of backend/test_agent_runtime_state.py (manager and legacy import
// parts; the HTTP case is in internal/api/agents).

import (
	"configflow/internal/model"
	"context"
	"database/sql"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

var testMetrics = jsonx.Obj(
	"cpu", jsonx.Obj("usage_percent", 12.5, "core_count", 4),
	"memory", jsonx.Obj("total", 1024, "used", 512, "available", 512, "used_percent", 50.0),
	"disk", jsonx.Obj("total", 2048, "used", 1024, "free", 1024, "used_percent", 50.0),
	"network", jsonx.Obj("bytes_sent", 10, "bytes_recv", 20, "speed_sent", 1, "speed_recv", 2),
)

func metrics() *jsonx.Object { return testMetrics.DeepCopy() }

// setupRuntime is setup(tmp_path): edge @ 192.0.2.10, version 1.0.0.
func setupRuntime(t *testing.T) (*env, string) {
	t.Helper()
	ResetRuntime()
	ResetHooks()
	t.Cleanup(func() { ResetRuntime(); ResetHooks() })
	dir := t.TempDir()
	r := openRepo(t, dir)
	e := &env{t: t, ctx: context.Background(), repo: r, m: newManager(t, r), dir: dir}
	registered := e.register(jsonx.Obj("name", "edge", "host", "192.0.2.10", "version", "1.0.0"), nil)
	e.agent = registered
	return e, registered.GetString("id")
}

func (e *env) beat(id string, payload *jsonx.Object) bool {
	e.t.Helper()
	return must[bool](e.t)(e.m.UpdateHeartbeat(e.ctx, id, payload))
}

func (e *env) ageRegistration(minutes int) string {
	old := isoformat(time.Now().Add(-time.Duration(minutes)*time.Minute), false)
	e.editSystem(func(system *jsonx.Object) {
		for _, item := range system.GetList("agents") {
			item.(*jsonx.Object).Set("last_heartbeat", old)
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
		if !e.beat(id, jsonx.Obj("version", "1.0.0", "service_status", "running", "config_version", "0", "system_metrics", metrics())) {
			t.Fatal("heartbeat refused")
		}
	}
	if after, _ := e.repo.Head(e.ctx); after != head || e.revision() != revision {
		t.Fatal("heartbeat wrote the configuration")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "runtime.sqlite3")); err == nil {
		t.Fatal("runtime.sqlite3 created")
	}
	all := must[[]*jsonx.Object](t)(e.m.GetAllAgents(e.ctx, true))
	for _, current := range []*jsonx.Object{e.get(id), all[0]} {
		if current.Value("status") != "online" || current.Value("service_status") != "running" || !jsonx.Equal(current.Value("system_metrics"), testMetrics) {
			t.Fatalf("current %v", current)
		}
		for _, key := range metricFields {
			if !jsonx.Equal(current.Value(key), testMetrics.Value(key)) {
				t.Fatalf("%s %v", key, current.Value(key))
			}
		}
	}
}

func TestAChangedVersionIsWrittenOnceTogetherWithConfigVersion(t *testing.T) {
	e, id := setupRuntime(t)
	revision, registered := e.revision(), e.stored(id).Value("last_heartbeat")
	e.beat(id, jsonx.Obj("version", "2.0.0", "config_version", "cfg-2"))
	stored := e.stored(id)
	if e.revision() != revision+1 || stored.Value("version") != "2.0.0" || stored.Value("config_version") != "cfg-2" || stored.Value("last_heartbeat") != registered {
		t.Fatalf("stored %v", stored)
	}
	e.beat(id, jsonx.Obj("version", "2.0.0", "config_version", "cfg-2"))
	e.beat(id, jsonx.NewObject())
	if e.revision() != revision+1 {
		t.Fatal("unchanged heartbeat wrote")
	}
	e.beat(id, jsonx.Obj("config_version", "cfg-3"))
	if e.revision() != revision+2 || e.stored(id).Value("version") != "2.0.0" {
		t.Fatal("config_version change not written")
	}
}

func TestReturnedAgentsAreCopiesOfTheRememberedState(t *testing.T) {
	e, id := setupRuntime(t)
	e.beat(id, jsonx.Obj("system_metrics", metrics()))
	e.get(id).GetObject("system_metrics").GetObject("cpu").Set("usage_percent", 99)
	if !jsonx.Equal(e.get(id).Value("system_metrics"), testMetrics) {
		t.Fatal("shared state")
	}
}

func TestPartialHeartbeatsKeepEarlierValuesAndInheritRegisteredState(t *testing.T) {
	e, id := setupRuntime(t)
	e.editSystem(func(system *jsonx.Object) { e.firstAgent(system).Set("service_status", "running") })
	stamp, _, _ := fromISO(e.get(id).GetString("last_heartbeat"))
	e.beat(id, jsonx.NewObject())
	first := e.get(id)
	beat, _, _ := fromISO(first.GetString("last_heartbeat"))
	if first.Value("version") != "1.0.0" || first.Value("service_status") != "running" || beat.Before(stamp) {
		t.Fatalf("first %v", first)
	}
	e.beat(id, jsonx.Obj("system_metrics", metrics()))
	e.beat(id, jsonx.Obj("version", "2.0.0"))
	current := e.get(id)
	if current.Value("version") != "2.0.0" || !jsonx.Equal(current.Value("system_metrics"), testMetrics) {
		t.Fatalf("current %v", current)
	}
}

func TestReRegistrationWinsUntilTheNextHeartbeat(t *testing.T) {
	e, id := setupRuntime(t)
	e.beat(id, jsonx.Obj("version", "2.0.0", "system_metrics", metrics()))
	time.Sleep(2 * time.Millisecond)
	e.register(jsonx.Obj("name", "edge", "host", "192.0.2.10", "version", "3.0.0"), e.agent.Value("token"))
	current := e.get(id)
	if current.Value("version") != "3.0.0" || current.Has("system_metrics") {
		t.Fatalf("current %v", current)
	}
	e.beat(id, jsonx.NewObject())
	current = e.get(id)
	if current.Value("version") != "3.0.0" || current.Has("system_metrics") {
		t.Fatalf("after heartbeat %v", current)
	}
}

func TestUnknownAgentWritesAndRemembersNothing(t *testing.T) {
	e, _ := setupRuntime(t)
	head, _ := e.repo.Head(e.ctx)
	if e.beat("missing", jsonx.Obj("version", "2.0.0")) {
		t.Fatal("unknown accepted")
	}
	if after, _ := e.repo.Head(e.ctx); after != head || len(e.m.Runtime().Entries()) != 0 {
		t.Fatal("something written")
	}
}

func TestDeletingAnAgentForgetsItsHeartbeat(t *testing.T) {
	e, id := setupRuntime(t)
	e.beat(id, jsonx.Obj("system_metrics", metrics()))
	if !e.m.Runtime().RecordHeartbeat(jsonx.Obj("id", "stray"), jsonx.Obj("version", "9"), e.m.Runtime().Mark()) {
		t.Fatal("stray refused")
	}
	if all := must[[]*jsonx.Object](t)(e.m.GetAllAgents(e.ctx, true)); len(all) != 1 || all[0].Value("id") != id {
		t.Fatal("only registered Agents are listed")
	}
	if !must[bool](t)(e.m.DeleteAgent(e.ctx, id)) {
		t.Fatal("delete failed")
	}
	entries := e.m.Runtime().Entries()
	if _, ok := entries["stray"]; len(entries) != 1 || !ok {
		t.Fatalf("entries %v", entries)
	}
	if len(must[[]*jsonx.Object](t)(e.m.GetAllAgents(e.ctx, true))) != 0 {
		t.Fatal("deleted Agent listed")
	}
}

func TestRestartKeepsTheReportedVersionAndWaitsForTheNextHeartbeat(t *testing.T) {
	e, id := setupRuntime(t)
	registered := e.ageRegistration(10)
	e.beat(id, jsonx.Obj("version", "2.0.0", "service_status", "running", "system_metrics", metrics()))
	if must[[]*jsonx.Object](t)(e.m.GetAllAgents(e.ctx, true))[0].Value("status") != "online" {
		t.Fatal("not online")
	}
	restarted := e.restart()
	listed := must[[]*jsonx.Object](t)(restarted.GetAllAgents(e.ctx, true))[0]
	if listed.Value("version") != "2.0.0" || listed.Value("status") != "offline" || listed.Value("last_heartbeat") != registered ||
		listed.Has("system_metrics") || listed.Has("service_status") {
		t.Fatalf("listed %v", listed)
	}
	if must[*jsonx.Object](t)(restarted.GetAgentByID(e.ctx, id, true)).Value("status") != "online" {
		t.Fatal("get_agent_by_id derived offline")
	}
	must[bool](t)(restarted.UpdateHeartbeat(e.ctx, id, jsonx.Obj("version", "2.0.0", "service_status", "running")))
	listed = must[[]*jsonx.Object](t)(restarted.GetAllAgents(e.ctx, true))[0]
	if listed.Value("status") != "online" || listed.Value("service_status") != "running" {
		t.Fatalf("after heartbeat %v", listed)
	}
}

func TestEveryManagerForADataDirectorySharesTheState(t *testing.T) {
	e, id := setupRuntime(t)
	second := newManager(t, openRepo(t, e.dir))
	must[bool](t)(second.UpdateHeartbeat(e.ctx, id, jsonx.Obj("service_status", "running")))
	if e.get(id).Value("service_status") != "running" {
		t.Fatal("state not shared")
	}
	other := newManager(t, openRepo(t, filepath.Join(e.dir, "other")))
	if other.Runtime() == e.m.Runtime() || second.Runtime() != e.m.Runtime() {
		t.Fatal("wrong sharing")
	}
}

func TestDomainDiscoveryToggleReturnsTheLiveAgent(t *testing.T) {
	e, id := setupRuntime(t)
	e.beat(id, jsonx.Obj("version", "2.0.0", "system_metrics", metrics()))
	updated := must[*jsonx.Object](t)(e.m.SetDomainDiscovery(e.ctx, id, true))
	if updated.Value("version") != "2.0.0" || !jsonx.Equal(updated.Value("system_metrics"), testMetrics) || updated.Value("status") != "online" {
		t.Fatalf("updated %v", updated)
	}
}

func TestANewerHeartbeatInTheDocumentWins(t *testing.T) {
	e, id := setupRuntime(t)
	e.beat(id, jsonx.Obj("service_status", "running"))
	entry := e.m.Runtime().Entries()[id]
	entry.HeartbeatAt -= 5
	e.m.Runtime().SetEntry(id, entry)
	later := nowISO()
	e.editSystem(func(system *jsonx.Object) {
		e.firstAgent(system).Set("last_heartbeat", later)
		e.firstAgent(system).Set("service_status", "stopped")
	})
	current := e.get(id)
	if current.Value("service_status") != "stopped" || current.Value("last_heartbeat") != later {
		t.Fatalf("current %v", current)
	}
}

func TestStaleHeartbeatReadsAsOfflineOnlyInTheList(t *testing.T) {
	e, id := setupRuntime(t)
	e.ageRegistration(10)
	e.beat(id, jsonx.NewObject())
	entry := e.m.Runtime().Entries()[id]
	entry.LastHeartbeat = isoformat(time.Now().Add(-5*time.Minute), false)
	e.m.Runtime().SetEntry(id, entry)
	if must[[]*jsonx.Object](t)(e.m.GetAllAgents(e.ctx, true))[0].Value("status") != "offline" || e.get(id).Value("status") != "online" {
		t.Fatal("freshness")
	}
}

func TestOneFreshnessRuleTwoMinutesInclusive(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.Local)
	agent := func(seconds float64, status string) *jsonx.Object {
		at := now.Add(-time.Duration(seconds * float64(time.Second)))
		return jsonx.Obj("status", status, "last_heartbeat", isoformat(at, false))
	}
	if HeartbeatFreshSeconds != 120 || !HeartbeatFresh(agent(0, "online"), &now) || !HeartbeatFresh(agent(120, "online"), &now) ||
		HeartbeatFresh(agent(120.001, "online"), &now) {
		t.Fatal("freshness bounds")
	}
	if OnlineStatus(agent(120, "online"), &now) != "online" || OnlineStatus(agent(121, "online"), &now) != "offline" {
		t.Fatal("online status")
	}
	if !IsOnline(agent(30, "online"), &now) || IsOnline(agent(30, "offline"), &now) || OnlineStatus(agent(30, "error"), &now) != "error" {
		t.Fatal("is online")
	}
	for _, broken := range []*jsonx.Object{jsonx.NewObject(), jsonx.Obj("last_heartbeat", ""), jsonx.Obj("last_heartbeat", "yesterday"),
		jsonx.Obj("last_heartbeat", nil), jsonx.Obj("last_heartbeat", 5), nil} {
		if HeartbeatFresh(broken, &now) || OnlineStatus(broken, &now) != "offline" {
			t.Fatalf("broken %v", broken)
		}
	}
	aware := time.Now()
	if !IsOnline(jsonx.Obj("status", "online", "last_heartbeat", isoformat(aware.Add(-30*time.Second), true)), nil) ||
		IsOnline(jsonx.Obj("status", "online", "last_heartbeat", isoformat(aware.Add(-3*time.Minute), true)), nil) {
		t.Fatal("aware")
	}
	enabled := agent(30, "online")
	enabled.Set("enabled", true)
	disabled := agent(30, "online")
	disabled.Set("enabled", false)
	if !IsUsable(agent(30, "online"), &now) || !IsUsable(enabled, &now) || IsUsable(disabled, &now) || IsUsable(agent(121, "online"), &now) {
		t.Fatal("usable")
	}
}

func TestTheListDerivesStatusWithTheSharedRule(t *testing.T) {
	e, id := setupRuntime(t)
	if must[[]*jsonx.Object](t)(e.m.GetAllAgents(e.ctx, true))[0].Value("status") != "online" {
		t.Fatal("fresh registration offline")
	}
	e.ageRegistration(3)
	restarted := e.restart()
	listed := must[[]*jsonx.Object](t)(restarted.GetAllAgents(e.ctx, true))[0]
	if listed.Value("status") != "offline" || OnlineStatus(must[*jsonx.Object](t)(restarted.GetAgentByID(e.ctx, id, true)), nil) != "offline" {
		t.Fatal("derived status")
	}
}

func TestARegistrationTimeAheadOfTheClockDoesNotHideHeartbeats(t *testing.T) {
	e, id := setupRuntime(t)
	ahead := isoformat(time.Now().Add(8*time.Hour), false)
	e.editSystem(func(system *jsonx.Object) { e.firstAgent(system).Set("last_heartbeat", ahead) })
	e.beat(id, jsonx.Obj("service_status", "running"))
	current := e.get(id)
	beat, _, _ := fromISO(current.GetString("last_heartbeat"))
	if current.Value("service_status") != "running" || beat.After(time.Now()) {
		t.Fatalf("current %v", current)
	}
}

func TestAFailedVersionWriteStillRecordsTheHeartbeatAndRetries(t *testing.T) {
	e, id := setupRuntime(t)
	testFailUpdate = func() error { return errors.New("disk full") }
	if _, err := e.m.UpdateHeartbeat(e.ctx, id, jsonx.Obj("version", "2.0.0")); err == nil || err.Error() != "disk full" {
		t.Fatalf("err %v", err)
	}
	testFailUpdate = nil
	if e.get(id).Value("version") != "2.0.0" || e.stored(id).Value("version") != "1.0.0" {
		t.Fatal("state")
	}
	e.beat(id, jsonx.Obj("version", "2.0.0"))
	if e.stored(id).Value("version") != "2.0.0" {
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
				if _, err := e.m.UpdateHeartbeat(e.ctx, id, jsonx.Obj("service_status", "s"+string(rune('0'+index)), "system_metrics", metrics())); err != nil {
					errs <- err
					return
				}
				if _, err := e.m.GetAllAgents(e.ctx, true); err != nil {
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
	if !strings.HasPrefix(e.get(id).GetString("service_status"), "s") {
		t.Fatal("status")
	}
}

func TestAHeartbeatRacingADeleteLeavesNothingBehind(t *testing.T) {
	e, id := setupRuntime(t)
	testRecordHeartbeat = func(m *Manager, agent, payload *jsonx.Object, mark int64) bool {
		if !must[bool](t)(m.DeleteAgent(e.ctx, id)) {
			t.Fatal("delete")
		}
		return m.runtime.RecordHeartbeat(agent, payload, mark)
	}
	t.Cleanup(func() { testRecordHeartbeat = nil })
	revision := e.revision()
	if e.beat(id, jsonx.Obj("version", "2.0.0", "system_metrics", metrics())) {
		t.Fatal("accepted")
	}
	if len(e.m.Runtime().Entries()) != 0 || e.revision() != revision+1 {
		t.Fatal("left something")
	}
}

func TestADeleteAfterTheHeartbeatIsRecordedSkipsTheVersionWrite(t *testing.T) {
	e, id := setupRuntime(t)
	testRecordHeartbeat = func(m *Manager, agent, payload *jsonx.Object, mark int64) bool {
		if !m.runtime.RecordHeartbeat(agent, payload, mark) || !must[bool](t)(m.DeleteAgent(e.ctx, id)) {
			t.Fatal("setup")
		}
		return true
	}
	t.Cleanup(func() { testRecordHeartbeat = nil })
	revision := e.revision()
	if e.beat(id, jsonx.Obj("version", "2.0.0", "config_version", "cfg-2")) {
		t.Fatal("accepted")
	}
	if len(e.m.Runtime().Entries()) != 0 || e.revision() != revision+1 || len(e.system().GetList("agents")) != 0 {
		t.Fatal("wrote")
	}
}

func TestAnAgentRestoredAfterItsDeletionIsHeardAgain(t *testing.T) {
	e, id := setupRuntime(t)
	saved := e.stored(id)
	must[bool](t)(e.m.DeleteAgent(e.ctx, id))
	e.editSystem(func(system *jsonx.Object) { system.Set("agents", append(system.GetList("agents"), saved)) })
	e.beat(id, jsonx.Obj("service_status", "running"))
	if e.get(id).Value("service_status") != "running" {
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

func legacyRowOf(id string, moment time.Time, fields *jsonx.Object) [4]any {
	text, _ := jsonx.Default(fields)
	return [4]any{id, pyTimestamp(moment), isoformat(moment, false), string(text)}
}

func twoAgents(t *testing.T) (*env, string, string) {
	e, _ := setupRuntime(t)
	e.m.DeleteAgent(e.ctx, e.agent.GetString("id"))
	fresh := e.register(jsonx.Obj("name", "fresh", "host", "192.0.2.10", "version", "1.0.0"), nil).GetString("id")
	stale := e.register(jsonx.Obj("name", "stale", "host", "192.0.2.11", "version", "1.0.0"), nil).GetString("id")
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
		legacyRowOf(fresh, now, jsonx.Obj("version", "2.0.0", "service_status", "running", "system_metrics", metrics())),
		legacyRowOf(stale, older, jsonx.Obj("version", "1.5.0")),
		legacyRowOf("deleted", now, jsonx.Obj("version", "9.9.9")),
	})
	os.WriteFile(filepath.Join(e.dir, "runtime.sqlite3-journal"), nil, 0o644)
	os.WriteFile(filepath.Join(e.dir, "runtime.sqlite3.corrupt-1"), []byte("x"), 0o644)
	e.editSystem(func(system *jsonx.Object) {
		agent, _ := findAgent(system.GetList("agents"), stale)
		agent.Set("last_heartbeat", nowISO())
	})
	revision := e.revision()
	logs := captureLogs(t)
	ResetRuntime()
	manager := newManager(t, openRepo(t, e.dir))
	newManager(t, openRepo(t, e.dir))
	if got := logs(); strings.Join(got, ",") != "INFO" {
		t.Fatalf("logs %v", got)
	}
	if e.revision() != revision+1 || e.stored(fresh).Value("version") != "2.0.0" || e.stored(stale).Value("version") != "1.0.0" {
		t.Fatal("versions")
	}
	agents := map[string]*jsonx.Object{}
	for _, agent := range must[[]*jsonx.Object](t)(manager.GetAllAgents(e.ctx, true)) {
		agents[agent.GetString("id")] = agent
	}
	if agents[fresh].Value("status") != "online" || !jsonx.Equal(agents[fresh].Value("system_metrics"), testMetrics) ||
		agents[stale].Value("version") != "1.0.0" || agents[stale].Value("status") != "online" {
		t.Fatal("agents")
	}
	if entries := manager.Runtime().Entries(); len(entries) != 1 {
		t.Fatalf("entries %v", entries)
	}
	matches, _ := filepath.Glob(filepath.Join(e.dir, "runtime.sqlite3*"))
	if len(matches) != 0 {
		t.Fatalf("left %v", matches)
	}
}

func TestUpgradeWithUnchangedVersionsDoesNotWriteTheConfiguration(t *testing.T) {
	e, fresh, _ := twoAgents(t)
	legacyDatabase(t, filepath.Join(e.dir, "runtime.sqlite3"), [][4]any{legacyRowOf(fresh, time.Now(), jsonx.Obj("version", "1.0.0"))})
	revision := e.revision()
	manager := e.restart()
	if e.revision() != revision || must[*jsonx.Object](t)(manager.GetAgentByID(e.ctx, fresh, true)).Value("status") != "online" {
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
	if must[*jsonx.Object](t)(manager.GetAgentByID(e.ctx, fresh, true)).Value("version") != "1.0.0" || len(manager.Runtime().Entries()) != 0 {
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
		legacyRowOf(stale, time.Now(), jsonx.Obj("version", "2.0.0")),
	})
	manager := e.restart()
	entries := manager.Runtime().Entries()
	if _, ok := entries[stale]; len(entries) != 1 || !ok || e.stored(stale).Value("version") != "2.0.0" {
		t.Fatalf("entries %v", entries)
	}
}

func TestAFailedVersionImportIsNotRetriedOverNewerVersions(t *testing.T) {
	e, fresh, _ := twoAgents(t)
	legacyDatabase(t, filepath.Join(e.dir, "runtime.sqlite3"), [][4]any{legacyRowOf(fresh, time.Now(), jsonx.Obj("version", "2.0.0"))})
	ResetRuntime()
	logs := captureLogs(t)
	r := openRepo(t, e.dir)
	state := RuntimeFor(r.DataDir())
	if err := ImportLegacyDatabase(e.ctx, r, state, func(context.Context, map[string]*jsonx.Object) (map[string]bool, error) {
		return nil, errors.New("disk full")
	}); err != nil {
		t.Fatal(err)
	}
	manager := newManager(t, r)
	if got := logs(); strings.Join(got, ",") != "WARNING,INFO" {
		t.Fatalf("logs %v", got)
	}
	if must[*jsonx.Object](t)(manager.GetAgentByID(e.ctx, fresh, true)).Value("version") != "2.0.0" {
		t.Fatal("not shown live")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "runtime.sqlite3")); err == nil {
		t.Fatal("not removed")
	}
	must[bool](t)(manager.UpdateHeartbeat(e.ctx, fresh, jsonx.Obj("version", "3.0.0")))
	restarted := e.restart()
	if e.stored(fresh).Value("version") != "3.0.0" || must[*jsonx.Object](t)(restarted.GetAgentByID(e.ctx, fresh, true)).Value("version") != "3.0.0" {
		t.Fatal("next heartbeat did not write")
	}
}

func TestARuntimeSqliteThatCannotBeRemovedIsNotImported(t *testing.T) {
	e, fresh, _ := twoAgents(t)
	database := filepath.Join(e.dir, "runtime.sqlite3")
	legacyDatabase(t, database, [][4]any{legacyRowOf(fresh, time.Now(), jsonx.Obj("version", "2.0.0"))})
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
	if e.revision() != revision || len(manager.Runtime().Entries()) != 0 {
		t.Fatal("imported")
	}
	if _, err := os.Stat(database); err != nil {
		t.Fatal("removed")
	}
}

// --- presence logging across a restart ---

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
	list := func() []*jsonx.Object { return must[[]*jsonx.Object](t)(restarted.GetAllAgents(e.ctx, true)) }
	ObservePresence(list())
	must[bool](t)(restarted.UpdateHeartbeat(e.ctx, id, jsonx.NewObject()))
	ObservePresence(list())
	entry := restarted.Runtime().Entries()[id]
	entry.LastHeartbeat = isoformat(time.Now().Add(-5*time.Minute), false)
	restarted.Runtime().SetEntry(id, entry)
	ObservePresence(list())
	must[bool](t)(restarted.UpdateHeartbeat(e.ctx, id, jsonx.NewObject()))
	ObservePresence(list())
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

// Overlay adapts legacy records to the single typed runtime owner.
func (s *RuntimeState) Overlay(records []*jsonx.Object) []*jsonx.Object {
	for _, raw := range records {
		record, err := agentModel(raw)
		if err != nil {
			continue
		}
		overlaid := s.overlayModels([]model.Agent{*record})
		result, err := agentObject(&overlaid[0])
		if err == nil {
			raw.Update(result)
		}
	}
	return records
}

// RecordHeartbeat adapts a legacy heartbeat to the typed runtime owner.
func (s *RuntimeState) RecordHeartbeat(agent, payload *jsonx.Object, mark int64) bool {
	record, err := agentModel(agent)
	if err != nil {
		return false
	}
	fields, err := agentModel(payload)
	if err != nil {
		return false
	}
	ok, err := s.recordModel(context.Background(), record, fields, mark)
	return err == nil && ok
}

// Seed imports a legacy heartbeat projection through the typed owner.
func (s *RuntimeState) Seed(id string, at float64, heartbeat string, fields *jsonx.Object) {
	record, err := agentModel(fields)
	if err == nil {
		_ = s.seedModel(context.Background(), id, at, heartbeat, record)
	}
}
