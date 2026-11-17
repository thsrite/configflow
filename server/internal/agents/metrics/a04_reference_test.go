package metrics

// Reference results of metrics_history.py produced by CPython 3.11 on the
// test server (testdata/gen_a04_reference.py → testdata/a04_reference.json):
// reading saved JSON Lines and old JSON files, and the summary / traffic
// statistics / traffic trend arithmetic on points held in memory.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"encoding/json"
	"reflect"
	"strings"
)

func loadA04Reference(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/a04_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

func naiveOf(t *testing.T, text string) time.Time {
	t.Helper()
	at, aware, ok := parseNaiveISO(text)
	if !ok || aware {
		t.Fatalf("bad naive time %q", text)
	}
	return at
}

func pointsText(points []point) []any {
	out := []any{}
	for _, p := range points {
		out = append(out, []any{isoformat(p.at), p.line})
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

func referenceResult(t *testing.T, value any, err error) string {
	t.Helper()
	if err != nil {
		return pyErrorText(err)
	}
	data, err := jsonx.Encode(value, jsonx.Options{EnsureASCII: true, SortKeys: true, Compact: true})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func caseData(t *testing.T, item *jsonx.Object) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(item.GetString("data"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReferenceReadSavedLines(t *testing.T) {
	ref := loadA04Reference(t)
	limit := naiveOf(t, ref.GetString("cutoff"))
	h := NewHistory(t.TempDir(), MaxPointsPerAgent)
	for _, raw := range ref.GetList("read_file") {
		item := raw.(*jsonx.Object)
		if err := os.WriteFile(h.agentFile("a"), caseData(t, item), 0o600); err != nil {
			t.Fatal(err)
		}
		points, count, err := h.readFile("a", limit)
		if err != nil {
			t.Fatal(err)
		}
		if count != int(item.GetInt("count")) {
			t.Errorf("%s: count %d, Python %d", item.GetString("name"), count, item.GetInt("count"))
		}
		if item.GetString("name") == "numbers" {
			assertNativeNumberLines(t, points)
			continue
		}
		if !equalPointRecords(t, points, item.GetList("points")) {
			got, _ := jsonx.Encode(pointsText(points), jsonx.Options{EnsureASCII: true})
			want, _ := jsonx.Encode(item.Value("points"), jsonx.Options{EnsureASCII: true})
			t.Errorf("%s:\n got  %s\n want %s", item.GetString("name"), got, want)
		}
	}
}

func TestReferenceReadOldJSONFiles(t *testing.T) {
	ref := loadA04Reference(t)
	limit := naiveOf(t, ref.GetString("cutoff"))
	h := NewHistory(t.TempDir(), MaxPointsPerAgent)
	for _, raw := range ref.GetList("legacy") {
		item := raw.(*jsonx.Object)
		name := item.GetString("name")
		if err := os.WriteFile(h.legacyFile("a"), caseData(t, item), 0o600); err != nil {
			t.Fatal(err)
		}
		points, err := h.readLegacy("a", limit)
		want, failed := item.Get("error")
		switch {
		case !failed:
			if err != nil {
				t.Errorf("%s: %v, Python read it", name, err)
			} else if !equalPointRecords(t, points, item.GetList("points")) {
				t.Errorf("%s: points %v, Python %v", name, pointsText(points), item.Value("points"))
			}
		case want == "format":
			var format legacyFormatError
			if !errors.As(err, &format) {
				t.Errorf("%s: %v, Python ValueError (%s)", name, err, item.GetString("detail"))
			}
		default:
			if err == nil {
				t.Errorf("%s: %v, Python %v", name, err, want)
			}
		}
	}
}

// referenceHistory holds the case's points (timestamp parsed from each
// line) with datetime.now() fixed at the case's now.
func referenceHistory(t *testing.T, item *jsonx.Object) *History {
	t.Helper()
	current := naiveOf(t, item.GetString("now"))
	previous := now
	now = func() time.Time {
		return time.Date(current.Year(), current.Month(), current.Day(), current.Hour(), current.Minute(),
			current.Second(), current.Nanosecond(), time.Local)
	}
	t.Cleanup(func() { now = previous })
	h := NewHistory(t.TempDir(), MaxPointsPerAgent)
	var points []point
	for _, raw := range item.GetList("points") {
		line := raw.(string)
		value, err := jsonx.ParseString(line)
		if err != nil {
			t.Fatal(err)
		}
		points = append(points, point{naiveOf(t, value.(*jsonx.Object).GetString("timestamp")), line})
	}
	h.points["a"] = points
	return h
}

func bigArg(t *testing.T, item *jsonx.Object, key string) *big.Int {
	t.Helper()
	number, _ := item.GetNumber(key)
	value, ok := number.BigInt()
	if !ok {
		t.Fatalf("%s is not an int", key)
	}
	return value
}

func TestReferenceMetricsSummary(t *testing.T) {
	for _, raw := range loadA04Reference(t).GetList("summary") {
		item := raw.(*jsonx.Object)
		t.Run(item.GetString("name"), func(t *testing.T) {
			summary, err := referenceHistory(t, item).GetMetricsSummary("a", bigArg(t, item, "hours"))
			if expected, changed := nativeSummaryExpected(item); changed {
				if err != nil || !jsonx.Equal(summary, expected) {
					t.Fatalf("native missing-sample summary: %v %v, want %v", summary, err, expected)
				}
				return
			}
			if got, want := referenceResult(t, summary, err), item.GetString("result"); got != want {
				t.Errorf("\n got  %s\n want %s", got, want)
			}
		})
	}
}

func TestReferenceTrafficStats(t *testing.T) {
	for _, raw := range loadA04Reference(t).GetList("traffic_stats") {
		item := raw.(*jsonx.Object)
		t.Run(item.GetString("name"), func(t *testing.T) {
			stats, err := referenceHistory(t, item).GetTrafficStats("a", item.GetString("period"))
			if expected, changed := nativeTrafficExpected(item); changed {
				if err != nil || !jsonx.Equal(stats, expected) {
					t.Fatalf("native missing-counter stats: %v %v, want %v", stats, err, expected)
				}
				return
			}
			if got, want := referenceResult(t, stats, err), item.GetString("result"); got != want {
				t.Errorf("\n got  %s\n want %s", got, want)
			}
		})
	}
}

func TestReferenceTrafficTrend(t *testing.T) {
	for _, raw := range loadA04Reference(t).GetList("trend") {
		item := raw.(*jsonx.Object)
		t.Run(item.GetString("name"), func(t *testing.T) {
			trend, err := referenceHistory(t, item).GetTrafficTrend("a", bigArg(t, item, "hours"), bigArg(t, item, "interval"))
			if expected, changed := nativeTrendExpected(item); changed {
				if err != nil || !jsonx.Equal(trend, expected) {
					t.Fatalf("native missing-counter trend: %v %v, want %v", trend, err, expected)
				}
				return
			}
			if got, want := referenceResult(t, trend, err), item.GetString("result"); got != want {
				t.Errorf("\n got  %s\n want %s", got, want)
			}
		})
	}
}

// Only serialization changes and the approved non-finite repair are applied to
// old file goldens. Business records, timestamps, cardinality and values match.
func equalPointRecords(t *testing.T, points []point, want []any) bool {
	t.Helper()
	if len(points) != len(want) {
		return false
	}
	for i, p := range points {
		row := want[i].([]any)
		if isoformat(p.at) != row[0] {
			return false
		}
		var gotValue, wantValue any
		if err := jsonresp.DecodeStored([]byte(p.line), &gotValue); err != nil {
			t.Fatal(err)
		}
		if err := jsonresp.DecodeStored([]byte(row[1].(string)), &wantValue); err != nil {
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
func nativeSummaryExpected(item *jsonx.Object) (*jsonx.Object, bool) {
	values := map[string][3]any{
		"none value": {nil, nil, nil}, "huge int mean overflow": {nil, nil, nil},
		"huge int then float": {1.5, 1.5, 1.5}, "float then huge int": {1.5, 1.5, 1.5},
		"infinity and big int": {nil, nil, nil}, "infinities": {nil, nil, nil},
		"nan first": {1.5, 2, 1}, "nan middle": {1.5, 2, 1},
	}
	value, ok := values[item.GetString("name")]
	if !ok {
		return nil, false
	}
	expected := jsonx.Obj("cpu", zeroStats(), "memory", zeroStats(), "disk", zeroStats(), "network_sent", zeroStats(), "network_recv", zeroStats(), "data_points", len(item.GetList("points")), "time_range_hours", item.Value("hours"))
	key := "cpu"
	if item.GetString("name") == "none value" {
		key = "memory"
	}
	expected.Set(key, jsonx.Obj("avg", value[0], "max", value[1], "min", value[2]))
	return expected, true
}

func nativeTrafficExpected(item *jsonx.Object) (*jsonx.Object, bool) {
	switch item.GetString("name") {
	case "none values", "nan", "infinity", "huge equal ints":
		return emptyTrafficStats(item.GetString("period")), true
	case "huge ints":
		result := emptyTrafficStats(item.GetString("period"))
		result.Set("start_time", "2026-11-15T11:01:00")
		result.Set("end_time", "2026-11-15T11:01:00")
		result.Set("data_points", 1)
		return result, true
	}
	return nil, false
}

func nativeTrendExpected(item *jsonx.Object) ([]any, bool) {
	switch item.GetString("name") {
	case "huge":
		return []any{jsonx.Obj("timestamp", "2026-11-15T11:00:00", "bytes_sent", 0, "bytes_recv", 0, "speed_sent", 0, "speed_recv", 0)}, true
	case "nan":
		return []any{jsonx.Obj("timestamp", "2026-11-15T11:00:00", "bytes_sent", 1, "bytes_recv", 1, "speed_sent", 0, "speed_recv", 0)}, true
	}
	return nil, false
}
