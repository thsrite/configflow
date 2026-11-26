package metrics

// Ports of backend/test_traffic_history.py (the TrafficHistory and
// clean_report cases; the route cases are in internal/api/agents
// traffic_test.go, the flush task case below).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/testjson"
)

const (
	trafficNOW = 1_793_500_000 // 2026-11-01, not on the hour
	thisHour   = trafficNOW / Hour * Hour
)

var (
	chainAI     = []string{"人工智能", "美国自建", "IEPL专线"}
	chainDirect = []string{"全球直连", "DIRECT"}
)

func joined(layers []string) string { return strings.Join(layers, ChainSeparator) }

func layersOf(chain []string) []any {
	out := make([]any, len(chain))
	for i, name := range chain {
		out[i] = name
	}
	return out
}

type usageItem struct {
	hour            int64
	source          string
	chain           []string
	up, down, conns int64
}

func usage(overrides ...func(*usageItem)) map[string]any {
	item := usageItem{thisHour, "10.0.0.2", chainAI, 1, 10, 1}
	for _, o := range overrides {
		o(&item)
	}
	return map[string]any{
		"hour": item.hour, "source": item.source, "chain": layersOf(item.chain),
		"up": item.up, "down": item.down, "conns": item.conns,
	}
}

type targetItem struct {
	hour            int64
	source, domain  string
	chain           []string
	up, down, conns int64
}

func target(overrides ...func(*targetItem)) map[string]any {
	item := targetItem{thisHour, "10.0.0.2", "www.example.com", chainAI, 1, 10, 1}
	for _, o := range overrides {
		o(&item)
	}
	return map[string]any{
		"hour": item.hour, "source": item.source, "domain": item.domain, "chain": layersOf(item.chain),
		"up": item.up, "down": item.down, "conns": item.conns,
	}
}

func trafficReport(usageItems, targetItems []any, schema int) map[string]any {
	if usageItems == nil {
		usageItems = []any{}
	}
	if targetItems == nil {
		targetItems = []any{}
	}
	return map[string]any{"schema": schema, "usage": usageItems, "domains": targetItems, "dropped": 0}
}

func at(v float64) *float64 { return &v }

func cleanAt(t *testing.T, report any) *CleanedTraffic {
	t.Helper()
	cleaned := DecodeTrafficReport(mustMarshal(t, report), at(trafficNOW))
	if cleaned == nil {
		t.Fatalf("report refused: %v", report)
	}
	return cleaned
}

func TestCleanReportKeepsFullChainsAndRejectsBadItems(t *testing.T) {
	cleaned := cleanAt(t, trafficReport(
		[]any{usage(), usage(func(u *usageItem) { u.source = ""; u.chain = []string{"x"} })},
		[]any{target(func(x *targetItem) { x.domain = "a.b.example.com.cn" }),
			target(func(x *targetItem) { x.domain = "203.0.113.9"; x.chain = chainDirect }),
			target(func(x *targetItem) { x.domain = "" })}, 2))
	if u := cleaned.Usage[0]; u.Bucket != thisHour || u.Source != "10.0.0.2" || u.Chain != joined(chainAI) {
		t.Fatalf("usage[0] %+v", u)
	}
	// 未归属的一行没有链路
	if u := cleaned.Usage[1]; u.Bucket != thisHour || u.Source != Unattributed || u.Chain != "" {
		t.Fatalf("usage[1] %+v", u)
	}
	want := [][2]string{{"example.com.cn", joined(chainAI)}, {"203.0.113.9", joined(chainDirect)}, {OtherTarget, joined(chainAI)}}
	for i, row := range cleaned.Targets {
		if [2]string{row.Target, row.Chain} != want[i] {
			t.Fatalf("targets[%d] %+v", i, row)
		}
	}
	badChain := usage()
	badChain["chain"] = "Proxy"
	for i, bad := range []any{
		usage(func(u *usageItem) { u.hour = thisHour + 1 }), usage(func(u *usageItem) { u.up = -1 }),
		func() any { u := usage(); u["down"] = "1"; return u }(), usage(func(u *usageItem) { u.source = strings.Repeat("x", 65) }),
		badChain, usage(func(u *usageItem) { u.chain = []string{""} }),
		usage(func(u *usageItem) { u.chain = []string{"a" + ChainSeparator + "b"} }),
		usage(func(u *usageItem) { u.chain = []string{"x", "x", "x", "x", "x", "x", "x", "x", "x"} }), nil,
	} {
		if DecodeTrafficReport(mustMarshal(t, trafficReport([]any{bad}, nil, 2)), at(trafficNOW)) != nil {
			t.Errorf("bad item %d accepted", i)
		}
	}
	if DecodeTrafficReport(mustMarshal(t, map[string]any{"schema": 3, "usage": []any{}, "domains": []any{}}), at(trafficNOW)) != nil {
		t.Error("schema 3 accepted")
	}
	// 积压超过 2 天的整点数据直接丢弃，其余照收
	stale := cleanAt(t, trafficReport([]any{usage(func(u *usageItem) { u.hour = thisHour - 3*Day }), usage()}, nil, 2))
	if len(stale.Usage) != 1 {
		t.Fatalf("stale %+v", stale.Usage)
	}
}

