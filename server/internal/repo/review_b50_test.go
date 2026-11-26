package repo

// HR review of batch-50 (O17 storage version 2): the upgrade on a large
// registry shaped like a real one, interrupted and failing upgrades, the
// backup, and state/configuration commits racing each other. Synthetic data
// only.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo/repotest"
	"configflow/internal/store"
)

// b50Registry is a large Python-era agents row: mihomo Agents with long
// deployment histories, OxiDNS HA pairs with journals in every state, DNS
// follow bindings, IPv6 verifications, rule sync, upgrades, Python metrics,
// unknown members between state members, Python separators, and damaged
// entries and values.
func b50Registry(profiles []string) string {
	var entries []string
	sep := func(i int) (string, string) {
		if i%2 == 0 {
			return ", ", ": " // json.dumps defaults
		}
		return ",", ":"
	}
	deployments := func(id string, n int) string {
		var items []string
		for d := range n {
			status := []string{"succeeded", "failed", "rolled_back", "succeeded"}[d%4]
			items = append(items, fmt.Sprintf(`"%s-d%d":{"deployment_id":"%s-d%d","status":"%s","success":%t,"created_at":"2026-01-%02dT00:00:00Z","events":[{"at":"2026-01-01T00:00:00Z","status":"%s","message":"步骤 %d \u00e9\\n"}]}`,
				id, d, id, d, status, status == "succeeded", d%28+1, status, d))
		}
		return "{" + strings.Join(items, ",") + "}"
	}
	for i := range 60 {
		c, k := sep(i)
		profile := profiles[i%len(profiles)]
		id := fmt.Sprintf("m%02d", i)
		fields := []string{
			`"id"` + k + `"` + id + `"`,
			`"name"` + k + `"Mihomo ` + strconv.Itoa(i) + ` 客厅"`,
			`"host"` + k + `"192.0.2.` + strconv.Itoa(10+i) + `"`,
			`"port"` + k + `9090`,
			`"token"` + k + `"tok-` + id + `"`,
			`"service_type"` + k + `"mihomo"`,
			`"profile_id"` + k + `"` + profile + `"`,
			`"enabled"` + k + `true`,
			`"version"` + k + `"1.8.` + strconv.Itoa(i) + `-go"`,
			`"future_member"` + k + `{"x":[1,2,{"y":null}]}`,
			`"deployments"` + k + deployments(id, 5+i%20),
			`"latest_deployment"` + k + `{"deployment_id":"` + id + `-d0","status":"succeeded","success":true}`,
			`"applied_content"` + k + `{"deployment_id":"` + id + `-d0","fingerprint":"` + strings.Repeat("ab", 32) + `"}`,
			`"config_version"` + k + `"abcd1234"`,
			`"config_revision"` + k + strconv.Itoa(i),
			`"pushed_at"` + k + `"2026-02-01T00:00:00Z"`,
			`"rule_sync"` + k + `{"settings":{"library":true,"rules":` + strconv.FormatBool(i%3 == 0) + `},"state":{"status":"succeeded","message":"已同步","updated_at":"2026-02-01T00:00:00Z"},"other":1}`,
			`"latest_upgrade"` + k + `{"upgrade_id":"u` + id + `","status":"succeeded"}`,
			`"ipv6_verifications"` + k + `{"v1":{"verification_id":"v1","status":"passed"}}`,
			`"ipv6_verification_settings"` + k + `{"enabled":true}`,
			`"status"` + k + `"online"`,
			`"last_heartbeat"` + k + `"2026-02-01T00:00:00Z"`,
			`"cpu"` + k + `{"core_count":4,"usage":12.5}`,
			`"memory"` + k + `{"total":1024}`,
			`"created_at"` + k + `"2026-01-01T00:00:00Z"`,
			`"updated_at"` + k + `"2026-01-02T00:00:00Z"`,
		}
		if i%7 == 0 {
			fields = append(fields, `"traffic_history_enabled"`+k+`true`)
		}
		entries = append(entries, "{"+strings.Join(fields, c)+"}")
	}
	journals := []string{`"applying"`, `7`, `[]`, `null`, `{"publication_id":"p1","status":"applying","members":["o00","o01"]}`,
		`{"publication_id":"p2","status":"succeeded"}`, `{"status":"failed","handoff":{"from":"o04"}}`, `{}`}
	for i := range 16 {
		c, k := sep(i)
		id := fmt.Sprintf("o%02d", i)
		peer := fmt.Sprintf("o%02d", i^1)
		role := "primary"
		if i%2 == 1 {
			role = "secondary"
		}
		fields := []string{
			`"id"` + k + `"` + id + `"`,
			`"name"` + k + `"OxiDNS ` + id + `"`,
			`"host"` + k + `"198.51.100.` + strconv.Itoa(10+i) + `"`,
			`"port"` + k + `8081`,
			`"service_type"` + k + `"oxidns"`,
			`"profile_id"` + k + `"` + profiles[i%len(profiles)] + `"`,
			`"oxidns_ha"` + k + `{"enabled":true,"peer_id":"` + peer + `","role":"` + role + `","vip":"198.51.100.200","vrid":` + strconv.Itoa(50+i/2) + `,"group_id":"g` + strconv.Itoa(i/2) + `"}`,
			`"oxidns_ha_state"` + k + `{"status":"succeeded","members":["` + id + `","` + peer + `"]}`,
			`"oxidns_ha_publication"` + k + journals[i%len(journals)],
			`"dns_follow"` + k + `{"id":"f` + id + `","source_agent_id":"m0` + strconv.Itoa(i%10) + `","source_profile_id":"default","target_profile_id":"default","upstream_signature":"sig"}`,
			`"dns_follow_state"` + k + []string{`{"status":"succeeded","events":[]}`, `"publishing"`, `{"status":"waiting"}`, `5`}[i%4],
			`"dns_follow_job"` + k + `{"job_id":"j` + id + `","candidate_settings":{"upstreams":["192.0.2.53"]}}`,
			`"deployments"` + k + deployments(id, 3),
			`"version"` + k + `"1.8.60-go"`,
		}
		entries = append(entries, "{"+strings.Join(fields, c)+"}")
	}
	entries = append(entries,
		`{"name":"no id","version":"0.1","deployments":{}}`,
		`{"id":"m00","name":"repeated id","version":"9","deployments":{"x":{}}}`,
		`{"id":"","version":"empty id"}`,
		`{"id":42,"name":"numeric id","version":"2","latest_deployment":5}`,
		`{"id":"bad1","deployments":"not an object","latest_deployment":[],"rule_sync":"not an object","applied_content":null}`,
		`{"id":"bad2","rule_sync":{"state":"text","settings":{"library":"yes"}},"oxidns_ha_publication":{"status":7}}`,
		`{"id":"bad3","rule_sync":{"state":{"status":"running"},"state_extra":1},"service_type":"mihomo"}`,
		`{"id":"esc\"id","name":"quote \\ back","version":"1"}`,
		`7`, `null`, `"text"`, `[]`,
	)
	return "[" + strings.Join(entries, ", ") + "]"
}

