package metrics

// Reference results of metrics_history.py produced by CPython 3.11 on the
// test server (testdata/gen_a04_reference.py → testdata/a04_reference.json):
// reading saved JSON Lines and old JSON files, and the summary / traffic
// statistics / traffic trend arithmetic on points held in memory.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonresp"
	"configflow/internal/taskstatus/isoformtest"
	"configflow/internal/testjson"
)

type a04Reference struct {
	Cutoff       string            `json:"cutoff"`
	ReadFile     []a04ReadFileCase `json:"read_file"`
	Legacy       []a04LegacyCase   `json:"legacy"`
	Summary      []json.RawMessage `json:"summary"`
	TrafficStats []json.RawMessage `json:"traffic_stats"`
	Trend        []json.RawMessage `json:"trend"`
}

type a04ReadFileCase struct {
	Name   string     `json:"name"`
	Data   string     `json:"data"`
	Count  int        `json:"count"`
	Points [][]string `json:"points"`
}

type a04LegacyCase struct {
	Name   string     `json:"name"`
	Data   string     `json:"data"`
	Error  *string    `json:"error"`
	Detail string     `json:"detail"`
	Points [][]string `json:"points"`
}

type a04MetricCase struct {
	Name     string      `json:"name"`
	Now      string      `json:"now"`
	Points   []string    `json:"points"`
	Hours    json.Number `json:"hours"`
	Interval json.Number `json:"interval"`
	Period   string      `json:"period"`
	Result   string      `json:"result"`
}

