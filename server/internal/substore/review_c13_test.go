package substore

// Review C13: reference results of the real Python modules for edge inputs
// (testdata/gen_review.py → testdata/review_reference.json, CPython 3.11 /
// requests 2.31 / PyYAML 6.0.1 on the test server): odd userinfo headers,
// YAML detection, proxies with non-string keys, charset parameters, the
// runtime on malformed state.json, reuse/cooldown clock edges and in-flight
// waiters with mixed flags.

import (
	"configflow/internal/yamlx"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

func reviewReference(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/review_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

// checkOutcome compares a Go result with a reference row ({output} or
// {error, message}); withMessage also compares the exception text.
func checkOutcome(t *testing.T, label string, row *jsonx.Object, got any, err error, withMessage bool) {
	t.Helper()
	if want, ok := row.Get("output"); ok {
		if err != nil {
			t.Errorf("%s: Go error %v, Python %s", label, err, jsonx.Repr(want))
		} else if !jsonx.Equal(got, want) {
			t.Errorf("%s: Go %s, Python %s", label, jsonx.Repr(got), jsonx.Repr(want))
		}
		return
	}
	if err == nil {
		t.Errorf("%s: Go %s, Python %s: %s", label, jsonx.Repr(got), row.GetString("error"), row.GetString("message"))
		return
	}
	if class := pythonClass(err); class != row.GetString("error") {
		t.Errorf("%s: Go %s (%v), Python %s", label, class, err, row.GetString("error"))
	}
	if withMessage && err.Error() != row.GetString("message") {
		t.Errorf("%s: message %q, Python %q", label, err.Error(), row.GetString("message"))
	}
}

func TestReviewParseUserinfo(t *testing.T) {
	for _, item := range reviewReference(t).GetList("parse_userinfo") {
		row := item.(*jsonx.Object)
		info, err := ParseUserinfo(row.GetString("input"))
		var got any
		if info != nil {
			got = info
		}
		checkOutcome(t, "parse_userinfo "+jsonx.Repr(row.Value("input")), row, got, err, true)
	}
}

func TestReviewIsYAMLResponse(t *testing.T) {
	oracle := nativeReaderCases(t)
	for i, item := range reviewReference(t).GetList("is_yaml_response") {
		row := item.(*jsonx.Object)
		assertNativeReaderCase(t, oracle, fmt.Sprintf("substore/review/is_yaml_response/%d", i), isYAMLResponse(row.GetString("input")), nil)
	}
}

// TestReviewParseProxiesFromYAML covers coordinator decision 10: mappings
// with int/bool/None keys load as in Python (keys read as json.dumps writes
// them, the form the subscription cache keeps).
func TestReviewParseProxiesFromYAML(t *testing.T) {
	ref := reviewReference(t)
	oracle := nativeReaderCases(t)
	for _, fn := range []string{"parse_proxies_from_yaml", "proxies_to_nodes"} {
		for i, item := range ref.GetList(fn) {
			row := item.(*jsonx.Object)
			text := row.GetString("input")
			label := fn + " " + jsonx.Repr(text)
			proxies, err := ParseProxiesFromYAML(text)
			var got any = proxies
			if err == nil && fn == "proxies_to_nodes" {
				got, err = ProxiesToNodes(proxies)
			}
			if row.Has("unserializable") {
				// Python keeps a date key until json.dumps refuses it when
				// the cache is written; Go refuses it here (TypeError).
				if err == nil || !errors.Is(err, yamlx.ErrType) {
					t.Errorf("%s: Go %s %v, want a TypeError", label, jsonx.Repr(got), err)
				}
				continue
			}
			assertNativeReaderCase(t, oracle, fmt.Sprintf("substore/review/%s/%d", fn, i), got, err)
		}
	}
}

func TestReviewResponseText(t *testing.T) {
	for _, item := range reviewReference(t).GetList("response_text") {
		row := item.(*jsonx.Object)
		in := row.GetList("input")
		resp := &Response{Header: http.Header{"Content-Type": {latin1Encode(in[0].(string))}}, Body: bytesOf(in[1].([]any))}
		checkOutcome(t, "response_text "+jsonx.Repr(in[0]), row, resp.Text(), nil, false)
	}
}

// latin1Encode is the header bytes http.client decodes as latin-1.
func latin1Encode(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r))
	}
	return string(b)
}

