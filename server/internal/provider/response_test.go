package provider

// Ports of backend/test_provider_response.py and
// backend/test_provider_filter_isolation.py.

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/repo"
	"configflow/internal/store"
	"configflow/internal/testjson"
)

type serveFixture struct {
	t      *testing.T
	srv    *httpxtest.Server
	mu     sync.Mutex
	render Render
	kind   string
	// renderFor builds the render from the request (the routes' closures).
	renderFor func(c *httpx.Ctx) Render
	// keyFor overrides the item id and head the request renders (nil: the
	// fixture's fixed ones).
	keyFor func(c *httpx.Ctx) (itemID string, head *store.Head)
}

func newServeFixture(t *testing.T, seconds float64) *serveFixture {
	f := &serveFixture{t: t, kind: "aggregation"}
	f.srv = httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/provider", "test.provider", func(c *httpx.Ctx) (*httpx.Response, error) {
			f.mu.Lock()
			render, kind, keyFor := f.render, f.kind, f.keyFor
			if f.renderFor != nil {
				render = f.renderFor(c)
			}
			f.mu.Unlock()
			profile := c.ArgDefault("profile", "alpha")
			itemID := map[string]string{"aggregation": "agg-1", "subscription": "feed"}[kind]
			head := &store.Head{StoreID: "serve-fixture", Generation: 1}
			if keyFor != nil {
				itemID, head = keyFor(c)
			}
			return Serve(c, kind, itemID, profile, head, render)
		})
	})})
	ctx := f.srv.Context()
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runner.Start(ctx)
	previous := flights
	flights = lockx.NewFlight[flightKey, *responseCopy](responseTestSpawner{runner}, "provider-render")
	t.Cleanup(func() { runner.Shutdown(context.Background()); flights = previous })
	for _, id := range []string{"alpha", "beta"} {
		if _, err := f.srv.Repo.CreateProfileRecord(ctx, repo.ProfileCreation{ID: id, Name: id}, ""); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(SetResponseSeconds(seconds))
	// Registered last, so it runs first: a render still running in the
	// background (released by the test just before it returned) must finish
	// writing its last good file before the repository's temp directory is
	// removed — otherwise the cleanup races the write and the test flakes
	// under load ("directory not empty").
	t.Cleanup(func() { waitFor(t, func() bool { return flightCount() == 0 }) })
	return f
}

type served struct {
	status int
	body   string
	marker string
}

func (f *serveFixture) serve(render Render, query string) served {
	f.mu.Lock()
	f.render = render
	f.mu.Unlock()
	res := f.srv.Get("/provider" + query)
	return served{res.Status, string(res.Body), res.Header.Get("X-ConfigFlow-Provider")}
}

func yamlResponse(content string) Render {
	return func(context.Context) (*httpx.Response, error) {
		return httpx.Bytes(200, "text/yaml; charset=utf-8", []byte(content)), nil
	}
}

func slowRender(content string, gate chan struct{}, calls *[]int, mu *sync.Mutex) Render {
	return func(context.Context) (*httpx.Response, error) {
		if calls != nil {
			mu.Lock()
			*calls = append(*calls, 1)
			mu.Unlock()
		}
		select {
		case <-gate:
		case <-time.After(5 * time.Second):
		}
		return httpx.Bytes(200, "text/yaml; charset=utf-8", []byte(content)), nil
	}
}

func waitFor(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !predicate() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !predicate() {
		t.Fatal("condition not reached")
	}
}

var activeProviderJobs atomic.Int64

type responseTestSpawner struct{ runner *lifecycle.Runtime }

func (s responseTestSpawner) Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	activeProviderJobs.Add(1)
	err := s.runner.Spawn(ctx, name, func(work context.Context) { defer activeProviderJobs.Add(-1); fn(work) })
	if err != nil {
		activeProviderJobs.Add(-1)
	}
	return err
}
func flightCount() int { return int(activeProviderJobs.Load()) }

// test_immediate_render_clears_flight_without_reentering_locked_callback:
// a render finishing at once leaves no flight behind.
func TestImmediateRenderClearsFlightWithoutReenteringLockedCallback(t *testing.T) {
	f := newServeFixture(t, 0.2)
	for _, content := range []string{"proxies: [first]", "proxies: [second]"} {
		if got := f.serve(yamlResponse(content), ""); got != (served{200, content, ""}) {
			t.Fatalf("got %+v", got)
		}
		waitFor(t, func() bool { return flightCount() == 0 })
	}
}

