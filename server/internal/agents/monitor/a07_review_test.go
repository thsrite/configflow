package monitor

// A07 review regressions, expectations from CPython 3.11 (testdata/gen_a07_review.py
// → testdata/a07_review.json).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"configflow/internal/instance"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/testjson"
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

// Original Python limit rows stay frozen; exact native bindings remove the
// interpreter digit cap while the public machine-version budget fails closed.
func TestReviewNewerReleaseDigitLimit(t *testing.T) {
	for _, raw := range loadA07Review(t).NewerRelease {
		var c []any
		json.Unmarshal(raw, &c)
		want := c[2].(map[string]any)
		got, err := newerRelease(c[0].(string), c[1].(string))
		if native, bound := nativeVersionExpected(t, "newer_release", raw); bound {
			if err != nil || got != native["result"] {
				t.Errorf("native release count: %v %v want %v", got, err, native)
			}
			continue
		}
		if exception, ok := want["exception"]; ok {
			if err == nil || !strings.HasPrefix(err.Error(), exception.(string)+":") {
				t.Errorf("newer_release(%.12q, %.12q): %v, %v; want %v", c[0], c[1], got, err, exception)
			}
		} else if err != nil || got != want["result"].(bool) {
			t.Errorf("newer_release(%.12q, %.12q): %v, %v; want %v", c[0], c[1], got, err, want["result"])
		}
	}
}

// A malformed machine version cannot fetch or overwrite a cached release.
// Original Python outcomes and side effects are retained in the bound fixture.
func TestReviewCoreUpdateDigitLimitFollowsPython(t *testing.T) {
	for _, raw := range loadA07Review(t).CoreUpdate {
		var c []any
		json.Unmarshal(raw, &c)
		current, cached, force, want := c[0].(string), c[1].(bool), c[2].(bool), c[3].(map[string]any)
		latest := installLatest(t)
		m, client := panelManager(t, panelAgentRecord("version", "1.8.60-go"))
		client.add(jsonResponse(map[string]any{"version": current}, 200))
		if cached {
			settings, _ := downloadSettings(context.Background())
			coreLatest["release"] = &coreCached{finishedAt: monotonic(), latest: "v1.2.3", settings: settings}
		}
		latest.add("v1.2.3\n", "v1.2.3\n")
		args := [][2]string{}
		if force {
			args = append(args, [2]string{"force", "1"})
		}
		before := coreCachedOf("release")
		result, err := panelCall(m, "core/update", "GET", args, nil)
		if native, bound := nativeVersionExpected(t, "core_update", raw); bound {
			wantVal, parseErr := testjson.Parse(mustMarshal(t, native))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if diff := diffRefVal(t, outcomeOf(result, err), wantVal); diff != "" {
				t.Errorf("native machine version: %s", diff)
			}
			if latest.count() != 0 || coreCachedOf("release") != before {
				t.Fatal("invalid machine version fetched or changed cached release")
			}
			continue
		}
		label := fmt.Sprintf("%s cached=%v force=%v", truncateText(current, 8), cached, force)
		if want["exception"] != nil {
			sameOutcome(t, label, reviewOutcome(err), want)
		} else if err != nil {
			t.Errorf("%s: %v", label, err)
		} else {
			data := asMap(toJSONMap(t, result)["data"])
			w := want["result"].(map[string]any)["data"].(map[string]any)
			if data["has_update"] != w["has_update"] || data["latest"] != w["latest"] || data["current"] != w["current"] ||
				data["release_url"] != w["release_url"] || data["error"] != w["error"] {
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
	requestRule = func(_ context.Context, u string, r rulefetch.DownloadRequest) (*rulefetch.Response, error) {
		latest.mu.Lock()
		hostURL = r.HostURL
		latest.calls = append(latest.calls, ruleCall{u, r})
		latest.mu.Unlock()
		return nil, errors.New("Invalid HTTP/HTTPS URL")
	}
	m, client := panelManager(t, panelAgentRecord("version", "1.8.60-go"))
	client.add(jsonResponse(map[string]any{"version": "v1.19.1"}, 200))
	result, err := CallPanel(WithHostURL(context.Background(), "http://panel.test:5000/"), m, "mihomo", "core/update", "GET", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if hostURL != "http://panel.test:5000/" || latest.count() != 1 {
		t.Fatalf("host_url %q, %d fetches", hostURL, latest.count())
	}
	if e := asStr(asMap(toJSONMap(t, result)["data"])["error"]); !strings.Contains(e, "version.txt 内容不是版本号") {
		t.Fatal(e)
	}
}

// oxidns_cache.call evaluates _agent_origin inside its try block before
// the token is encoded: a bad host wins over a non-latin-1 token, with the
// monitor's AGENT_UNREACHABLE message.
func TestReviewCacheOriginBeforeTokenEncoding(t *testing.T) {
	want := loadA07Review(t).CacheBadHost
	m, client := cacheManager(t, map[string][]any{"dns": {"host", "bad/host", "token", "令牌"}})
	_, err := CallCache(ctx, m, "dns", "GET", nil)
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
		field := parseJSONVal(t, c[0])
		var want map[string]any
		json.Unmarshal(c[1], &want)
		m, client := ruleManager(t, ruleAgent("oxidns_ha_publication", field))
		client.set(errors.New("no network"))
		_, _, err := CallRuleUpdate(ctx, m, "dns", "POST", nil)
		label := string(c[0])
		if nativeHARefusal(t, raw, nil) {
			// Go-native refusal of a damaged journal: since O18 T1b such a
			// journal reads as missing (accepted).
			continue
		}
		if strings.Contains(label, "null") {
			continue // accepted O18 T1b difference: a null member is a missing one
		}
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
