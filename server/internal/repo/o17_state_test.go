package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo/repotest"
	"configflow/internal/store"
)

// o17Registry is a Python-era registry row: state members inline, entries
// without an id, a repeated id, a non-object entry and damaged values.
const o17Registry = `[` +
	`{"id":"m1","name":"M1","host":"192.0.2.10","port":8080,"service_type":"mihomo","profile_id":"default","enabled":true,` +
	`"version":"1.8.0","config_version":"abcd1234","config_revision":3,"pushed_at":"2026-01-01T00:00:00Z",` +
	`"deployments":{"d1":{"deployment_id":"d1","status":"succeeded","success":true}},` +
	`"latest_deployment":{"deployment_id":"d1","status":"succeeded","success":true},` +
	`"applied_content":{"fingerprint":"f","deployment_id":"d1"},` +
	`"rule_sync":{"settings":{"library":true},"state":{"status":"succeeded","message":"ok"}},` +
	`"status":"online","last_heartbeat":"2026-01-01T00:00:00Z","cpu":{"core_count":2}},` +
	`{"id":"o1","name":"O1","host":"192.0.2.11","port":8081,"service_type":"oxidns","profile_id":"default",` +
	`"oxidns_ha":{"enabled":true,"peer_id":"o2","vip":"192.0.2.100","vrid":51},` +
	`"oxidns_ha_publication":"applying","dns_follow_state":{"status":"publishing"},"ipv6_verifications":[]},` +
	`{"name":"no id","version":"0.1"},` +
	`{"id":"m1","name":"repeated id","version":"9"},` +
	`7]`

// o17V1Database builds a version-1 data directory holding o17Registry.
func o17V1Database(t *testing.T) (dir string, want string) {
	t.Helper()
	dir = t.TempDir()
	r := must(Open(dir, Options{}))(t)
	path := r.DatabasePath()
	r.Close()
	repotest.WriteRow(t, path, "system", "", "agents", []byte(o17Registry))
	repotest.DowngradeToV1(t, path)
	return dir, o17Decoded(t, []byte(o17Registry))
}

func o17Decoded(t testing.TB, row []byte) string {
	t.Helper()
	var system model.System
	if _, _, err := model.DecodeStoredMember(&system, "agents", row, "agents"); err != nil {
		t.Fatal(err)
	}
	return string(must(model.Marshal(system.Agents))(t))
}

func o17View(t testing.TB, r *Repository) string {
	t.Helper()
	system := must(r.System(bg))(t)
	return string(must(model.Marshal(system.Agents))(t))
}

func o17Count(t testing.TB, path, query string) int {
	t.Helper()
	db := must(sql.Open("sqlite", "file:"+path))(t)
	defer db.Close()
	var n int
	noError(t, db.QueryRow(query).Scan(&n))
	return n
}

func o17Version(t testing.TB, path string) int {
	t.Helper()
	s := store.New(path, LockTimeout)
	defer s.Close()
	return must(s.Version())(t)
}

func o17Fingerprint(t testing.TB, path string) store.Fingerprint {
	t.Helper()
	s := store.New(path, LockTimeout)
	defer s.Close()
	return must(s.Fingerprint())(t)
}

