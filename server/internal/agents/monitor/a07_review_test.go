package monitor

// A07 review regressions, expectations from CPython 3.11 (testdata/gen_a07_review.py
// → testdata/a07_review.json).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"configflow/internal/instance"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

type a07Review struct {
	NewerRelease []json.RawMessage `json:"newer_release"`
	CoreUpdate   []json.RawMessage `json:"core_update"`
	CacheBadHost map[string]any    `json:"cache_bad_host_token"`
	RuleUpdateHA []json.RawMessage `json:"rule_update_ha"`
}

func loadA07Review(t *testing.T) a07Review {
	t.Helper()
	data, err := os.ReadFile("testdata/a07_review.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref a07Review
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

// reviewOutcome renders a Go result like the generator's outcome(): the
// MonitorError fields, the Python exception class, or nothing for success.
func reviewOutcome(err error) map[string]any {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return map[string]any{"error": e.Code, "status": float64(e.Status), "message": e.Message}
	}
	class, _, _ := strings.Cut(err.Error(), ":")
	return map[string]any{"exception": class}
}

func sameOutcome(t *testing.T, label string, got map[string]any, want map[string]any) {
	t.Helper()
	for _, key := range []string{"error", "status", "message", "exception"} {
		if want[key] != nil && got[key] != want[key] || want[key] == nil && got[key] != nil {
			t.Errorf("%s: got %v, want %v", label, got, want)
			return
		}
	}
}

// _newer_release converts the version parts with int(): a part of more than
// 4300 digits is a ValueError (the panel route answers 500), checked only
// when both versions match the release pattern.
func TestReviewNewerReleaseDigitLimit(t *testing.T) {
	for _, raw := range loadA07Review(t).NewerRelease {
		var c []any
		json.Unmarshal(raw, &c)
		want := c[2].(map[string]any)
		got, err := newerRelease(c[0].(string), c[1].(string))
		if exception, ok := want["exception"]; ok {
			if err == nil || !strings.HasPrefix(err.Error(), exception.(string)+":") {
				t.Errorf("newer_release(%.12q, %.12q): %v, %v; want %v", c[0], c[1], got, err, exception)
			}
		} else if err != nil || got != want["result"].(bool) {
			t.Errorf("newer_release(%.12q, %.12q): %v, %v; want %v", c[0], c[1], got, err, want["result"])
		}
	}
}

// check_core_update with a core version whose release part exceeds the
// digit limit: the error comes from _usable before a fetch (cached, not
// forced) or from the answer after the fetch was stored.
func TestReviewCoreUpdateDigitLimitFollowsPython(t *testing.T) {
	for _, raw := range loadA07Review(t).CoreUpdate {
		var c []any
		json.Unmarshal(raw, &c)
		current, cached, force, want := c[0].(string), c[1].(bool), c[2].(bool), c[3].(map[string]any)
		latest := installLatest(t)
		m, client := panelManager(t, panelAgentRecord("version", "1.8.60-go"))
		client.add(jsonResponse(jsonx.Obj("version", current), 200))
		if cached {
			settings, _ := downloadSettings(context.Background())
			coreLatest["release"] = &coreCached{finishedAt: monotonic(), latest: "v1.2.3", settings: settings}
		}
		latest.add("v1.2.3\n", "v1.2.3\n")
		args := [][2]string{}
		if force {
			args = append(args, [2]string{"force", "1"})
		}
		result, err := panelCall(m, "core/update", "GET", args, nil)
		label := pySlice(current, 8) + " cached=" + jsonx.Str(cached) + " force=" + jsonx.Str(force)
		if want["exception"] != nil {
			sameOutcome(t, label, reviewOutcome(err), want)
		} else if err != nil {
			t.Errorf("%s: %v", label, err)
		} else {
			data := result.GetObject("data")
			w := want["result"].(map[string]any)["data"].(map[string]any)
			if data.Value("has_update") != w["has_update"] || data.Value("latest") != w["latest"] || data.Value("current") != w["current"] ||
				data.Value("release_url") != w["release_url"] || data.Value("error") != w["error"] {
				t.Errorf("%s: %v, want %v", label, data, w)
			}
		}
		if float64(latest.count()) != want["fetched"].(float64) {
			t.Errorf("%s: fetched %d, want %v", label, latest.count(), want["fetched"])
		}
		if entry := coreCachedOf("release"); entry == nil || entry.latest != "v1.2.3" || entry.err != "" {
			t.Errorf("%s: cache %#v", label, entry)
		}
	}
}

// request_rule reads flask.request.host_url: the route passes it to the
// version.txt fetch (a GitHub mirror under the panel's own /api/ is then an
// internal request).
func TestReviewCoreUpdateFetchCarriesTheHostURL(t *testing.T) {
	latest := installLatest(t)
	var hostURL string
	requestRule = func(_ context.Context, u string, r rulefetch.Request) (*rulefetch.Response, error) {
		latest.mu.Lock()
		hostURL = r.HostURL
		latest.calls = append(latest.calls, ruleCall{u, r})
		latest.mu.Unlock()
		return nil, errors.New("Invalid HTTP/HTTPS URL")
	}
	m, client := panelManager(t, panelAgentRecord("version", "1.8.60-go"))
	client.add(jsonResponse(jsonx.Obj("version", "v1.19.1"), 200))
	result, err := PanelCall(WithHostURL(context.Background(), "http://panel.test:5000/"), m, "mihomo", "core/update", "GET", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if hostURL != "http://panel.test:5000/" || latest.count() != 1 {
		t.Fatalf("host_url %q, %d fetches", hostURL, latest.count())
	}
	if e := result.GetObject("data").GetString("error"); !strings.Contains(e, "version.txt 内容不是版本号") {
		t.Fatal(e)
	}
}

// oxidns_cache.call evaluates _agent_origin inside its try block before
// the token is encoded: a bad host wins over a non-latin-1 token, with the
// monitor's AGENT_UNREACHABLE message.
func TestReviewCacheOriginBeforeTokenEncoding(t *testing.T) {
	want := loadA07Review(t).CacheBadHost
	m, client := cacheManager(t, map[string][]any{"dns": {"host", "bad/host", "token", "令牌"}})
	_, err := CacheCall(ctx, m, "dns", "GET", nil)
	sameOutcome(t, "cache", reviewOutcome(err), want)
	if client.count() != 0 {
		t.Fatal("contacted")
	}
}

// oxidns_ha_publish.active on malformed publication records raises like
// Python (AttributeError / TypeError → 500); a hashable non-terminal status
// is busy; terminal, empty or falsy records let the update reach the Agent.
func TestReviewRuleUpdateHAActiveFollowsPython(t *testing.T) {
	for _, raw := range loadA07Review(t).RuleUpdateHA {
		var c []json.RawMessage
		json.Unmarshal(raw, &c)
		field, err := jsonx.Parse(c[0])
		if err != nil {
			t.Fatal(err)
		}
		var want map[string]any
		json.Unmarshal(c[1], &want)
		m, client := ruleManager(t, ruleAgent("oxidns_ha_publication", field))
		client.set(errors.New("no network"))
		_, _, err = RuleUpdateCall(ctx, m, "dns", "POST", nil)
		label := string(c[0])
		if want["exception"] == "RuntimeError" { // reached the Agent request
			if client.count() != 1 {
				t.Errorf("%s: %v, want the Agent request", label, err)
			}
			continue
		}
		sameOutcome(t, label, reviewOutcome(err), want)
		if client.count() != 0 {
			t.Errorf("%s: contacted", label)
		}
	}
}

func TestReviewRankingLoopRunsUnderInstanceLock(t *testing.T) {
	repository, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), repository)
	owner, err := instance.Acquire(ctx, repository.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Release()
	store := GetRankingStore(repository.DataDir())
	stamp := LastCompleteSlotEnd(nowSeconds()) - HistorySlot
	addSlot(t, store, "removed-agent", "records", stamp, testSlot(1, i64(0), map[string]*Counts{"domains": NewCounts("example.com.", 1)}))
	clock := lifecycletest.NewClock(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	reg := lifecycle.NewRegistry()
	spec := rankingPeriodic()
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
	for _, delay := range []time.Duration{60 * time.Second, 60 * time.Second} {
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
	fetched, err := store.Fetched("removed-agent", "records", []int64{stamp})
	if err != nil || fetched[stamp] {
		t.Fatal("ranking round did not remove retired Agent history under the instance lock", fetched, err)
	}

}
