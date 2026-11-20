package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"weak"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func resetJobRuntime(t *testing.T) {
	t.Helper()
	options := lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := lifecycle.Configure(options)
		if err == nil {
			return
		}
		// A worker releases its busy key before Runtime finishes its scope
		// cleanup. Wait for that tracked tail, rather than replacing live work.
		if !errors.Is(err, lifecycle.ErrRunning) || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
}

func jobFixture(t *testing.T) context.Context {
	t.Helper()
	// Async jobs use DefaultSpawner. Give each fixture an empty Runtime as
	// well as fresh metadata; a preceding shutdown must stay local to its test.
	resetJobRuntime(t)
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	jobState.Lock()
	old := jobState.jobs
	jobState.jobs = nil
	jobState.Unlock()
	t.Cleanup(func() {
		defer func() { jobState.Lock(); jobState.jobs = old; jobState.Unlock() }()
		lifecycle.Stop(time.Second)
		// Leave the process default idle and admitting, just like a fresh
		// process, also for tests that use it without opening a job fixture.
		resetJobRuntime(t)
	})
	return repo.WithRepository(context.Background(), r)
}

func TestJobFixtureStartsFreshRuntimeAfterShutdown(t *testing.T) {
	var stopped context.Context
	t.Run("previous owner", func(t *testing.T) {
		resetJobRuntime(t)
		lifecycle.Start(context.Background())
		stopped = lifecycle.BindRequest(context.Background())
		lifecycle.Stop(time.Second)
	})
	t.Run("next fixture", func(t *testing.T) {
		ctx := jobFixture(t)
		j, _, err := startJob(ctx, "default", "probe", false, func(context.Context, func(int, int)) (outcome, error) { return outcome{}, nil })
		if err != nil {
			t.Fatalf("fresh fixture inherited stopped Runtime: %v", err)
		}
		if saved := awaitJob(t, j.ID, "default"); saved.Status != "done" {
			t.Fatal(saved)
		}
		if _, _, err := startJob(stopped, "default", "probe", false, func(context.Context, func(int, int)) (outcome, error) {
			t.Error("old generation was revived")
			return outcome{}, nil
		}); !errors.Is(err, context.Canceled) {
			t.Fatalf("old generation lost shutdown cancellation: %v", err)
		}
	})
}

