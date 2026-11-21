package taskstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/taskstatus/isoformtest"
	"configflow/internal/testjson"
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

// The original Python fixture stays unchanged. Native decimal counters reject
// CPython-only Unicode digits; persisted nonfinite values become null before use.
func TestReviewRecordCountersLikePython(t *testing.T) {
	records, _ := loadReview(t)
	for _, row := range records {
		dir := t.TempDir()
		raw := strings.NewReplacer(`"__Infinity__"`, "Infinity", `"__NaN__"`, "NaN").Replace(string(row.Value))
		keyJSON, err := json.Marshal(row.Field)
		if err != nil {
			t.Fatal(err)
		}
		before := []byte(fmt.Sprintf(`{"k":{%s:%s}}`, keyJSON, raw))
		if err := os.WriteFile(filepath.Join(dir, Filename), before, 0o600); err != nil {
			t.Fatal(err)
		}
		entry, err := New(dir).SaveResult(context.Background(), "k", Result{OK: row.OK, StartedAt: "S", DurationMs: 1})
		name := row.Field + "=" + string(row.Value)
		used := row.Field == "run_count" || !row.OK
		nonfinite := raw == "Infinity" || raw == "NaN"
		var decimal string
		_ = json.Unmarshal(row.Value, &decimal)
		unicodeDigits := decimal == "０３" || decimal == "١٢"
		wantError := row.Out.Error != ""
		if nonfinite {
			wantError = false
		}
		if unicodeDigits && used {
			wantError = true
		}
		if wantError {
			if err == nil {
				t.Errorf("%s ok=%v: invalid counter accepted", name, row.OK)
			}
			after, _ := os.ReadFile(filepath.Join(dir, Filename))
			if string(after) != string(before) {
				t.Errorf("%s: file changed after failure", name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s ok=%v: %v", name, row.OK, err)
			continue
		}
		entryBytes, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		var entryFields map[string]json.RawMessage
		if err := json.Unmarshal(entryBytes, &entryFields); err != nil {
			t.Fatal(err)
		}
		if nonfinite {
			want := "null"
			if used {
				want = "1"
			}
			if row.Field == "consecutive_failures" && row.OK {
				want = "0"
			}
			got := string(entryFields[row.Field])
			if got != want {
				t.Errorf("%s: native value %s want %s", name, got, want)
			}
			// Other fields still compare against the original fixture below.
		}
		for key, want := range row.Out.Entry {
			if nonfinite && key == row.Field {
				continue
			}
			gotRaw := entryFields[key]
			wantText := strings.NewReplacer(`"__Infinity__"`, "Infinity", `"__NaN__"`, "NaN").Replace(string(want))
			if !testjson.Equal(json.RawMessage(wantText), gotRaw) {
				t.Errorf("%s ok=%v: %s = %s, want %s", name, row.OK, key, gotRaw, wantText)
			}
		}
	}
}

// Regression (review C02, reported by the C15 port): parse_time keeps the
// parsed UTC offset (Python's aware datetime keeps its tzinfo), naive and
// zero offsets are UTC. Rows outside the stored grammar are skipped.
func TestReviewParseTimeKeepsOffset(t *testing.T) {
	_, times := loadReview(t)
	checked := 0
	for _, row := range times {
		if !isoformtest.Stored(row.In) {
			continue
		}
		checked++
		got, ok := ParseTimestamp(row.In)
		if !ok {
			t.Errorf("%q: not parsed", row.In)
			continue
		}
		if _, offset := got.Zone(); int(row.Offset) != offset {
			t.Errorf("%q: offset %d, want %v", row.In, offset, row.Offset)
		}
		if row.Offset == 0 && got.Location() != time.UTC {
			t.Errorf("%q: zero offset in %v, want UTC", row.In, got.Location())
		}
		if iso := got.Format("2006-01-02T15:04:05"); !strings.HasPrefix(row.ISO, iso) {
			t.Errorf("%q: wall clock %s, want %s", row.In, iso, row.ISO)
		}
	}
	if checked < 6 {
		t.Fatalf("only %d review rows in the stored grammar", checked)
	}
}