// TestReviewRuntimeMalformedState: a state.json that is not an object (or
// holds a last_used Python cannot subtract) fails where Python fails
// (TypeError in _write, AttributeError at state.get, TypeError/OverflowError
// at now - last_used) instead of being treated as empty.
func TestReviewRuntimeMalformedState(t *testing.T) {
	for _, item := range reviewReference(t).GetList("runtime") {
		row := item.(*jsonx.Object)
		state, action, running := row.GetString("state"), row.GetString("action"), row.GetBool("running")
		base := t.TempDir()
		builtin := filepath.Join(base, "opt")
		os.MkdirAll(builtin, 0o755)
		os.WriteFile(filepath.Join(builtin, SubStoreAsset), []byte("x"), 0o644)
		clock := newFakeClock()
		clock.set(1000)
		env := &Env{
			LookupEnv: func(key string) (string, bool) {
				if key == "SUB_STORE_BUILTIN_DIR" {
					return builtin, true
				}
				return "", false
			},
			DataDir: func() string { return base }, Clock: clock,
		}
		r := NewRuntime(env, filepath.Join(base, "rt"), 300)
		os.MkdirAll(r.Directory, 0o755)
		if err := os.WriteFile(filepath.Join(r.Directory, "state.json"), []byte(latin1Encode(state)), 0o644); err != nil {
			t.Fatal(err)
		}
		commands := []any{}
		r.command = func(_ context.Context, name string) (string, error) {
			commands = append(commands, name)
			if name == "status" {
				return "RUNNING", nil
			}
			return "", nil
		}
		r.ready = func(context.Context) (bool, error) { return running, nil }
		r.getMode = func(context.Context) (string, error) { return "on_demand", nil }
		var got any
		var err error
		switch action {
		case "tick":
			err = r.Tick(context.Background())
		case "status":
			var status *jsonx.Object
			if status, err = r.Status(context.Background(), running); status != nil {
				got = status
			}
		default:
			err = r.Use(context.Background(), func(ctx context.Context) error { return nil })
		}
		label := action + " " + jsonx.Repr(state) + " running=" + jsonx.Repr(running)
		checkOutcome(t, label, row, got, err, true)
		if !jsonx.Equal(commands, row.Value("commands")) {
			t.Errorf("%s: commands %s, Python %s", label, jsonx.Repr(commands), jsonx.Repr(row.Value("commands")))
		}
		after, _ := os.ReadFile(filepath.Join(r.Directory, "state.json"))
		if string(after) != latin1Encode(row.GetString("after")) {
			t.Errorf("%s: state.json %q, Python %q", label, after, latin1Encode(row.GetString("after")))
		}
	}
}

func coalescingClient(t *testing.T, limit int) (*Client, *fakeClock, *string, *int) {
	clock := newFakeClock()
	builtin := t.TempDir() // no bundle: SUB_STORE_URL is the base
	env := &Env{
		LookupEnv: func(key string) (string, bool) {
			switch key {
			case "SUB_STORE_BUILTIN_DIR":
				return builtin, true
			case "SUB_STORE_URL":
				return "http://127.0.0.1:9", true
			}
			return "", false
		},
		Clock: clock,
	}
	c := NewClient(env, nil)
	if limit > 0 {
		c.cacheLimit = limit
	}
	outcome := new(string)
	count := new(int)
	c.fetch = func(_ context.Context, _, _, _ string) (*SubscriptionResult, error) {
		*count++
		switch *outcome {
		case "ok":
			return &SubscriptionResult{Text: "r" + jsonx.Str(jsonx.Int(*count)), Source: "direct_yaml", FetchedAt: "t"}, nil
		case "fail":
			return nil, &SubscriptionFetchError{pyErr("ValueError", "s"), pyErr("ValueError", "d")}
		}
		status := map[string]int{"403": 403, "429": 429, "500": 500}[*outcome]
		return nil, &SubscriptionFetchError{nil, &RequestError{Class: "HTTPError", Status: status}}
	}
	return c, clock, outcome, count
}

