package metrics

// Reference values produced by running backend/agents/traffic_history.py
// and traffic_store.py on the test server (testdata/gen_traffic_reference.py
// → testdata/traffic_reference.json): _target_key on IPs, IDNA, Unicode
// lowering and URL-like inputs; clean_report edge cases of both modules;
// fromtimestamp rounding; float floor division; and a query scenario
// (two flushes, every range and aggregate, daily compaction and pruning).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"

	"configflow/internal/agents/trafficstore"
	"configflow/internal/testjson"
)

type trafficReference struct {
	Now            float64           `json:"now"`
	TargetKeys     map[string]string `json:"target_keys"`
	HistoryReports map[string]struct {
		Input  json.RawMessage `json:"input"`
		Output json.RawMessage `json:"output"`
	} `json:"history_reports"`
	HistorySizes map[string]json.RawMessage `json:"history_sizes"`
	StoreReports map[string]struct {
		Input  json.RawMessage `json:"input"`
		Output json.RawMessage `json:"output"`
	} `json:"store_reports"`
	ISO             map[string]string          `json:"iso"`
	FloorDiv        map[string]json.RawMessage `json:"floor_div"`
	Scenario        [][]json.RawMessage        `json:"scenario"`
	ScenarioResults map[string]json.RawMessage `json:"scenario_results"`
}