func TestO17UpgradeMovesAgentState(t *testing.T) {
	dir, want := o17V1Database(t)
	path := filepath.Join(dir, DatabaseFilename)
	before := o17Fingerprint(t, path)
	r := openRepo(t, dir)
	if v := o17Version(t, path); v != store.StorageVersion {
		t.Fatalf("version %d", v)
	}
	if got := o17View(t, r); got != want {
		t.Fatalf("merged registry differs from the version-1 registry\ngot  %s\nwant %s", got, want)
	}
	// m1: version config_version config_revision pushed_at deployments latest_deployment applied_content
	// rule_sync.state status last_heartbeat cpu = 11; o1: oxidns_ha_publication dns_follow_state ipv6_verifications = 3.
	if n := o17Count(t, path, "SELECT count(*) FROM agent_state"); n != 14 {
		t.Fatalf("%d state rows", n)
	}
	raw := must(store.New(path, LockTimeout).Load())(t)
	var agents []byte
	for _, field := range raw.Document.System.Fields {
		if field.Name == "agents" {
			agents = field.Value
		}
	}
	wantConfig := `[{"id":"m1","name":"M1","host":"192.0.2.10","port":8080,"service_type":"mihomo","profile_id":"default","enabled":true,` +
		`"rule_sync":{"settings":{"library":true}}},` +
		`{"id":"o1","name":"O1","host":"192.0.2.11","port":8081,"service_type":"oxidns","profile_id":"default",` +
		`"oxidns_ha":{"enabled":true,"peer_id":"o2","vip":"192.0.2.100","vrid":51}},` +
		`{"name":"no id","version":"0.1"},{"id":"m1","name":"repeated id","version":"9"},7]`
	if string(agents) != wantConfig {
		t.Fatalf("configuration row\n%s\nwant\n%s", agents, wantConfig)
	}
	// The damaged HA journal still reads as an active publication (fails closed).
	system := must(r.System(bg))(t)
	if j := system.Agents[1].OxiDNSHAPublication; j == nil || j.Status != "" {
		t.Fatalf("damaged journal %+v", j)
	}
	// The backup is the untouched version-1 file.
	backups := must(filepath.Glob(filepath.Join(dir, BackupsDirName, "configflow-v1-*.sqlite3")))(t)
	if len(backups) != 1 {
		t.Fatalf("backups %v", backups)
	}
	if got := o17Fingerprint(t, backups[0]); got.Digest != before.Digest {
		t.Fatal("the backup differs from the version-1 database")
	}
}

func TestO17UpgradeIsIdempotent(t *testing.T) {
	dir, want := o17V1Database(t)
	path := filepath.Join(dir, DatabaseFilename)
	r := openRepo(t, dir)
	first := o17Fingerprint(t, path)
	r.Close()
	for range 3 {
		again := openRepo(t, dir)
		if got := o17View(t, again); got != want {
			t.Fatalf("view after reopening\n%s", got)
		}
		noError(t, again.upgradeLocked(bg))
		again.Close()
	}
	if got := o17Fingerprint(t, path); got != first {
		t.Fatal("reopening an upgraded database changed it")
	}
	if n := len(must(filepath.Glob(filepath.Join(dir, BackupsDirName, "*")))(t)); n != 1 {
		t.Fatalf("%d backups", n)
	}
}

func TestO17UpgradeBackupFailureRefusesStart(t *testing.T) {
	dir, _ := o17V1Database(t)
	path := filepath.Join(dir, DatabaseFilename)
	before := o17Fingerprint(t, path)
	noError(t, os.WriteFile(filepath.Join(dir, BackupsDirName), []byte("not a directory"), 0o600))
	_, err := Open(dir, Options{})
	wantError(t, err, ErrRepository, "前的备份失败")
	wantError(t, err, ErrRepository, "服务没有启动")
	if got := o17Fingerprint(t, path); got != before || o17Version(t, path) != 1 {
		t.Fatal("the database changed although the backup failed")
	}
}

func TestO17UpgradeStoppedAtEveryRowLeavesVersion1(t *testing.T) {
	dir, want := o17V1Database(t)
	path := filepath.Join(dir, DatabaseFilename)
	before := o17Fingerprint(t, path)
	stop := errors.New("stopped")
	for i := range 14 {
		SetUpgradeRowHookForTest(func(row int) error {
			if row == i {
				return stop
			}
			return nil
		})
		_, err := Open(dir, Options{})
		SetUpgradeRowHookForTest(nil)
		wantError(t, err, ErrRepository, "数据库未改动")
		if !errors.Is(err, stop) {
			t.Fatalf("row %d: %v", i, err)
		}
		if got := o17Fingerprint(t, path); got != before {
			t.Fatalf("row %d: the database changed", i)
		}
	}
	r := openRepo(t, dir)
	if got := o17View(t, r); got != want {
		t.Fatalf("view after the retried upgrade\n%s", got)
	}
}