// TestReviewCoalescingClockEdges replays reuse windows (300/30/900 s), the
// 600 s retention, failure cooldowns (3 s doubling to 300 s, ≥60 s for
// 403/429), the failure reset, the one-hour failure purge and the entry
// bound against Python's dict bookkeeping, step by step.
func TestReviewCoalescingClockEdges(t *testing.T) {
	scenarios := reviewReference(t).Value("coalescing").(*jsonx.Object)
	steps := map[string][][]any{}
	const u = "https://sub.test/a"
	step := func(at float64, url string, refresh, prefer bool, outcome string) []any {
		return []any{at, url, refresh, prefer, outcome}
	}
	steps["normal reuse edge"] = [][]any{step(0, u, false, false, "ok"), step(299.999, u, false, false, "ok"),
		step(300, u, false, false, "ok"), step(599.999, u, false, false, "ok"), step(600, u, false, false, "ok")}
	steps["refresh edge"] = [][]any{step(0, u, false, false, "ok"), step(29.999, u, true, false, "ok"),
		step(30, u, true, false, "ok"), step(59.5, u, false, false, "ok"), step(60, u, true, false, "ok")}
	steps["background edge"] = [][]any{step(0, u, false, false, "ok"), step(899.999, u, false, true, "ok"),
		step(900, u, false, true, "ok"), step(1000, u, true, true, "ok"), step(1029.999, u, true, true, "ok")}
	steps["retention cleanup"] = [][]any{step(0, u, false, false, "ok"), step(899.999, u, false, true, "ok"),
		step(900, u+"2", false, false, "ok"), step(900, u, false, true, "ok")}
	steps["failure doubling"] = [][]any{step(0, u, false, false, "fail"), step(2.999, u, true, false, "ok"),
		step(3, u, false, false, "fail"), step(8.999, u, false, true, "ok"), step(9, u, false, false, "fail"),
		step(21, u, false, false, "fail"), step(45, u, false, false, "fail"), step(93, u, false, false, "fail"),
		step(189, u, false, false, "fail"), step(381, u, false, false, "fail"), step(680.999, u, false, false, "ok"),
		step(681, u, false, false, "fail"), step(981, u, false, false, "ok"), step(981.5, u, false, false, "fail")}
	steps["blocked cooldown"] = [][]any{step(0, u, false, false, "403"), step(59.999, u, true, false, "ok"),
		step(60, u, false, false, "429"), step(119.999, u, false, false, "ok"), step(120, u, false, false, "500"),
		step(123, u, false, false, "403"), step(182.999, u, false, false, "ok"), step(183, u, false, false, "ok")}
	steps["success resets failures"] = [][]any{step(0, u, false, false, "fail"), step(3, u, false, false, "fail"),
		step(9, u, false, false, "ok"), step(309, u, false, false, "fail"), step(311.999, u, false, false, "ok"),
		step(312, u, false, false, "ok")}
	steps["failure purge after an hour"] = [][]any{step(0, u, false, false, "fail"), step(3, u, false, false, "fail"),
		step(3603, u, false, false, "fail"), step(3605.999, u, false, false, "ok"), step(3606, u, false, false, "ok")}
	steps["other url independent"] = [][]any{step(0, u, false, false, "fail"), step(1, u+"2", false, false, "ok"),
		step(2, u+"2", true, false, "ok"), step(31, u+"2", true, false, "ok")}
	steps["bound 3"] = [][]any{step(0, "u1", false, false, "ok"), step(1, "u2", false, false, "fail"),
		step(2, "u3", false, false, "ok"), step(3, "u4", false, false, "ok"), step(4, "u1", false, false, "ok"),
		step(5, "u3", false, false, "ok"), step(6, "u2", false, false, "ok"), step(7, "u4", false, false, "ok")}
	steps["failure bound 3"] = [][]any{step(0, "u1", false, false, "fail"), step(10, "u2", false, false, "fail"),
		step(20, "u3", false, false, "fail"), step(30, "u4", false, false, "fail"), step(40, "u1", false, false, "fail"),
		step(50, "u4", false, false, "fail")}
	if len(steps) != scenarios.Len() {
		t.Fatalf("%d scenarios here, %d in the reference", len(steps), scenarios.Len())
	}
	for name, script := range steps {
		limit := 0
		if strings.Contains(name, "bound 3") {
			limit = 3
		}
		c, clock, outcome, count := coalescingClient(t, limit)
		want := scenarios.GetList(name)
		for i, s := range script {
			clock.set(s[0].(float64))
			*outcome = s[4].(string)
			before := *count
			result, err := c.GetSubscriptionProxiesYAML(context.Background(), "s", s[1].(string), "",
				FetchOptions{Refresh: s[2].(bool), PreferCached: s[3].(bool)})
			got := ""
			if err != nil {
				fetchErr := err.(*SubscriptionFetchError)
				got = "error None"
				if status := StatusOf(fetchErr.DirectError); status != 0 {
					got = "error " + jsonx.Str(jsonx.Int(status))
				}
			} else {
				got = result.Text
			}
			c.mu.Lock()
			row := jsonx.Obj("fetched", *count != before, "got", got, "results", jsonx.Int(c.results.len()),
				"failures", jsonx.Int(c.failures.len()))
			c.mu.Unlock()
			if !jsonx.Equal(row, want[i]) {
				t.Errorf("%s step %d (%v): Go %s, Python %s", name, i, s, jsonx.Repr(row), jsonx.Repr(want[i]))
			}
		}
	}
}

