package substore

// Review C13: reference results of the real Python modules for edge inputs
// (testdata/gen_review.py → testdata/review_reference.json, CPython 3.11 /
// requests 2.31 / PyYAML 6.0.1 on the test server): odd userinfo headers,
// YAML detection, proxies with non-string keys, charset parameters, the
// runtime on malformed state.json, reuse/cooldown clock edges and in-flight
// waiters with mixed flags.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func reviewReference(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/review_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	// Python's json.dumps emits bare Infinity for YAML .inf test outputs.
	data = bytes.ReplaceAll(data, []byte(": Infinity"), []byte(`: "__Infinity__"`))
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		t.Fatal(err)
	}
	return root
}

func reviewList(m map[string]any, key string) []any {
	l, _ := m[key].([]any)
	return l
}

// checkOutcome compares a Go result with a reference row ({output} or
// {error, message}); withMessage also compares the exception text.
func checkOutcome(t *testing.T, label string, row map[string]any, got any, err error, withMessage bool) {
	t.Helper()
	if want, ok := row["output"]; ok {
		if err != nil {
			t.Errorf("%s: Go error %v, Python %v", label, err, want)
		} else if !testjson.Equal(want, got) {
			t.Errorf("%s:\n%s", label, testjson.Diff(want, got))
		}
		return
	}
	errClass, _ := row["error"].(string)
	errMsg, _ := row["message"].(string)
	if err == nil {
		t.Errorf("%s: Go %v, Python %s: %s", label, got, errClass, errMsg)
		return
	}
	if class := pythonClass(err); class != errClass {
		t.Errorf("%s: Go %s (%v), Python %s", label, class, err, errClass)
	}
	if withMessage && err.Error() != errMsg {
		t.Errorf("%s: message %q, Python %q", label, err.Error(), errMsg)
	}
}

func TestReviewParseUserinfo(t *testing.T) {
	native := nativeClientExpectations(t)
	for i, item := range reviewList(reviewReference(t), "parse_userinfo") {
		row := item.(map[string]any)
		in, _ := row["input"].(string)
		info, err := ReadUserinfo(in)
		var got any
		if info != nil {
			got = info
		}
		if expected := native[fmt.Sprintf("review/userinfo/%d", i)]; expected != nil {
			row = expected
		}
		checkOutcome(t, fmt.Sprintf("parse_userinfo %q", row["input"]), row, got, err, true)
	}
}

func TestReviewIsYAMLResponse(t *testing.T) {
	oracle := nativeReaderCases(t)
	for i, item := range reviewList(reviewReference(t), "is_yaml_response") {
		row := item.(map[string]any)
		in, _ := row["input"].(string)
		assertNativeReaderCase(t, oracle, fmt.Sprintf("substore/review/is_yaml_response/%d", i), isYAMLResponse(in), nil)
	}
}

// TestReviewParseProxiesFromYAML covers coordinator decision 10: mappings
// with int/bool/None keys load as in Python (keys read as json.dumps writes
// them, the form the subscription cache keeps).
func TestReviewParseProxiesFromYAML(t *testing.T) {
	native := nativeClientExpectations(t)
	ref := reviewReference(t)
	oracle := nativeReaderCases(t)
	for _, fn := range []string{"parse_proxies_from_yaml", "proxies_to_nodes"} {
		for i, item := range reviewList(ref, fn) {
			row := item.(map[string]any)
			text, _ := row["input"].(string)
			label := fmt.Sprintf("%s %q", fn, text)
			nodes, err := ParseProxyNodes(text)
			var got any
			if err == nil {
				if fn == "proxies_to_nodes" {
					got, err = ProxyNodes(nodes)
				} else {
					got, err = yamlNodesToJSON(nodes)
				}
			}
			if expected := native[fmt.Sprintf("review/%s/%d", fn, i)]; expected != nil {
				if out, ok := expected["output"]; ok && fn == "proxies_to_nodes" {
					// Stored form of plain Node records (storedNodesForm); a
					// port that is not a number ("x") reads as 0 and is left
					// out (accepted O18 T1c difference: wrong-typed member).
					expected = map[string]any{"output": storedNodesForm(t, out)}
				}
				checkOutcome(t, label, expected, got, err, true)
				continue
			}
			if _, has := row["unserializable"]; has {
				if err == nil || !errors.Is(err, yamlx.ErrType) {
					t.Errorf("%s: Go %v %v, want a TypeError", label, got, err)
				}
				continue
			}
			if fn == "proxies_to_nodes" {
				assertNativeReaderCaseAs(t, oracle, fmt.Sprintf("substore/review/%s/%d", fn, i), got, err, storedNodesForm)
				continue
			}
			assertNativeReaderCase(t, oracle, fmt.Sprintf("substore/review/%s/%d", fn, i), got, err)
		}
	}
}

