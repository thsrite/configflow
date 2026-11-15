package metrics

// Review references for A05 (testdata/gen_a05_reference.py →
// testdata/a05_reference.json, produced by CPython 3.11 with SQLite 3.53 on
// the test server): _target_key on IPv6 zones and domain edge cases,
// str.lower() over every code point, int64 overflow in the SQLite counters
// (REAL values after `up + excluded.up`, SUM() overflow while compacting),
// compaction / pruning / query boundaries, hand-edited traffic store files,
// the host check of the traffic store and ROLLBACK without a transaction.

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"configflow/internal/agents/trafficstore"
	"configflow/internal/jsonx"
)

func loadA05Reference(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/a05_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

func TestA05ReferenceTargetKeys(t *testing.T) {
	ref := loadA05Reference(t)
	for input, want := range ref.GetObject("target_keys").All() {
		if got := targetKey(input); got != want {
			t.Errorf("_target_key(%q) = %q, Python %q", input, got, want)
		}
	}
}

func TestA05ReferenceLowerEveryCodePoint(t *testing.T) {
	ref := loadA05Reference(t)
	want := map[rune]string{}
	for key, value := range ref.GetObject("lower_map").All() {
		cp, err := strconv.ParseInt(key, 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		want[rune(cp)] = value.(string)
	}
	for cp := rune(0); cp < 0x110000; cp++ {
		if cp >= 0xd800 && cp <= 0xdfff {
			continue
		}
		expected, ok := want[cp]
		if !ok {
			expected = string(cp)
		}
		if got := pyLower(string(cp)); got != expected {
			t.Errorf("U+%04X.lower() = %q, Python %q", cp, got, expected)
		}
	}
}

// sameJSON compares the sorted JSON texts: ints and floats must match in
// type too (1.0 is not 1).
func sameJSON(a, b any) bool {
	x, errX := jsonx.Sorted(a)
	y, errY := jsonx.Sorted(b)
	return errX == nil && errY == nil && string(x) == string(y)
}

func TestA05ReferenceFinalSigmaContext(t *testing.T) {
	ref := loadA05Reference(t).GetObject("sigma_context")
	inRanges := func(name string) map[rune]bool {
		out := map[rune]bool{}
		for _, entry := range ref.GetList(name) {
			pair := entry.([]any)
			lo, _ := pair[0].(jsonx.Number).Int64()
			hi, _ := pair[1].(jsonx.Number).Int64()
			for cp := lo; cp <= hi; cp++ {
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
		if got := after[1] == 0x3c3; got != casedNotIgnorable[cp] {
			t.Errorf("('AΣ' + U+%04X).lower(): σ = %v, Python %v", cp, got, casedNotIgnorable[cp])
		}
		before := []rune(pyLower("A" + c + "\u03a3"))
		if got := before[len(before)-1] == 0x3c2; got != ignorableOrCased[cp] {
			t.Errorf("('A' + U+%04X + 'Σ').lower(): ς = %v, Python %v", cp, got, ignorableOrCased[cp])
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
				row[i] = jsonx.Int(x)
			case float64:
				row[i] = jsonx.Float(x)
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

func tablesOf(t *testing.T, path string) *jsonx.Object {
	return jsonx.Obj("traffic_usage", dumpTable(t, path, "traffic_usage"), "traffic_targets", dumpTable(t, path, "traffic_targets"))
}

func a05FlushResult(result TrafficFlushResult, err error) any {
	if err != nil {
		return jsonx.Obj("error", pyErrorName(err), "message", err.Error())
	}
	return result.Object()
}

func a05QueryResult(result *jsonx.Object, err error) any {
	if err != nil {
		return jsonx.Obj("error", pyErrorName(err), "message", err.Error())
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
	ref := loadA05Reference(t).GetObject("overflow")
	now := loadA05Reference(t).GetFloat("now")
	this := int64(now) / Hour * Hour
	big := int64(1)<<62 - 1
	check := func(name string, got any) {
		t.Helper()
		if !sameJSON(got, ref.Value(name)) {
			t.Errorf("%s:\n  go     %s\n  python %s", name, jsonx.Repr(got), jsonx.Repr(ref.Value(name)))
		}
	}
	at := func(offset float64) *float64 { v := now + offset; return &v }

	dir := t.TempDir()
	h := NewTrafficHistory(dir)
	path := filepath.Join(dir, TrafficFilename)
	usage := []UsageRow{{this, "s", "G\x1fN", big, 1, 1}, {this - Hour, "s2", "H", 5, 6, 7}}
	targets := []TargetRow{{this, "s", "example.com", "G\x1fN", big, 1, 1}, {this, "s", "example.org", "G\x1fN", 3, 3, 1},
		{this, "s2", "example.com", "H", 2, 2, 1}}
	h.AddReport("a", &CleanedTraffic{Usage: usage, Targets: targets})
	h.AddReport("a", &CleanedTraffic{Usage: usage[:1], Targets: targets[:1]})
	check("flush1", a05FlushResult(h.Flush(DefaultRetention, at(0))))
	h.AddReport("a", &CleanedTraffic{Usage: usage[:1], Targets: targets[:1]})
	check("flush2", a05FlushResult(h.Flush(DefaultRetention, at(0))))
	check("tables2", tablesOf(t, path))
	h.AddReport("a", &CleanedTraffic{
		Usage:   []UsageRow{{this, "s", "G\x1fN", 10, 0, 0}, {this, "s", "X", big, big, 0}},
		Targets: []TargetRow{{this, "s", "example.com", "G\x1fN", 10, 0, 0}},
	})
	for _, by := range Aggregates {
		check("query/"+by, a05QueryResult(h.Query("a", "24h", by, at(0))))
	}
	check("flush3", a05FlushResult(h.Flush(DefaultRetention, at(3*Day))))
	check("tables3", tablesOf(t, path))
	check("query/30d/destination", a05QueryResult(h.Query("a", "30d", "destination", at(3*Day))))
	check("flush4", a05FlushResult(h.Flush(DefaultRetention, at(9*Day))))
	check("tables4", tablesOf(t, path))
	check("query/30d/source", a05QueryResult(h.Query("a", "30d", "source", at(9*Day))))

	dir = t.TempDir()
	h = NewTrafficHistory(dir)
	rows := []UsageRow{{this, "s", "G", big, 0, 0}, {this - Hour, "s", "G", big, 0, 0}}
	for i := 0; i < 3; i++ {
		h.AddReport("a", &CleanedTraffic{Usage: rows})
	}
	check("pending-overflow", a05FlushResult(h.Flush(DefaultRetention, at(0))))
	check("pending-after", h.PendingCount())
	h.mu.Lock()
	h.usage = map[usageKey]*triple{}
	h.mu.Unlock()
	for i := 0; i < 2; i++ {
		h.AddReport("a", &CleanedTraffic{Usage: rows})
	}
	check("flush-max", a05FlushResult(h.Flush(DefaultRetention, at(0))))
	check("sum-overflow", a05FlushResult(h.Flush(DefaultRetention, at(9*Day))))
	check("sum-overflow-tables", dumpTable(t, filepath.Join(dir, TrafficFilename), "traffic_usage"))
}

func TestA05ReferenceBoundaries(t *testing.T) {
	ref := loadA05Reference(t).GetObject("boundaries")
	check := func(name string, got any) {
		t.Helper()
		if !jsonx.Equal(got, ref.Value(name)) {
			t.Errorf("%s:\n  go     %s\n  python %s", name, jsonx.Repr(got), jsonx.Repr(ref.Value(name)))
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
	h.AddReport("a", &CleanedTraffic{Usage: usage, Targets: targets})
	check("flush", a05FlushResult(h.Flush(30, &now)))
	check("tables", tablesOf(t, path))
	for rangeKey := range Ranges {
		for _, when := range []float64{now, float64(day0), float64(day0) - 0.5} {
			w := when
			check("query/"+rangeKey+"/"+pyFloatRepr(w), a05QueryResult(h.Query("a", rangeKey, "destination", &w)))
		}
	}
	later := now + Day
	check("flush-later", a05FlushResult(h.Flush(30, &later)))
	check("tables-later", tablesOf(t, path))

	this := int64(loadA05Reference(t).GetFloat("now")) / Hour * Hour
	item := func(hour int64) any {
		return jsonx.Obj("hour", hour, "source", "s", "chain", []any{"G"}, "up", 1, "down", 1, "conns", 1)
	}
	report := jsonx.Obj("schema", 2, "usage", []any{item(this - 2*Day), item(this - 2*Day - Hour), item(this + Hour), item(this + 2*Hour)}, "domains", []any{})
	rowsOf := func(c *CleanedTraffic) any {
		out := []any{}
		for _, r := range c.Usage {
			out = append(out, []any{r.Bucket, r.Source, r.Chain, r.Up, r.Down, r.Conns})
		}
		return out
	}
	whole := float64(this)
	check("clean-integral-now", rowsOf(CleanTrafficReport(report, &whole)))
	minus := whole - 1e-6
	check("clean-integral-now-minus", rowsOf(CleanTrafficReport(report, &minus)))
}

func pyFloatRepr(f float64) string { return jsonx.Float(f).String() }

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
	for name, value := range ref.GetObject("store_edits").All() {
		c := value.(*jsonx.Object)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(c.GetString("file")), 0o644); err != nil {
			t.Fatal(err)
		}
		cleaned, err := trafficstore.CleanReport(c.Value("report"))
		if err != nil || cleaned == nil {
			t.Fatalf("%s: clean %v", name, err)
		}
		status, err := trafficstore.New(dir).AddReport("a", cleaned, &at)
		var got any = status
		if err != nil {
			got = jsonx.Obj("error", storeErrorName(err), "message", err.Error())
		}
		want := c.Value("result")
		if wantObject, ok := want.(*jsonx.Object); ok && wantObject.Has("error") {
			// Error texts only reach the "Exception on …" log line; the
			// outcome (error, nothing written) and the class must match.
			gotObject, isError := got.(*jsonx.Object)
			if !isError || !gotObject.Has("error") || gotObject.GetString("error") != wantObject.GetString("error") {
				t.Errorf("%s: go %s, python %s", name, jsonx.Repr(got), jsonx.Repr(want))
			}
		} else if !jsonx.Equal(got, want) {
			t.Errorf("%s: go %s, python %s", name, jsonx.Repr(got), jsonx.Repr(want))
		}
		stored, _ := os.ReadFile(filepath.Join(dir, "a.json"))
		if string(stored) != c.GetString("stored") {
			t.Errorf("%s: stored\n  go     %s\n  python %s", name, stored, c.GetString("stored"))
		}
	}
}

func TestA05ReferenceStoreCapWithFloatWeights(t *testing.T) {
	ref := loadA05Reference(t).GetObject("store_cap")
	old := trafficstore.MaxHostsPerDay
	trafficstore.MaxHostsPerDay = 3
	defer func() { trafficstore.MaxHostsPerDay = old }()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(ref.GetString("file")), 0o644); err != nil {
		t.Fatal(err)
	}
	cleaned, _ := trafficstore.CleanReport(jsonx.Obj("schema", 1, "status", "unreachable"))
	at := time.Date(2026, 11, 15, 8, 0, 0, 0, time.Local)
	status, err := trafficstore.New(dir).AddReport("a", cleaned, &at)
	if err != nil || !jsonx.Equal(status, ref.Value("result")) {
		t.Fatalf("go %v %v, python %s", jsonx.Repr(status), err, jsonx.Repr(ref.Value("result")))
	}
	stored, _ := os.ReadFile(filepath.Join(dir, "a.json"))
	if string(stored) != ref.GetString("stored") {
		t.Errorf("stored\n  go     %s\n  python %s", stored, ref.GetString("stored"))
	}
}

func TestA05ReferenceStoreHosts(t *testing.T) {
	ref := loadA05Reference(t)
	for host, want := range ref.GetObject("store_hosts").All() {
		report := jsonx.Obj("schema", 1, "status", "running", "items", []any{jsonx.Obj("host", host, "outlet", "direct", "policy", "p")})
		cleaned, err := trafficstore.CleanReport(report)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(cleaned.GetList("items")) == 1; got != want.(bool) {
			t.Errorf("host %q accepted = %v, Python %v", host, got, want)
		}
	}
}

func TestA05ReferenceRollbackWithoutTransaction(t *testing.T) {
	ref := loadA05Reference(t).GetObject("rollback_without_transaction")
	h := NewTrafficHistory(t.TempDir())
	c, err := h.connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	// _delete_agents / _migrate_legacy: an unconditional ROLLBACK raises.
	if err := c.rollbackAlways(nil); err == nil || err.Error() != ref.GetString("message") {
		t.Errorf("unconditional ROLLBACK = %v, Python %s", err, ref.GetString("message"))
	}
	// flush: `if connection.in_transaction` — SQLite rolled back by itself.
	c.inTransaction = true
	if err := c.rollback(); err != nil {
		t.Errorf("ROLLBACK after SQLite ended the transaction = %v, Python skips it", err)
	}
}
