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
	"context"
	"database/sql"
	"errors"
	"math"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"configflow/internal/agents"
	"configflow/internal/discovery"
	"configflow/internal/jsonx"
	"configflow/internal/logx"

	"modernc.org/sqlite"
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
		"ON CONFLICT (agent_id, bucket, span, source, chain) DO UPDATE " + counters
}

func targetUpsert(span int64) string {
	return "INSERT INTO traffic_targets VALUES (?, ?, " + strconv.FormatInt(span, 10) + ", ?, ?, ?, ?, ?, ?) " +
		"ON CONFLICT (agent_id, bucket, span, source, target, chain) DO UPDATE " + counters
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

// RetentionDays is retention_days(system): the stored choice, or the
// default when it is missing or not one of the choices (bool is not int).
// A system_config that is truthy but not a dict raised AttributeError.
func RetentionDays(system *jsonx.Object) (int64, error) {
	config := system.Value("system_config")
	if !jsonx.Truthy(config) {
		return DefaultRetention, nil
	}
	object, ok := config.(*jsonx.Object)
	if !ok {
		return 0, &AttributeError{Message: "'" + PyTypeName(config) + "' object has no attribute 'get'"}
	}
	value, present := object.Get(RetentionSetting)
	if !present {
		return DefaultRetention, nil
	}
	if days, ok := retentionChoice(value); ok {
		return days, nil
	}
	return DefaultRetention, nil
}

func retentionChoice(value any) (int64, bool) {
	number, ok := value.(jsonx.Number)
	if !ok {
		return 0, false
	}
	days, ok := number.Int64()
	if !ok {
		return 0, false
	}
	for _, choice := range RetentionChoices {
		if days == choice {
			return days, true
		}
	}
	return 0, false
}

// RetentionValueError is the ValueError of set_retention_days.
var RetentionValueError = &ValueError{Message: "保留时长只能是 30、90、180 或 365 天"}

// ValueError is Python's ValueError.
type ValueError struct{ Message string }

func (e *ValueError) Error() string { return e.Message }

// SetRetentionDays is set_retention_days(days): store the choice in
// system_config (system.setdefault('system_config', {}).update(...)).
func SetRetentionDays(ctx context.Context, m *agents.Manager, value any) (int64, error) {
	days, ok := retentionChoice(value)
	if !ok {
		return 0, RetentionValueError
	}
	_, err := m.Repository().UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		config := system.SetDefault("system_config", jsonx.NewObject())
		object, ok := config.(*jsonx.Object)
		if !ok || object == nil {
			return nil, &AttributeError{Message: "'" + PyTypeName(config) + "' object has no attribute 'update'"}
		}
		object.Set(RetentionSetting, days)
		return nil, nil
	})
	if err != nil {
		return 0, err
	}
	return days, nil
}

// AgentSupported is agent_supported(version, minimum): a "-go" version at
// least minimum.
func AgentSupported(version any, minimum string) bool {
	text, ok := version.(string)
	return ok && strings.HasSuffix(text, "-go") && agents.CompareVersions(text, minimum) >= 0
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
	if text, ok := ipAddressString(value); ok {
		return text
	}
	return discovery.RegistrableDomain(strings.TrimRight(pyLower(value), "."))
}

// ---------- report validation ----------

// UsageRow and TargetRow are the tuples of clean_report.
type UsageRow struct {
	Bucket          int64
	Source, Chain   string
	Up, Down, Conns int64
}

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
func trafficCount(value any) (int64, bool) {
	number, ok := value.(jsonx.Number)
	if !ok {
		return 0, false
	}
	n, ok := number.BigInt()
	if !ok || n.Sign() < 0 || n.Cmp(twoTo62) >= 0 {
		return 0, false
	}
	return n.Int64(), true
}

// text is _text(item, field, limit).
func text(item *jsonx.Object, field string, limit int) (string, bool) {
	value, ok := item.Value(field).(string)
	if !ok || utf8.RuneCountInString(value) > limit {
		return "", false
	}
	return value, true
}

