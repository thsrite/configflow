package discovery

import (
	"configflow/internal/instance"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/tasks"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"weak"
)

func collectGarbage() {
	for range 5 {
		runtime.GC()
		runtime.Gosched()
	}
}

func TestReviewNullNoteCannotDeleteExistingAnnotation(t *testing.T) {
	svc, ctx := jobFixture(t)
	if _, err := svc.SaveWebsiteNote(ctx, "default", json.RawMessage(`{"domain":"example.com","note":"keep"}`)); err != nil {
		t.Fatal(err)
	}
	_, err := svc.SaveWebsiteNote(ctx, "default", json.RawMessage(`{"domain":"example.com","note":null}`))
	if err == nil || err.Error() != msgNote {
		t.Errorf("null note accepted: %v", err)
	}
	cfg, err := svc.loadConfig(ctx, "default")
	if err != nil || cfg.Settings.WebsiteNotes["example.com"] != "keep" {
		t.Fatalf("annotation deleted: %v", err)
	}
}

func TestReviewExplicitNullApplyTypeIsRejected(t *testing.T) {
	_, err := ParseApplyItems(json.RawMessage(`[{"value":"example.com","rule_type":null,"target":"direct"}]`))
	if err == nil || err.Error() != msgItem {
		t.Fatalf("null rule type accepted: %v", err)
	}
}

func TestReviewDiscardedJobsAreActuallyCollected(t *testing.T) {
	svc, ctx := jobFixture(t)
	refs := []weak.Pointer[Job]{}
	for range 80 {
		if _, _, err := svc.startJob(ctx, "default", "probe", true, func(context.Context, func(int, int)) (outcome, error) { return outcome{}, nil }); err != nil {
			t.Fatal(err)
		}
		svc.jobMu.Lock()
		refs = append(refs, weak.Make(svc.jobs[len(svc.jobs)-1]))
		svc.jobMu.Unlock()
	}
	collectGarbage()
	for i, ref := range refs[:len(refs)-21] {
		if ref.Value() != nil {
			t.Errorf("discarded metadata %d remains reachable", i)
		}
	}
	svc.jobMu.Lock()
	n := len(svc.jobs)
	svc.jobMu.Unlock()
	if n != 21 {
		t.Fatalf("retained %d jobs", n)
	}
}

func TestReviewLargeAgentSummariesDoNotSurviveRequests(t *testing.T) {
	for round := range 8 {
		domain, host := func() (weak.Pointer[Domain], weak.Pointer[Host]) {
			reports := []struct {
				agent Agent
				data  traffic
			}{}
			day := now().Format("2006-01-02")
			for a := range 80 {
				var bucket trafficDay
				for h := range 150 {
					name := fmt.Sprintf("h%d.a%d.example.com", h, a)
					host := trafficHost{First: day, Last: day, Routes: trafficRows[Stats]{{"Match\t\tdirect\tDIRECT", Stats{Conns: 1}}}}
					bucket.Hosts = append(bucket.Hosts, trafficRow[trafficHost]{name, host})
				}
				reports = append(reports, struct {
					agent Agent
					data  traffic
				}{Agent{ID: fmt.Sprint(a)}, traffic{Days: trafficRows[trafficDay]{{day, bucket}}}})
			}
			result := summarize(reports, 7, "all", "", nil, nil)
			if len(result.Items) != 1 || len(result.Items[0].Hosts) != 12000 {
				t.Fatalf("large round %d summary incomplete", round)
			}
			return weak.Make(result.Items[0]), weak.Make(result.Items[0].Hosts[0])
		}()
		collectGarbage()
		if domain.Value() != nil || host.Value() != nil {
			t.Fatalf("round %d retained completed large traffic response", round)
		}
	}
}