func TestReportsFrom1851AgentsBecomeTwoLayerChains(t *testing.T) {
	oldUsage := []any{
		map[string]any{"hour": thisHour, "source": "10.0.0.2", "group": "Proxy", "outbound": "HK", "up": 1, "down": 2, "conns": 1},
		map[string]any{"hour": thisHour, "source": "10.0.0.2", "group": "DIRECT", "outbound": "DIRECT", "up": 1, "down": 2, "conns": 1},
	}
	oldTargets := []any{map[string]any{"hour": thisHour, "source": "10.0.0.2", "domain": "a.example.com", "outbound": "HK", "up": 1, "down": 2, "conns": 1}}
	cleaned := cleanAt(t, trafficReport(oldUsage, oldTargets, 1))
	if cleaned.Usage[0].Chain != joined([]string{"Proxy", "HK"}) || cleaned.Usage[1].Chain != "DIRECT" || cleaned.Targets[0].Chain != "HK" {
		t.Fatalf("%+v", cleaned)
	}
}

func keyDown(rows []TrafficRow) [][2]any {
	var out [][2]any
	for _, row := range rows {
		var down int64
		_ = json.Unmarshal(row.Down, &down)
		out = append(out, [2]any{row.Key, down})
	}
	return out
}

func eq(t *testing.T, label string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) && !testjson.Equal(want, got) {
		t.Fatalf("%s: got %#v want %#v", label, got, want)
	}
}

func pairs(values ...any) [][2]any {
	var out [][2]any
	for i := 0; i < len(values); i += 2 {
		out = append(out, [2]any{values[i], int64(values[i+1].(int))})
	}
	return out
}