func TestSlowRenderReturnsLastGoodAndRefreshesItInTheBackground(t *testing.T) {
	f := newServeFixture(t, 0.2)
	if got := f.serve(yamlResponse("proxies: [old]"), ""); got.body != "proxies: [old]" {
		t.Fatalf("got %+v", got)
	}
	gate := make(chan struct{})
	started := time.Now()
	got := f.serve(slowRender("proxies: [new]", gate, nil, nil), "")
	if got != (served{200, "proxies: [old]", "last-good"}) {
		t.Fatalf("got %+v", got)
	}
	if time.Since(started) >= 2*time.Second {
		t.Fatal("fallback was not immediate")
	}
	close(gate)
	path, err := f.srv.Repo.ProfilePath("alpha", "providers/served/aggregation-agg-1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		data, _ := os.ReadFile(path)
		return string(data) == "proxies: [new]"
	})
	if got := f.serve(yamlResponse("proxies: [new]"), ""); got.body != "proxies: [new]" {
		t.Fatalf("got %+v", got)
	}
}

func TestSlowRenderWithoutLastGoodStillWaits(t *testing.T) {
	f := newServeFixture(t, 0.2)
	gate := make(chan struct{})
	time.AfterFunc(500*time.Millisecond, func() { close(gate) })
	if got := f.serve(slowRender("proxies: [first]", gate, nil, nil), ""); got != (served{200, "proxies: [first]", ""}) {
		t.Fatalf("got %+v", got)
	}
}

func TestConcurrentRequestsShareOneRender(t *testing.T) {
	f := newServeFixture(t, 5)
	gate := make(chan struct{})
	var mu sync.Mutex
	var calls []int
	render := slowRender("proxies: [shared]", gate, &calls, &mu)
	f.mu.Lock()
	f.render = render
	f.mu.Unlock()
	results := make(chan string, 3)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- string(f.srv.Get("/provider").Body)
		}()
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(calls) > 0 })
	time.Sleep(200 * time.Millisecond)
	close(gate)
	wg.Wait()
	close(results)
	if len(calls) != 1 {
		t.Fatalf("calls %v", calls)
	}
	for body := range results {
		if body != "proxies: [shared]" {
			t.Fatalf("body %q", body)
		}
	}
}

func TestOtherFormatsNeverFallBack(t *testing.T) {
	f := newServeFixture(t, 0.2)
	f.serve(yamlResponse("proxies: [old]"), "")
	gate := make(chan struct{})
	time.AfterFunc(500*time.Millisecond, func() { close(gate) })
	if got := f.serve(slowRender("surge-text", gate, nil, nil), "?format=surge"); got != (served{200, "surge-text", ""}) {
		t.Fatalf("got %+v", got)
	}
}

func TestLastGoodIsScopedToTheProfile(t *testing.T) {
	f := newServeFixture(t, 0.2)
	f.serve(yamlResponse("proxies: [beta]"), "?profile=beta")
	gate := make(chan struct{})
	time.AfterFunc(500*time.Millisecond, func() { close(gate) })
	if got := f.serve(slowRender("proxies: [alpha]", gate, nil, nil), "?profile=alpha"); got.body != "proxies: [alpha]" {
		t.Fatalf("got %+v", got)
	}
}