func loadA04Reference(t *testing.T) a04Reference {
	t.Helper()
	data, err := os.ReadFile("testdata/a04_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref a04Reference
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

func decodeMetricCase(t *testing.T, raw json.RawMessage) a04MetricCase {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var item a04MetricCase
	if err := dec.Decode(&item); err != nil {
		t.Fatal(err)
	}
	return item
}

func naiveOf(t *testing.T, text string) time.Time {
	t.Helper()
	at, aware, ok := parseNaiveISO(text)
	if !ok || aware {
		t.Fatalf("bad naive time %q", text)
	}
	return at
}

func pointsText(points []point) [][]string {
	out := [][]string{}
	for _, p := range points {
		out = append(out, []string{isoformat(p.at), p.line})
	}
	return out
}

func pyErrorText(err error) string {
	var typeError *TypeError
	var overflow *OverflowError
	var attribute *AttributeError
	switch {
	case errors.As(err, &typeError):
		return "TypeError: " + typeError.Message
	case errors.As(err, &overflow):
		return "OverflowError: " + overflow.Message
	case errors.As(err, &attribute):
		return "AttributeError: " + attribute.Message
	}
	return fmt.Sprintf("%T: %v", err, err)
}

func compareReferenceResult(t *testing.T, value any, err error, want string) {
	t.Helper()
	if err != nil {
		if got := pyErrorText(err); got != want {
			t.Errorf("\n got  %s\n want %s", got, want)
		}
		return
	}
	wantParsed, parseErr := testjson.Parse([]byte(want))
	if parseErr != nil {
		t.Errorf("expected error %q, got value %+v", want, value)
		return
	}
	if diff := testjson.Diff(wantParsed, value, testjson.CompareOptions{DistinguishNumberLiterals: true}); diff != "" {
		gotBytes, _ := json.Marshal(value)
		t.Errorf("%s\n got  %s\n want %s", diff, gotBytes, want)
	}
}

func decodeBase64(t *testing.T, encoded string) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReferenceReadSavedLines(t *testing.T) {
	ref := loadA04Reference(t)
	limit := naiveOf(t, ref.Cutoff)
	h := NewHistory(t.TempDir(), MaxPointsPerAgent)
	for _, item := range ref.ReadFile {
		if err := os.WriteFile(h.agentFile("a"), decodeBase64(t, item.Data), 0o600); err != nil {
			t.Fatal(err)
		}
		points, count, err := h.readFile("a", limit)
		if err != nil {
			t.Fatal(err)
		}
		// Lines whose timestamp uses a CPython-only ISO spelling Python
		// accepted are no longer chased; Go skips them.
		want := item.Points[:0:0]
		for _, row := range item.Points {
			var line struct {
				Timestamp string `json:"timestamp"`
			}
			if json.Unmarshal([]byte(row[1]), &line) == nil && !isoformtest.Stored(line.Timestamp) {
				continue
			}
			want = append(want, row)
		}
		item.Points = want
		if count != item.Count {
			t.Errorf("%s: count %d, Python %d", item.Name, count, item.Count)
		}
		if item.Name == "numbers" {
			assertNativeNumberLines(t, points)
			continue
		}
		if !equalPointRecords(t, points, item.Points) {
			got, _ := json.Marshal(pointsText(points))
			want, _ := json.Marshal(item.Points)
			t.Errorf("%s:\n got  %s\n want %s", item.Name, got, want)
		}
	}
}

func TestReferenceReadOldJSONFiles(t *testing.T) {
	ref := loadA04Reference(t)
	limit := naiveOf(t, ref.Cutoff)
	h := NewHistory(t.TempDir(), MaxPointsPerAgent)
	for _, item := range ref.Legacy {
		if err := os.WriteFile(h.legacyFile("a"), decodeBase64(t, item.Data), 0o600); err != nil {
			t.Fatal(err)
		}
		points, err := h.readLegacy("a", limit)
		switch {
		case item.Error == nil:
			if err != nil {
				t.Errorf("%s: %v, Python read it", item.Name, err)
			} else if !equalPointRecords(t, points, item.Points) {
				t.Errorf("%s: points %v, Python %v", item.Name, pointsText(points), item.Points)
			}
		case *item.Error == "format":
			var format legacyFormatError
			if !errors.As(err, &format) {
				t.Errorf("%s: %v, Python ValueError (%s)", item.Name, err, item.Detail)
			}
		default:
			if err == nil {
				t.Errorf("%s: %v, Python %v", item.Name, err, *item.Error)
			}
		}
	}
}

// referenceHistory holds the case's points (timestamp parsed from each
// line) with datetime.now() fixed at the case's now.
func referenceHistory(t *testing.T, item a04MetricCase) *History {
	t.Helper()
	current := naiveOf(t, item.Now)
	previous := now
	now = func() time.Time {
		return time.Date(current.Year(), current.Month(), current.Day(), current.Hour(), current.Minute(),
			current.Second(), current.Nanosecond(), time.Local)
	}
	t.Cleanup(func() { now = previous })
	h := NewHistory(t.TempDir(), MaxPointsPerAgent)
	var points []point
	for _, line := range item.Points {
		// Build the fixture's cache through the approved persisted-data decoder.
		// Samples itself is strict: native cache lines must already be valid JSON.
		var value map[string]json.RawMessage
		if err := jsonresp.DecodeStored([]byte(line), &value); err != nil {
			t.Fatal(err)
		}
		encoded, err := jsonresp.Encode(value)
		if err != nil {
			t.Fatal(err)
		}
		var ts string
		_ = json.Unmarshal(value["timestamp"], &ts)
		points = append(points, point{naiveOf(t, ts), string(encoded)})
	}
	h.points["a"] = points
	return h
}

func bigNumber(t *testing.T, n json.Number) *big.Int {
	t.Helper()
	value, ok := new(big.Int).SetString(string(n), 10)
	if !ok {
		t.Fatalf("%q is not an int", n)
	}
	return value
}

func TestReferenceMetricsSummary(t *testing.T) {
	for _, raw := range loadA04Reference(t).Summary {
		item := decodeMetricCase(t, raw)
		t.Run(item.Name, func(t *testing.T) {
			summary, err := referenceHistory(t, item).Summarize("a", bigNumber(t, item.Hours))
			if expected, changed := nativeMetricExpected(t, "summary", item.Name, raw); changed {
				if err != nil || !testjson.Equal(expected, summary) {
					t.Fatalf("native summary: %+v %v, want %v", summary, err, expected)
				}
				return
			}
			if expected, changed := nativeSummaryExpected(item); changed {
				if err != nil || !testjson.Equal(expected, summary) {
					t.Fatalf("native missing-sample summary: %+v %v, want %v", summary, err, expected)
				}
				return
			}
			compareReferenceResult(t, summary, err, item.Result)
		})
	}
}

func TestReferenceTrafficStats(t *testing.T) {
	for _, raw := range loadA04Reference(t).TrafficStats {
		item := decodeMetricCase(t, raw)
		t.Run(item.Name, func(t *testing.T) {
			stats, err := referenceHistory(t, item).TrafficStatistics("a", item.Period)
			if expected, changed := nativeMetricExpected(t, "traffic_stats", item.Name, raw); changed {
				if err != nil || !testjson.Equal(expected, stats) {
					t.Fatalf("native stats: %+v %v, want %v", stats, err, expected)
				}
				return
			}
			if expected, changed := nativeTrafficExpected(item); changed {
				if err != nil || !testjson.Equal(expected, stats) {
					t.Fatalf("native missing-counter stats: %+v %v, want %v", stats, err, expected)
				}
				return
			}
			compareReferenceResult(t, stats, err, item.Result)
		})
	}
}

func TestReferenceTrafficTrend(t *testing.T) {
	for _, raw := range loadA04Reference(t).Trend {
		item := decodeMetricCase(t, raw)
		t.Run(item.Name, func(t *testing.T) {
			trend, err := referenceHistory(t, item).TrafficTrend("a", bigNumber(t, item.Hours), bigNumber(t, item.Interval))
			if expected, changed := nativeMetricExpected(t, "trend", item.Name, raw); changed {
				if err != nil || !testjson.Equal(expected, trend) {
					t.Fatalf("native trend: %+v %v, want %v", trend, err, expected)
				}
				return
			}
			if expected, changed := nativeTrendExpected(item); changed {
				if err != nil || !testjson.Equal(expected, trend) {
					t.Fatalf("native missing-counter trend: %+v %v, want %v", trend, err, expected)
				}
				return
			}
			compareReferenceResult(t, trend, err, item.Result)
		})
	}
}

// Only serialization changes and the approved non-finite repair are applied to
// old file goldens. Business records, timestamps, cardinality and values match.
func equalPointRecords(t *testing.T, points []point, want [][]string) bool {
	t.Helper()
	if len(points) != len(want) {
		return false
	}
	for i, p := range points {
		row := want[i]
		if isoformat(p.at) != row[0] {
			return false
		}
		var gotValue, wantValue any
		if err := jsonresp.DecodeStored([]byte(p.line), &gotValue); err != nil {
			t.Fatal(err)
		}
		if err := jsonresp.DecodeStored([]byte(row[1]), &wantValue); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotValue, wantValue) {
			return false
		}
	}
	return true
}

func assertNativeNumberLines(t *testing.T, points []point) {
	t.Helper()
	if len(points) != 5 {
		t.Fatalf("native number records=%d; want 5 (includes 4301 digits)", len(points))
	}
	expected := []map[string]json.RawMessage{
		{"timestamp": json.RawMessage(`"2026-11-15T10:07:00"`), "v": json.RawMessage(`[null,null,null,-0.0,1e400,-1e400,1E5,1.0,1.50,0.1,1e-400,12345678901234567890123]`)},
		{"timestamp": json.RawMessage(`"2026-11-15T10:07:01"`), "v": json.RawMessage(strings.Repeat("9", 4300))},
		{"timestamp": json.RawMessage(`"2026-11-15T10:07:02"`), "v": json.RawMessage(strings.Repeat("9", 4301))},
		{"timestamp": json.RawMessage(`"2026-11-15T10:07:03"`), "v": json.RawMessage(`-0`)},
		{"timestamp": json.RawMessage(`"2026-11-15T10:07:04"`), "v": json.RawMessage(`1.7976931348623157e308`), "w": json.RawMessage(`5e-324`), "x": json.RawMessage(`2.5e-308`)},
	}
	for i, p := range points {
		var got map[string]json.RawMessage
		if err := json.Unmarshal([]byte(p.line), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, expected[i]) {
			t.Fatalf("native number row %d differs", i)
		}
	}
}

// Approved arithmetic changes are an explicit per-fixture table. Old Python
// goldens remain immutable; all other rows still compare their complete result.
func nativeSummaryExpected(item a04MetricCase) (map[string]any, bool) {
	values := map[string][3]any{
		"none value": {nil, nil, nil}, "huge int mean overflow": {nil, nil, nil},
		"huge int then float": {1.5, 1.5, 1.5}, "float then huge int": {1.5, 1.5, 1.5},
		"infinity and big int": {nil, nil, nil}, "infinities": {nil, nil, nil},
		"nan first": {1.5, 2, 1}, "nan middle": {1.5, 2, 1},
	}
	value, ok := values[item.Name]
	if !ok {
		return nil, false
	}
	expected := map[string]any{
		"cpu": zeroStats(), "memory": zeroStats(), "disk": zeroStats(),
		"network_sent": zeroStats(), "network_recv": zeroStats(),
		"data_points": len(item.Points), "time_range_hours": item.Hours,
	}
	key := "cpu"
	if item.Name == "none value" {
		key = "memory"
	}
	expected[key] = map[string]any{"avg": value[0], "max": value[1], "min": value[2]}
	return expected, true
}

func nativeTrafficExpected(item a04MetricCase) (map[string]any, bool) {
	switch item.Name {
	case "none values", "nan", "infinity", "huge equal ints":
		return emptyTrafficStats(item.Period), true
	case "huge ints":
		result := emptyTrafficStats(item.Period)
		result["start_time"] = "2026-11-15T11:01:00"
		result["end_time"] = "2026-11-15T11:01:00"
		result["data_points"] = 1
		return result, true
	}
	return nil, false
}

func nativeTrendExpected(item a04MetricCase) ([]any, bool) {
	switch item.Name {
	case "huge":
		return []any{map[string]any{"timestamp": "2026-11-15T11:00:00", "bytes_sent": 0, "bytes_recv": 0, "speed_sent": 0, "speed_recv": 0}}, true
	case "nan":
		return []any{map[string]any{"timestamp": "2026-11-15T11:00:00", "bytes_sent": 1, "bytes_recv": 1, "speed_sent": 0, "speed_recv": 0}}, true
	}
	return nil, false
}
