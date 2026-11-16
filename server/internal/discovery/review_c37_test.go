package discovery

import (
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/tasks"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
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
	ctx := jobFixture(t)
	if _, err := SaveWebsiteNote(ctx, "default", json.RawMessage(`{"domain":"example.com","note":"keep"}`)); err != nil {
		t.Fatal(err)
	}
	_, err := SaveWebsiteNote(ctx, "default", json.RawMessage(`{"domain":"example.com","note":null}`))
	if err == nil || err.Error() != msgNote {
		t.Errorf("null note accepted: %v", err)
	}
	cfg, err := loadConfig(ctx, "default")
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
	ctx := jobFixture(t)
	refs := []weak.Pointer[Job]{}
	for range 80 {
		if _, _, err := startJob(ctx, "default", "probe", true, func(context.Context, func(int, int)) (outcome, error) { return outcome{}, nil }); err != nil {
			t.Fatal(err)
		}
		jobState.Lock()
		refs = append(refs, weak.Make(jobState.jobs[len(jobState.jobs)-1]))
		jobState.Unlock()
	}
	collectGarbage()
	for i, ref := range refs[:len(refs)-21] {
		if ref.Value() != nil {
			t.Errorf("discarded metadata %d remains reachable", i)
		}
	}
	jobState.Lock()
	n := len(jobState.jobs)
	jobState.Unlock()
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
				bucket := trafficDay{Hosts: map[string]trafficHost{}}
				for h := range 150 {
					name := fmt.Sprintf("h%d.a%d.example.com", h, a)
					bucket.hostOrder = append(bucket.hostOrder, name)
					bucket.Hosts[name] = trafficHost{First: day, Last: day, Routes: map[string]Stats{"Match\t\tdirect\tDIRECT": {Conns: 1}}, routeOrder: []string{"Match\t\tdirect\tDIRECT"}}
				}
				reports = append(reports, struct {
					agent Agent
					data  traffic
				}{Agent{ID: fmt.Sprint(a)}, traffic{Days: map[string]trafficDay{day: bucket}, dayOrder: []string{day}}})
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
	ctx := jobFixture(t)
	for round := range 60 {
		release := make(chan struct{})
		request, cancel := context.WithCancel(ctx)
		j, _, err := startJob(request, "default", "probe", false, func(worker context.Context, progress func(int, int)) (outcome, error) {
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
		_, _, err = startJob(ctx, "other", "region", true, func(context.Context, func(int, int)) (outcome, error) {
			t.Error("competing worker started")
			return outcome{}, nil
		})
		if _, ok := err.(*Busy); !ok {
			t.Fatalf("round %d lost exclusivity: %v", round, err)
		}
		close(release)
		if done := awaitJob(t, j.ID, "default"); done.Status != "done" {
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
			if strings.Contains(raw, "auto_probe") && (!settings.AutoProbe || settings.AutoApply || settings.RegionEnabled || len(settings.RegionalRulesets) != 0 || len(settings.WebsiteNotes) != 0) {
				t.Fatal("legacy defaults changed")
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
	opts := matcherOptions()
	if opts.MaxMatchers != 4 || opts.MaxHostResults != 1024 || opts.IdleSeconds != 300 {
		t.Fatalf("unexpected production limits: %+v", opts)
	}
	dir := t.TempDir()
	opts.RulesDir = func() string { return dir }
	opts.StoredLibrary = nil
	var clock atomic.Int64
	opts.Clock = func() float64 { return float64(clock.Load()) }
	bounded := rules.NewMatcherCache(opts)
	t.Cleanup(bounded.Clear)
	config, err := object(map[string]any{"rule_configs": []ruleConfig{{ID: "rule", ItemType: "rule", RuleType: "DOMAIN-SUFFIX", Value: "example.com", Policy: "DIRECT", Enabled: true}}, "rule_library": []libraryItem{}})
	if err != nil {
		t.Fatal(err)
	}
	refs := []weak.Pointer[rules.CachedMatcher]{}
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
	clock.Store(299)
	bounded.EvictIdle(299)
	if bounded.Size() != 4 {
		t.Fatal("matchers expired before five minutes")
	}
	clock.Store(300)
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
}

func (f *reviewBlockedClient) Probe(ctx context.Context, a Agent, p ProbeRequest) ([]ProbeRow, error) {
	close(f.started)
	<-f.release
	return f.autoClient.Probe(ctx, a, p)
}

func TestReviewManualAutoTaskDropsRequestValuesWhileRunning(t *testing.T) {
	ctx := jobFixture(t)
	r, _ := repo.GetRepository(ctx)
	repo.SetRepository(r)
	t.Cleanup(func() { repo.SetRepository(nil) })
	fields, err := object(map[string]any{"proxy_groups": []map[string]string{{"id": "g", "name": "PROXY", "type": "select"}}, "rule_configs": []ruleConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.UpdateProfileFields(ctx, "default", fields, nil); err != nil {
		t.Fatal(err)
	}
	system, err := object(map[string]any{"agents": []Agent{{ID: "a1", Name: "Synthetic", ProfileID: "default", ServiceType: "mihomo", Enabled: true, Version: minRegionVersion}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.SaveSystem(ctx, system, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = InitRulesets(ctx, "default", nil, "PROXY"); err != nil {
		t.Fatal(err)
	}
	if err = editSettings(ctx, "default", func(s *Settings) error { s.AutoProbe = true; return nil }); err != nil {
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
	fake := &reviewBlockedClient{started: make(chan struct{}), release: make(chan struct{})}
	previous := client
	client = fake
	t.Cleanup(func() { client = previous })
	wireTasks()
	type marker struct{ data [1 << 20]byte }
	type key struct{}
	var ref weak.Pointer[marker]
	func() {
		payload := new(marker)
		ref = weak.Make(payload)
		request, cancel := context.WithCancel(context.WithValue(repo.WithScope(ctx), key{}, payload))
		defer cancel()
		if err := tasks.Runner(tasks.SlotDomains)(request, "domain_discovery:default", "default"); err != nil {
			t.Fatal(err)
		}
	}()

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
	collectGarbage()
	if ref.Value() != nil {
		t.Error("manual automatic task retained cancelled request values")
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
	ctx := jobFixture(t)
	j, _, err := startJob(ctx, "default", "probe", true, func(context.Context, func(int, int)) (outcome, error) {
		return outcome{Errors: []JobError{{Message: "partial"}}}, fmt.Errorf("synthetic final failure")
	})
	if err == nil || j.Status != "failed" || len(j.Errors) != 0 {
		t.Fatalf("failed job retained partial outcome: %+v %v", j, err)
	}
}

func TestReviewJobSnapshotDoesNotShareCompletionPointer(t *testing.T) {
	ctx := jobFixture(t)
	returned, _, err := startJob(ctx, "default", "probe", true, func(context.Context, func(int, int)) (outcome, error) { return outcome{}, nil })
	if err != nil || returned.FinishedAt == nil {
		t.Fatalf("job failed: %v", err)
	}
	original := *returned.FinishedAt
	*returned.FinishedAt = "caller modification"
	if got := GetJob(returned.ID, "default"); got.FinishedAt == nil || *got.FinishedAt != original {
		t.Fatal("caller mutated stored completion time")
	}
}

func TestReviewAutoLoopCancellationReleasesExclusiveFileLock(t *testing.T) {
	ctx := jobFixture(t)
	r, _ := repo.GetRepository(ctx)
	running, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); autoLoop(running) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("auto loop leaked after cancellation")
		}
	}()
	path := filepath.Join(r.DataDir(), "probes", ".auto.lock")
	deadline := time.Now().Add(5 * time.Second)
	locked := false
	for time.Now().Before(deadline) {
		handle, err := os.OpenFile(path, os.O_WRONLY, 0600)
		if err == nil {
			err = syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				_ = syscall.Flock(int(handle.Fd()), syscall.LOCK_UN)
			} else if errors.Is(err, syscall.EWOULDBLOCK) {
				locked = true
			}
			handle.Close()
		}
		if locked {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !locked {
		t.Fatal("auto loop did not acquire its process lock")
	}
	second := make(chan struct{})
	go func() { defer close(second); autoLoop(running) }()
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("competing auto loop did not stop")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("first-delay cancellation was ignored")
	}
	handle, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if err = syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("shutdown retained auto lock: %v", err)
	}
	_ = syscall.Flock(int(handle.Fd()), syscall.LOCK_UN)
}
