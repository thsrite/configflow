package metrics

// Port of backend/agents/traffic_history.py (A05): Mihomo 流量记录, the
// hourly traffic of every device that passes Mihomo.
//
// Agents report increments every 5 minutes; they are added up in memory and
// written by 系统设置 → 定时任务「监控历史保存」 at the same interval to
// DATA_DIR/metrics/traffic_history.sqlite3, and once more when the service
// stops, never per report. Queries include what is not saved yet.
//
// Two tables:
//   - traffic_usage: device × chain. An empty source is "unattributed"
//     (short connections that started and ended within one sample).
//   - traffic_targets: device × target (registrable domain or IP) × chain.
//     Target "*" is "other" (targets beyond the limit), still split by chain
//     so that direct and proxy totals stay right.
//
// A chain is the names from the entry policy group to the exit node joined
// by ChainSeparator; 1.8.51-go Agents report only group and exit node.
// Hourly rows older than UsageHourlyDays / TargetHourlyDays become daily
// rows (UTC days); daily targets keep only each device's TargetDailyTop
// largest; rows beyond the retention are deleted.
//
// Python kept one TrafficHistory per AgentManager; the Go state is kept per
// repository data directory (TrafficFor / TrafficForManager), like the
// monitoring history.

import (
	"configflow/internal/version"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"configflow/internal/agents"
	"configflow/internal/discovery"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/pathx"
	"configflow/internal/rules"
	"encoding/json"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"
)

var trafficLogger = logx.Get("backend.agents.traffic_history")

// Constants of traffic_history.py.
const (
	TrafficFilename               = "traffic_history.sqlite3"
	TrafficReportMaxContentLength = 4 * 1024 * 1024
	MinTrafficAgentVersion        = "1.8.51-go"
	// FullChainAgentVersion is the first version that reports whole chains.
	FullChainAgentVersion = "1.8.52-go"

	RetentionSetting = "traffic_history_retention_days"
	DefaultRetention = 90

	Hour             = 3600
	Day              = 86400
	UsageHourlyDays  = 7
	TargetHourlyDays = 2
	TargetDailyTop   = 100
	// TrafficReportMaxItems is the item limit of one report (the Agent's
	// memory limit).
	TrafficReportMaxItems = 20000

	Unattributed     = ""
	OtherTarget      = "*"
	ChainSeparator   = "\x1f"
	ChainMaxLayers   = 8
	QueryRowLimit    = 500
	QueryPartLimit   = 50
	trafficTimeoutMs = 10000
)

var (
	// TrafficReportSchemas are REPORT_SCHEMAS: 1 = 1.8.51-go (group and exit
	// node only), 2 = whole chains.
	TrafficReportSchemas = []int{1, 2}
	// RetentionChoices are RETENTION_CHOICES.
	RetentionChoices = []int64{30, 90, 180, 365}
	// Ranges are RANGES (seconds).
	Ranges = map[string]int64{"24h": Day, "7d": 7 * Day, "30d": 30 * Day}
	// Aggregates are AGGREGATES.
	Aggregates = []string{"source", "destination", "chain", "outbound", "group"}

	maxText = map[string]int{"source": 64, "group": 128, "outbound": 128, "domain": 253}
	twoTo62 = new(big.Int).Lsh(big.NewInt(1), 62)
)

// Schema and statements, as in Python.
var trafficSchema = []string{
	"CREATE TABLE IF NOT EXISTS traffic_usage (" +
		" agent_id TEXT NOT NULL, bucket INTEGER NOT NULL, span INTEGER NOT NULL," +
		" source TEXT NOT NULL, chain TEXT NOT NULL," +
		" up INTEGER NOT NULL, down INTEGER NOT NULL, conns INTEGER NOT NULL," +
		" PRIMARY KEY (agent_id, bucket, span, source, chain)) WITHOUT ROWID",
	"CREATE TABLE IF NOT EXISTS traffic_targets (" +
		" agent_id TEXT NOT NULL, bucket INTEGER NOT NULL, span INTEGER NOT NULL," +
		" source TEXT NOT NULL, target TEXT NOT NULL, chain TEXT NOT NULL," +
		" up INTEGER NOT NULL, down INTEGER NOT NULL, conns INTEGER NOT NULL," +
		" PRIMARY KEY (agent_id, bucket, span, source, target, chain)) WITHOUT ROWID",
}

const counters = "SET up = up + excluded.up, down = down + excluded.down, conns = conns + excluded.conns"

func usageUpsert(span int64) string {
	return "INSERT INTO traffic_usage VALUES (?, ?, " + strconv.FormatInt(span, 10) + ", ?, ?, ?, ?, ?) " +
		"ON CONFLICT (agent_id, bucket, span, source, chain) DO UPDATE " + counters + counterUpdateSQL("traffic_usage")
}

func targetUpsert(span int64) string {
	return "INSERT INTO traffic_targets VALUES (?, ?, " + strconv.FormatInt(span, 10) + ", ?, ?, ?, ?, ?, ?) " +
		"ON CONFLICT (agent_id, bucket, span, source, target, chain) DO UPDATE " + counters + counterUpdateSQL("traffic_targets")
}

// legacyChain is _LEGACY_CHAIN: 1.8.51-go tables had only group and exit
// node; the chain becomes those two layers (one when they are equal).
func legacyChain(group string) string {
	return "CASE WHEN " + group + " = '' OR " + group + " = outbound THEN outbound " +
		"WHEN outbound = '' THEN " + group + " ELSE " + group + " || char(31) || outbound END"
}

var legacyMigrations = []struct{ table, statement string }{
	{"usage", "INSERT INTO traffic_usage SELECT agent_id, bucket, span, source, " +
		"CASE WHEN source = '' THEN '' ELSE " + legacyChain("policy_group") + " END, " +
		"SUM(up), SUM(down), SUM(conns) FROM usage WHERE true GROUP BY 1, 2, 3, 4, 5 " +
		"ON CONFLICT (agent_id, bucket, span, source, chain) DO UPDATE " + counters},
	{"targets", "INSERT INTO traffic_targets SELECT agent_id, bucket, span, source, target, outbound, " +
		"SUM(up), SUM(down), SUM(conns) FROM targets WHERE true GROUP BY 1, 2, 3, 4, 5, 6 " +
		"ON CONFLICT (agent_id, bucket, span, source, target, chain) DO UPDATE " + counters},
}