// TestO17UpgradeKilled kills a real process inside the upgrade transaction
// (power loss): the next start sees the untouched version-1 file and upgrades.
func TestO17UpgradeKilled(t *testing.T) {
	if dir := os.Getenv("CONFIGFLOW_O17_UPGRADE_DIR"); dir != "" {
		pause := must(strconv.Atoi(os.Getenv("CONFIGFLOW_O17_UPGRADE_PAUSE")))(t)
		SetUpgradeRowHookForTest(func(row int) error {
			if row == pause {
				noError(t, os.WriteFile(filepath.Join(dir, "paused"), nil, 0o600))
				select {} // killed here
			}
			return nil
		})
		_, err := Open(dir, Options{})
		t.Fatalf("upgrade finished: %v", err)
	}
	for _, pause := range []int{0, 7, 13} {
		t.Run(fmt.Sprint("row", pause), func(t *testing.T) {
			dir, want := o17V1Database(t)
			path := filepath.Join(dir, DatabaseFilename)
			before := o17Fingerprint(t, path)
			child := exec.Command(os.Args[0], "-test.run=^TestO17UpgradeKilled$", "-test.count=1")
			child.Env = append(os.Environ(), "CONFIGFLOW_O17_UPGRADE_DIR="+dir, "CONFIGFLOW_O17_UPGRADE_PAUSE="+strconv.Itoa(pause),
				"CONFIGFLOW_REPO_ORDER_HELPER=1")
			noError(t, child.Start())
			deadline := time.Now().Add(60 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(dir, "paused")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					_ = child.Process.Kill()
					t.Fatal("the child never reached the pause")
				}
				time.Sleep(20 * time.Millisecond)
			}
			noError(t, child.Process.Kill())
			_ = child.Wait()
			if _, err := os.Stat(path + "-journal"); err != nil {
				t.Fatalf("no hot journal after the kill: %v", err)
			}
			if got := o17Fingerprint(t, path); got != before || o17Version(t, path) != 1 {
				t.Fatal("a killed upgrade left a changed database")
			}
			r := openRepo(t, dir)
			if got := o17View(t, r); got != want {
				t.Fatalf("view after the upgrade\n%s", got)
			}
			if v := o17Version(t, path); v != store.StorageVersion {
				t.Fatalf("version %d", v)
			}
		})
	}
}

func TestO17StateCommitsKeepTheConfigurationGeneration(t *testing.T) {
	r := newRepo(t)
	path := r.DatabasePath()
	edit := func(fn func(*model.Agent)) store.Head {
		t.Helper()
		noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
			if len(system.Agents) == 0 {
				system.Agents = []model.Agent{{ID: "a", Name: "A", Host: "192.0.2.1", Port: 8080, ServiceType: "mihomo", ProfileID: "default"}}
			}
			fn(&system.Agents[0])
			return nil
		}))
		return must(r.Head(bg))(t)
	}
	created := edit(func(*model.Agent) {})
	state := edit(func(a *model.Agent) { a.Version = "1.2.3"; a.ConfigVersion = "abcd1234" })
	if !state.SameConfig(created) || state.State != created.State+1 || state == created {
		t.Fatalf("state commit: %+v → %+v", created, state)
	}
	config := edit(func(a *model.Agent) { a.Name = "B" })
	if config.SameConfig(state) || config.State != state.State {
		t.Fatalf("configuration commit: %+v → %+v", state, config)
	}
	both := edit(func(a *model.Agent) { a.Name = "C"; a.Version = "2.0.0" })
	if both.SameConfig(config) || both.State != config.State+1 {
		t.Fatalf("mixed commit: %+v → %+v", config, both)
	}
	if n := o17Count(t, path, "SELECT count(*) FROM agent_state WHERE agent_id='a'"); n != 2 {
		t.Fatalf("%d state rows", n)
	}
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		system.Agents = nil
		return nil
	}))
	if n := o17Count(t, path, "SELECT count(*) FROM agent_state"); n != 0 {
		t.Fatalf("deleting the Agent left %d state rows", n)
	}
	// An edit that changes nothing still advances the system revision (as
	// before), a configuration commit; the state counters stay.
	same := must(r.Head(bg))(t)
	noError(t, r.UpdateSystem(bg, func(context.Context, *model.System) error { return nil }))
	if next := must(r.Head(bg))(t); next.SameConfig(same) || next.State != same.State {
		t.Fatalf("empty edit: %+v → %+v", same, next)
	}
}