// b50V1Store builds a version-1 data directory with several profiles and the
// large registry, and returns the typed document a version-1 reader sees.
func b50V1Store(t *testing.T) (dir, want string, registry string) {
	t.Helper()
	dir = t.TempDir()
	r := must(Open(dir, Options{}))(t)
	profiles := []string{"default"}
	for i := range 4 {
		id := fmt.Sprintf("p%d", i)
		must(r.CreateProfileRecord(bg, ProfileCreation{ID: id, Name: "Profile " + id}, ""))(t)
		profiles = append(profiles, id)
	}
	path := r.DatabasePath()
	r.Close()
	registry = b50Registry(profiles)
	repotest.WriteRow(t, path, "system", "", "agents", []byte(registry))
	repotest.DowngradeToV1(t, path)
	return dir, b50StoredDocument(t, path), registry
}

// b50StoredDocument decodes the stored file the way the repository reads it
// (merged view) and encodes the whole typed document.
func b50StoredDocument(t testing.TB, path string) string {
	t.Helper()
	s := store.New(path, LockTimeout)
	defer s.Close()
	entry := must(decodeEntry(must(s.Load())(t)))(t)
	return string(must(model.Marshal(entry.Model))(t))
}

func b50View(t testing.TB, r *Repository) string {
	t.Helper()
	var out string
	noError(t, r.View(bg, func(document *model.Document) error {
		raw, err := model.Marshal(document)
		out = string(raw)
		return err
	}))
	return out
}

