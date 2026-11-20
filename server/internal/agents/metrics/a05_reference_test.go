package metrics

// Review references for A05 (testdata/gen_a05_reference.py →
// testdata/a05_reference.json, produced by CPython 3.11 with SQLite 3.53 on
// the test server): _target_key on IPv6 zones and domain edge cases,
// str.lower() over every code point, int64 overflow in the SQLite counters
// (REAL values after `up + excluded.up`, SUM() overflow while compacting),
// compaction / pruning / query boundaries, hand-edited traffic store files,
// the host check of the traffic store and ROLLBACK without a transaction.

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents/trafficstore"
	"configflow/internal/testjson"
)

type a05Reference struct {
	Now          float64                    `json:"now"`
	TargetKeys   map[string]string          `json:"target_keys"`
	LowerMap     map[string]string          `json:"lower_map"`
	SigmaContext map[string][][2]int64      `json:"sigma_context"`
	Overflow     map[string]json.RawMessage `json:"overflow"`
	Boundaries   map[string]json.RawMessage `json:"boundaries"`
	StoreEdits   map[string]json.RawMessage `json:"store_edits"`
	StoreCap     struct {
		File   string          `json:"file"`
		Result json.RawMessage `json:"result"`
		Stored string          `json:"stored"`
	} `json:"store_cap"`
	StoreHosts map[string]bool `json:"store_hosts"`
	Rollback   struct {
		Message string `json:"message"`
	} `json:"rollback_without_transaction"`
}