func TestReviewJobExclusivityAndCancellationStress(t *testing.T) {
	svc, ctx := jobFixture(t)
	for round := range 60 {
		release := make(chan struct{})
		request, cancel := context.WithCancel(ctx)
		j, _, err := svc.startJob(request, "default", "probe", false, func(worker context.Context, progress func(int, int)) (outcome, error) {
			<-release
			if worker.Err() != nil {
				return outcome{}, worker.Err()
			}
			progress(1, 1)
			return outcome{}, nil
		})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = svc.startJob(ctx, "other", "region", true, func(context.Context, func(int, int)) (outcome, error) {
			t.Error("competing worker started")
			return outcome{}, nil
		})
		if _, ok := err.(*Busy); !ok {
			t.Fatalf("round %d lost exclusivity: %v", round, err)
		}
		close(release)
		if done := awaitJob(t, svc, j.ID, "default"); done.Status != "done" {
			t.Fatalf("cancelled request stopped worker: %+v", done)
		}
	}

}

func TestReviewTypedSettingsDefaultsAndLegacyNumber(t *testing.T) {
	for _, raw := range []string{`{"auto_apply_min_confidence":80.0}`, `{"auto_probe":1,"auto_apply":[],"region_check_enabled":{},"regional_rulesets":[],"website_notes":false}`, `{"region_probe_port":"bad"}`, `{"ignored":[null,3,"example.com"],"history":[null,"invalid",{}]}`} {
		t.Run(strings.ReplaceAll(raw, "/", "_"), func(t *testing.T) {
			var settings Settings
			if err := json.Unmarshal([]byte(raw), &settings); err != nil {
				t.Fatalf("read-time defaults rejected persisted settings: %v", err)
			}
			settings.normalize()
			// O18 T1c: auto_probe 1 keeps its truth value (Python truthiness);
			// auto_apply is switch:"off"; [] and {} are off; wrong-typed maps are empty.
			if strings.Contains(raw, "auto_probe") && (!settings.AutoProbe || settings.AutoApply || settings.RegionEnabled || len(settings.RegionalRulesets) != 0 || len(settings.WebsiteNotes) != 0) {
				t.Fatalf("lenient stored switch defaults changed: %+v", settings)
			}
			if strings.Contains(raw, "80.0") && settings.Confidence != 80 {
				t.Fatal("lost supported confidence")
			}
			if strings.Contains(raw, "bad") && settings.RegionPort != 17999 {
				t.Fatal("invalid persisted port did not default")
			}
			if strings.Contains(raw, "ignored") && (len(settings.Ignored) != 1 || len(settings.History) != 1) {
				t.Fatal("invalid list members not filtered")
			}
		})
	}
}

func TestReviewProductionMatcherLimitsAndRelease(t *testing.T) {
	opts := New(Options{}).matcherOptions(context.Background())
	if opts.MaxMatchers != 4 || opts.MaxHostResults != 1024 || opts.IdleSeconds != 300 {
		t.Fatalf("unexpected production limits: %+v", opts)
	}
	dir := t.TempDir()
	opts.RulesDir = func() string { return dir }
	opts.StoredLibrary = nil
	clock := lifecycletest.NewClock(time.Unix(0, 0))
	opts.Clock = clock
	opts.Spawner = nil // This test drives eviction explicitly for deterministic GC.
	bounded := rules.NewCache(opts)
	t.Cleanup(bounded.Clear)
	enabled := true
	policy := "DIRECT"
	config := rules.MatcherInput{Rules: []model.RuleConfig{{ID: "rule", ItemType: "rule", RuleType: "DOMAIN-SUFFIX", Value: new("example.com"), Policy: &policy, Enabled: &enabled}}}
	refs := []weak.Pointer[rules.Cached]{}
	for profile := range 60 {
		func() {
			matcher := bounded.LocalMatcher(config, fmt.Sprintf("profile-%d", profile))
			refs = append(refs, weak.Make(matcher))
			for host := range 1400 {
				matcher.Match(fmt.Sprintf("h%d.example.com", host))
			}
			if len(matcher.HostResultKeys()) != 1024 {
				t.Fatal("host result cap not enforced")
			}
		}()
		if bounded.Size() > 4 {
			t.Fatal("matcher registry grew beyond four")
		}
	}
	collectGarbage()
	for _, ref := range refs[:len(refs)-4] {
		if ref.Value() != nil {
			t.Fatal("evicted matcher still retained")
		}
	}
	clock.Advance(299 * time.Second)
	bounded.EvictIdle(299)
	if bounded.Size() != 4 {
		t.Fatal("matchers expired before five minutes")
	}
	clock.Advance(time.Second)
	bounded.EvictIdle(300)
	if bounded.Size() != 0 {
		t.Fatal("idle matchers remained at five-minute deadline")
	}
	collectGarbage()
	for _, ref := range refs {
		if ref.Value() != nil {
			t.Fatal("idle eviction retained indexes or host results")
		}
	}
}