// TestReviewInflightWaitersIgnoreFlags: while a download is in flight,
// refresh / prefer_cached callers wait for it instead of starting another.
func TestReviewInflightWaitersIgnoreFlags(t *testing.T) {
	want := reviewReference(t).Value("inflight").(*jsonx.Object)
	c, _, _, _ := coalescingClient(t, 0)
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	c.fetch = func(context.Context, string, string, string) (*SubscriptionResult, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		<-release
		return &SubscriptionResult{Text: "shared", Source: "direct_yaml", FetchedAt: "t"}, nil
	}
	var wg sync.WaitGroup
	got := []string{}
	start := func(refresh, prefer bool) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := c.GetSubscriptionProxiesYAML(context.Background(), "s", "https://sub.test/a", "",
				FetchOptions{Refresh: refresh, PreferCached: prefer})
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			got = append(got, result.Text)
			mu.Unlock()
		}()
	}
	start(false, false)
	time.Sleep(100 * time.Millisecond)
	start(true, false)
	start(false, true)
	start(true, true)
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	sort.Strings(got)
	gotList := []any{}
	for _, g := range got {
		gotList = append(gotList, g)
	}
	if row := jsonx.Obj("calls", jsonx.Int(calls), "got", gotList); !jsonx.Equal(row, want) {
		t.Errorf("Go %s, Python %s", jsonx.Repr(row), jsonx.Repr(want))
	}
}

// stateOf is the runtime's state.json as an object (nil when it is not one).
func stateOf(r *Runtime) *jsonx.Object {
	object, _ := r.read().(*jsonx.Object)
	return object
}

// lastUsed is the stored last_used as a float.
func lastUsed(state *jsonx.Object) float64 { return state.GetFloat("last_used") }
