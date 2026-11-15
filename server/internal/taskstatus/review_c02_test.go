package taskstatus

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

type reviewRecord struct {
	Field string          `json:"field"`
	Value json.RawMessage `json:"value"`
	OK    bool            `json:"ok"`
	Out   struct {
		Entry map[string]json.RawMessage `json:"entry"`
		Error string                     `json:"error"`
	} `json:"out"`
}

type reviewTime struct {
	In     string  `json:"in"`
	ISO    string  `json:"iso"`
	Offset float64 `json:"offset"`
	TZName string  `json:"tzname"`
}

func loadReview(t *testing.T) ([]reviewRecord, []reviewTime) {
	t.Helper()
	data, err := os.ReadFile("testdata/review.json")
	if err != nil {
		t.Fatal(err)
	}
	// encoding/json has no NaN/Infinity: quote them here, unquote per value.
	data = []byte(strings.NewReplacer(": Infinity", `: "__Infinity__"`, ": NaN", `: "__NaN__"`).Replace(string(data)))
	var ref struct {
		Records []reviewRecord `json:"records"`
		Times   []reviewTime   `json:"times"`
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	return ref.Records, ref.Times
}

// Regression (review C02, reported by the C15 port): record() converts a
// hand-edited counter with int(): "4" counts on from 4, full-width and
// Arabic-Indic digits and white space are accepted, floats are truncated,
// big integers kept; values int() refuses make record() fail and leave the
// file as it was (references from CPython 3.11.17).
func TestReviewRecordCountersLikePython(t *testing.T) {
	records, _ := loadReview(t)
	for _, row := range records {
		dir := t.TempDir()
		raw := strings.NewReplacer(`"__Infinity__"`, "Infinity", `"__NaN__"`, "NaN").Replace(string(row.Value))
		value, err := jsonx.Parse([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		before, _ := jsonx.Compact(jsonx.Obj("k", jsonx.Obj(row.Field, value)))
		if err := os.WriteFile(filepath.Join(dir, Filename), before, 0o600); err != nil {
			t.Fatal(err)
		}
		entry, err := New(dir).Record("k", Result{OK: row.OK, StartedAt: "S", DurationMs: 1})
		name := row.Field + "=" + string(row.Value)
		if row.Out.Error != "" {
			var class interface{ PythonClass() string }
			if !errors.As(err, &class) || class.PythonClass() != row.Out.Error {
				t.Errorf("%s ok=%v: want %s, got %v", name, row.OK, row.Out.Error, err)
			}
			after, _ := os.ReadFile(filepath.Join(dir, Filename))
			if string(after) != string(before) {
				t.Errorf("%s: file changed after a failed record: %s", name, after)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s ok=%v: %v", name, row.OK, err)
			continue
		}
		for key, want := range row.Out.Entry {
			got, _ := jsonx.Compact(entry.Value(key))
			wantValue, _ := jsonx.Parse([]byte(strings.NewReplacer(`"__Infinity__"`, "Infinity", `"__NaN__"`, "NaN").Replace(string(want))))
			if wantBytes, _ := jsonx.Compact(wantValue); string(got) != string(wantBytes) {
				t.Errorf("%s ok=%v: %s = %s, want %s", name, row.OK, key, got, want)
			}
		}
	}
}

// Regression (review C02, reported by the C15 port): parse_time keeps the
// parsed UTC offset (Python's aware datetime keeps its tzinfo), naive and
// zero offsets are UTC.
func TestReviewParseTimeKeepsOffset(t *testing.T) {
	_, times := loadReview(t)
	for _, row := range times {
		got, ok := ParseTime(row.In)
		if !ok {
			t.Errorf("%q: not parsed", row.In)
			continue
		}
		name, offset := got.Zone()
		if int(row.Offset) != offset || name != row.TZName {
			t.Errorf("%q: zone %s %d, want %s %v", row.In, name, offset, row.TZName, row.Offset)
		}
		iso := got.Format("2006-01-02T15:04:05")
		// A sub-second offset cannot be a time.Location offset (accepted
		// difference): the instant and the name are right, the wall clock is
		// a fraction of a second off.
		if row.Offset == float64(int(row.Offset)) && !strings.HasPrefix(row.ISO, iso) {
			t.Errorf("%q: wall clock %s, want %s", row.In, iso, row.ISO)
		}
		if instant, err := time.Parse("2006-01-02T15:04:05.999999-07:00", row.ISO); err == nil && !instant.Equal(got) {
			t.Errorf("%q: instant %s, want %s", row.In, got, row.ISO)
		}
	}
}