func query(t *testing.T, h *TrafficHistory, agent, rangeKey, by string) *TrafficQuery {
	t.Helper()
	result, err := h.QueryTraffic(context.Background(), agent, rangeKey, by, at(trafficNOW))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func queryInts(t *testing.T, db *sql.DB, statement string, args ...any) [][]int64 {
	t.Helper()
	rows, err := db.Query(statement, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, _ := rows.Columns()
	var out [][]int64
	for rows.Next() {
		row := make([]int64, len(columns))
		pointers := make([]any, len(columns))
		for i := range row {
			pointers[i] = &row[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	return out
}

func TestReportsStayInMemoryUntilFlushAndQueriesIncludePending(t *testing.T) {
	dir := t.TempDir()
	h := NewTrafficHistory(dir)
	_ = h.AddTraffic(context.Background(), "a1", cleanAt(t, trafficReport([]any{
		usage(func(u *usageItem) { u.down = 100 }), usage(func(u *usageItem) { u.chain = chainDirect; u.down = 40 }),
		usage(func(u *usageItem) { u.source = "10.0.0.3"; u.chain = chainDirect; u.down = 50 }),
		usage(func(u *usageItem) { u.source = ""; u.down = 9 }),
	}, []any{target(func(x *targetItem) { x.down = 100 }), target(func(x *targetItem) { x.chain = chainDirect; x.down = 40 })}, 2)))
	if _, err := os.Stat(filepath.Join(dir, TrafficFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("written before flush")
	}
	bySource := query(t, h, "a1", "24h", "source")
	rows := bySource.Rows
	eq(t, "by source", keyDown(rows), pairs("10.0.0.2", 140, "10.0.0.3", 50, Unattributed, 9))
	// 设备按完整链路拆开，未归属没有明细；出口节点总量不含未归属
	first := rows[0]
	eq(t, "parts", keyDown(first.Parts), pairs(joined(chainAI), 100, joined(chainDirect), 40))
	if first.PartCount == nil || *first.PartCount != 2 || len(rows[2].Parts) != 0 {
		t.Fatal("part counts")
	}
	eq(t, "outbounds", keyDown(bySource.Outbounds), pairs("IEPL专线", 100, "DIRECT", 90))
	wantEqual(t, "total", bySource.Total, map[string]any{"up": 4, "down": 199, "conns": 4})
	// 按链路、出站节点汇总时按设备拆开
	var direct *TrafficRow
	for i := range query(t, h, "a1", "24h", "chain").Rows {
		row := query(t, h, "a1", "24h", "chain").Rows[i]
		if row.Key == joined(chainDirect) {
			direct = &row
		}
	}
	eq(t, "chain parts", keyDown(direct.Parts), pairs("10.0.0.3", 50, "10.0.0.2", 40))
	eq(t, "by outbound", keyDown(query(t, h, "a1", "24h", "outbound").Rows),
		pairs("IEPL专线", 100, "DIRECT", 90, Unattributed, 9))
	// 策略组是链路的第一层，按链路拆开
	byGroup := query(t, h, "a1", "24h", "group").Rows
	for i, want := range [][2]string{{"人工智能", joined(chainAI)}, {"全球直连", joined(chainDirect)}} {
		row := byGroup[i]
		if row.Key != want[0] || len(row.Parts) != 1 || row.Parts[0].Key != want[1] {
			t.Fatalf("group row %d %+v", i, row)
		}
	}
	destination := query(t, h, "a1", "24h", "destination")
	eq(t, "destination parts", keyDown(destination.Rows[0].Parts),
		pairs(joined(chainAI), 100, joined(chainDirect), 40))
	result, err := h.Flush(t.Context(), DefaultRetention, at(trafficNOW))
	if err != nil || result != (TrafficFlushResult{Rows: 6}) {
		t.Fatalf("flush %+v %v", result, err)
	}
	if h.PendingCount() != 0 {
		t.Fatal("pending after flush")
	}
	// 保存后再收到同一小时的数据，累加而不是覆盖
	_ = h.AddTraffic(context.Background(), "a1", cleanAt(t, trafficReport([]any{usage(func(u *usageItem) { u.down = 1 })}, nil, 2)))
	if _, err := h.Flush(t.Context(), DefaultRetention, at(trafficNOW)); err != nil {
		t.Fatal(err)
	}
	db := openDB(t, filepath.Join(dir, TrafficFilename))
	eq(t, "accumulated", queryInts(t, db, "SELECT down FROM traffic_usage WHERE source = '10.0.0.2' AND chain = ?", joined(chainAI)), [][]int64{{101}})
	destination = query(t, h, "a1", "24h", "destination")
	eq(t, "destination", keyDown(destination.Rows), pairs("example.com", 140, Unattributed, 9))
	trend := destination.Trend
	if len(trend) != 25 || !testjson.Equal(200, trend[24].Down) {
		t.Fatalf("trend %d %+v", len(trend), trend[len(trend)-1])
	}
}

func TestTablesFrom1851AreMigratedToChains(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, filepath.Join(dir, TrafficFilename))
	for _, statement := range []string{
		"CREATE TABLE usage (agent_id TEXT, bucket INTEGER, span INTEGER, source TEXT, policy_group TEXT, outbound TEXT," +
			" up INTEGER, down INTEGER, conns INTEGER, PRIMARY KEY (agent_id, bucket, span, source, policy_group, outbound))",
		"CREATE TABLE targets (agent_id TEXT, bucket INTEGER, span INTEGER, source TEXT, target TEXT, outbound TEXT," +
			" up INTEGER, down INTEGER, conns INTEGER, PRIMARY KEY (agent_id, bucket, span, source, target, outbound))",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range [][]any{{"a1", thisHour, "10.0.0.2", "Proxy", "HK", 1, 10, 1}, {"a1", thisHour, "10.0.0.2", "DIRECT", "DIRECT", 1, 5, 1},
		{"a1", thisHour, "", "", "", 0, 3, 0}} {
		if _, err := db.Exec("INSERT INTO usage VALUES (?, ?, 3600, ?, ?, ?, ?, ?, ?)", row...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO targets VALUES ('a1', ?, 3600, '10.0.0.2', 'example.com', 'HK', 1, 10, 1)", thisHour); err != nil {
		t.Fatal(err)
	}
	db.Close()
	h := NewTrafficHistory(dir)
	bySource := query(t, h, "a1", "24h", "source")
	eq(t, "parts", keyDown(bySource.Rows[0].Parts), pairs(joined([]string{"Proxy", "HK"}), 10, "DIRECT", 5))
	if !testjson.Equal(18, bySource.Total.Down) {
		t.Fatalf("total: %+v", bySource.Total)
	}
	if key := query(t, h, "a1", "24h", "destination").Rows[0].Parts[0].Key; key != "HK" {
		t.Fatal(key)
	}
	rows, err := openDB(t, filepath.Join(dir, TrafficFilename)).Query("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		tables = append(tables, name)
	}
	rows.Close()
	if strings.Join(tables, ",") != "traffic_targets,traffic_usage" {
		t.Fatal(tables)
	}
}

func TestFailedFlushKeepsTheDataForTheNextRun(t *testing.T) {
	h := NewTrafficHistory(filepath.Join(t.TempDir(), "metrics"))
	_ = h.AddTraffic(context.Background(), "a1", cleanAt(t, trafficReport([]any{usage(func(u *usageItem) { u.down = 7 })}, nil, 2)))
	restore := h.SetBrokenConnect(errors.New("disk I/O error"))
	if _, err := h.Flush(t.Context(), DefaultRetention, at(trafficNOW)); err == nil || err.Error() != "disk I/O error" {
		t.Fatalf("err %v", err)
	}
	_ = h.AddTraffic(context.Background(), "a1", cleanAt(t, trafficReport([]any{usage(func(u *usageItem) { u.down = 3 })}, nil, 2)))
	restore()
	if _, err := h.Flush(t.Context(), DefaultRetention, at(trafficNOW)); err != nil {
		t.Fatal(err)
	}
	if down := query(t, h, "a1", "24h", "source").Total.Down; !testjson.Equal(10, down) {
		t.Fatalf("%s", down)
	}
}

func TestOldHoursAreMergedIntoDaysAndExpiredDaysAreDeleted(t *testing.T) {
	dir := t.TempDir()
	h := NewTrafficHistory(dir)
	today := int64(trafficNOW) / Day * Day
	old := today - 10*Day + 5*Hour
	ai, direct := joined(chainAI), joined(chainDirect)
	cleaned := &CleanedTraffic{Usage: []UsageRow{{old, "10.0.0.2", ai, 1, 10, 1}, {old + Hour, "10.0.0.2", ai, 1, 20, 1}, {today - 200*Day, "10.0.0.2", ai, 1, 1, 1}}}
	// 同一目标走两条链路时按合计排名；被挤出的目标按链路并入「其他」
	for i := 0; i < TargetDailyTop+2; i++ {
		cleaned.Targets = append(cleaned.Targets, TargetRow{old, "10.0.0.2", "site" + strconv.Itoa(i) + ".example", ai, 0, int64(1000 - i), 1})
	}
	cleaned.Targets = append(cleaned.Targets, TargetRow{old + Hour, "10.0.0.2", "site101.example", direct, 0, 1, 1})
	_ = h.AddTraffic(context.Background(), "a1", cleaned)
	result, err := h.Flush(t.Context(), 90, at(trafficNOW))
	if err != nil || result.Pruned != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	db := openDB(t, filepath.Join(dir, TrafficFilename))
	eq(t, "usage", queryInts(t, db, "SELECT bucket, span, down FROM traffic_usage"), [][]int64{{today - 10*Day, Day, 30}})
	targets := map[[2]string]int64{}
	rows, _ := db.Query("SELECT target, chain, down FROM traffic_targets")
	for rows.Next() {
		var targetName, chain string
		var down int64
		rows.Scan(&targetName, &chain, &down)
		targets[[2]string{targetName, chain}] = down
	}
	rows.Close()
	if len(targets) != TargetDailyTop+2 {
		t.Fatal(len(targets))
	}
	if targets[[2]string{OtherTarget, ai}] != (1000-TargetDailyTop)+(1000-TargetDailyTop-1) || targets[[2]string{OtherTarget, direct}] != 1 {
		t.Fatal(targets[[2]string{OtherTarget, ai}], targets[[2]string{OtherTarget, direct}])
	}
	if down := query(t, h, "a1", "30d", "source").Total.Down; !testjson.Equal(30, down) {
		t.Fatalf("%s", down)
	}
}

func TestDeleteAndRetainRemoveAgents(t *testing.T) {
	h := NewTrafficHistory(t.TempDir())
	for _, agent := range []string{"a1", "a2", "a3"} {
		_ = h.AddTraffic(context.Background(), agent, cleanAt(t, trafficReport([]any{usage()}, nil, 2)))
	}
	if _, err := h.Flush(t.Context(), DefaultRetention, at(trafficNOW)); err != nil {
		t.Fatal(err)
	}
	_ = h.AddTraffic(context.Background(), "a1", cleanAt(t, trafficReport([]any{usage()}, nil, 2)))
	if err := h.Delete(t.Context(), "a1"); err != nil {
		t.Fatal(err)
	}
	if len(query(t, h, "a1", "24h", "source").Rows) != 0 {
		t.Fatal("a1 kept")
	}
	if n, err := h.RetainAgents(t.Context(), []string{"a2"}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if len(query(t, h, "a3", "24h", "source").Rows) != 0 {
		t.Fatal("a3 kept")
	}
	if down := query(t, h, "a2", "24h", "source").Total.Down; !testjson.Equal(10, down) {
		t.Fatalf("%s", down)
	}
}

// test_flush_task_saves_traffic_history_and_drops_deleted_agents and
// test_deleting_an_agent_deletes_its_traffic_history (the route parts of
// the Python tests are in internal/api/agents).
func TestFlushTaskSavesTrafficHistoryAndDropsDeletedAgents(t *testing.T) {
	svc, _, r, ctx := agentManager(t)
	defer SetTrafficNow(func() float64 { return trafficNOW })()
	_ = svc.Traffic().AddTraffic(ctx, "agent_1", cleanAt(t, trafficReport([]any{usage()}, nil, 2)))
	result, err := svc.FlushNow(ctx, "manual", false)
	if err != nil || result.TrafficRows == nil || *result.TrafficRows != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	history := svc.Traffic()
	if _, err := os.Stat(history.Path); err != nil || history.PendingCount() != 0 {
		t.Fatal("not saved")
	}
	// 导入或恢复配置后不存在的 Agent，下次保存时删除其数据
	if err := r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FlushNow(ctx, "manual", false); err != nil {
		t.Fatal(err)
	}
	eq(t, "rows", queryInts(t, openDB(t, history.Path), "SELECT COUNT(*) FROM traffic_usage"), [][]int64{{0}})
}

func TestDeletingAnAgentDeletesItsTrafficHistory(t *testing.T) {
	svc, manager, _, ctx := agentManager(t)
	_ = svc.Traffic().AddTraffic(ctx, "agent_1", cleanAt(t, trafficReport([]any{usage()}, nil, 2)))
	if ok, err := manager.DeleteAgent(ctx, "agent_1"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if svc.Traffic().PendingCount() != 0 {
		t.Fatal("pending kept")
	}
}

// A failed traffic save is reported as 流量记录：… next to the monitoring
// history result (TrafficHistoryFlushError).
func TestFlushTaskReportsTrafficFailure(t *testing.T) {
	svc, _, _, ctx := agentManager(t)
	history := svc.Traffic()
	_ = history.AddTraffic(ctx, "agent_1", cleanAt(t, trafficReport([]any{usage()}, nil, 2)))
	defer history.SetBrokenConnect(errors.New("database is locked"))()
	defer SetTrafficNow(func() float64 { return trafficNOW })()
	_, err := svc.FlushNow(ctx, "manual", false)
	if err == nil || err.Error() != "流量记录：database is locked" {
		t.Fatalf("err %v", err)
	}
	if history.PendingCount() != 1 {
		t.Fatal("increments lost")
	}
}

func TestRetentionDaysAndChoices(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want int64
	}{
		{`{}`, 90},
		{`{"` + RetentionSetting + `":30}`, 30},
		{`{"` + RetentionSetting + `":365}`, 365},
		{`{"` + RetentionSetting + `":7}`, 90},
		{`{"` + RetentionSetting + `":"90"}`, 90},
		{`{"` + RetentionSetting + `":true}`, 90},
		{`{"` + RetentionSetting + `":180.0}`, 90},
	} {
		if got, err := retentionDaysJSON(json.RawMessage(c.raw)); err != nil || got != c.want {
			t.Errorf("%s: %d %v", c.raw, got, err)
		}
	}
	if got, err := retentionDaysJSON(json.RawMessage(`null`)); err != nil || got != 90 {
		t.Errorf("null system_config: %d %v", got, err)
	}
	if _, err := retentionDaysJSON(json.RawMessage(`[1]`)); err == nil {
		t.Errorf("list system_config accepted")
	}
}

func TestAgentSupported(t *testing.T) {
	for _, c := range []struct {
		version string
		min     string
		want    bool
	}{
		{"1.8.51-go", MinTrafficAgentVersion, true},
		{"1.8.50-go", MinTrafficAgentVersion, false},
		{"1.9.0-go", FullChainAgentVersion, true},
		{"1.8.51-go", FullChainAgentVersion, false},
		{"1.8.60", MinTrafficAgentVersion, false},
		{"", MinTrafficAgentVersion, false},
	} {
		if got := SupportedTrafficVersion(c.version, c.min); got != c.want {
			t.Errorf("%q %s: %v", c.version, c.min, got)
		}
	}
}
