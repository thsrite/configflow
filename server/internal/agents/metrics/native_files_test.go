package metrics

import (
	"encoding/json"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
)

func TestNativeFilesRetainPointsAndSkipMissingSamples(t *testing.T) {
	c := useClock(t)
	dir := t.TempDir()
	h := NewHistory(dir, MaxPointsPerAgent)
	stamp := isoformat(naiveNow())
	raw := `{"timestamp":"` + stamp + `","cpu":{"usage_percent":NaN},"network":{"bytes_sent":Infinity,"bytes_recv":7}}`
	if err := os.WriteFile(h.agentFile("a"), []byte(raw+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Load([]string{"a"}); err != nil {
		t.Fatal(err)
	}
	got := get(t, h, "a", 24)
	if len(got) != 1 || got[0].(*jsonx.Object).GetObject("cpu").Value("usage_percent") != nil {
		t.Fatalf("lost raw point: %v", got)
	}
	summary, err := h.GetMetricsSummary("a", big.NewInt(1))
	if err != nil || summary.GetObject("cpu").Value("avg") != nil {
		t.Fatalf("all-null sample: %v %v", summary, err)
	}
	traffic, err := h.GetTrafficStats("a", "total")
	if err != nil || !jsonx.Equal(traffic, emptyTrafficStats("total")) {
		t.Fatalf("%v %v", traffic, err)
	}
	trend, err := h.GetTrafficTrend("a", big.NewInt(24), big.NewInt(5))
	if err != nil || len(trend) != 0 {
		t.Fatalf("%v %v", trend, err)
	}
	add(t, h, c, 1, "a", time.Second)
	summary, err = h.GetMetricsSummary("a", big.NewInt(1))
	if err != nil || summary.GetObject("cpu").GetInt("avg") != 0 {
		t.Fatalf("valid sample not retained: %v %v", summary, err)
	}
	// A forced rewrite repairs stored literals without dropping the point.
	h.rewrite["a"] = true
	if _, err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(h.agentFile("a"))
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if !json.Valid([]byte(line)) {
			t.Fatalf("invalid line: %s", line)
		}
	}
}

func TestPythonLoadsAndSavesNativeNullFilesButLegacyMathFails(t *testing.T) {
	home, _ := os.UserHomeDir()
	root, _ := filepath.Abs("../../../..")
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	dir := t.TempDir()
	stamp := time.Now().Format("2006-01-02T15:04:05")
	point := struct {
		Timestamp string              `json:"timestamp"`
		CPU       map[string]*float64 `json:"cpu"`
		Network   map[string]*float64 `json:"network"`
	}{stamp, map[string]*float64{"usage_percent": nil}, map[string]*float64{"bytes_sent": nil, "bytes_recv": nil}}
	encoded, err := jsonresp.Encode(point)
	if err != nil {
		t.Fatal(err)
	}
	for _, legacy := range []bool{false, true} {
		suffix := FileSuffix
		data := append(append([]byte(nil), encoded...), '\n')
		if legacy {
			suffix = LegacySuffix
			data, err = jsonresp.Encode(struct {
				Metrics []json.RawMessage `json:"metrics"`
			}{[]json.RawMessage{encoded}})
			if err != nil {
				t.Fatal(err)
			}
		}
		sub := filepath.Join(dir, suffix)
		if err = os.MkdirAll(sub, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(sub, "a"+suffix), data, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(python, "-c", `import json,sys; from backend.agents.metrics_history import MetricsHistory
h=MetricsHistory(sys.argv[1]); h.load(['a']); assert len(h.get_metrics('a'))==1
for call in (lambda:h.get_metrics_summary('a'),lambda:h.get_traffic_stats('a')):
 try: call()
 except TypeError: pass
 else: raise AssertionError('legacy math unexpectedly accepted None')
h._rewrite.add('a'); h.flush()
for line in open(h._get_agent_file('a')): assert json.loads(line)['cpu']['usage_percent'] is None
print('Python loaded and saved 1 null-bearing point; summary/traffic TypeError')`, sub)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "PYTHONPATH="+root)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", suffix, err, out)
		}
		// Python's real flush writes JSONL; native loading reads it back too.
		round := NewHistory(sub, MaxPointsPerAgent)
		if _, err := round.Load([]string{"a"}); err != nil {
			t.Fatal(err)
		}
		points, err := round.GetMetrics("a", nil)
		if err != nil || len(points) != 1 || points[0].(*jsonx.Object).GetObject("cpu").Value("usage_percent") != nil {
			t.Fatalf("Python→Go: %v %v", points, err)
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