// ---------- settings ----------

// SupportedTrafficVersion reports whether version is a "-go" version at
// least minimum.
func SupportedTrafficVersion(text, minimum string) bool {
	return strings.HasSuffix(text, "-go") && version.CompareAgent(text, minimum) >= 0
}

// ChainLayers is chain_layers(chain).
func ChainLayers(chain string) []string {
	if chain == "" {
		return nil
	}
	return strings.Split(chain, ChainSeparator)
}

// targetKey is _target_key(value): targets become registrable domains; IPs
// stay as they are (normalised); the empty target the Agent folded into
// "other" is "*".
func targetKey(value string) string {
	if value == "" {
		return OtherTarget
	}
	if text, ok := rules.CanonicalIPAddress(value); ok {
		return text
	}
	return discovery.RegistrableDomain(strings.TrimRight(lowerTrafficDomain(value), "."))
}

// ---------- report validation ----------

// UsageRow and TargetRow are the tuples of clean_report.
type UsageRow struct {
	Bucket          int64
	Source, Chain   string
	Up, Down, Conns int64
}

// TargetRow is one cleaned device, target and chain increment.
type TargetRow struct {
	Bucket                int64
	Source, Target, Chain string
	Up, Down, Conns       int64
}

// CleanedTraffic is clean_report's {'usage': [...], 'targets': [...]}.
type CleanedTraffic struct {
	Usage   []UsageRow
	Targets []TargetRow
}

// trafficCount is _count(value): an int (not bool) in [0, 2**62).
func trafficCount(value json.RawMessage) (int64, bool) {
	n, ok := integerJSON(value)
	if !ok || n.Sign() < 0 || n.Cmp(twoTo62) >= 0 {
		return 0, false
	}
	return n.Int64(), true
}

// text is _text(item, field, limit).
func text(item trafficFields, field string, limit int) (string, bool) {
	var value string
	ok := jsondecode.Unmarshal(item[field], &value) == nil && string(item[field]) != "null"
	if !ok || utf8.RuneCountInString(value) > limit {
		return "", false
	}
	return value, true
}

// chainOf is _chain(item, schema, with_group).
func chainOf(item trafficFields, schema2 bool, withGroup bool) (string, bool) {
	if schema2 {
		var layers []string
		ok := jsondecode.Unmarshal(item["chain"], &layers) == nil && layers != nil
		if !ok || len(layers) > ChainMaxLayers {
			return "", false
		}
		names := make([]string, len(layers))
		for i, layer := range layers {
			name := layer
			if name == "" || utf8.RuneCountInString(name) > maxText["outbound"] || strings.Contains(name, ChainSeparator) {
				return "", false
			}
			names[i] = name
		}
		return strings.Join(names, ChainSeparator), true
	}
	outbound, ok := text(item, "outbound", maxText["outbound"])
	if !ok {
		return "", false
	}
	group := ""
	if withGroup {
		if group, ok = text(item, "group", maxText["group"]); !ok {
			return "", false
		}
	}
	var layers []string
	for _, name := range []string{group, outbound} {
		if name != "" {
			layers = append(layers, name)
		}
	}
	if len(layers) == 2 && layers[0] == layers[1] {
		layers = layers[1:]
	}
	return strings.Join(layers, ChainSeparator), true
}

// DecodeTrafficReport decodes Agent JSON with exact member names: the validated rows, or
// nil when the format is wrong. Hours must be whole hours, not older than
// 2 days and not later than the next hour; stale hours are dropped (not a
// format error). now nil = time.time().
func DecodeTrafficReport(report json.RawMessage, at *float64) *CleanedTraffic {
	var object trafficFields
	if jsondecode.Unmarshal(report, &object) != nil || object == nil {
		return nil
	}
	var schema float64
	rawSchema := object["schema"]
	// The released protocol accepts true and integral floating schema versions.
	if string(rawSchema) == "true" {
		schema = 1
	} else if jsondecode.Unmarshal(rawSchema, &schema) != nil {
		return nil
	}
	if schema != 1 && schema != 2 {
		return nil
	}
	schema2 := schema == 2
	var usage, domains []trafficFields
	if jsondecode.Unmarshal(object["usage"], &usage) != nil || usage == nil || jsondecode.Unmarshal(object["domains"], &domains) != nil || domains == nil || len(usage)+len(domains) > TrafficReportMaxItems {
		return nil
	}
	current := trafficNow()
	if at != nil {
		current = *at
	}
	oldest, newest := current-2*Day, current+Hour

	countersOf := func(item trafficFields) ([3]int64, bool) {
		var values [3]int64
		for i, field := range []string{"up", "down", "conns"} {
			value, ok := trafficCount(item[field])
			if !ok {
				return values, false
			}
			values[i] = value
		}
		return values, true
	}
	hour := func(item trafficFields) (int64, bool, bool) {
		value, ok := integerJSON(item["hour"])
		if !ok || new(big.Int).Mod(value, big.NewInt(Hour)).Sign() != 0 {
			return 0, false, false
		}
		// 积压太久或时间不对的整点数据直接丢弃，不算格式错误
		if !value.IsInt64() {
			return 0, true, false
		}
		v := value.Int64()
		return v, true, float64(v) >= oldest && float64(v) <= newest
	}

	result := &CleanedTraffic{Usage: []UsageRow{}, Targets: []TargetRow{}}
	for _, entry := range usage {
		item := entry
		if item == nil {
			return nil
		}
		bucket, okHour, fresh := hour(item)
		source, okSource := text(item, "source", maxText["source"])
		chain, okChain := chainOf(item, schema2, true)
		values, okCounters := countersOf(item)
		if !okHour || !okSource || !okChain || !okCounters {
			return nil
		}
		if !fresh {
			continue
		}
		if source == Unattributed {
			chain = ""
		}
		result.Usage = append(result.Usage, UsageRow{bucket, source, chain, values[0], values[1], values[2]})
	}
	for _, entry := range domains {
		item := entry
		if item == nil {
			return nil
		}
		bucket, okHour, fresh := hour(item)
		source, okSource := text(item, "source", maxText["source"])
		domain, okDomain := text(item, "domain", maxText["domain"])
		chain, okChain := chainOf(item, schema2, false)
		values, okCounters := countersOf(item)
		if !okHour || !okSource || !okDomain || !okChain || !okCounters || source == Unattributed {
			return nil
		}
		if !fresh {
			continue
		}
		result.Targets = append(result.Targets, TargetRow{bucket, source, targetKey(domain), chain, values[0], values[1], values[2]})
	}
	return result
}

