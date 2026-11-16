package discovery

import (
	"configflow/internal/rules"
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
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
	// Fresh process state starts without running metadata; completion is persisted by stores,
	// not restored as a phantom running job. Bounded history is independent of old job input size.
	jobState.Lock()
	jobState.jobs = nil
	jobState.Unlock()
	if runningJob("") != nil {
		t.Fatal("restart retained running job")
	}
}

func TestReviewTypedSettingsDefaultsAndLegacyNumber(t *testing.T) {
	for _, raw := range []string{`{"auto_apply_min_confidence":80.0}`, `{"region_probe_port":"bad"}`, `{"ignored":[null,3,"example.com"],"history":[null,"invalid",{}]}`} {
		t.Run(strings.ReplaceAll(raw, "/", "_"), func(t *testing.T) {
			var settings Settings
			if err := json.Unmarshal([]byte(raw), &settings); err != nil {
				t.Fatalf("read-time defaults rejected persisted settings: %v", err)
			}
			settings.normalize()
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