func TestReviewResponseText(t *testing.T) {
	for _, item := range reviewList(reviewReference(t), "response_text") {
		row := item.(map[string]any)
		in := row["input"].([]any)
		resp := &Response{Header: http.Header{"Content-Type": {latin1Encode(in[0].(string))}}, Body: bytesOf(in[1].([]any))}
		checkOutcome(t, fmt.Sprintf("response_text %q", in[0]), row, resp.Text(), nil, false)
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
// holds a last_used Python cannot subtract) fails where Python fails.
func TestReviewRuntimeMalformedState(t *testing.T) {
	reference := nativeRuntimeReference(t)
	for index, item := range reviewList(reviewReference(t), "runtime") {
		row := item.(map[string]any)
		state, _ := row["state"].(string)
		action, _ := row["action"].(string)
		running, _ := row["running"].(bool)
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
		commands := []string{}
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
			var status *RuntimeStatus
			if status, err = r.Snapshot(context.Background(), running); status != nil {
				got = status
			}
		default:
			err = r.Use(context.Background(), func(ctx context.Context) error { return nil })
		}
		expected := reference.Expectations[index]
		label := fmt.Sprintf("%s %q running=%v", action, state, running)
		checkOutcome(t, label, expected, got, err, true)
		if !testjson.Equal(expected["commands"], commands) {
			t.Errorf("%s: commands %v, Python %v", label, commands, expected["commands"])
		}
		after, _ := os.ReadFile(filepath.Join(r.Directory, "state.json"))
		wantAfter, _ := expected["after"].(string)
		compareRuntimeStateFile(t, label, after, []byte(latin1Encode(wantAfter)))
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
	c.fetch = func(_ context.Context, _, _, _ string) (*FetchResult, error) {
		*count++
		switch *outcome {
		case "ok":
			return &FetchResult{Text: "r" + strconv.Itoa(*count), Source: "direct_yaml", FetchedAt: "t"}, nil
		case "fail":
			return nil, &SubscriptionFetchError{pyErr("ValueError", "s"), pyErr("ValueError", "d")}
		}
		status := map[string]int{"403": 403, "429": 429, "500": 500}[*outcome]
		return nil, &SubscriptionFetchError{nil, &RequestError{Class: "HTTPError", Status: status}}
	}
	return c, clock, outcome, count
}

func TestReviewCoalescingClockEdges(t *testing.T) {
	scenarios := reviewReference(t)["coalescing"].(map[string]any)
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
	if len(steps) != len(scenarios) {
		t.Fatalf("%d scenarios here, %d in the reference", len(steps), len(scenarios))
	}
	for name, script := range steps {
		limit := 0
		if strings.Contains(name, "bound 3") {
			limit = 3
		}
		c, clock, outcome, count := coalescingClient(t, limit)
		want := scenarios[name].([]any)
		for i, s := range script {
			clock.set(s[0].(float64))
			*outcome = s[4].(string)
			before := *count
			result, err := c.Fetch(context.Background(), "s", s[1].(string), "",
				FetchOptions{Refresh: s[2].(bool), PreferCached: s[3].(bool)})
			got := ""
			if err != nil {
				fetchErr := err.(*SubscriptionFetchError)
				got = "error None"
				if status := StatusOf(fetchErr.DirectError); status != 0 {
					got = "error " + strconv.Itoa(status)
				}
			} else {
				got = result.Text
			}
			c.mu.Lock()
			row := map[string]any{"fetched": *count != before, "got": got, "results": c.results.len(), "failures": c.failures.len()}
			c.mu.Unlock()
			if !testjson.Equal(want[i], row) {
				t.Errorf("%s step %d (%v):\n%s", name, i, s, testjson.Diff(want[i], row))
			}
		}
	}
}

func TestReviewInflightWaitersIgnoreFlags(t *testing.T) {
	want := reviewReference(t)["inflight"]
	c, _, _, _ := coalescingClient(t, 0)
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	c.fetch = func(context.Context, string, string, string) (*FetchResult, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		<-release
		return &FetchResult{Text: "shared", Source: "direct_yaml", FetchedAt: "t"}, nil
	}
	var wg sync.WaitGroup
	got := []string{}
	start := func(refresh, prefer bool) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := c.Fetch(context.Background(), "s", "https://sub.test/a", "",
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
	if row := map[string]any{"calls": calls, "got": got}; !testjson.Equal(want, row) {
		t.Errorf("%s", testjson.Diff(want, row))
	}
}

// stateOf returns the runtime's state.json record (nil on read error).
func stateOf(r *Runtime) *runtimeState {
	st, err := r.loadState()
	if err != nil {
		return nil
	}
	return st
}

// lastUsed is the stored last_used as a float.
func lastUsed(state *runtimeState) float64 {
	if state == nil || state.LastUsed == nil {
		return 0
	}
	return *state.LastUsed
}

func hasLastUsed(r *Runtime) bool {
	data, err := os.ReadFile(filepath.Join(r.Root(), "state.json"))
	if err != nil {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	return m["last_used"] != nil
}
