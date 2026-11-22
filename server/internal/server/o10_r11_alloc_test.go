package server

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// O10 R11: per-route allocation ceilings. Every route measured by
// tools/o10perf (19 routes, the heartbeat included) runs on the synthetic
// fixture of o10_r11_fixture_test.go; the bytes allocated per request must
// stay under the route's ceiling. A ceiling is about 1.5 times the value
// measured when it was set (go test on the test server; generator routes and
// notifications re-measured after O10 R8/R9), with a floor of
// 0.3 MB, so an accidental whole-record copy or encode (one deep copy of a
// Config or of the Agent list costs several MB here) fails the test. -race
// allocates up to about twice as much, so it has its own columns. Lower a
// ceiling when a route gets cheaper; raising one needs a reason in the commit.
var o10RouteCeilings = []struct {
	label                     string
	measured, ceiling         float64 // MB per request, normal build
	measuredRace, ceilingRace float64 // MB per request, -race build (the gate's core run)
}{
	{"GET /api/config/<profile>/mihomo", 9.21, 13.9, 13.54, 21},
	{"GET /api/config/<profile>/surge", 2.59, 3.9, 3.79, 5.7},
	{"GET /api/config/<profile>/loon", 2.74, 4.2, 3.91, 5.9},
	{"GET /api/config/<profile>/mosdns", 5.03, 7.6, 5.85, 8.8},
	{"GET /api/config/<profile>/oxidns", 3.68, 5.6, 4.44, 6.7},
	{"GET /api/profiles/<profile>/aggregations/<agg>/provider", 30.43, 46, 39.91, 60},
	{"GET /api/agents?summary=1", 0.18, 0.3, 0.24, 0.4},
	{"GET /api/agents?history=0", 0.41, 0.7, 0.55, 0.9},
	{"GET /api/agents", 4.6, 6.9, 6.05, 9.1},
	{"GET /api/notifications", 2.37, 3.6, 5.61, 8.5},
	{"GET /api/profiles", 0.11, 0.3, 0.26, 0.4},
	{"GET /api/agents/<id>", 1.51, 2.3, 1.87, 2.9},
	{"GET /api/agents/<id>/rule-sync", 0.15, 0.3, 0.2, 0.4},
	{"POST /api/agents/<id>/heartbeat", 0.26, 0.4, 0.38, 0.6},
	{"GET /api/routers", 0.17, 0.3, 0.36, 0.6},
	{"GET /api/routers/<id>/policy", 0.76, 1.2, 1.3, 2},
	{"GET /api/routers/<id>/policy-settings", 5.53, 8.3, 7.39, 11.1},
	{"GET /api/routers/<id>/rule-sync", 0.15, 0.3, 0.27, 0.5},
	{"GET /api/agents/<id>/upstream-routers", 0.28, 0.5, 0.45, 0.7},
}

type o10Request struct {
	method, path, token string
	body                any
}

// o10Requests maps each label to its requests, like tools/o10perf/perf.py
// (every configuration space, every Agent, every router).
func (f *o10Fixture) o10Requests() map[string][]o10Request {
	out := map[string][]o10Request{}
	add := func(label string, r o10Request) { out[label] = append(out[label], r) }
	for _, profile := range []string{"default", "phone"} {
		for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
			add("GET /api/config/<profile>/"+engine, o10Request{method: "GET", path: "/api/config/" + profile + "/" + engine})
		}
		add("GET /api/profiles/<profile>/aggregations/<agg>/provider", o10Request{method: "GET", path: "/api/profiles/" + profile + "/aggregations/" + o10Aggregation + "/provider"})
	}
	for _, path := range []string{"/api/agents?summary=1", "/api/agents?history=0", "/api/agents", "/api/notifications", "/api/profiles", "/api/routers"} {
		add("GET "+path, o10Request{method: "GET", path: path})
	}
	for _, agent := range f.agents {
		add("GET /api/agents/<id>", o10Request{method: "GET", path: "/api/agents/" + agent.id})
		add("GET /api/agents/<id>/rule-sync", o10Request{method: "GET", path: "/api/agents/" + agent.id + "/rule-sync"})
		add("GET /api/agents/<id>/upstream-routers", o10Request{method: "GET", path: "/api/agents/" + agent.id + "/upstream-routers"})
		add("POST /api/agents/<id>/heartbeat", o10Request{method: "POST", path: "/api/agents/" + agent.id + "/heartbeat", token: agent.token, body: map[string]any{
			"service_status": "running", "version": "1.9.1-go",
			"system_metrics": map[string]any{"collected_at": time.Now().UTC().Format(time.RFC3339),
				"cpu":     map[string]any{"usage_percent": 3.5, "core_count": 4},
				"memory":  map[string]any{"total": 4 << 30, "used": 1 << 30, "available": 3 << 30, "used_percent": 25.0},
				"disk":    map[string]any{"total": 32 << 30, "used": 8 << 30, "free": 24 << 30, "used_percent": 25.0},
				"network": map[string]any{"bytes_sent": 123456, "bytes_recv": 654321, "speed_sent": 100, "speed_recv": 200}}}})
	}
	for _, id := range f.routers {
		for _, suffix := range []string{"policy", "policy-settings", "rule-sync"} {
			add("GET /api/routers/<id>/"+suffix, o10Request{method: "GET", path: "/api/routers/" + id + "/" + suffix})
		}
	}
	return out
}

func TestO10RouteAllocationCeilings(t *testing.T) {
	if testing.Short() {
		t.Skip("allocation ceilings need the full fixture")
	}
	f := newO10Fixture(t)
	requests := f.o10Requests()
	// Measure the routes, not the test's own borrow checks.
	f.srv.Repo.SetViewGuard(nil)
	var table strings.Builder
	for _, route := range o10RouteCeilings {
		group := requests[route.label]
		if len(group) == 0 {
			t.Fatalf("%s: no request", route.label)
		}
		// Warm-up: lazy caches, first-use initialisation, status checks.
		for _, r := range group {
			if res := f.do(r.method, r.path, r.token, r.body); res.Code != 200 || res.Body.Len() == 0 {
				t.Fatalf("%s %s: %d %.300s", r.method, r.path, res.Code, res.Body.String())
			}
		}
		rounds := 3
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		for range rounds {
			for _, r := range group {
				f.do(r.method, r.path, r.token, r.body)
			}
		}
		runtime.ReadMemStats(&after)
		perRequest := float64(after.TotalAlloc-before.TotalAlloc) / float64(rounds*len(group)) / (1 << 20)
		measured, ceiling := route.measured, route.ceiling
		if o10RaceEnabled {
			measured, ceiling = route.measuredRace, route.ceilingRace
		}
		fmt.Fprintf(&table, "%-58s %8.2f MB/request (ceiling %.2f)\n", route.label, perRequest, ceiling)
		if perRequest > ceiling {
			t.Errorf("%s: %.2f MB/request exceeds the O10 R11 ceiling %.2f MB (measured %.2f when set); a whole-record copy or encode on this route?",
				route.label, perRequest, ceiling, measured)
		}
	}
	t.Logf("per-request allocation:\n%s", table.String())
	if len(requests) != len(o10RouteCeilings) {
		t.Fatalf("%d request groups for %d ceilings", len(requests), len(o10RouteCeilings))
	}
}