func TestAggregationFallsBackToCommittedArtifactBeforeFirstServe(t *testing.T) {
	f := newServeFixture(t, 0.2)
	if _, err := f.srv.Repo.WriteProfileText(f.srv.Context(), "alpha", "providers/agg-1.yaml", []byte("proxies: [committed]")); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	got := f.serve(slowRender("proxies: [new]", gate, nil, nil), "")
	close(gate)
	if got != (served{200, "proxies: [committed]", "last-good"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestFailedRenderIsNotStoredAsLastGood(t *testing.T) {
	f := newServeFixture(t, 0.2)
	got := f.serve(func(context.Context) (*httpx.Response, error) {
		return httpx.JSONStatus(400, map[string]any{"success": false}), nil
	}, "")
	if got.status != 400 || got.body != "{\"success\":false}" { // the copy uses the common native JSON response exit
		t.Fatalf("got %+v", got)
	}
	path, _ := f.srv.Repo.ProfilePath("alpha", "providers/served/aggregation-agg-1.yaml")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("failed render stored as last good")
	}
}

// The fallback answer and copied responses keep the headers clients see.
func TestResponseHeadersAreCopiedAndLastGoodIsTextYAML(t *testing.T) {
	f := newServeFixture(t, 0.2)
	render := func(context.Context) (*httpx.Response, error) {
		return httpx.Bytes(200, "text/yaml; charset=utf-8", []byte("proxies: [h]\r\nx: 1\r\n")).
			WithHeader("Content-Disposition", `inline; filename="proxies_agg-1.yaml"`), nil
	}
	f.mu.Lock()
	f.render = render
	f.mu.Unlock()
	res := f.srv.Get("/provider")
	if res.Header.Get("Content-Type") != "text/yaml; charset=utf-8" ||
		res.Header.Get("Content-Disposition") != `inline; filename="proxies_agg-1.yaml"` ||
		string(res.Body) != "proxies: [h]\r\nx: 1\r\n" {
		t.Fatalf("got %v %q", res.Header, res.Body)
	}
	gate := make(chan struct{})
	defer close(gate)
	f.mu.Lock()
	f.render = slowRender("never", gate, nil, nil)
	f.mu.Unlock()
	res = f.srv.Get("/provider")
	// read_text() translates newlines of the stored last good content.
	if res.Header.Get("Content-Type") != "text/yaml; charset=utf-8" || res.Header.Get("X-ConfigFlow-Provider") != "last-good" ||
		res.Header.Get("Content-Disposition") != "" || string(res.Body) != "proxies: [h]\nx: 1\n" {
		t.Fatalf("got %v %q", res.Header, res.Body)
	}
}

// test_provider_filter_isolation.py::test_concurrent_group_filters_use_independent_provider_flights
func TestConcurrentGroupFiltersUseIndependentProviderFlights(t *testing.T) {
	for _, format := range []string{"surge", "loon"} {
		for _, field := range []string{"regex", "exclude_regex"} {
			t.Run(format+"/"+field, func(t *testing.T) {
				f := newServeFixture(t, 12)
				f.kind = "subscription"
				started := make(chan struct{})
				release := make(chan struct{})
				var mu sync.Mutex
				var patterns []string
				f.mu.Lock()
				f.renderFor = func(c *httpx.Ctx) Render {
					return func(context.Context) (*httpx.Response, error) {
						pattern, _ := c.Arg(field)
						mu.Lock()
						patterns = append(patterns, pattern)
						if len(patterns) == 2 {
							close(started)
						}
						mu.Unlock()
						select {
						case <-release:
						case <-time.After(3 * time.Second):
						}
						return httpx.Text(200, pattern), nil
					}
				}
				f.mu.Unlock()
				results := map[string]string{}
				var wg sync.WaitGroup
				for _, pattern := range []string{"HK", "JP"} {
					wg.Add(1)
					go func(pattern string) {
						defer wg.Done()
						res := f.srv.Get("/provider?format=" + format + "&" + field + "=" + pattern)
						mu.Lock()
						results[pattern] = string(res.Body)
						mu.Unlock()
					}(pattern)
				}
				select {
				case <-started:
				case <-time.After(2 * time.Second):
					close(release)
					wg.Wait()
					t.Fatal("different filters incorrectly shared one provider render")
				}
				close(release)
				wg.Wait()
				if results["HK"] != "HK" || results["JP"] != "JP" {
					t.Fatalf("results %v", results)
				}
			})
		}
	}
}

func TestRequestedFilters(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/f", "test.filters", func(c *httpx.Ctx) (*httpx.Response, error) {
			filters, err := RequestedFilters(c)
			if err != nil {
				return httpx.Message(400, false, err.Error()), nil
			}
			if filters == nil {
				return httpx.JSON(nil), nil
			}
			return httpx.JSON([]any{filters[0], filters[1]}), nil
		})
	})})
	cases := []struct {
		query string
		want  any
		code  int
	}{
		{"", nil, 200},
		{"?format=surge", nil, 200},
		{"?regex=HK", map[string]any{"success": false, "message": "来源正则筛选仅支持 Surge/Loon 格式"}, 400},
		{"?format=clash&exclude_regex=", map[string]any{"success": false, "message": "来源正则筛选仅支持 Surge/Loon 格式"}, 400},
		{"?format=surge&regex=HK", []any{"HK", ""}, 200},
		{"?format=loon&exclude_regex=JP&regex=", []any{"", "JP"}, 200},
		{"?format=surge&regex=(", map[string]any{"success": false, "message": "正则表达式无效"}, 400},
		{"?format=loon&regex=%2B%2B", map[string]any{"success": false, "message": "正则表达式无效"}, 400},
		{"?format=loon&regex=%20%20", []any{"  ", ""}, 200},
	}
	for _, c := range cases {
		res := srv.Get("/f" + c.query).WantStatus(c.code)
		if diff := testjson.Diff(json.RawMessage(res.Body), c.want); diff != "" {
			t.Fatalf("%s: body %s\n%s", c.query, res.Body, diff)
		}
	}
}