// trafficNow is time.time() (tests replace it).
var trafficNow = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// SetTrafficNow replaces the clock of the traffic history and returns a
// function restoring it (tests).
func SetTrafficNow(fn func() float64) func() {
	previous := trafficNow
	trafficNow = fn
	return func() { trafficNow = previous }
}

// ---------- exact counters and SQLite numeric storage ----------

// counter accumulates exact pending integers, promoting to big.Int on overflow.
// Existing SQLite upserts may promote an INTEGER sum to REAL; keeping that
// distinction preserves those files without truncating their numeric values.
type counter struct {
	small   int64
	big     *big.Int
	isFloat bool
	f       float64
}

// add is n += v. A big value is never changed in place (copies of a
// triple share it).
func (n *counter) add(v counter) {
	if n.isFloat || v.isFloat {
		// int + float: float(int) + float. float(int) is out of range only
		// for ints beyond 1e308, which counters below 2**62 per report never
		// reach.
		*n = counter{isFloat: true, f: n.float() + v.float()}
		return
	}
	if n.big == nil && v.big == nil {
		s := n.small + v.small
		if (v.small > 0 && s < n.small) || (v.small < 0 && s > n.small) {
			n.big = new(big.Int).Add(big.NewInt(n.small), big.NewInt(v.small))
			return
		}
		n.small = s
		return
	}
	n.big = new(big.Int).Add(n.toBig(), v.toBig())
}

// float is float(n) (correctly rounded).
func (n counter) float() float64 {
	if n.isFloat {
		return n.f
	}
	if n.big == nil {
		f, _ := new(big.Float).SetInt64(n.small).Float64()
		return f
	}
	f, _ := new(big.Float).SetInt(n.big).Float64()
	return f
}

func (n counter) toBig() *big.Int {
	if n.big != nil {
		return new(big.Int).Set(n.big)
	}
	return big.NewInt(n.small)
}

func (n counter) json() json.RawMessage {
	if n.isFloat {
		return finiteJSON(n.f)
	}
	if n.big != nil {
		return json.RawMessage(n.big.String())
	}
	return json.RawMessage(strconv.FormatInt(n.small, 10))
}

// exact is the value as an exact big.Float (int or float).
func (n counter) exact() *big.Float {
	if n.isFloat {
		return new(big.Float).SetFloat64(n.f)
	}
	return new(big.Float).SetInt(n.toBig())
}

// cmp compares exactly, also between integer and float counters.
func (n counter) cmp(v counter) int {
	// Finite manually edited REAL inputs can overflow while aggregating. NaN
	// has no numeric order: put missing totals after numeric totals, then use
	// the caller's stable key tie-breaker. Never pass NaN to big.Float.
	nMissing := n.isFloat && math.IsNaN(n.f)
	vMissing := v.isFloat && math.IsNaN(v.f)
	if nMissing || vMissing {
		if nMissing == vMissing {
			return 0
		}
		if nMissing {
			return -1
		}
		return 1
	}
	if n.isFloat || v.isFloat {
		return n.exact().Cmp(v.exact())
	}
	if n.big == nil && v.big == nil {
		switch {
		case n.small < v.small:
			return -1
		case n.small > v.small:
			return 1
		}
		return 0
	}
	return n.toBig().Cmp(v.toBig())
}

// sqlValue is the value bound to a statement: an integer beyond int64 or a
// non-finite float cannot be stored (errCounterRange); a float is bound as a
// REAL.
func (n counter) sqlValue() (any, error) {
	if n.isFloat {
		if math.IsNaN(n.f) || math.IsInf(n.f, 0) {
			return nil, errCounterRange
		}
		return n.f, nil
	}
	if n.big != nil {
		if !n.big.IsInt64() {
			return nil, errCounterRange
		}
		return n.big.Int64(), nil
	}
	return n.small, nil
}

// counterFromSQL reads a counter column: INTEGER and finite REAL values are
// counters; anything else (only a hand-edited file) is errCounterInvalid.
func counterFromSQL(value any) (counter, error) {
	switch v := value.(type) {
	case int64:
		return counter{small: v}, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return counter{}, errCounterInvalid
		}
		return counter{isFloat: true, f: v}, nil
	}
	return counter{}, errCounterInvalid
}

func tripleFromSQL(up, down, conns any) (triple, error) {
	var t triple
	for i, value := range []any{up, down, conns} {
		v, err := counterFromSQL(value)
		if err != nil {
			return t, err
		}
		t[i] = v
	}
	return t, nil
}

type triple [3]counter

func tripleOf(up, down, conns int64) triple {
	return triple{{small: up}, {small: down}, {small: conns}}
}

func (t *triple) add(v triple) {
	for i := range t {
		t[i].add(v[i])
	}
}

// ---------- the history ----------

type usageKey struct {
	agent  string
	bucket int64
	source string
	chain  string
}

type targetKey2 struct {
	agent  string
	bucket int64
	source string
	target string
	chain  string
}