// chainOf is _chain(item, schema, with_group).
func chainOf(item *jsonx.Object, schema2 bool, withGroup bool) (string, bool) {
	if schema2 {
		layers, ok := item.Value("chain").([]any)
		if !ok || len(layers) > ChainMaxLayers {
			return "", false
		}
		names := make([]string, len(layers))
		for i, layer := range layers {
			name, ok := layer.(string)
			if !ok || name == "" || utf8.RuneCountInString(name) > maxText["outbound"] || strings.Contains(name, ChainSeparator) {
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

// CleanTrafficReport is clean_report(report, now): the validated rows, or
// nil when the format is wrong. Hours must be whole hours, not older than
// 2 days and not later than the next hour; stale hours are dropped (not a
// format error). now nil = time.time().
func CleanTrafficReport(report any, at *float64) *CleanedTraffic {
	object, ok := report.(*jsonx.Object)
	if !ok || object == nil {
		return nil
	}
	schemaValue := object.Value("schema")
	known := false
	for _, schema := range TrafficReportSchemas {
		if jsonx.Equal(schemaValue, jsonx.Int(schema)) {
			known = true
		}
	}
	if !known {
		return nil
	}
	schema2 := jsonx.Equal(schemaValue, jsonx.Int(2))
	usage, ok1 := object.Value("usage").([]any)
	domains, ok2 := object.Value("domains").([]any)
	if !ok1 || !ok2 || len(usage)+len(domains) > TrafficReportMaxItems {
		return nil
	}
	current := trafficNow()
	if at != nil {
		current = *at
	}
	oldest, newest := current-2*Day, current+Hour

	countersOf := func(item *jsonx.Object) ([3]int64, bool) {
		var values [3]int64
		for i, field := range []string{"up", "down", "conns"} {
			value, ok := trafficCount(item.Value(field))
			if !ok {
				return values, false
			}
			values[i] = value
		}
		return values, true
	}
	hour := func(item *jsonx.Object) (int64, bool, bool) {
		number, ok := item.Value("hour").(jsonx.Number)
		if !ok {
			return 0, false, false
		}
		value, ok := number.BigInt()
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
		item, ok := entry.(*jsonx.Object)
		if !ok || item == nil {
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
		item, ok := entry.(*jsonx.Object)
		if !ok || item == nil {
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

// ---------- counters with Python int semantics ----------

// pyInt is a Python int that is usually small: int64 until an addition
// overflows, then a big.Int.
type pyInt struct {
	small int64
	big   *big.Int
}

// add is n += v. A big value is never changed in place (copies of a
// triple share it).
func (n *pyInt) add(v pyInt) {
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

func (n pyInt) toBig() *big.Int {
	if n.big != nil {
		return new(big.Int).Set(n.big)
	}
	return big.NewInt(n.small)
}

func (n pyInt) json() jsonx.Number {
	if n.big != nil {
		return jsonx.BigInt(n.big)
	}
	return jsonx.Int(n.small)
}

func (n pyInt) cmp(v pyInt) int {
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

// sqlValue is the value bound to a statement; a Python int beyond int64
// raised OverflowError when converted to an SQLite INTEGER.
func (n pyInt) sqlValue() (int64, error) {
	if n.big != nil {
		if !n.big.IsInt64() {
			return 0, &OverflowError{Message: "Python int too large to convert to SQLite INTEGER"}
		}
		return n.big.Int64(), nil
	}
	return n.small, nil
}

type triple [3]pyInt

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
	DataDir string
	Path    string

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
func (h *TrafficHistory) AddReport(agentID string, cleaned *CleanedTraffic) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, row := range cleaned.Usage {
		addTriple(h.usage, usageKey{agentID, row.Bucket, row.Source, row.Chain}, tripleOf(row.Up, row.Down, row.Conns))
	}
	for _, row := range cleaned.Targets {
		addTriple(h.targets, targetKey2{agentID, row.Bucket, row.Source, row.Target, row.Chain}, tripleOf(row.Up, row.Down, row.Conns))
	}
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

// trafficConn is one SQLite connection (Python opened one per operation,
// autocommit mode with explicit BEGIN IMMEDIATE).
type trafficConn struct {
	db            *sql.DB
	conn          *sql.Conn
	inTransaction bool
}

var bg = context.Background()

func (c *trafficConn) exec(statement string, args ...any) (int64, error) {
	result, err := c.conn.ExecContext(bg, statement, args...)
	if err != nil {
		return 0, sqliteText(err)
	}
	n, _ := result.RowsAffected()
	return n, nil
}

func (c *trafficConn) begin() error {
	if _, err := c.exec("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	c.inTransaction = true
	return nil
}

func (c *trafficConn) commit() error {
	if _, err := c.exec("COMMIT"); err != nil {
		return err
	}
	c.inTransaction = false
	return nil
}

// rollback is `if connection.in_transaction: connection.execute('ROLLBACK')`.
func (c *trafficConn) rollback() error {
	if !c.inTransaction {
		return nil
	}
	c.inTransaction = false
	_, err := c.exec("ROLLBACK")
	return err
}

func (c *trafficConn) close() {
	c.conn.Close()
	c.db.Close()
}

func (c *trafficConn) query(statement string, args []any, scan func(*sql.Rows) error) error {
	rows, err := c.conn.QueryContext(bg, statement, args...)
	if err != nil {
		return sqliteText(err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return sqliteText(err)
		}
	}
	return sqliteText(rows.Err())
}

var sqliteCodeSuffix = regexp.MustCompile(` \(\d+\)$`)

// SQLiteError carries the text of str(sqlite3.Error) (sqlite3_errmsg).
type SQLiteError struct {
	Message string
	Cause   error
}

func (e *SQLiteError) Error() string { return e.Message }
func (e *SQLiteError) Unwrap() error { return e.Cause }

// sqliteText gives a driver error the text of str(sqlite3.Error): the
// driver writes "errstr: errmsg (code)".
func sqliteText(err error) error {
	var target *sqlite.Error
	if err == nil || !errors.As(err, &target) {
		return err
	}
	message := strings.TrimSuffix(target.Error(), " (SQLITE_BUSY)")
	message = sqliteCodeSuffix.ReplaceAllString(message, "")
	if _, detail, found := strings.Cut(message, ": "); found {
		message = detail
	}
	return &SQLiteError{Message: message, Cause: err}
}

// makedirs is os.makedirs(path, exist_ok=True) with Python's OSError text.
func makedirs(path string) error {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return nil
		}
		return &OSError{Errno: syscall.EEXIST, Path: path}
	}
	if err := os.MkdirAll(path, 0o777); err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			if errno, ok := pathErr.Err.(syscall.Errno); ok {
				return &OSError{Errno: errno, Path: pathErr.Path}
			}
		}
		return err
	}
	return nil
}

// OSError is str(OSError): "[Errno 17] File exists: '/data/metrics'".
type OSError struct {
	Errno syscall.Errno
	Path  string
}

func (e *OSError) Error() string {
	text := e.Errno.Error()
	if text != "" {
		text = strings.ToUpper(text[:1]) + text[1:]
	}
	return "[Errno " + strconv.Itoa(int(e.Errno)) + "] " + text + ": " + jsonx.Repr(e.Path)
}

func (e *OSError) Unwrap() error { return e.Errno }

// connect is _connect(): create the directory, open the file (journal mode
// DELETE like the configuration database: no -wal file in backups or the
// data volume), create the tables and migrate 1.8.51-go tables.
func (h *TrafficHistory) connect() (*trafficConn, error) {
	if h.connectHook != nil {
		return h.connectHook()
	}
	if err := makedirs(h.DataDir); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(h.Path)
	if err != nil {
		return nil, err
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, sqliteText(err)
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(bg)
	if err != nil {
		db.Close()
		return nil, sqliteText(err)
	}
	c := &trafficConn{db: db, conn: conn}
	statements := append([]string{"PRAGMA busy_timeout=" + strconv.Itoa(trafficTimeoutMs), "PRAGMA journal_mode=DELETE"}, trafficSchema...)
	for _, statement := range statements {
		if _, err := c.exec(statement); err != nil {
			c.close()
			return nil, err
		}
	}
	if err := migrateLegacy(c); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

// migrateLegacy is _migrate_legacy: fold the 1.8.51-go tables into the
// chain tables and drop them; happens only on the first open.
func migrateLegacy(c *trafficConn) error {
	legacy := map[string]bool{}
	err := c.query("SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('usage', 'targets')", nil, func(rows *sql.Rows) error {
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
	if err := c.begin(); err != nil {
		return err
	}
	for _, migration := range legacyMigrations {
		if !legacy[migration.table] {
			continue
		}
		if _, err := c.exec(migration.statement); err != nil {
			c.rollbackAlways()
			return err
		}
		if _, err := c.exec("DROP TABLE " + migration.table); err != nil {
			c.rollbackAlways()
			return err
		}
	}
	if err := c.commit(); err != nil {
		c.rollbackAlways()
		return err
	}
	trafficLogger.Info("流量记录已迁移为按链路保存")
	return nil
}

// rollbackAlways is the unconditional `connection.execute('ROLLBACK')` of
// _migrate_legacy and _delete_agents.
func (c *trafficConn) rollbackAlways() {
	c.inTransaction = false
	c.exec("ROLLBACK")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// pyFloorDiv is float floor division (float.__floordiv__).
func pyFloorDiv(vx, wx float64) float64 {
	mod := math.Mod(vx, wx)
	div := (vx - mod) / wx
	if mod != 0 && (wx < 0) != (mod < 0) {
		div -= 1.0
	}
	if div == 0 {
		return math.Copysign(0, vx/wx)
	}
	floor := math.Floor(div)
	if div-floor > 0.5 {
		floor += 1.0
	}
	return floor
}

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

// Object is the dict flush() returns.
func (r TrafficFlushResult) Object() *jsonx.Object {
	return jsonx.Obj("rows", r.Rows, "compacted", r.Compacted, "pruned", r.Pruned)
}

// Flush is flush(retention, now): save the increments, fold old hours into
// days and delete expired rows. On failure the increments stay in memory
// (merged with reports that arrived meanwhile) and the error is returned.
// at nil = time.time().
func (h *TrafficHistory) Flush(retention int64, at *float64) (TrafficFlushResult, error) {
	current := trafficNow()
	if at != nil {
		current = *at
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	usage, targets := h.usage, h.targets
	if len(usage) == 0 && len(targets) == 0 && !fileExists(h.Path) {
		return TrafficFlushResult{}, nil
	}
	h.usage, h.targets = map[usageKey]*triple{}, map[targetKey2]*triple{}
	result, err := h.save(usage, targets, retention, current)
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

func (h *TrafficHistory) save(usage map[usageKey]*triple, targets map[targetKey2]*triple, retention int64, now float64) (TrafficFlushResult, error) {
	c, err := h.connect()
	if err != nil {
		return TrafficFlushResult{}, err
	}
	defer c.close()
	result, err := func() (TrafficFlushResult, error) {
		var result TrafficFlushResult
		if err := c.begin(); err != nil {
			return result, err
		}
		statement := usageUpsert(Hour)
		for key, values := range usage {
			up, down, conns, err := sqlTriple(values)
			if err != nil {
				return result, err
			}
			if _, err := c.exec(statement, key.agent, key.bucket, key.source, key.chain, up, down, conns); err != nil {
				return result, err
			}
		}
		statement = targetUpsert(Hour)
		for key, values := range targets {
			up, down, conns, err := sqlTriple(values)
			if err != nil {
				return result, err
			}
			if _, err := c.exec(statement, key.agent, key.bucket, key.source, key.target, key.chain, up, down, conns); err != nil {
				return result, err
			}
		}
		compacted, err := compact(c, now)
		if err != nil {
			return result, err
		}
		pruned, err := pruneRows(c, now-float64(retention)*Day)
		if err != nil {
			return result, err
		}
		if err := c.commit(); err != nil {
			return result, err
		}
		return TrafficFlushResult{Compacted: compacted, Pruned: pruned}, nil
	}()
	if err != nil {
		if rollbackErr := c.rollback(); rollbackErr != nil {
			return result, rollbackErr
		}
		return result, err
	}
	return result, nil
}

func sqlTriple(values *triple) (int64, int64, int64, error) {
	var out [3]int64
	for i, value := range values {
		v, err := value.sqlValue()
		if err != nil {
			return 0, 0, 0, err
		}
		out[i] = v
	}
	return out[0], out[1], out[2], nil
}

// compact is _compact(connection, now): fold older hourly rows into daily
// rows; daily targets keep only each device's largest, the rest go into
// "other".
func compact(c *trafficConn, now float64) (int64, error) {
	var compacted int64
	usageBefore := int64(pyFloorDiv(now, Day)*Day - UsageHourlyDays*Day)
	var usageRows [][]any
	err := c.query("SELECT agent_id, bucket / 86400 * 86400, source, chain, SUM(up), SUM(down), SUM(conns) "+
		"FROM traffic_usage WHERE span = 3600 AND bucket < ? GROUP BY 1, 2, 3, 4", []any{usageBefore}, func(rows *sql.Rows) error {
		row := make([]any, 7)
		pointers := make([]any, 7)
		for i := range row {
			pointers[i] = &row[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return err
		}
		usageRows = append(usageRows, row)
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(usageRows) > 0 {
		n, err := c.exec("DELETE FROM traffic_usage WHERE span = 3600 AND bucket < ?", usageBefore)
		if err != nil {
			return 0, err
		}
		compacted += n
		statement := usageUpsert(Day)
		for _, row := range usageRows {
			if _, err := c.exec(statement, row...); err != nil {
				return 0, err
			}
		}
	}
	targetsBefore := int64(pyFloorDiv(now, Day)*Day - TargetHourlyDays*Day)
	var targetRows [][]any
	err = c.query("SELECT agent_id, bucket / 86400 * 86400, source, target, chain, SUM(up), SUM(down), SUM(conns) "+
		"FROM traffic_targets WHERE span = 3600 AND bucket < ? GROUP BY 1, 2, 3, 4, 5", []any{targetsBefore}, func(rows *sql.Rows) error {
		row := make([]any, 8)
		pointers := make([]any, 8)
		for i := range row {
			pointers[i] = &row[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return err
		}
		targetRows = append(targetRows, row)
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(targetRows) > 0 {
		n, err := c.exec("DELETE FROM traffic_targets WHERE span = 3600 AND bucket < ?", targetsBefore)
		if err != nil {
			return 0, err
		}
		compacted += n
		statement := targetUpsert(Day)
		for _, row := range targetRows {
			if _, err := c.exec(statement, row...); err != nil {
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
			n, err := trimDay(c, row[0], row[1])
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
	up, down, conns       int64
}

// trimDay is _trim_day(connection, agent_id, day): each device keeps only
// the day's TargetDailyTop largest targets (ranked by the total over all
// chains); the others are merged per chain into "other".
func trimDay(c *trafficConn, agentID, day any) (int64, error) {
	var rows []targetDayRow
	err := c.query("SELECT source, target, chain, up, down, conns FROM traffic_targets WHERE agent_id = ? AND bucket = ? AND span = 86400",
		[]any{agentID, day}, func(scan *sql.Rows) error {
			var row targetDayRow
			if err := scan.Scan(&row.source, &row.target, &row.chain, &row.up, &row.down, &row.conns); err != nil {
				return err
			}
			rows = append(rows, row)
			return nil
		})
	if err != nil {
		return 0, err
	}
	type pair struct{ source, target string }
	totals := map[pair]*pyInt{}
	var order []pair
	for _, row := range rows {
		if row.target == OtherTarget {
			continue
		}
		key := pair{row.source, row.target}
		if totals[key] == nil {
			totals[key] = &pyInt{}
			order = append(order, key)
		}
		totals[key].add(pyInt{small: row.up})
		totals[key].add(pyInt{small: row.down})
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
		merged[key].add(tripleOf(row.up, row.down, row.conns))
	}
	for _, row := range extra {
		if _, err := c.exec("DELETE FROM traffic_targets WHERE agent_id = ? AND bucket = ? AND span = 86400 AND source = ? AND target = ? AND chain = ?",
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
		if _, err := c.exec(statement, agentID, day, key.source, OtherTarget, key.chain, up, down, conns); err != nil {
			return 0, err
		}
	}
	return int64(len(extra)), nil
}

// pruneRows is _prune(connection, cutoff): daily rows expire only as a
// whole day.
func pruneRows(c *trafficConn, cutoff float64) (int64, error) {
	var pruned int64
	for _, table := range []string{"traffic_usage", "traffic_targets"} {
		n, err := c.exec("DELETE FROM "+table+" WHERE bucket + span <= ?", int64(cutoff))
		if err != nil {
			return 0, err
		}
		pruned += n
	}
	return pruned, nil
}

// deleteAgents is _delete_agents(connection, agent_ids).
func deleteAgents(c *trafficConn, agentIDs []string) error {
	if err := c.begin(); err != nil {
		return err
	}
	for _, agentID := range agentIDs {
		for _, statement := range []string{"DELETE FROM traffic_usage WHERE agent_id = ?", "DELETE FROM traffic_targets WHERE agent_id = ?"} {
			if _, err := c.exec(statement, agentID); err != nil {
				c.rollbackAlways()
				return err
			}
		}
	}
	if err := c.commit(); err != nil {
		c.rollbackAlways()
		return err
	}
	return nil
}

// Delete is delete(agent_id): forget the Agent's increments and rows.
func (h *TrafficHistory) Delete(agentID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
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
	if !fileExists(h.Path) {
		return nil
	}
	c, err := h.connect()
	if err != nil {
		return err
	}
	defer c.close()
	return deleteAgents(c, []string{agentID})
}

// RetainAgents is retain_agents(agent_ids): delete the data of Agents that
// no longer exist (after an import, restore or reset); the number of
// Agents removed from the file.
func (h *TrafficHistory) RetainAgents(agentIDs []string) (int64, error) {
	keep := map[string]bool{}
	for _, id := range agentIDs {
		keep[id] = true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
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
	if !fileExists(h.Path) {
		return 0, nil
	}
	c, err := h.connect()
	if err != nil {
		return 0, err
	}
	defer c.close()
	var removed []string
	err = c.query("SELECT DISTINCT agent_id FROM traffic_usage UNION SELECT DISTINCT agent_id FROM traffic_targets", nil, func(rows *sql.Rows) error {
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
		if err := deleteAgents(c, removed); err != nil {
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
func (h *TrafficHistory) rowsInRange(agentID string, start float64, withTargets bool) ([]usageRecord, []targetRecord, error) {
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
	if !fileExists(h.Path) {
		return usage, targets, nil
	}
	c, err := h.connect()
	if err != nil {
		return nil, nil, err
	}
	defer c.close()
	err = c.query("SELECT bucket, span, source, chain, up, down, conns FROM traffic_usage "+
		"WHERE agent_id = ? AND bucket + span > ?", []any{agentID, int64(start)}, func(rows *sql.Rows) error {
		var r usageRecord
		var up, down, conns int64
		if err := rows.Scan(&r.bucket, &r.span, &r.source, &r.chain, &up, &down, &conns); err != nil {
			return err
		}
		r.v = tripleOf(up, down, conns)
		usage = append(usage, r)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if withTargets {
		err = c.query("SELECT bucket, span, source, target, chain, up, down, conns FROM traffic_targets "+
			"WHERE agent_id = ? AND bucket + span > ?", []any{agentID, int64(start)}, func(rows *sql.Rows) error {
			var r targetRecord
			var up, down, conns int64
			if err := rows.Scan(&r.bucket, &r.span, &r.source, &r.target, &r.chain, &up, &down, &conns); err != nil {
				return err
			}
			r.v = tripleOf(up, down, conns)
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
func rowsOf(a *aggregate, limit int) []any {
	type entry struct {
		key   string
		v     *triple
		total pyInt
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
	out := make([]any, len(entries))
	for i, e := range entries {
		out[i] = jsonx.Obj("key", e.key, "up", e.v[0].json(), "down", e.v[1].json(), "conns", e.v[2].json())
	}
	return out
}

// isoUTC is datetime.fromtimestamp(ts, timezone.utc).isoformat()
// (microseconds rounded half to even, as CPython does).
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

// Query is query(agent_id, range_key, by, now): the rows of the range
// grouped by `by` (with their parts), totals, totals per exit node and the
// trend. Parts: by device, target or policy group they are split by chain;
// by chain or exit node they are split by device. Unattributed traffic has
// the empty key and no parts. The trend is hourly for 24 h and 7 d, daily
// for 30 d. rangeKey must be a key of Ranges. at nil = time.time().
func (h *TrafficHistory) Query(agentID, rangeKey, by string, at *float64) (*jsonx.Object, error) {
	current := trafficNow()
	if at != nil {
		current = *at
	}
	start := current - float64(Ranges[rangeKey])
	step := int64(Hour)
	if rangeKey == "30d" {
		step = Day
	}
	usageRows, targetRows, err := h.rowsInRange(agentID, start, by == "destination")
	if err != nil {
		return nil, err
	}
	var totals triple
	outbounds := newAggregate()
	trend := map[int64]*[2]pyInt{}
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
			point = &[2]pyInt{}
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
	for _, item := range rows {
		row := item.(*jsonx.Object)
		rowParts := parts[row.GetString("key")]
		if rowParts == nil {
			rowParts = newAggregate()
		}
		row.Set("parts", rowsOf(rowParts, QueryPartLimit))
		row.Set("part_count", len(rowParts.values))
	}
	first := int64(pyFloorDiv(start, float64(step)) * float64(step))
	points := []any{}
	for at := first; at < int64(current)+1; at += step {
		up, down := jsonx.Int(0), jsonx.Int(0)
		if point := trend[at]; point != nil {
			up, down = point[0].json(), point[1].json()
		}
		points = append(points, jsonx.Obj("at", isoUTC(float64(at)), "up", up, "down", down))
	}
	omitted := len(grouped.values) - QueryRowLimit
	if omitted < 0 {
		omitted = 0
	}
	return jsonx.Obj(
		"range", rangeKey, "by", by, "step", step, "chain_separator", ChainSeparator,
		"start", isoUTC(start),
		"rows", rows, "omitted", omitted,
		"total", jsonx.Obj("up", totals[0].json(), "down", totals[1].json(), "conns", totals[2].json()),
		"outbounds", rowsOf(outbounds, len(outbounds.values)),
		"trend", points,
	), nil
}