func awaitJob(t *testing.T, id, profile string) *Job {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); {
		j := GetJob(id, profile)
		if j != nil && j.Status != "running" {
			return j
		}
		runtime.Gosched()
	}
	t.Fatal("job did not finish")
	return nil
}
func TestJobOutlivesCancelledRequestAndDoesNotRetainScope(t *testing.T) {
	ctx := jobFixture(t)
	type marker struct{ bytes [1 << 20]byte }
	type key struct{}
	started, release := make(chan struct{}), make(chan struct{})
	var ref weak.Pointer[marker]
	var original *repo.Scope
	j := func() *Job {
		payload := new(marker)
		ref = weak.Make(payload)
		request, cancel := context.WithCancel(context.WithValue(repo.WithScope(ctx), key{}, payload))
		original = repo.ScopeOf(request)
		cancel()
		j, _, err := startJob(request, "default", "probe", false, func(worker context.Context, p func(int, int)) (outcome, error) {
			if worker.Err() != nil || worker.Value(key{}) != nil || repo.ScopeOf(worker) == original {
				t.Error("worker inherited the request cancellation or scope")
			}
			p(0, 1)
			close(started)
			<-release
			_, err := SaveWebsiteNote(worker, "default", json.RawMessage(`{"domain":"example.com","note":"completed"}`))
			p(1, 1)
			return outcome{Results: map[string]Probe{"example.com": {Host: "example.com"}}}, err
		})
		if err != nil {
			t.Fatal(err)
		}
		return j
	}()
	<-started
	for i := 0; i < 5 && ref.Value() != nil; i++ {
		runtime.GC()
	}
	if ref.Value() != nil {
		t.Error("running worker retained the request values")
	}
	_, _, err := startJob(ctx, "other", "region", false, func(context.Context, func(int, int)) (outcome, error) { return outcome{}, nil })
	var busy *Busy
	if !errors.As(err, &busy) || busy.JobID != j.ID {
		t.Fatalf("exclusive job: %v", err)
	}
	if payload := BusyPayload(busy, "other"); payload.Job != nil || payload.Message != msgOtherBusy {
		t.Fatalf("cross-profile job disclosed: %v", payload)
	}
	close(release)
	saved := awaitJob(t, j.ID, "default")
	if saved.Status != "done" || saved.Done != 1 || saved.Total != 1 || saved.FinishedAt == nil {
		t.Fatalf("metadata: %+v", saved)
	}
	c, err := loadConfig(ctx, "default")
	if err != nil || c.Settings.WebsiteNotes["example.com"] != "completed" {
		t.Fatalf("cancelled write was dropped: %v", err)
	}
	b, _ := json.Marshal(saved)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	if _, ok := fields["results"]; ok {
		t.Fatal("job retained result payloads")
	}
}
func TestJobFailureAndResultLifetime(t *testing.T) {
	for _, tt := range []struct {
		name    string
		err     error
		message string
	}{{"user", &ProbeError{"synthetic failure"}, "synthetic failure"}, {"internal", errors.New("synthetic internal failure"), msgFailed}, {"panic", nil, msgFailed}} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := jobFixture(t)
			j, _, err := startJob(ctx, "default", "probe", true, func(context.Context, func(int, int)) (outcome, error) {
				if tt.name == "panic" {
					panic("synthetic")
				}
				return outcome{}, tt.err
			})
			var pe *ProbeError
			if !errors.As(err, &pe) || pe.Message != tt.message || j.Status != "failed" || j.FinishedAt == nil {
				t.Fatalf("failure: %+v %v", j, err)
			}
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatal("job lost its cause", err)
			}
			if tt.name == "panic" && !errors.Is(err, lifecycle.ErrPanic) {
				t.Fatal("panic sentinel lost", err)
			}

		})
	}
	ctx := jobFixture(t)
	var ref weak.Pointer[PathStats]
	func() {
		j, results, err := startJob(ctx, "default", "probe", true, func(_ context.Context, p func(int, int)) (outcome, error) {
			stats := &PathStats{Delays: make([]float64, 1<<18)}
			ref = weak.Make(stats)
			p(1, 1)
			return outcome{Results: map[string]Probe{"example.com": {Direct: stats}}, Errors: []JobError{{Message: "partial"}}}, nil
		})
		if err != nil || len(results) != 1 || j.Errors[0].Message != "partial" {
			t.Fatalf("synchronous result: %v", err)
		}
	}()
	for i := 0; i < 5 && ref.Value() != nil; i++ {
		runtime.GC()
	}
	if ref.Value() != nil {
		t.Fatal("finished history retained probe results")
	}
}
func TestJobHistoryBoundAndImmutableSnapshots(t *testing.T) {
	ctx := jobFixture(t)
	for i := 0; i < 45; i++ {
		j, _, err := startJob(ctx, "default", "probe", true, func(_ context.Context, p func(int, int)) (outcome, error) {
			p(1, 1)
			return outcome{Errors: []JobError{{Message: "partial"}}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		j.Errors[0].Message = "modified"
		if GetJob(j.ID, "default").Errors[0].Message != "partial" {
			t.Fatal("caller mutated stored metadata")
		}
	}
	jobState.Lock()
	n := len(jobState.jobs)
	jobState.Unlock()
	if n > 21 {
		t.Fatalf("unbounded history: %d", n)
	}
}
func TestAutomaticRoundPrerequisitesAndTaskReceipts(t *testing.T) {
	ctx := jobFixture(t)
	if out, err := RunAutoTask(ctx, "default", "schedule"); err != nil || out != nil {
		t.Fatalf("disabled: %v %v", out, err)
	}
	if err := editSettings(ctx, "default", func(s *Settings) error { s.AutoProbe = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := RunAutoTask(ctx, "default", "manual"); err != nil {
		t.Fatal(err)
	}
	r, _ := repo.GetRepository(ctx)
	b, err := osReadTask(r.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	entry := b["domain_discovery:default"]
	if fmt.Sprint(entry["last_status"]) != "skipped" || entry["last_error"] != msgAutoPath || entry["last_trigger"] != "manual" {
		t.Fatalf("receipt: %v", entry)
	}
}

func osReadTask(dir string) (map[string]map[string]any, error) {
	b, err := os.ReadFile(filepath.Join(dir, "task-status.json"))
	if err != nil {
		return nil, err
	}
	var out map[string]map[string]any
	err = json.Unmarshal(b, &out)
	return out, err
}

type autoClient struct {
	calls int
	empty bool
}

func (f *autoClient) Probe(_ context.Context, _ Agent, p ProbeRequest) ([]ProbeRow, error) {
	f.calls++
	if f.empty {
		return []ProbeRow{}, nil
	}
	rows := []ProbeRow{}
	for i, t := range p.Targets {
		direct := 3
		if i >= 2 && t.Host == "www.example.com" {
			direct = 0
		}
		rows = append(rows, ProbeRow{Host: t.Host, Paths: map[string]*PathStats{"DIRECT": {OK: direct, Total: 3, Delays: []float64{100, 100, 100}, Errors: map[string]int{}}, "PROXY": {OK: 3, Total: 3, Delays: []float64{100, 100, 100}, Errors: map[string]int{}}}})
	}
	return rows, nil
}
func (*autoClient) Targets(context.Context, Agent) ([]AvailableTarget, error) { return nil, nil }
func (*autoClient) Region(context.Context, Agent, RegionRequest) ([]RegionResult, error) {
	return nil, nil
}
func TestAutomaticProbeAdoptionRecheckAndNoRepeatedWrites(t *testing.T) {
	ctx := jobFixture(t)
	r, _ := repo.GetRepository(ctx)
	repo.SetRepository(r)
	t.Cleanup(func() { repo.SetRepository(nil) })
	fake := &autoClient{}
	previous := client
	client = fake
	t.Cleanup(func() { client = previous })
	var err error
	if _, err = r.UpdateProfile(ctx, "default", func(_ context.Context, profile *model.Profile) error {
		profile.ProxyGroups = []model.ProxyGroup{{ID: "g1", Name: "PROXY", Type: "select"}}
		profile.RuleConfigs = []model.RuleConfig{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{{ID: "a1", Name: "Synthetic", ProfileID: "default", ServiceType: "mihomo", DomainDiscoveryEnabled: ptr(true), Version: minRegionVersion}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = InitRulesets(ctx, "default", nil, "PROXY"); err != nil {
		t.Fatal(err)
	}
	// Write a synthetic collector document: the route cannot create passive history.
	data := fmt.Sprintf(`{"status":{"value":"running"},"days":{%q:{"hosts":{"www.example.com":{"first_seen":"2026-11-01T00:00:00","last_seen":"2026-11-01T00:00:00","ports":{"443":1},"by_route":{"Match\t\tdirect\tDIRECT":{"conns":0,"fails":5}}},"fine.example.net":{"first_seen":"2026-11-01T00:00:00","last_seen":"2026-11-01T00:00:00","by_route":{"Match\t\tdirect\tDIRECT":{"conns":1,"fails":0}}}}}}}`, now().Format("2006-01-02"))
	dir := filepath.Join(r.DataDir(), "traffic")
	if err = os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "a1.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = editSettings(ctx, "default", func(s *Settings) error { s.AutoProbe = true; s.AutoApply = true; return nil }); err != nil {
		t.Fatal(err)
	}
	out, err := RunAutoTask(ctx, "default", "schedule")
	if err != nil || out == nil || out.Probed != 2 || out.Applied != 1 {
		t.Fatalf("automatic adoption: %+v %v", out, err)
	}
	c, err := loadConfig(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Settings.History) != 1 || c.Settings.History[0].Source != "auto" || *c.Settings.History[0].Confidence != 99 {
		t.Fatalf("history: %+v", c.Settings.History)
	}
	before := fake.calls
	out, err = RunAutoTask(ctx, "default", "schedule")
	if err != nil || out.Probed != 0 || fake.calls != before {
		t.Fatalf("recent probes repeated: %+v %v", out, err)
	}
	old := now().AddDate(0, 0, -40).Format("2006-01-02T15:04:05")
	if err = editSettings(ctx, "default", func(s *Settings) error { s.History[0].AppliedAt = old; return nil }); err != nil {
		t.Fatal(err)
	}
	fake.empty = true
	out, err = RunAutoTask(ctx, "default", "schedule")
	if err != nil || out.Rechecked != 1 {
		t.Fatalf("recheck: %+v %v", out, err)
	}
	c, _ = loadConfig(ctx, "default")
	if c.Settings.History[0].Recheck == nil || c.Settings.History[0].Recheck.Reason != msgRecheckEmpty {
		t.Fatalf("missing-result recheck: %+v", c.Settings.History)
	}
	revision := r.CommitCount()
	out, err = RunAutoTask(ctx, "default", "schedule")
	if err != nil || out.Rechecked != 0 || r.CommitCount() != revision {
		t.Fatalf("no-result recheck repeatedly rewrote config: %+v %v", out, err)
	}
}