// TrafficHistory is TrafficHistory: the increments waiting to be saved plus
// the SQLite file; every method is safe for concurrent use.
type TrafficHistory struct {
	warnings warningLimiter
	DataDir  string
	Path     string

	mu      sync.Mutex
	usage   map[usageKey]*triple
	targets map[targetKey2]*triple

	// connectHook replaces _connect (tests: a broken database).
	connectHook func() (*trafficConn, error)
}

// NewTrafficHistory is TrafficHistory(data_dir).
func NewTrafficHistory(dataDir string) *TrafficHistory {
	return &TrafficHistory{
		DataDir: dataDir, Path: filepath.Join(dataDir, TrafficFilename),
		usage: map[usageKey]*triple{}, targets: map[targetKey2]*triple{},
	}
}

var (
	trafficHistoriesMu sync.Mutex
	trafficHistories   = map[string]*TrafficHistory{}
)

// TrafficFor returns the TrafficHistory of a repository data directory
// (os.path.join(repository.data_dir, 'metrics')), creating it on first use.
func TrafficFor(dataDir string) *TrafficHistory {
	trafficHistoriesMu.Lock()
	defer trafficHistoriesMu.Unlock()
	h := trafficHistories[dataDir]
	if h == nil {
		h = NewTrafficHistory(filepath.Join(dataDir, "metrics"))
		trafficHistories[dataDir] = h
	}
	return h
}

// TrafficForManager is manager.traffic_history.
func TrafficForManager(m *agents.Manager) *TrafficHistory {
	return TrafficFor(m.Repository().DataDir())
}

// ResetTraffic forgets every TrafficHistory (tests).
func ResetTraffic() {
	trafficHistoriesMu.Lock()
	trafficHistories = map[string]*TrafficHistory{}
	trafficHistoriesMu.Unlock()
}

// SetBrokenConnect makes every database access of h fail with err until
// the returned function is called (tests: monkeypatched _connect).
func (h *TrafficHistory) SetBrokenConnect(err error) func() {
	h.mu.Lock()
	h.connectHook = func() (*trafficConn, error) { return nil, err }
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		h.connectHook = nil
		h.mu.Unlock()
	}
}

// AddReport is add_report(agent_id, cleaned).
// AddTraffic admits validated increments only while their owner is live.
func (h *TrafficHistory) AddTraffic(ctx context.Context, agentID string, cleaned *CleanedTraffic) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if cleaned == nil {
		return errors.New("missing traffic report")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, row := range cleaned.Usage {
		addTriple(h.usage, usageKey{agentID, row.Bucket, row.Source, row.Chain}, tripleOf(row.Up, row.Down, row.Conns))
	}
	for _, row := range cleaned.Targets {
		addTriple(h.targets, targetKey2{agentID, row.Bucket, row.Source, row.Target, row.Chain}, tripleOf(row.Up, row.Down, row.Conns))
	}
	return nil
}

func addTriple[K comparable](table map[K]*triple, key K, values triple) {
	if current := table[key]; current != nil {
		current.add(values)
		return
	}
	copied := values
	table[key] = &copied
}

// PendingCount is pending_count().
func (h *TrafficHistory) PendingCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.usage) + len(h.targets)
}

// trafficConn is one SQLite connection (one per operation); tx is the open
// transaction of a change, if any.
type trafficConn struct {
	db   *sql.DB
	conn *sql.Conn
	tx   *sql.Tx
}

var bg = context.Background()

// querier is the open transaction, or the connection outside one.
func (c *trafficConn) querier() interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
} {
	if c.tx != nil {
		return c.tx
	}
	return c.conn
}

func (c *trafficConn) exec(statement string, args ...any) (int64, error) {
	return c.execContext(bg, statement, args...)
}
func (c *trafficConn) execContext(ctx context.Context, statement string, args ...any) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	result, err := c.querier().ExecContext(ctx, statement, args...)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}

// beginContext starts a transaction (BEGIN IMMEDIATE: the connection is
// opened with _txlock=immediate, so writers queue on busy_timeout instead of
// failing on a lock upgrade).
func (c *trafficConn) beginContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := c.conn.BeginTx(ctx, nil)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("begin traffic transaction: %w", err)
	}
	c.tx = tx
	return nil
}

func (c *trafficConn) commitContext(ctx context.Context) error {
	tx := c.tx
	c.tx = nil
	if err := ctx.Err(); err != nil {
		logRollback(tx)
		return err
	}
	if err := tx.Commit(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("commit traffic transaction: %w", err)
	}
	return nil
}

// rollbackContext ends an open transaction without saving it. The caller
// returns the error that caused it; a failed ROLLBACK is only logged
// (closing the connection discards whatever is still open).
func (c *trafficConn) rollbackContext(context.Context) error {
	if c.tx != nil {
		logRollback(c.tx)
		c.tx = nil
	}
	return nil
}

// logRollback rolls tx back and logs a failure. sql.ErrTxDone means
// database/sql already rolled it back because its context ended.
func logRollback(tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		trafficLogger.Warningf(msgTrafficRollbackFailed, err)
	}
}

// inTransaction runs body in one transaction: committed when body succeeds,
// rolled back otherwise.
func (c *trafficConn) inTransaction(ctx context.Context, body func() error) error {
	if err := c.beginContext(ctx); err != nil {
		return err
	}
	if err := body(); err != nil {
		c.rollbackContext(ctx)
		return err
	}
	return c.commitContext(ctx)
}

func (c *trafficConn) close() {
	if c.tx != nil {
		logRollback(c.tx)
		c.tx = nil
	}
	c.conn.Close()
	c.db.Close()
}
func (c *trafficConn) query(statement string, args []any, scan func(*sql.Rows) error) error {
	return c.queryContext(bg, statement, args, scan)
}
func (c *trafficConn) queryContext(ctx context.Context, statement string, args []any, scan func(*sql.Rows) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rows, err := c.querier().QueryContext(ctx, statement, args...)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return rows.Err()
}

