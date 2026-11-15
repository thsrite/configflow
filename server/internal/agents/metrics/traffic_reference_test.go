package metrics

// Reference values produced by running backend/agents/traffic_history.py
// and traffic_store.py on the test server (testdata/gen_traffic_reference.py
// → testdata/traffic_reference.json): _target_key on IPs, IDNA, Unicode
// lowering and URL-like inputs; clean_report edge cases of both modules;
// fromtimestamp rounding; float floor division; and a query scenario
// (two flushes, every range and aggregate, daily compaction and pruning).

import (
	"os"
	"strconv"
	"testing"

	"configflow/internal/agents/trafficstore"
	"configflow/internal/jsonx"
)

func loadTrafficReference(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/traffic_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
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
	return jsonx.Obj("usage", usage, "targets", targets)
}

func TestTrafficReferenceTargetKeys(t *testing.T) {
	ref := loadTrafficReference(t)
	for input, want := range ref.GetObject("target_keys").All() {
		if got := targetKey(input); got != want {
			t.Errorf("_target_key(%q) = %q, Python %q", input, got, want)
		}
	}
}

func TestTrafficReferenceHistoryReports(t *testing.T) {
	ref := loadTrafficReference(t)
	now := ref.GetFloat("now")
	for name, value := range ref.GetObject("history_reports").All() {
		c := value.(*jsonx.Object)
		got := cleanedJSON(CleanTrafficReport(c.Value("input"), &now))
		if !jsonx.Equal(got, c.Value("output")) {
			t.Errorf("%s: go %s python %s", name, jsonx.Repr(got), jsonx.Repr(c.Value("output")))
		}
	}
	thisHour := int64(now) / Hour * Hour
	item := func(withDomain bool, hour int64) *jsonx.Object {
		o := jsonx.Obj("hour", hour, "source", "10.0.0.2", "chain", []any{"G", "N"}, "up", 1, "down", 2, "conns", 1)
		if withDomain {
			o.Set("domain", "www.example.com")
		}
		return o
	}
	repeat := func(n int, o *jsonx.Object) []any {
		out := make([]any, n)
		for i := range out {
			out[i] = o
		}
		return out
	}
	sizes := ref.GetObject("history_sizes")
	for name, report := range map[string]*jsonx.Object{
		"too-many":    jsonx.Obj("schema", 2, "usage", repeat(10001, item(false, thisHour)), "domains", repeat(10000, item(true, thisHour))),
		"exactly-max": jsonx.Obj("schema", 2, "usage", repeat(10000, item(false, 0)), "domains", repeat(10000, item(true, 0))),
	} {
		var got any
		if c := CleanTrafficReport(report, &now); c != nil {
			got = []any{len(c.Usage), len(c.Targets)}
		}
		if !jsonx.Equal(got, sizes.Value(name)) {
			t.Errorf("%s: go %v python %v", name, got, sizes.Value(name))
		}
	}
}

func TestTrafficReferenceStoreReports(t *testing.T) {
	ref := loadTrafficReference(t)
	for name, value := range ref.GetObject("store_reports").All() {
		c := value.(*jsonx.Object)
		cleaned, err := trafficstore.CleanReport(c.Value("input"))
		var got any = cleaned
		if cleaned == nil {
			got = nil
		}
		if err != nil {
			got = jsonx.Obj("error", "TypeError: "+err.Error())
		}
		if !jsonx.Equal(got, c.Value("output")) {
			t.Errorf("%s: go %s python %s", name, jsonx.Repr(got), jsonx.Repr(c.Value("output")))
		}
	}
}

func TestTrafficReferenceTimeHelpers(t *testing.T) {
	ref := loadTrafficReference(t)
	for text, want := range ref.GetObject("iso").All() {
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			t.Fatal(err)
		}
		if got := isoUTC(value); got != want {
			t.Errorf("fromtimestamp(%s) = %s, Python %s", text, got, want)
		}
	}
	for text, want := range ref.GetObject("floor_div").All() {
		value, _ := strconv.ParseFloat(text, 64)
		got := []any{jsonx.Float(pyFloorDiv(value, Day)), jsonx.Float(pyFloorDiv(value, Hour))}
		if !jsonx.Equal(got, want) {
			t.Errorf("%s // day, hour = %v, Python %v", text, got, want)
		}
	}
}

func TestTrafficReferenceQueryScenario(t *testing.T) {
	ref := loadTrafficReference(t)
	now := ref.GetFloat("now") + 3*86400
	var usageRows []UsageRow
	var targetRows []TargetRow
	for _, entry := range ref.GetList("scenario") {
		row := entry.([]any)
		n := func(i int) int64 { v, _ := row[i].(jsonx.Number).Int64(); return v }
		s := func(i int) string { return row[i].(string) }
		if s(0) == "usage" {
			usageRows = append(usageRows, UsageRow{n(1), s(2), s(3), n(4), n(5), n(6)})
		} else {
			targetRows = append(targetRows, TargetRow{n(1), s(2), s(3), s(4), n(5), n(6), n(7)})
		}
	}
	results := ref.GetObject("scenario_results")
	h := NewTrafficHistory(t.TempDir())
	var firstUsage []UsageRow
	for i, row := range usageRows {
		if i < 5 {
			firstUsage = append(firstUsage, row)
		}
	}
	h.AddReport("a1", &CleanedTraffic{Usage: firstUsage, Targets: targetRows[:3]})
	h.AddReport("a2", &CleanedTraffic{Usage: usageRows[:1]})
	check := func(name string, got any) {
		t.Helper()
		if !jsonx.Equal(got, results.Value(name)) {
			t.Errorf("%s:\n  go     %s\n  python %s", name, jsonx.Repr(got), jsonx.Repr(results.Value(name)))
		}
	}
	flush := func(name string) {
		result, err := h.Flush(30, &now)
		if err != nil {
			t.Fatal(err)
		}
		check(name, result.Object())
	}
	flush("flush1")
	h.AddReport("a1", &CleanedTraffic{Usage: usageRows[5:], Targets: targetRows[3:]})
	for rangeKey := range Ranges {
		for _, by := range Aggregates {
			result, err := h.Query("a1", rangeKey, by, &now)
			if err != nil {
				t.Fatal(err)
			}
			check(rangeKey+"/"+by, result)
		}
	}
	flush("flush2")
	result, _ := h.Query("a1", "30d", "destination", &now)
	check("after/30d/destination", result)
	later := now + 0.5
	result, _ = h.Query("a1", "7d", "source", &later)
	check("after/7d/source", result)
}
