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

	"configflow/internal/jsonx"
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
		if !jsonx.Equal(pointsText(points), item.Value("points")) {
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
			} else if !jsonx.Equal(pointsText(points), item.Value("points")) {
				t.Errorf("%s: points %v, Python %v", name, pointsText(points), item.Value("points"))
			}
		case want == "format":
			var format legacyFormatError
			if !errors.As(err, &format) {
				t.Errorf("%s: %v, Python ValueError (%s)", name, err, item.GetString("detail"))
			}
		default:
			if err == nil || pyErrorText(err) != want {
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
			if got, want := referenceResult(t, trend, err), item.GetString("result"); got != want {
				t.Errorf("\n got  %s\n want %s", got, want)
			}
		})
	}
}