func TestReviewB50UpgradeLargeRegistryKeepsTheView(t *testing.T) {
	dir, want, registry := b50V1Store(t)
	path := filepath.Join(dir, DatabaseFilename)
	r := openRepo(t, dir)
	if v := o17Version(t, path); v != 2 {
		t.Fatalf("version %d", v)
	}
	if got := b50View(t, r); got != want {
		t.Fatalf("typed document changed by the upgrade\n got %d bytes\nwant %d bytes\n%s", len(got), len(want), firstDifference(got, want))
	}
	if got := b50StoredDocument(t, path); got != want {
		t.Fatal("stored merged view differs from the version-1 document")
	}
	// Every state row is a byte copy of a member of the version-1 row.
	s := store.New(path, LockTimeout)
	entry := must(s.Load())(t)
	s.Close()
	if len(entry.Document.State) < 60*15 {
		t.Fatalf("only %d state rows", len(entry.Document.State))
	}
	for _, row := range entry.Document.State {
		if !strings.Contains(registry, string(row.Value)) {
			t.Fatalf("state row %s/%s is not a byte copy: %s", row.Owner, row.Member, row.Value)
		}
	}
	var config []byte
	for _, field := range entry.Document.System.Fields {
		if field.Name == "agents" {
			config = field.Value
		}
	}
	if again, rows := splitAgents(config); rows != nil || string(again) != string(config) {
		t.Fatalf("the configuration row still holds %d state members", len(rows))
	}
	// The reopen keeps the view.
	r.Close()
	again := openRepo(t, dir)
	if got := b50View(t, again); got != want {
		t.Fatal("view after reopening differs")
	}
}

func firstDifference(a, b string) string {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			lo := max(0, i-200)
			return fmt.Sprintf("at %d:\n got …%s\nwant …%s", i, a[lo:min(len(a), i+200)], b[lo:min(len(b), i+200)])
		}
	}
	return "one is a prefix of the other"
}

// TestReviewB50FailingStartsReuseOneBackup: a start whose upgrade fails
// keeps refusing, but does not write a new copy of the database each time.
func TestReviewB50FailingStartsReuseOneBackup(t *testing.T) {
	dir, want := o17V1Database(t)
	stop := errors.New("stopped")
	SetUpgradeRowHookForTest(func(int) error { return stop })
	for range 5 {
		if _, err := Open(dir, Options{}); !errors.Is(err, stop) {
			SetUpgradeRowHookForTest(nil)
			t.Fatalf("open: %v", err)
		}
	}
	SetUpgradeRowHookForTest(nil)
	backups := must(filepath.Glob(filepath.Join(dir, BackupsDirName, "*")))(t)
	if len(backups) != 1 {
		t.Fatalf("%d backups after 5 failed starts: %v", len(backups), backups)
	}
	r := openRepo(t, dir)
	if got := o17View(t, r); got != want {
		t.Fatalf("view %s", got)
	}
	// The reused backup is still the version-1 file.
	if v := o17Version(t, backups[0]); v != 1 {
		t.Fatalf("backup version %d", v)
	}
}

// TestReviewB50ForeignBackupIsNotReused: a backup of other content (an older
// database) is never taken for the backup of this one.
func TestReviewB50ForeignBackupIsNotReused(t *testing.T) {
	dir, _ := o17V1Database(t)
	other, _ := o17V1Database(t)
	noError(t, os.MkdirAll(filepath.Join(dir, BackupsDirName), 0o700))
	foreign := filepath.Join(dir, BackupsDirName, "configflow-v1-20200101T000000Z.sqlite3")
	data := must(os.ReadFile(filepath.Join(other, DatabaseFilename)))(t)
	noError(t, os.WriteFile(foreign, data, 0o600))
	openRepo(t, dir)
	backups := must(filepath.Glob(filepath.Join(dir, BackupsDirName, "*.sqlite3")))(t)
	if len(backups) != 2 {
		t.Fatalf("backups %v", backups)
	}
}

func TestReviewB50ReadOnlyBackupDirectoryRefusesStart(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir, want := o17V1Database(t)
	path := filepath.Join(dir, DatabaseFilename)
	before := o17Fingerprint(t, path)
	backups := filepath.Join(dir, BackupsDirName)
	noError(t, os.Mkdir(backups, 0o500))
	_, err := Open(dir, Options{})
	wantError(t, err, ErrRepository, "前的备份失败")
	if got := o17Fingerprint(t, path); got != before || o17Version(t, path) != 1 {
		t.Fatal("the database changed")
	}
	noError(t, os.Chmod(backups, 0o700))
	r := openRepo(t, dir)
	if got := o17View(t, r); got != want {
		t.Fatalf("view %s", got)
	}
}