func loadA05Reference(t *testing.T) a05Reference {
	t.Helper()
	data, err := os.ReadFile("testdata/a05_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref a05Reference
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

func floatJSON(f float64) json.RawMessage {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return json.RawMessage("null")
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return json.RawMessage(s)
}

func TestA05ReferenceTargetKeys(t *testing.T) {
	t.Parallel()
	ref := loadA05Reference(t)
	for input, want := range ref.TargetKeys {
		if got := targetKey(input); got != want {
			t.Errorf("_target_key(%q) = %q, Python %q", input, got, want)
		}
	}
}

func TestA05ReferenceLowerEveryCodePoint(t *testing.T) {
	t.Parallel()
	ref := loadA05Reference(t)
	native := loadNativeCaseReference(t)
	want := map[rune]string{}
	for key, value := range ref.LowerMap {
		cp, err := strconv.ParseInt(key, 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		want[rune(cp)] = value
	}
	for cp := rune(0); cp < 0x110000; cp++ {
		if cp >= 0xd800 && cp <= 0xdfff {
			continue
		}
		expected, ok := want[cp]
		if !ok {
			expected = string(cp)
		}
		if err := native.checkLower(cp, pyLower(string(cp)), expected); err != nil {
			t.Error(err)
		}
	}
}

// sameJSON compares structure while keeping integer vs decimal literal types distinct (1.0 is not 1).
func sameJSON(a, b any) bool {
	return testjson.Equal(a, b, testjson.CompareOptions{DistinguishNumberLiterals: true})
}

func TestA05ReferenceFinalSigmaContext(t *testing.T) {
	t.Parallel()
	ref := loadA05Reference(t).SigmaContext
	native := loadNativeCaseReference(t)
	inRanges := func(name string) map[rune]bool {
		out := map[rune]bool{}
		for _, pair := range ref[name] {
			for cp := pair[0]; cp <= pair[1]; cp++ {
				out[rune(cp)] = true
			}
		}
		return out
	}
	casedNotIgnorable, ignorableOrCased := inRanges("cased_not_ignorable"), inRanges("ignorable_or_cased")
	for cp := rune(0); cp < 0x110000; cp++ {
		if cp >= 0xd800 && cp <= 0xdfff {
			continue
		}
		c := string(cp)
		after := []rune(pyLower("A\u03a3" + c))
		before := []rune(pyLower("A" + c + "\u03a3"))
		if err := native.checkSigma(cp, after[1] == 0x3c3, before[len(before)-1] == 0x3c2, casedNotIgnorable[cp], ignorableOrCased[cp]); err != nil {
			t.Error(err)
		}
	}
}

// dumpTable is the generator's table(): every row with typeof(up).
func dumpTable(t *testing.T, path, name string) []any {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	target := ""
	if name == "traffic_targets" {
		target = "target, "
	}
	rows, err := db.Query("SELECT agent_id, bucket, span, source, " + target + "chain, up, typeof(up), down, conns FROM " + name + " ORDER BY 1, 2, 3, 4, 5")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, _ := rows.Columns()
	out := []any{}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		row := make([]any, len(values))
		for i, v := range values {
			switch x := v.(type) {
			case int64:
				row[i] = x
			case float64:
				row[i] = floatJSON(x)
			case []byte:
				row[i] = string(x)
			default:
				row[i] = x
			}
		}
		out = append(out, row)
	}
	return out
}

func tablesOf(t *testing.T, path string) map[string]any {
	return map[string]any{
		"traffic_usage":   dumpTable(t, path, "traffic_usage"),
		"traffic_targets": dumpTable(t, path, "traffic_targets"),
	}
}

func a05FlushResult(result TrafficFlushResult, err error) any {
	if err != nil {
		return map[string]any{"error": pyErrorName(err), "message": err.Error()}
	}
	return map[string]any{"rows": result.Rows, "compacted": result.Compacted, "pruned": result.Pruned}
}

func a05QueryResult(result *TrafficQuery, err error) any {
	if err != nil {
		return map[string]any{"error": pyErrorName(err), "message": err.Error()}
	}
	return result
}

func pyErrorName(err error) string {
	switch err.(type) {
	case *OverflowError:
		return "OverflowError"
	case *SQLiteError:
		return "OperationalError"
	}
	return "?"
}

func TestA05ReferenceInt64OverflowInSQLite(t *testing.T) {
	full := loadA05Reference(t)
	ref := full.Overflow
	now := full.Now
	this := int64(now) / Hour * Hour
	big := int64(1)<<62 - 1
	check := func(name string, got any) {
		t.Helper()
		if !sameJSON(ref[name], got) {
			t.Errorf("%s: %s", name, testjson.Diff(ref[name], got, testjson.CompareOptions{DistinguishNumberLiterals: true}))
		}
	}
	at := func(offset float64) *float64 { v := now + offset; return &v }

	dir := t.TempDir()
	h := NewTrafficHistory(dir)
	path := filepath.Join(dir, TrafficFilename)
	usage := []UsageRow{{this, "s", "G\x1fN", big, 1, 1}, {this - Hour, "s2", "H", 5, 6, 7}}
	targets := []TargetRow{{this, "s", "example.com", "G\x1fN", big, 1, 1}, {this, "s", "example.org", "G\x1fN", 3, 3, 1},
		{this, "s2", "example.com", "H", 2, 2, 1}}
	_ = h.AddTraffic(context.Background(), "a", &CleanedTraffic{Usage: usage, Targets: targets})
	_ = h.AddTraffic(context.Background(), "a", &CleanedTraffic{Usage: usage[:1], Targets: targets[:1]})
	check("flush1", a05FlushResult(h.Flush(DefaultRetention, at(0))))
	_ = h.AddTraffic(context.Background(), "a", &CleanedTraffic{Usage: usage[:1], Targets: targets[:1]})
	check("flush2", a05FlushResult(h.Flush(DefaultRetention, at(0))))
	check("tables2", tablesOf(t, path))
	_ = h.AddTraffic(context.Background(), "a", &CleanedTraffic{
		Usage:   []UsageRow{{this, "s", "G\x1fN", 10, 0, 0}, {this, "s", "X", big, big, 0}},
		Targets: []TargetRow{{this, "s", "example.com", "G\x1fN", 10, 0, 0}},
	})
	for _, by := range Aggregates {
		check("query/"+by, a05QueryResult(h.QueryTraffic(context.Background(), "a", "24h", by, at(0))))
	}
	check("flush3", a05FlushResult(h.Flush(DefaultRetention, at(3*Day))))
	check("tables3", tablesOf(t, path))
	check("query/30d/destination", a05QueryResult(h.QueryTraffic(context.Background(), "a", "30d", "destination", at(3*Day))))
	check("flush4", a05FlushResult(h.Flush(DefaultRetention, at(9*Day))))
	check("tables4", tablesOf(t, path))
	check("query/30d/source", a05QueryResult(h.QueryTraffic(context.Background(), "a", "30d", "source", at(9*Day))))

	dir = t.TempDir()
	h = NewTrafficHistory(dir)
	rows := []UsageRow{{this, "s", "G", big, 0, 0}, {this - Hour, "s", "G", big, 0, 0}}
	for i := 0; i < 3; i++ {
		_ = h.AddTraffic(context.Background(), "a", &CleanedTraffic{Usage: rows})
	}
	check("pending-overflow", a05FlushResult(h.Flush(DefaultRetention, at(0))))
	check("pending-after", h.PendingCount())
	h.mu.Lock()
	h.usage = map[usageKey]*triple{}
	h.mu.Unlock()
	for i := 0; i < 2; i++ {
		_ = h.AddTraffic(context.Background(), "a", &CleanedTraffic{Usage: rows})
	}
	check("flush-max", a05FlushResult(h.Flush(DefaultRetention, at(0))))
	check("sum-overflow", a05FlushResult(h.Flush(DefaultRetention, at(9*Day))))
	check("sum-overflow-tables", dumpTable(t, filepath.Join(dir, TrafficFilename), "traffic_usage"))
}

func TestA05ReferenceBoundaries(t *testing.T) {
	full := loadA05Reference(t)
	ref := full.Boundaries
	check := func(name string, got any) {
		t.Helper()
		if diff := testjson.Diff(ref[name], got); diff != "" {
			t.Errorf("%s: %s", name, diff)
		}
	}
	now := 1_793_491_200 + 0.75
	day0 := int64(now) / Day * Day
	var usage []UsageRow
	var targets []TargetRow
	for _, offset := range []int64{-7*Day - Hour, -7 * Day, -7*Day + Hour, -2*Day - Hour, -2 * Day, -30*Day - Hour, -30 * Day,
		-31 * Day, -29*Day - 23*Hour, -Hour, 0} {
		usage = append(usage, UsageRow{day0 + offset, "s", "G\x1fN", 1, 2, 1})
		targets = append(targets, TargetRow{day0 + offset, "s", "example.com", "G\x1fN", 1, 2, 1})
	}
	dir := t.TempDir()
	path := filepath.Join(dir, TrafficFilename)
	h := NewTrafficHistory(dir)
	_ = h.AddTraffic(context.Background(), "a", &CleanedTraffic{Usage: usage, Targets: targets})
	check("flush", a05FlushResult(h.Flush(30, &now)))
	check("tables", tablesOf(t, path))
	for rangeKey := range Ranges {
		for _, when := range []float64{now, float64(day0), float64(day0) - 0.5} {
			w := when
			check("query/"+rangeKey+"/"+pyFloatKey(w), a05QueryResult(h.QueryTraffic(context.Background(), "a", rangeKey, "destination", &w)))
		}
	}
	later := now + Day
	check("flush-later", a05FlushResult(h.Flush(30, &later)))
	check("tables-later", tablesOf(t, path))

	this := int64(full.Now) / Hour * Hour
	item := func(hour int64) map[string]any {
		return map[string]any{"hour": hour, "source": "s", "chain": []any{"G"}, "up": 1, "down": 1, "conns": 1}
	}
	report := map[string]any{
		"schema":  2,
		"usage":   []any{item(this - 2*Day), item(this - 2*Day - Hour), item(this + Hour), item(this + 2*Hour)},
		"domains": []any{},
	}
	rowsOf := func(c *CleanedTraffic) any {
		out := []any{}
		for _, r := range c.Usage {
			out = append(out, []any{r.Bucket, r.Source, r.Chain, r.Up, r.Down, r.Conns})
		}
		return out
	}
	rawReport := mustMarshal(t, report)
	whole := float64(this)
	check("clean-integral-now", rowsOf(DecodeTrafficReport(rawReport, &whole)))
	minus := whole - 1e-6
	check("clean-integral-now-minus", rowsOf(DecodeTrafficReport(rawReport, &minus)))
}

func pyFloatKey(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

func storeErrorName(err error) string {
	switch err.(type) {
	case *trafficstore.TypeError:
		return "TypeError"
	case *trafficstore.AttributeError:
		return "AttributeError"
	case *trafficstore.KeyError:
		return "KeyError"
	}
	return "?"
}

func TestA05ReferenceHandEditedStoreFiles(t *testing.T) {
	ref := loadA05Reference(t)
	at := time.Date(2026, 11, 15, 12, 30, 45, 999999000, time.Local)
	for name, rawCase := range ref.StoreEdits {
		var c struct {
			File   string          `json:"file"`
			Report json.RawMessage `json:"report"`
			Result json.RawMessage `json:"result"`
			Stored string          `json:"stored"`
		}
		if err := json.Unmarshal(rawCase, &c); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(c.File), 0o644); err != nil {
			t.Fatal(err)
		}
		cleaned, err := trafficstore.DecodeReport(c.Report)
		if err != nil || cleaned == nil {
			t.Fatalf("%s: clean %v", name, err)
		}
		nativeStatus, err := trafficstore.NewNative(dir).Add(context.Background(), "a", cleaned, &at)
		var got any = nativeStatus
		if err != nil {
			got = map[string]any{"error": storeErrorName(err), "message": err.Error()}
		}
		if nativeStoreRefused(t, name, rawCase) {
			if err == nil {
				t.Fatalf("native corrupted bucket accepted: %s", name)
			}
			stored, _ := os.ReadFile(filepath.Join(dir, "a.json"))
			if string(stored) != c.File {
				t.Fatalf("refused write changed file: %s", name)
			}
			continue
		}
		if _, wantHasErr := testjson.Path(c.Result, "error"); wantHasErr {
			// Error texts only reach the "Exception on …" log line; the
			// outcome (error, nothing written) and the class must match.
			if _, gotHasErr := testjson.Path(got, "error"); !gotHasErr {
				t.Errorf("%s: go %v, python %s", name, got, c.Result)
			}
		} else if diff := testjson.Diff(c.Result, got); diff != "" {
			t.Errorf("%s: %s", name, diff)
		}
		stored, _ := os.ReadFile(filepath.Join(dir, "a.json"))
		if !equalStoredTrafficJSON(stored, []byte(c.Stored)) {
			t.Errorf("%s: stored\n  go     %s\n  python %s", name, stored, c.Stored)
		}
	}
}

func TestA05ReferenceStoreCapWithFloatWeights(t *testing.T) {
	ref := loadA05Reference(t).StoreCap
	old := trafficstore.MaxHostsPerDay
	trafficstore.MaxHostsPerDay = 3
	defer func() { trafficstore.MaxHostsPerDay = old }()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(ref.File), 0o644); err != nil {
		t.Fatal(err)
	}
	cleaned, _ := trafficstore.DecodeReport(json.RawMessage(`{"schema":1,"status":"unreachable"}`))
	at := time.Date(2026, 11, 15, 8, 0, 0, 0, time.Local)
	nativeStatus, err := trafficstore.NewNative(dir).Add(context.Background(), "a", cleaned, &at)
	if err != nil || !testjson.Equal(ref.Result, nativeStatus) {
		t.Fatalf("go %+v %v, python %s", nativeStatus, err, ref.Result)
	}
	stored, _ := os.ReadFile(filepath.Join(dir, "a.json"))
	if !equalStoredTrafficJSON(stored, []byte(ref.Stored)) {
		t.Errorf("stored\n  go     %s\n  python %s", stored, ref.Stored)
	}
}

func TestA05ReferenceStoreHosts(t *testing.T) {
	ref := loadA05Reference(t)
	for host, want := range ref.StoreHosts {
		report := map[string]any{
			"schema": 1, "status": "running",
			"items": []any{map[string]any{"host": host, "outlet": "direct", "policy": "p"}},
		}
		cleaned, err := nativeStoreReport(report)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(cleaned.Items) == 1; got != want {
			t.Errorf("host %q accepted = %v, Python %v", host, got, want)
		}
	}
}

func TestA05ReferenceRollbackWithoutTransaction(t *testing.T) {
	ref := loadA05Reference(t).Rollback
	h := NewTrafficHistory(t.TempDir())
	c, err := h.connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	// _delete_agents / _migrate_legacy: an unconditional ROLLBACK raises.
	if err := c.rollbackAlways(nil); err == nil || err.Error() != ref.Message {
		t.Errorf("unconditional ROLLBACK = %v, Python %s", err, ref.Message)
	}
	// flush: `if connection.in_transaction` — SQLite rolled back by itself.
	c.inTransaction = true
	if err := c.rollback(); err != nil {
		t.Errorf("ROLLBACK after SQLite ended the transaction = %v, Python skips it", err)
	}
}