func loadTrafficReference(t *testing.T) trafficReference {
	t.Helper()
	data, err := os.ReadFile("testdata/traffic_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref trafficReference
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

func cleanedJSON(c *CleanedTraffic) any {
	if c == nil {
		return nil
	}
	usage := []any{}
	for _, r := range c.Usage {
		usage = append(usage, []any{r.Bucket, r.Source, r.Chain, r.Up, r.Down, r.Conns})
	}
	targets := []any{}
	for _, r := range c.Targets {
		targets = append(targets, []any{r.Bucket, r.Source, r.Target, r.Chain, r.Up, r.Down, r.Conns})
	}
	return map[string]any{"usage": usage, "targets": targets}
}

func TestTrafficReferenceTargetKeys(t *testing.T) {
	ref := loadTrafficReference(t)
	for input, want := range ref.TargetKeys {
		if got := targetKey(input); got != want {
			t.Errorf("_target_key(%q) = %q, Python %q", input, got, want)
		}
	}
}

func TestTrafficReferenceHistoryReports(t *testing.T) {
	ref := loadTrafficReference(t)
	now := ref.Now
	for name, c := range ref.HistoryReports {
		got := cleanedJSON(DecodeTrafficReport(c.Input, &now))
		if diff := testjson.Diff(c.Output, got); diff != "" {
			t.Errorf("%s: %s", name, diff)
		}
	}
	thisHour := int64(now) / Hour * Hour
	item := func(withDomain bool, hour int64) map[string]any {
		o := map[string]any{"hour": hour, "source": "10.0.0.2", "chain": []any{"G", "N"}, "up": 1, "down": 2, "conns": 1}
		if withDomain {
			o["domain"] = "www.example.com"
		}
		return o
	}
	repeat := func(n int, o map[string]any) []any {
		out := make([]any, n)
		for i := range out {
			out[i] = o
		}
		return out
	}
	sizes := ref.HistorySizes
	for name, report := range map[string]map[string]any{
		"too-many":    {"schema": 2, "usage": repeat(10001, item(false, thisHour)), "domains": repeat(10000, item(true, thisHour))},
		"exactly-max": {"schema": 2, "usage": repeat(10000, item(false, 0)), "domains": repeat(10000, item(true, 0))},
	} {
		var got any
		if c := DecodeTrafficReport(mustMarshal(t, report), &now); c != nil {
			got = []any{len(c.Usage), len(c.Targets)}
		}
		if diff := testjson.Diff(sizes[name], got); diff != "" {
			t.Errorf("%s: %s", name, diff)
		}
	}
}

func TestTrafficReferenceStoreReports(t *testing.T) {
	ref := loadTrafficReference(t)
	for name, c := range ref.StoreReports {
		cleaned, err := trafficstore.DecodeReport(c.Input)
		var result any = cleaned
		if cleaned == nil {
			result = nil
		}
		if err != nil {
			// The reference records Python's exception text; only the
			// refusal itself must match.
			if _, want := testjson.Path(c.Output, "error"); !want || !errors.Is(err, trafficstore.ErrInvalid) {
				t.Errorf("%s: go %v, python %s", name, err, c.Output)
			}
			continue
		}
		if diff := testjson.Diff(c.Output, result); diff != "" {
			t.Errorf("%s: %s", name, diff)
		}
	}
}

func TestTrafficReferenceTimeHelpers(t *testing.T) {
	ref := loadTrafficReference(t)
	for text, want := range ref.ISO {
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			t.Fatal(err)
		}
		if got := isoUTC(value); got != want {
			t.Errorf("fromtimestamp(%s) = %s, Python %s", text, got, want)
		}
	}
	for text, want := range ref.FloorDiv {
		value, _ := strconv.ParseFloat(text, 64)
		got := []any{floatJSON(pyFloorDiv(value, Day)), floatJSON(pyFloorDiv(value, Hour))}
		if diff := testjson.Diff(want, got); diff != "" {
			t.Errorf("%s // day, hour: %s", text, diff)
		}
	}
}

func TestTrafficReferenceQueryScenario(t *testing.T) {
	ref := loadTrafficReference(t)
	now := ref.Now + 3*86400
	var usageRows []UsageRow
	var targetRows []TargetRow
	for _, row := range ref.Scenario {
		n := func(i int) int64 {
			var v int64
			if err := json.Unmarshal(row[i], &v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		s := func(i int) string {
			var v string
			if err := json.Unmarshal(row[i], &v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		if s(0) == "usage" {
			usageRows = append(usageRows, UsageRow{n(1), s(2), s(3), n(4), n(5), n(6)})
		} else {
			targetRows = append(targetRows, TargetRow{n(1), s(2), s(3), s(4), n(5), n(6), n(7)})
		}
	}
	results := ref.ScenarioResults
	h := NewTrafficHistory(t.TempDir())
	var firstUsage []UsageRow
	for i, row := range usageRows {
		if i < 5 {
			firstUsage = append(firstUsage, row)
		}
	}
	_ = h.AddTraffic(context.Background(), "a1", &CleanedTraffic{Usage: firstUsage, Targets: targetRows[:3]})
	_ = h.AddTraffic(context.Background(), "a2", &CleanedTraffic{Usage: usageRows[:1]})
	check := func(name string, got any) {
		t.Helper()
		if diff := testjson.Diff(results[name], got); diff != "" {
			t.Errorf("%s: %s", name, diff)
		}
	}
	flush := func(name string) {
		result, err := h.Flush(30, &now)
		if err != nil {
			t.Fatal(err)
		}
		check(name, map[string]any{"rows": result.Rows, "compacted": result.Compacted, "pruned": result.Pruned})
	}
	flush("flush1")
	_ = h.AddTraffic(context.Background(), "a1", &CleanedTraffic{Usage: usageRows[5:], Targets: targetRows[3:]})
	for rangeKey := range Ranges {
		for _, by := range Aggregates {
			result, err := h.QueryTraffic(context.Background(), "a1", rangeKey, by, &now)
			if err != nil {
				t.Fatal(err)
			}
			check(rangeKey+"/"+by, result)
		}
	}
	flush("flush2")
	result, _ := h.QueryTraffic(context.Background(), "a1", "30d", "destination", &now)
	check("after/30d/destination", result)
	later := now + 0.5
	result, _ = h.QueryTraffic(context.Background(), "a1", "7d", "source", &later)
	check("after/7d/source", result)
}
