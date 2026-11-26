package metrics

import (
	"configflow/internal/timefmt"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"configflow/internal/testjson"
)

func TestNativeFilesRetainPointsAndSkipMissingSamples(t *testing.T) {
	c := useClock(t)
	dir := t.TempDir()
	h := newTestHistory(t, dir, MaxPointsPerAgent)
	stamp := timefmt.Naive(naiveNow())
	raw := `{"timestamp":"` + stamp + `","cpu":{"usage_percent":NaN},"network":{"bytes_sent":Infinity,"bytes_recv":7}}`
	if err := os.WriteFile(h.agentFile("a"), []byte(raw+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Load(t.Context(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	got := get(t, h, "a", 24)
	if len(got) != 1 {
		t.Fatalf("lost raw point: %v", got)
	}
	if val, ok := testjson.Path(got[0], "cpu", "usage_percent"); !ok || val != nil {
		t.Fatalf("lost raw point: %s", got[0])
	}
	summary, err := h.Summarize("a", big.NewInt(1))
	if err != nil || string(summary.CPU.Avg) != "null" {
		t.Fatalf("all-null sample: %+v %v", summary, err)
	}
	traffic, err := h.TrafficStatistics("a", "total")
	if err != nil || !testjson.Equal(emptyTrafficStats("total"), traffic) {
		t.Fatalf("%+v %v", traffic, err)
	}
	trend, err := h.TrafficTrend("a", big.NewInt(24), big.NewInt(5))
	if err != nil || len(trend) != 0 {
		t.Fatalf("%+v %v", trend, err)
	}
	add(t, h, c, 1, "a", time.Second)
	summary, err = h.Summarize("a", big.NewInt(1))
	if err != nil || !testjson.Equal(0, summary.CPU.Avg) {
		t.Fatalf("valid sample not retained: %+v %v", summary, err)
	}
	// A forced rewrite repairs stored literals without dropping the point.
	h.rewrite["a"] = true
	if _, err := h.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(h.agentFile("a"))
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if !json.Valid([]byte(line)) {
			t.Fatalf("invalid line: %s", line)
		}
	}
}

func FuzzSavedMetricLines(f *testing.F) {
	for _, line := range []string{`{"timestamp":"2026-11-15T12:00:00","cpu":{"usage_percent":NaN}}`, `{"timestamp":"2026-11-15T12:00:00","network":{"bytes_sent":1e400}}`, `{"timestamp":"2026-11-15T12:00:00","extra":[true,null,"Infinity"]}`, `{"timestamp":1}`} {
		f.Add(line)
	}
	f.Fuzz(func(t *testing.T, line string) {
		points, _ := parseLines([]string{line}, time.Time{})
		for _, p := range points {
			if !json.Valid([]byte(p.line)) {
				t.Fatalf("native stored line invalid: %q", p.line)
			}
		}
	})
}

func TestMemoryCacheDoesNotApplyStoredJSONRepair(t *testing.T) {
	useClock(t)
	h := newTestHistory(t, t.TempDir(), MaxPointsPerAgent)
	at := naiveNow()
	h.points["a"] = []point{{at, `{"timestamp":"` + timefmt.Naive(at) + `","cpu":{"usage_percent":NaN}}`}}
	samples, err := h.Samples("a", nil)
	if err == nil {
		for _, raw := range samples {
			if _, parseErr := testjson.Parse(raw); parseErr != nil {
				return
			}
		}
		t.Fatal("invalid native cache line was silently repaired")
	}
}