// TestReviewB50DiskFullDuringUpgrade runs the upgrade in a child whose file
// size limit lets the backup through but not a larger database or journal:
// the start fails, the file stays version 1, and the next start upgrades.
func TestReviewB50DiskFullDuringUpgrade(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RLIMIT_FSIZE child: linux only")
	}
	if dir := os.Getenv("CONFIGFLOW_B50_FSIZE_DIR"); dir != "" {
		limit := must(strconv.ParseUint(os.Getenv("CONFIGFLOW_B50_FSIZE"), 10, 64))(t)
		signal.Ignore(syscall.SIGXFSZ)
		noError(t, syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: limit, Max: limit}))
		_, err := Open(dir, Options{})
		if err == nil {
			fmt.Println("B50 OPENED")
			return
		}
		fmt.Println("B50 REFUSED:", err)
		return
	}
	dir, want, _ := b50V1Store(t)
	path := filepath.Join(dir, DatabaseFilename)
	before := o17Fingerprint(t, path)
	info := must(os.Stat(path))(t)
	// Dry run on a copy: the upgrade must need more than the file size.
	probe := t.TempDir()
	noError(t, os.WriteFile(filepath.Join(probe, DatabaseFilename), must(os.ReadFile(path))(t), 0o600))
	pr := openRepo(t, probe)
	pr.Close()
	grown := must(os.Stat(filepath.Join(probe, DatabaseFilename)))(t).Size()
	t.Logf("v1 %d bytes, upgraded %d bytes", info.Size(), grown)
	limit := info.Size()
	if grown <= limit {
		t.Fatalf("the upgraded file (%d bytes) does not outgrow the limit %d: no failure to inject", grown, limit)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestReviewB50DiskFullDuringUpgrade$", "-test.count=1", "-test.v")
	child.Env = append(os.Environ(), "CONFIGFLOW_B50_FSIZE_DIR="+dir, "CONFIGFLOW_B50_FSIZE="+strconv.FormatInt(limit, 10),
		"CONFIGFLOW_REPO_ORDER_HELPER=1")
	out, _ := child.CombinedOutput()
	text := string(out)
	switch {
	case strings.Contains(text, "B50 REFUSED:"):
		if !strings.Contains(text, "数据库未改动") {
			t.Fatalf("refusal without the promise: %s", text)
		}
		if got := o17Fingerprint(t, path); got != before || o17Version(t, path) != 1 {
			t.Fatal("a failed upgrade changed the database")
		}
	default:
		t.Fatalf("child: %s", text)
	}
	r := openRepo(t, dir)
	if got := b50View(t, r); got != want {
		t.Fatal("view after the retried upgrade differs")
	}
}

// TestReviewB50StateAndConfigurationCommitsRace interleaves state-only and
// configuration commits with readers: the configuration generation and the
// system revision count configuration commits only, observers see every
// commit by kind, and readers always see a consistent merged record.
func TestReviewB50StateAndConfigurationCommitsRace(t *testing.T) {
	r := newRepo(t)
	o17AgentWithState(t, r)
	start := must(r.Head(bg))(t)
	startRevision := must(r.System(bg))(t).Revision
	var mu sync.Mutex
	var configs, states int
	cancel1 := r.OnCommit(ChangeConfig, func() { mu.Lock(); configs++; mu.Unlock() })
	cancel2 := r.OnCommit(ChangeState, func() { mu.Lock(); states++; mu.Unlock() })
	defer cancel1()
	defer cancel2()
	const writers, each = 4, 15
	var wg sync.WaitGroup
	errs := make(chan error, 4*writers*each)
	for w := range writers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := range each {
				errs <- r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
					a := &system.Agents[0]
					id := fmt.Sprintf("s%d-%d", w, i)
					a.Deployments.Set(id, model.Deployment{DeploymentID: id, Status: "succeeded"})
					a.LatestDeployment = &model.Deployment{DeploymentID: id, Status: "succeeded"}
					return nil
				})
			}
		}()
		go func() {
			defer wg.Done()
			for i := range each {
				errs <- r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
					system.Agents[0].Name = fmt.Sprintf("name %d-%d", w, i)
					return nil
				})
			}
		}()
	}
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 3 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				records, _, err := r.agentRecords(bg)
				if err != nil {
					errs <- err
					return
				}
				a := records[0]
				if a.LatestDeployment == nil || !hasDeployment(a.Deployments, a.LatestDeployment.DeploymentID) {
					errs <- fmt.Errorf("inconsistent record: latest %+v", a.LatestDeployment)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	readers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	end := must(r.Head(bg))(t)
	system := must(r.System(bg))(t)
	if got := end.Generation - start.Generation; got != writers*each {
		t.Fatalf("configuration generation advanced %d times for %d configuration commits", got, writers*each)
	}
	if got := system.Revision - startRevision; got != writers*each {
		t.Fatalf("system revision advanced %d for %d configuration commits", got, writers*each)
	}
	if got := end.State - start.State; got != writers*each {
		t.Fatalf("state generation advanced %d for %d state commits", got, writers*each)
	}
	if configs != writers*each || states != writers*each {
		t.Fatalf("observers: config %d state %d", configs, states)
	}
	if n := system.Agents[0].Deployments.Len(); n != writers*each+1 {
		t.Fatalf("%d deployments", n)
	}
}

func hasDeployment(m model.OrderedMap[model.Deployment], id string) bool {
	_, ok := m.Get(id)
	return ok
}