func o17AgentWithState(t *testing.T, r *Repository) {
	t.Helper()
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{{ID: "a", Name: "A", Host: "192.0.2.1", Port: 8080, ServiceType: "mihomo", ProfileID: "default",
			Version: "1.2.3", ConfigVersion: "abcd1234", LatestDeployment: &model.Deployment{DeploymentID: "d1", Status: "succeeded"}}}
		system.Agents[0].Deployments.Set("d1", model.Deployment{DeploymentID: "d1", Status: "succeeded"})
		return nil
	}))
}

func TestO17ExportCarriesConfigurationOnly(t *testing.T) {
	r := newRepo(t)
	o17AgentWithState(t, r)
	exported := string(must(r.ExportAllJSON(bg, false))(t))
	for _, member := range []string{`"deployments"`, `"latest_deployment"`, `"version"`, `"config_version"`} {
		if strings.Contains(exported, member) {
			t.Fatalf("export holds %s: %s", member, exported)
		}
	}
	if !strings.Contains(exported, `"host":"192.0.2.1"`) {
		t.Fatalf("export lost the configuration: %s", exported)
	}
	// The repository itself still has the state.
	if a := must(r.System(bg))(t).Agents[0]; a.Version != "1.2.3" || a.LatestDeployment == nil {
		t.Fatalf("state lost: %+v", a)
	}
}

func TestO17ImportMarksAgentsRestoredWithoutRunRecords(t *testing.T) {
	r := newRepo(t)
	o17AgentWithState(t, r)
	backup := must(r.ExportAllJSON(bg, false))(t)
	must(r.ImportAllJSON(bg, backup))(t)
	a := must(r.System(bg))(t).Agents[0]
	if a.StateRestoredAt == nil || a.LatestDeployment != nil || a.Version != "" {
		t.Fatalf("new-format backup: %+v", a)
	}
	if n := o17Count(t, r.DatabasePath(), "SELECT count(*) FROM agent_state WHERE member='state_restored_at'"); n != 1 {
		t.Fatalf("%d markers stored", n)
	}
	// An older backup that carries the run records restores them, unmarked.
	old := strings.Replace(string(backup), `"host":"192.0.2.1"`, `"host":"192.0.2.1","version":"1.0.0","latest_deployment":{"deployment_id":"d0","status":"succeeded"}`, 1)
	must(r.ImportAllJSON(bg, []byte(old)))(t)
	a = must(r.System(bg))(t).Agents[0]
	if a.StateRestoredAt != nil || a.Version != "1.0.0" || a.LatestDeployment == nil || a.LatestDeployment.DeploymentID != "d0" {
		t.Fatalf("older backup: %+v", a)
	}
}

func TestO17CommitObserversByKind(t *testing.T) {
	r := newRepo(t)
	var config, state int
	cancelConfig := r.OnCommit(ChangeConfig, func() { config++ })
	defer cancelConfig()
	cancelState := r.OnCommit(ChangeState, func() { state++ })
	o17AgentWithState(t, r) // configuration and state
	if config != 1 || state != 1 {
		t.Fatalf("mixed commit: config %d state %d", config, state)
	}
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		system.Agents[0].Version = "2.0.0"
		return nil
	}))
	if config != 1 || state != 2 {
		t.Fatalf("state commit: config %d state %d", config, state)
	}
	cancelState()
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		system.Agents[0].Name = "B"
		return nil
	}))
	if config != 2 || state != 2 {
		t.Fatalf("configuration commit after cancel: config %d state %d", config, state)
	}
}
