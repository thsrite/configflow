package discovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func roundSummaryFixture(t testing.TB) (*Service, context.Context, string) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	ctx := context.WithValue(t.Context(), roundTrafficKey{}, make(roundTraffic))
	id, err := r.DefaultProfileID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents = []model.Agent{{ID: "observed", Name: "Example", ProfileID: id, ServiceType: "mihomo", Version: "1.8.0-go", DomainDiscoveryEnabled: new(true)}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := agents.New(ctx, r, agents.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	svc := New(Options{Repo: r, Agents: m})
	t.Cleanup(svc.clearMatcherCache)
	store, err := svc.trafficStore()
	if err != nil {
		t.Fatal(err)
	}
	path := store.Path("observed")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	var file strings.Builder
	file.WriteString(`{"status":null,"days":{`)
	for day := range 7 {
		if day != 0 {
			file.WriteByte(',')
		}
		fmt.Fprintf(&file, `%q:{"ip_only_conns":5,"hosts":{`, now().AddDate(0, 0, -day).Format("2006-01-02"))
		for host := range 500 {
			if host != 0 {
				file.WriteByte(',')
			}
			fmt.Fprintf(&file, `"h%d.example.com":{"ports":{"443":1},"by_route":{"Match\t\tdirect\tDIRECT":{"conns":1,"fail_kinds":{}}}}`, host)
		}
		file.WriteString("}}")
	}
	file.WriteString("}}")
	if err := os.WriteFile(path, []byte(file.String()), 0600); err != nil {
		t.Fatal(err)
	}
	probes := map[string]Probe{}
	for i := range 500 {
		host := fmt.Sprintf("h%d.example.com", i)
		probes[host] = Probe{Host: host, CheckedAt: timestamp(), Verdict: "both_ok", Confidence: 80}
	}
	if err := svc.saveProbes(ctx, "observed", probes); err != nil {
		t.Fatal(err)
	}
	return svc, ctx, id
}

func BenchmarkAutoRefreshDomains(b *testing.B) {
	svc, ctx, id := roundSummaryFixture(b)
	var base trafficSummary
	load := func() {
		list, err := svc.loadDomains(ctx, id, 7, "uncovered", "", &base)
		if err != nil || len(list.Items) != 1 || len(list.Items[0].Hosts) != 500 {
			b.Fatalf("domains %d: %v", len(list.Items), err)
		}
	}
	load()
	b.ReportAllocs()
	for b.Loop() {
		load()
	}
}

func TestRoundRefreshMatchesIndependentLoad(t *testing.T) {
	svc, ctx, id := roundSummaryFixture(t)
	var base trafficSummary
	compare := func() Listing {
		t.Helper()
		got, err := svc.loadDomains(ctx, id, 7, "uncovered", "", &base)
		if err != nil {
			t.Fatal(err)
		}
		want, err := svc.LoadDomains(ctx, id, 7, "uncovered", "")
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("fresh load differs, %v", err)
		}
		return got
	}
	compare()
	group := base.groups[0]
	host := "h0.example.com"
	if err := svc.saveProbes(ctx, "observed", map[string]Probe{host: {Host: host, CheckedAt: timestamp(), Target: new("proxy"), Confidence: 95}}); err != nil {
		t.Fatal(err)
	}
	refreshed := compare()
	if base.groups[0] != group || refreshed.Items[0].Suggestion.Confidence != 95 {
		t.Fatal("stale probe or passive group rebuilt")
	}
	if err := svc.editSettings(ctx, id, func(s *Settings) error { s.Ignored = []string{host}; return nil }); err != nil {
		t.Fatal(err)
	}
	ignored := compare()
	if base.groups[0] != group || !ignored.Items[0].Hosts[0].Ignored {
		t.Fatal("ignore change did not reach refreshed listing")
	}
	store, _ := svc.trafficStore()
	path := store.Path("observed")
	raw := fmt.Sprintf(`{"status":{"value":"running"},"days":{"%s":{"hosts":{"new.example.com":{"by_route":{"Match\t\tdirect\tDIRECT":{"conns":9}}}}}}}`, now().Format("2006-01-02"))
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	changed := compare()
	if base.groups[0] == group || changed.Items[0].Hosts[0].Host != "new.example.com" || changed.Agents[0].Status != "running" {
		t.Fatal("stale traffic after replacement")
	}
}

func TestRoundSummaryRerunsRulesAndInvalidatesInputs(t *testing.T) {
	today := now().Format("2006-01-02")
	hosts := trafficRows[trafficHost]{{"one.example.com", trafficHost{Routes: trafficRows[Stats]{{"Match\t\tdirect\tDIRECT", Stats{Conns: 1}}}}}}
	reports := []agentTraffic{{Agent{ID: "a", Name: "one"}, traffic{Days: trafficRows[trafficDay]{{today, trafficDay{Hosts: hosts}}}}}}
	var base trafficSummary
	var hit *Match
	match := func(string) *Match { return hit }
	run := func(days int, selected string) Listing {
		t.Helper()
		got := summarizeRound(reports, days, "uncovered", selected, nil, match, &base)
		want := summarize(reports, days, "uncovered", selected, nil, match)
		if !reflect.DeepEqual(got, want) {
			t.Fatal("reused summary differs from fresh one")
		}
		return got
	}
	run(7, "")
	first := base.groups[0]
	hit = &Match{}
	if got := run(7, ""); got.Items[0].Tags[0] != "pending" || first != base.groups[0] {
		t.Fatal("current rule was not used")
	}
	// The production reader returns a new Agent slice and reuses immutable day arrays.
	reports = slices.Clone(reports)
	reports[0].agent.Name = "two"
	if got := run(7, ""); got.Items[0].Agents[0] != "two" || first == base.groups[0] {
		t.Fatal("stale Agent label")
	}
	first = base.groups[0]
	run(1, "")
	if base.groups[0] == first {
		t.Fatal("stale retention cutoff")
	}
	if len(run(1, "b").Items) != 0 {
		t.Fatal("stale selected Agent")
	}
}

func BenchmarkAutoRefreshAfterProbe(b *testing.B) {
	for _, tc := range []struct {
		name  string
		reuse bool
	}{{"fresh", false}, {"round", true}} {
		b.Run(tc.name, func(b *testing.B) {
			svc, ctx, id := roundSummaryFixture(b)
			var base *trafficSummary
			if tc.reuse {
				base = new(trafficSummary)
			}
			load := func() {
				list, err := svc.loadDomains(ctx, id, 7, "uncovered", "", base)
				if err != nil || len(list.Items) != 1 || len(list.Items[0].Hosts) != 500 {
					b.Fatalf("domains %d: %v", len(list.Items), err)
				}
			}
			load()
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				host := "h0.example.com"
				if err := svc.saveProbes(ctx, "observed", map[string]Probe{host: {Host: host, CheckedAt: timestamp(), Target: new("proxy"), Confidence: 90}}); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				load()
			}
		})
	}
}
