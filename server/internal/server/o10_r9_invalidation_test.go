package server

import (
	"context"
	"os"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/substore"
)

// O10 R8/R9 review: the reuse tables (decoded node caches, provider texts
// rendered from them, parsed sources, converted aggregation nodes) never
// serve a previous answer once an input changes. Every route that carries
// subscription nodes is read warm (so the tables are filled), then each
// input is changed and the next read must show it: the node cache rewritten
// within the same second with the same size, the subscription's filter, and
// the aggregation's members and filter.
func TestO10ReuseTablesFollowInputChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the full fixture")
	}
	f := newO10Fixture(t)
	ctx := f.srv.Context()
	routes := []string{
		"/api/profiles/default/subscriptions/" + o10SubA + "/proxies",
		"/api/profiles/default/subscriptions/" + o10SubA + "/proxies?format=surge",
		"/api/profiles/default/subscriptions/" + o10SubA + "/proxies?format=loon",
		"/api/profiles/default/aggregations/" + o10Aggregation + "/provider",
	}
	get := func(path string) string {
		t.Helper()
		res := f.do("GET", path, "", nil)
		if res.Code != 200 {
			t.Fatalf("GET %s: %d %.300s", path, res.Code, res.Body.String())
		}
		return res.Body.String()
	}
	readAll := func() []string {
		var out []string
		for range 2 { // the second read is served from the tables
			out = out[:0]
			for _, path := range routes {
				out = append(out, get(path))
			}
		}
		return out
	}
	check := func(step string, bodies []string, want func(path, body string) bool) {
		t.Helper()
		for i, path := range routes {
			if !want(path, bodies[i]) {
				t.Errorf("%s: GET %s still shows the previous input:\n%.600s", step, path, bodies[i])
			}
		}
	}
	const hongKong, japan = "香港 00 | " + o10SubA, "日本 01 | " + o10SubA
	check("baseline", readAll(), func(_, body string) bool {
		return strings.Contains(body, hongKong) && strings.Contains(body, japan)
	})

	// 1. The node cache is rewritten within the same second with a file of
	// the same size: one node renamed to a name of the same length.
	path, err := substore.CachePath(ctx, o10SubA)
	if err != nil {
		t.Fatal(err)
	}
	oldInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := substore.LoadCache(ctx, o10SubA)
	if err != nil || before == nil {
		t.Fatalf("cache: %v", err)
	}
	renamed := strings.Replace(before.Nodes[0].Name, "香港 00", "香港 X0", 1)
	if len(renamed) != len(before.Nodes[0].Name) || renamed == before.Nodes[0].Name {
		t.Fatalf("rename %q", before.Nodes[0].Name)
	}
	nodes := append([]model.Node(nil), before.Nodes...)
	nodes[0].Name = renamed
	metadata := *before.Metadata
	if _, err := substore.SaveCache(ctx, o10SubA, nodes, &metadata); err != nil {
		t.Fatal(err)
	}
	newInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if newInfo.Size() != oldInfo.Size() {
		t.Fatalf("the rewrite changed the cache size %d -> %d", oldInfo.Size(), newInfo.Size())
	}
	check("node cache rewritten", readAll(), func(_, body string) bool {
		return strings.Contains(body, "香港 X0 | "+o10SubA) && !strings.Contains(body, hongKong)
	})

	// 2. The subscription's filter changes.
	setFilter := func(filter *string) {
		t.Helper()
		if err := f.srv.Repo.UpdateShared(ctx, func(_ context.Context, s *model.Shared) error {
			for i := range s.Subscriptions {
				if s.Subscriptions[i].ID == o10SubA {
					s.Subscriptions[i].RegexFilter = filter
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	onlyJapan := "日本"
	setFilter(&onlyJapan)
	check("subscription filter", readAll(), func(_, body string) bool {
		return strings.Contains(body, japan) && !strings.Contains(body, "香港 X0 | "+o10SubA)
	})
	setFilter(nil)
	check("subscription filter removed", readAll(), func(_, body string) bool {
		return strings.Contains(body, japan) && strings.Contains(body, "香港 X0 | "+o10SubA)
	})

	// 3. The aggregation's members and filter change (only its provider).
	routes = routes[3:]
	updateAggregation := func(fn func(*model.Aggregation)) {
		t.Helper()
		if err := f.srv.Repo.UpdateShared(ctx, func(_ context.Context, s *model.Shared) error {
			for i := range s.Aggregations {
				if s.Aggregations[i].ID == o10Aggregation {
					fn(&s.Aggregations[i])
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	updateAggregation(func(a *model.Aggregation) { a.Subscriptions = []string{o10SubB} })
	check("aggregation members", readAll(), func(_, body string) bool {
		return !strings.Contains(body, "| "+o10SubA) && strings.Contains(body, "| "+o10SubB)
	})
	updateAggregation(func(a *model.Aggregation) {
		a.Subscriptions = []string{o10SubA, o10SubB}
		filter := "新加坡"
		a.RegexFilter = &filter
	})
	check("aggregation filter", readAll(), func(_, body string) bool {
		return strings.Contains(body, "新加坡") && !strings.Contains(body, "日本") && strings.Contains(body, "| "+o10SubA)
	})
}