type reviewBlockedClient struct {
	autoClient
	started chan struct{}
	release chan struct{}
	seen    chan context.Context
}

func (f *reviewBlockedClient) Probe(ctx context.Context, a Agent, p ProbeRequest) ([]ProbeRow, error) {
	if f.seen != nil {
		f.seen <- ctx
	}
	close(f.started)
	<-f.release
	return f.autoClient.Probe(ctx, a, p)
}

func TestReviewManualAutoTaskKeepsWorkValuesWithFreshScope(t *testing.T) {
	svc, ctx := jobFixture(t)
	r := svc.Repo
	rt := svc.Runtime
	var err error
	if _, err = r.UpdateProfile(ctx, "default", func(_ context.Context, profile *model.Profile) error {
		profile.ProxyGroups = []model.ProxyGroup{{ID: "g", Name: "PROXY", Type: "select"}}
		profile.RuleConfigs = []model.RuleConfig{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{{ID: "a1", Name: "Synthetic", ProfileID: "default", ServiceType: "mihomo", DomainDiscoveryEnabled: new(true), Version: minRegionVersion}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.InitRulesets(ctx, "default", nil, "PROXY"); err != nil {
		t.Fatal(err)
	}
	if err = svc.editSettings(ctx, "default", func(s *Settings) error { s.AutoProbe = true; return nil }); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(r.DataDir(), "traffic")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf(`{"status":{"value":"running"},"days":{%q:{"hosts":{"www.example.com":{"first_seen":"2026-11-01T00:00:00","last_seen":"2026-11-01T00:00:00","by_route":{"Match\t\tdirect\tDIRECT":{"conns":1}}}}}}}`, now().Format("2006-01-02"))
	if err = os.WriteFile(filepath.Join(dir, "a1.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	fake := &reviewBlockedClient{started: make(chan struct{}), release: make(chan struct{}), seen: make(chan context.Context, 1)}
	svc.client = fake
	tk := tasks.NewRegistry(r, rt)
	svc.registerTasks(tk)
	type key struct{}
	request, cancel := context.WithCancel(rt.BindRequest(context.WithValue(repo.WithScope(ctx), key{}, "kept-work-value")))
	original := repo.ScopeOf(request)
	if err := tk.Runner(tasks.SlotDomains)(request, "domain_discovery:default", "default"); err != nil {
		t.Fatal(err)
	}
	cancel()

	// Always drain before restoring global collaborators, including failed GC assertions.
	defer func() {
		close(fake.release)
		for end := time.Now().Add(3 * time.Second); tasks.IsBackground("domain_discovery:default") && time.Now().Before(end); {
			runtime.Gosched()
		}
		if tasks.IsBackground("domain_discovery:default") {
			t.Error("manual task did not stop")
		}
	}()
	select {
	case <-fake.started:
	case <-time.After(3 * time.Second):
		t.Fatal("manual auto task did not start")
	}
	worker := <-fake.seen
	if worker.Value(key{}) != "kept-work-value" || worker.Err() != nil || repo.ScopeOf(worker) == original {
		t.Fatal("work lost its lifetime/values or retained the request repository scope")
	}
	rt.Shutdown(context.Background())
	if worker.Err() == nil {
		t.Fatal("manual task lost its Runtime shutdown lifetime")
	}
	rt.Start(context.Background())
	if worker.Err() == nil {
		t.Fatal("old task moved to the successor generation")
	}

}

func TestReviewRegionTargetOrdering(t *testing.T) {
	targets := []RegionTarget{{Name: "Z-PREFERRED", Kind: "group"}, {Name: "DIRECT", Kind: "direct"}, {Name: "A-OTHER", Kind: "group"}}
	raw := map[string][]RegionResult{}
	for _, target := range targets {
		var result RegionResult
		if err := json.Unmarshal([]byte(`{"id":"domain-0","status":200,"final_url":"https://example.com/"}`), &result); err != nil {
			t.Fatal(err)
		}
		raw[target.Name] = []RegionResult{result}
	}
	out, err := evaluateRegion(targets, raw, []ProbeTarget{{Host: "example.com", URL: "https://example.com/"}}, false, nil, timestamp())
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Usable []string `json:"usable"`
	}
	if err = json.Unmarshal(out.Domains["example.com"], &result); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Usable, []string{"Z-PREFERRED", "DIRECT", "A-OTHER"}) {
		t.Fatalf("target priority reordered: %v", result.Usable)
	}
}

func TestReviewOverallFailureDoesNotExposePartialErrors(t *testing.T) {
	svc, ctx := jobFixture(t)
	j, _, err := svc.startJob(ctx, "default", "probe", true, func(context.Context, func(int, int)) (outcome, error) {
		return outcome{Errors: []JobError{{Message: "partial"}}}, fmt.Errorf("synthetic final failure")
	})
	if err == nil || j.Status != "failed" || len(j.Errors) != 0 {
		t.Fatalf("failed job retained partial outcome: %+v %v", j, err)
	}
}

func TestReviewJobSnapshotDoesNotShareCompletionPointer(t *testing.T) {
	svc, ctx := jobFixture(t)
	returned, _, err := svc.startJob(ctx, "default", "probe", true, func(context.Context, func(int, int)) (outcome, error) { return outcome{}, nil })
	if err != nil || returned.FinishedAt == nil {
		t.Fatalf("job failed: %v", err)
	}
	original := *returned.FinishedAt
	*returned.FinishedAt = "caller modification"
	if got := svc.GetJob(returned.ID, "default"); got.FinishedAt == nil || *got.FinishedAt != original {
		t.Fatal("caller mutated stored completion time")
	}
}

func TestReviewAutoLoopRunsUnderInstanceLock(t *testing.T) {
	svc, ctx := jobFixture(t)
	if err := svc.editSettings(ctx, "default", func(s *Settings) error { s.AutoProbe = true; return nil }); err != nil {
		t.Fatal(err)
	}
	r := svc.Repo
	owner, err := instance.Acquire(ctx, r.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Release()
	clock := lifecycletest.NewClock(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	reg := lifecycle.NewRegistry()
	spec := svc.autoPeriodic()
	original := spec.Round
	rounds := make(chan struct{}, 3)
	spec.Round = func(ctx context.Context) error {
		err := original(ctx)
		clock.Advance(7 * time.Second)
		rounds <- struct{}{}
		return err
	}
	reg.RegisterPeriodic(spec)
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: reg, Clock: clock})
	runner.Start(ctx)
	defer runner.Shutdown(context.Background())
	for _, delay := range []time.Duration{120 * time.Second, 600 * time.Second} {
		deadline := time.Now().Add(5 * time.Second)
		for clock.Pending() != 1 {
			if time.Now().After(deadline) {
				t.Fatal("no timer after round")
			}
			time.Sleep(time.Millisecond)
		}
		clock.Advance(delay - time.Second)
		select {
		case <-rounds:
			t.Fatal("early round")
		default:
		}
		clock.Advance(time.Second)
		select {
		case <-rounds:
		case <-time.After(5 * time.Second):
			t.Fatal("round skipped under instance lock")
		}
		if err := runner.WaitIdle(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	receipts, err := osReadTask(r.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	entry := receipts["domain_discovery:default"]
	if entry["last_trigger"] != "schedule" || entry["last_status"] != "skipped" || entry["last_error"] != msgAutoPath {
		t.Fatal("automatic round did not record its prerequisite result", entry)
	}

}