// connect is _connect(): create the directory, open the file (journal mode
// DELETE like the configuration database: no -wal file in backups or the
// data volume), create the tables and migrate 1.8.51-go tables.
func (h *TrafficHistory) connect() (*trafficConn, error) { return h.connectContext(bg) }
func (h *TrafficHistory) connectContext(ctx context.Context) (*trafficConn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h.connectHook != nil {
		return h.connectHook()
	}
	if err := os.MkdirAll(h.DataDir, 0o777); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(h.Path)
	if err != nil {
		return nil, err
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "_txlock=immediate"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}
	c := &trafficConn{db: db, conn: conn}
	// Closed unless it is handed to the caller, also when the migration
	// panics: an open BEGIN IMMEDIATE would block every later writer.
	opened := false
	defer func() {
		if !opened {
			c.close()
		}
	}()
	statements := append([]string{"PRAGMA busy_timeout=" + strconv.Itoa(trafficTimeoutMs), "PRAGMA journal_mode=DELETE"}, trafficSchema...)
	for _, statement := range statements {
		if _, err := c.execContext(ctx, statement); err != nil {
			return nil, err
		}
	}
	if err := migrateLegacy(ctx, c); err != nil {
		return nil, err
	}
	opened = true
	return c, nil
}

// migrateLegacy is _migrate_legacy: fold the 1.8.51-go tables into the
// chain tables and drop them; happens only on the first open.
func migrateLegacy(ctx context.Context, c *trafficConn) error {
	legacy := map[string]bool{}
	err := c.queryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('usage', 'targets')", nil, func(rows *sql.Rows) error {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		legacy[name] = true
		return nil
	})
	if err != nil || len(legacy) == 0 {
		return err
	}
	err = c.inTransaction(ctx, func() error {
		for _, migration := range legacyMigrations {
			if !legacy[migration.table] {
				continue
			}
			if _, err := c.execContext(ctx, migration.statement); err != nil {
				return err
			}
			if _, err := c.execContext(ctx, "DROP TABLE "+migration.table); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	trafficLogger.Info("流量记录已迁移为按链路保存")
	return nil
}

// floorDiv is float floor division (float.__floordiv__).
func floorDiv(value, divisor float64) float64 { return math.Floor(value / divisor) }

// floorDivInt is int // int.
func floorDivInt(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// TrafficFlushResult is flush()'s {'rows', 'compacted', 'pruned'}.
type TrafficFlushResult struct {
	Rows, Compacted, Pruned int64
}

// Flush is flush(retention, now): save the increments, fold old hours into
// days and delete expired rows. On failure the increments stay in memory
// (merged with reports that arrived meanwhile) and the error is returned.
// at nil = time.time().
func (h *TrafficHistory) Flush(retention int64, at *float64) (TrafficFlushResult, error) {
	return h.FlushContext(bg, retention, at)
}

// FlushContext keeps the caller's work lifetime through SQLite commit.
func (h *TrafficHistory) FlushContext(ctx context.Context, retention int64, at *float64) (TrafficFlushResult, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return TrafficFlushResult{}, err
	}
	current := trafficNow()
	if at != nil {
		current = *at
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return TrafficFlushResult{}, err
	}
	usage, targets := h.usage, h.targets
	if len(usage) == 0 && len(targets) == 0 && !pathx.Accessible(h.Path) {
		return TrafficFlushResult{}, nil
	}
	h.usage, h.targets = map[usageKey]*triple{}, map[targetKey2]*triple{}
	result, err := h.save(ctx, usage, targets, retention, current)
	if err != nil {
		// 放回去，与保存期间新收到的上报合并，下次再保存
		for key, values := range usage {
			addTriple(h.usage, key, *values)
		}
		for key, values := range targets {
			addTriple(h.targets, key, *values)
		}
		return TrafficFlushResult{}, err
	}
	result.Rows = int64(len(usage) + len(targets))
	return result, nil
}

func (h *TrafficHistory) save(ctx context.Context, usage map[usageKey]*triple, targets map[targetKey2]*triple, retention int64, now float64) (TrafficFlushResult, error) {
	c, err := h.connectContext(ctx)
	if err != nil {
		return TrafficFlushResult{}, err
	}
	defer c.close()
	var result TrafficFlushResult
	err = c.inTransaction(ctx, func() error {
		statement := usageUpsert(Hour)
		for key, values := range usage {
			up, down, conns, err := sqlTriple(values)
			if err != nil {
				return err
			}
			if err := c.upsertContext(ctx, statement, key.agent, key.bucket, key.source, key.chain, up, down, conns); err != nil {
				return err
			}
		}
		statement = targetUpsert(Hour)
		for key, values := range targets {
			up, down, conns, err := sqlTriple(values)
			if err != nil {
				return err
			}
			if err := c.upsertContext(ctx, statement, key.agent, key.bucket, key.source, key.target, key.chain, up, down, conns); err != nil {
				return err
			}
		}
		compacted, err := compact(ctx, c, now)
		if err != nil {
			return err
		}
		pruned, err := pruneRows(ctx, c, now-float64(retention)*Day)
		if err != nil {
			return err
		}
		result = TrafficFlushResult{Compacted: compacted, Pruned: pruned}
		return nil
	})
	if err != nil {
		return TrafficFlushResult{}, err
	}
	return result, nil
}

func sqlTriple(values *triple) (any, any, any, error) {
	var out [3]any
	for i, value := range values {
		v, err := value.sqlValue()
		if err != nil {
			return nil, nil, nil, err
		}
		out[i] = v
	}
	return out[0], out[1], out[2], nil
}

// compact is _compact(connection, now): fold older hourly rows into daily
// rows; daily targets keep only each device's largest, the rest go into
// "other".
func compact(ctx context.Context, c *trafficConn, now float64) (int64, error) {
	var compacted int64
	usageBefore := int64(floorDiv(now, Day)*Day - UsageHourlyDays*Day)
	var usageRows [][]any
	err := c.queryContext(ctx, "SELECT agent_id, bucket / 86400 * 86400, source, chain, SUM(up), SUM(down), SUM(conns), MIN("+numericCountersSQL("")+") "+
		"FROM traffic_usage WHERE span = 3600 AND bucket < ? GROUP BY 1, 2, 3, 4", []any{usageBefore}, func(rows *sql.Rows) error {
		row := make([]any, 7)
		if err := validateGroupedCounters(rows, row); err != nil {
			return err
		}
		usageRows = append(usageRows, row)
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(usageRows) > 0 {
		n, err := c.execContext(ctx, "DELETE FROM traffic_usage WHERE span = 3600 AND bucket < ?", usageBefore)
		if err != nil {
			return 0, err
		}
		compacted += n
		statement := usageUpsert(Day)
		for _, row := range usageRows {
			if err := c.upsertContext(ctx, statement, row...); err != nil {
				return 0, err
			}
		}
	}
	targetsBefore := int64(floorDiv(now, Day)*Day - TargetHourlyDays*Day)
	var targetRows [][]any
	err = c.queryContext(ctx, "SELECT agent_id, bucket / 86400 * 86400, source, target, chain, SUM(up), SUM(down), SUM(conns), MIN("+numericCountersSQL("")+") "+
		"FROM traffic_targets WHERE span = 3600 AND bucket < ? GROUP BY 1, 2, 3, 4, 5", []any{targetsBefore}, func(rows *sql.Rows) error {
		row := make([]any, 8)
		if err := validateGroupedCounters(rows, row); err != nil {
			return err
		}
		targetRows = append(targetRows, row)
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(targetRows) > 0 {
		n, err := c.execContext(ctx, "DELETE FROM traffic_targets WHERE span = 3600 AND bucket < ?", targetsBefore)
		if err != nil {
			return 0, err
		}
		compacted += n
		statement := targetUpsert(Day)
		for _, row := range targetRows {
			if err := c.upsertContext(ctx, statement, row...); err != nil {
				return 0, err
			}
		}
		type agentDay struct {
			agent any
			day   any
		}
		seen := map[agentDay]bool{}
		for _, row := range targetRows {
			key := agentDay{scalarKey(row[0]), scalarKey(row[1])}
			if seen[key] {
				continue
			}
			seen[key] = true
			n, err := trimDay(ctx, c, row[0], row[1])
			if err != nil {
				return 0, err
			}
			compacted += n
		}
	}
	return compacted, nil
}

// scalarKey makes a scanned SQLite value usable as a map key ([]byte → string).
func scalarKey(value any) any {
	if b, ok := value.([]byte); ok {
		return string(b)
	}
	return value
}

type targetDayRow struct {
	source, target, chain string
	v                     triple
}

// trimDay is _trim_day(connection, agent_id, day): each device keeps only
// the day's TargetDailyTop largest targets (ranked by the total over all
// chains); the others are merged per chain into "other".
func trimDay(ctx context.Context, c *trafficConn, agentID, day any) (int64, error) {
	var rows []targetDayRow
	err := c.queryContext(ctx, "SELECT source, target, chain, up, down, conns FROM traffic_targets WHERE agent_id = ? AND bucket = ? AND span = 86400",
		[]any{agentID, day}, func(scan *sql.Rows) error {
			var row targetDayRow
			var up, down, conns any
			if err := scan.Scan(&row.source, &row.target, &row.chain, &up, &down, &conns); err != nil {
				return err
			}
			v, err := tripleFromSQL(up, down, conns)
			if err != nil {
				return err
			}
			row.v = v
			rows = append(rows, row)
			return nil
		})
	if err != nil {
		return 0, err
	}
	type pair struct{ source, target string }
	totals := map[pair]*counter{}
	var order []pair
	for _, row := range rows {
		if row.target == OtherTarget {
			continue
		}
		key := pair{row.source, row.target}
		if totals[key] == nil {
			totals[key] = &counter{}
			order = append(order, key)
		}
		totals[key].add(row.v[0])
		totals[key].add(row.v[1])
	}
	sort.SliceStable(order, func(i, j int) bool {
		if c := totals[order[i]].cmp(*totals[order[j]]); c != 0 {
			return c > 0
		}
		return order[i].target < order[j].target
	})
	ranked := map[string]int{}
	dropped := map[pair]bool{}
	for _, key := range order {
		ranked[key.source]++
		if ranked[key.source] > TargetDailyTop {
			dropped[key] = true
		}
	}
	if len(dropped) == 0 {
		return 0, nil
	}
	type sourceChain struct{ source, chain string }
	merged := map[sourceChain]*triple{}
	var mergedOrder []sourceChain
	var extra []targetDayRow
	for _, row := range rows {
		if !dropped[pair{row.source, row.target}] {
			continue
		}
		extra = append(extra, row)
		key := sourceChain{row.source, row.chain}
		if merged[key] == nil {
			merged[key] = &triple{}
			mergedOrder = append(mergedOrder, key)
		}
		merged[key].add(row.v)
	}
	for _, row := range extra {
		if _, err := c.execContext(ctx, "DELETE FROM traffic_targets WHERE agent_id = ? AND bucket = ? AND span = 86400 AND source = ? AND target = ? AND chain = ?",
			agentID, day, row.source, row.target, row.chain); err != nil {
			return 0, err
		}
	}
	statement := targetUpsert(Day)
	for _, key := range mergedOrder {
		up, down, conns, err := sqlTriple(merged[key])
		if err != nil {
			return 0, err
		}
		if err := c.upsertContext(ctx, statement, agentID, day, key.source, OtherTarget, key.chain, up, down, conns); err != nil {
			return 0, err
		}
	}
	return int64(len(extra)), nil
}

// pruneRows is _prune(connection, cutoff): daily rows expire only as a
// whole day.
func pruneRows(ctx context.Context, c *trafficConn, cutoff float64) (int64, error) {
	var pruned int64
	for _, table := range []string{"traffic_usage", "traffic_targets"} {
		n, err := c.execContext(ctx, "DELETE FROM "+table+" WHERE bucket + span <= ?", int64(cutoff))
		if err != nil {
			return 0, err
		}
		pruned += n
	}
	return pruned, nil
}

// deleteAgents is _delete_agents(connection, agent_ids).
func deleteAgents(ctx context.Context, c *trafficConn, agentIDs []string) error {
	return c.inTransaction(ctx, func() error {
		for _, agentID := range agentIDs {
			for _, statement := range []string{"DELETE FROM traffic_usage WHERE agent_id = ?", "DELETE FROM traffic_targets WHERE agent_id = ?"} {
				if _, err := c.execContext(ctx, statement, agentID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Delete is delete(agent_id): forget the Agent's increments and rows.
func (h *TrafficHistory) Delete(agentID string) error { return h.DeleteContext(bg, agentID) }

// DeleteContext keeps the caller's work lifetime through SQLite commit.
func (h *TrafficHistory) DeleteContext(ctx context.Context, agentID string) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	for key := range h.usage {
		if key.agent == agentID {
			delete(h.usage, key)
		}
	}
	for key := range h.targets {
		if key.agent == agentID {
			delete(h.targets, key)
		}
	}
	if !pathx.Accessible(h.Path) {
		return nil
	}
	c, err := h.connectContext(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	return deleteAgents(ctx, c, []string{agentID})
}

// RetainAgents is retain_agents(agent_ids): delete the data of Agents that
// no longer exist (after an import, restore or reset); the number of
// Agents removed from the file.
func (h *TrafficHistory) RetainAgents(agentIDs []string) (int64, error) {
	return h.RetainAgentsContext(bg, agentIDs)
}

// RetainAgentsContext keeps the caller's work lifetime through SQLite commit.
func (h *TrafficHistory) RetainAgentsContext(ctx context.Context, agentIDs []string) (int64, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	keep := map[string]bool{}
	for _, id := range agentIDs {
		keep[id] = true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	for key := range h.usage {
		if !keep[key.agent] {
			delete(h.usage, key)
		}
	}
	for key := range h.targets {
		if !keep[key.agent] {
			delete(h.targets, key)
		}
	}
	if !pathx.Accessible(h.Path) {
		return 0, nil
	}
	c, err := h.connectContext(ctx)
	if err != nil {
		return 0, err
	}
	defer c.close()
	var removed []string
	err = c.queryContext(ctx, "SELECT DISTINCT agent_id FROM traffic_usage UNION SELECT DISTINCT agent_id FROM traffic_targets", nil, func(rows *sql.Rows) error {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if !keep[id] {
			removed = append(removed, id)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(removed) > 0 {
		if err := deleteAgents(ctx, c, removed); err != nil {
			return 0, err
		}
	}
	return int64(len(removed)), nil
}

// ---------- query ----------

type usageRecord struct {
	bucket, span  int64
	source, chain string
	v             triple
}

type targetRecord struct {
	bucket, span          int64
	source, target, chain string
	v                     triple
}

// rowsInRange is _rows(agent_id, start, with_targets): the rows of the
// range; a daily row counts when it overlaps the range.
func (h *TrafficHistory) rowsInRange(ctx context.Context, agentID string, start float64, withTargets bool) ([]usageRecord, []targetRecord, error) {
	var usage []usageRecord
	var targets []targetRecord
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, values := range h.usage {
		if key.agent == agentID && float64(key.bucket+Hour) > start {
			usage = append(usage, usageRecord{key.bucket, Hour, key.source, key.chain, *values})
		}
	}
	if withTargets {
		for key, values := range h.targets {
			if key.agent == agentID && float64(key.bucket+Hour) > start {
				targets = append(targets, targetRecord{key.bucket, Hour, key.source, key.target, key.chain, *values})
			}
		}
	}
	if !pathx.Accessible(h.Path) {
		return usage, targets, nil
	}
	c, err := h.connectContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer c.close()
	err = c.queryContext(ctx, "SELECT bucket, span, source, chain, up, down, conns FROM traffic_usage "+
		"WHERE agent_id = ? AND bucket + span > ?", []any{agentID, int64(start)}, func(rows *sql.Rows) error {
		var r usageRecord
		var up, down, conns any
		if err := rows.Scan(&r.bucket, &r.span, &r.source, &r.chain, &up, &down, &conns); err != nil {
			h.warnCorrupt(agentID, "traffic_usage")
			return nil
		}
		v, err := tripleFromSQL(up, down, conns)
		if err != nil {
			h.warnCorrupt(agentID, "traffic_usage.counter")
			return nil
		}
		r.v = v
		usage = append(usage, r)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if withTargets {
		err = c.queryContext(ctx, "SELECT bucket, span, source, target, chain, up, down, conns FROM traffic_targets "+
			"WHERE agent_id = ? AND bucket + span > ?", []any{agentID, int64(start)}, func(rows *sql.Rows) error {
			var r targetRecord
			var up, down, conns any
			if err := rows.Scan(&r.bucket, &r.span, &r.source, &r.target, &r.chain, &up, &down, &conns); err != nil {
				h.warnCorrupt(agentID, "traffic_targets")
				return nil
			}
			v, err := tripleFromSQL(up, down, conns)
			if err != nil {
				h.warnCorrupt(agentID, "traffic_targets.counter")
				return nil
			}
			r.v = v
			targets = append(targets, r)
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return usage, targets, nil
}

type aggregate struct {
	order  []string
	values map[string]*triple
}

func newAggregate() *aggregate { return &aggregate{values: map[string]*triple{}} }

func (a *aggregate) add(key string, values triple) {
	if current := a.values[key]; current != nil {
		current.add(values)
		return
	}
	copied := values
	a.values[key] = &copied
	a.order = append(a.order, key)
}

// rowsOf is _rows(table, limit): sorted by traffic (up + down) descending,
// then key.
func rowsOf(a *aggregate, limit int) []TrafficRow {
	type entry struct {
		key   string
		v     *triple
		total counter
	}
	entries := make([]entry, 0, len(a.values))
	for _, key := range a.order {
		v := a.values[key]
		total := v[0]
		total.add(v[1])
		entries = append(entries, entry{key, v, total})
	}
	sort.Slice(entries, func(i, j int) bool {
		if c := entries[i].total.cmp(entries[j].total); c != 0 {
			return c > 0
		}
		return entries[i].key < entries[j].key
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]TrafficRow, len(entries))
	for i, e := range entries {
		out[i] = TrafficRow{Key: e.key, TrafficTotals: trafficTotals(*e.v)}
	}
	return out
}

// isoUTC formats a Unix time in seconds as UTC ISO 8601 with microseconds
// ("+00:00" suffix). The fraction is rounded to the nearest microsecond with
// ties to even: the trend points are whole seconds, but the "start" of a
// query is now minus the range, whose fraction can be an exact tie (for
// example 1/128 s = 7812.5 µs); ties to even keeps the values the API has
// always returned.
func isoUTC(ts float64) string {
	whole, frac := math.Modf(ts)
	us := math.RoundToEven(frac * 1e6)
	if us >= 1e6 {
		us -= 1e6
		whole += 1
	} else if us < 0 {
		us += 1e6
		whole -= 1
	}
	t := time.Unix(int64(whole), int64(us)*1000).UTC()
	text := t.Format("2006-01-02T15:04:05")
	if micro := int64(us); micro != 0 {
		text += "." + leftPad(strconv.FormatInt(micro, 10), 6)
	}
	return text + "+00:00"
}

func leftPad(s string, width int) string {
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// QueryTraffic returns typed aggregates for one Agent: the rows of the range
// grouped by `by` (with their parts), totals, totals per exit node and the
// trend. Parts: by device, target or policy group they are split by chain;
// by chain or exit node they are split by device. Unattributed traffic has
// the empty key and no parts. The trend is hourly for 24 h and 7 d, daily
// for 30 d. rangeKey must be a key of Ranges. at nil = time.time().
func (h *TrafficHistory) QueryTraffic(ctx context.Context, agentID, rangeKey, by string, at *float64) (*TrafficQuery, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := Ranges[rangeKey]; !ok {
		return nil, errors.New("invalid traffic range")
	}
	validBy := false
	for _, choice := range Aggregates {
		if by == choice {
			validBy = true
			break
		}
	}
	if !validBy {
		return nil, errors.New("invalid traffic aggregate")
	}
	current := trafficNow()
	if at != nil {
		current = *at
	}
	start := current - float64(Ranges[rangeKey])
	step := int64(Hour)
	if rangeKey == "30d" {
		step = Day
	}
	usageRows, targetRows, err := h.rowsInRange(ctx, agentID, start, by == "destination")
	if err != nil {
		return nil, err
	}
	var totals triple
	outbounds := newAggregate()
	trend := map[int64]*[2]counter{}
	grouped := newAggregate()
	parts := map[string]*aggregate{}
	add := func(key string, part *string, values triple) {
		grouped.add(key, values)
		if part != nil {
			if parts[key] == nil {
				parts[key] = newAggregate()
			}
			parts[key].add(*part, values)
		}
	}
	for _, row := range usageRows {
		values := row.v
		totals.add(values)
		at := floorDivInt(row.bucket, step) * step
		point := trend[at]
		if point == nil {
			point = &[2]counter{}
			trend[at] = point
		}
		point[0].add(values[0])
		point[1].add(values[1])
		if row.source == Unattributed {
			add(Unattributed, nil, values)
			continue
		}
		layers := ChainLayers(row.chain)
		outbound := ""
		if len(layers) > 0 {
			outbound = layers[len(layers)-1]
		}
		outbounds.add(outbound, values)
		source, chain := row.source, row.chain
		switch by {
		case "source":
			add(source, &chain, values)
		case "chain":
			add(chain, &source, values)
		case "outbound":
			add(outbound, &source, values)
		case "group":
			group := ""
			if len(layers) > 0 {
				group = layers[0]
			}
			add(group, &chain, values)
		}
	}
	for _, row := range targetRows {
		chain := row.chain
		add(row.target, &chain, row.v)
	}

	rows := rowsOf(grouped, QueryRowLimit)
	for i := range rows {
		row := &rows[i]
		rowParts := parts[row.Key]
		if rowParts == nil {
			rowParts = newAggregate()
		}
		row.Parts = rowsOf(rowParts, QueryPartLimit)
		count := len(rowParts.values)
		row.PartCount = &count
	}
	first := int64(floorDiv(start, float64(step)) * float64(step))
	points := []TrafficPoint{}
	for at := first; at < int64(current)+1; at += step {
		up, down := json.RawMessage("0"), json.RawMessage("0")
		if point := trend[at]; point != nil {
			up, down = point[0].json(), point[1].json()
		}
		points = append(points, TrafficPoint{At: isoUTC(float64(at)), Up: up, Down: down})
	}
	omitted := len(grouped.values) - QueryRowLimit
	if omitted < 0 {
		omitted = 0
	}
	result := &TrafficQuery{Range: rangeKey, By: by, Step: step, ChainSeparator: ChainSeparator, Start: isoUTC(start), Rows: rows, Omitted: omitted, Total: trafficTotals(totals), Outbounds: rowsOf(outbounds, len(outbounds.values)), Trend: points}
	if queryMissingCounter(result) {
		h.warnCorrupt(agentID, "aggregate")
	}
	return result, nil
}

// queryMissingCounter detects generated non-finite sums, which the JSON DTO
// renders as null under the established native missing-value policy.
func queryMissingCounter(q *TrafficQuery) bool {
	missing := func(t TrafficTotals) bool {
		return string(t.Up) == "null" || string(t.Down) == "null" || string(t.Conns) == "null"
	}
	if missing(q.Total) {
		return true
	}
	for _, rows := range [][]TrafficRow{q.Rows, q.Outbounds} {
		for _, row := range rows {
			if missing(row.TrafficTotals) {
				return true
			}
			for _, part := range row.Parts {
				if missing(part.TrafficTotals) {
					return true
				}
			}
		}
	}
	for _, point := range q.Trend {
		if string(point.Up) == "null" || string(point.Down) == "null" {
			return true
		}
	}
	return false
}
