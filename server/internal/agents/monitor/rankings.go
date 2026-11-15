package monitor

// Port of backend/agents/oxidns_ranking_history.py (A07): complete OxiDNS
// request rankings, fetched slot by slot from the Agents, stored per hour in
// DATA_DIR/metrics/oxidns_rankings.sqlite3 and added up per range by the
// panel (history.go's RankingHistory interface, installed by Wire).
//
// The core ranks at most the newest 10,000 query records of a window
// (OxiDNS 1.6.1, fixed). Every 15 minutes the collector asks for the slot
// that just ended: with fewer records than the cap the core counts all of
// them. A slot at the cap is split in halves down to MIN_SLOT seconds;
// what is still at the cap is stored as partial. Hours older than
// HOURLY_DAYS days become one UTC day row with the DAILY_TOP entries per
// kind; data older than RETENTION_DAYS days is deleted. After a first start
// or a pause the slots the core still keeps are filled in, newest first, at
// most SLOTS_PER_ROUND per round.
//
// SQLite as Python's sqlite3 module (3.11, legacy transaction control) uses
// it: one connection per operation with a 30 s busy timeout, journal mode
// WAL, the schema statements on every open, an implicit BEGIN before the
// first INSERT/UPDATE/DELETE, COMMIT when the `with connection:` block ends
// and ROLLBACK when it raises.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/repo"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"
)

var rankingLogger = logx.Get("backend.agents.oxidns_ranking_history")

// Constants of oxidns_ranking_history.py (the others are in history.go).
const (
	RankingMinSlot           = 60
	RankingSettleSeconds     = 60
	RankingRows              = 2000
	RankingDailyTop          = 2000
	RankingHourlyDays        = 7
	RankingCoreRetentionDays = 7
	RankingSlotsPerRound     = 24
	RankingRoundSeconds      = 60
	RankingTop               = 10
	RankingQueryCache        = 32
	rankingTimeoutMs         = 30000
)

// RankingKinds is KINDS.
var RankingKinds = []string{"clients", "domains", "blocked"}

// rankingSchema is _SCHEMA (the text is stored in sqlite_master as written).
var rankingSchema = []string{
	"CREATE TABLE IF NOT EXISTS ranking_periods (" +
		" agent_id TEXT NOT NULL, recorder TEXT NOT NULL, start INTEGER NOT NULL, seconds INTEGER NOT NULL," +
		" records INTEGER NOT NULL DEFAULT 0, blocked_records INTEGER NOT NULL DEFAULT 0," +
		" blocked_slots INTEGER NOT NULL DEFAULT 0, slots INTEGER NOT NULL DEFAULT 0," +
		" partial_slots INTEGER NOT NULL DEFAULT 0," +
		" PRIMARY KEY (agent_id, recorder, start, seconds))",
	"CREATE TABLE IF NOT EXISTS ranking_rows (" +
		" agent_id TEXT NOT NULL, recorder TEXT NOT NULL, start INTEGER NOT NULL, seconds INTEGER NOT NULL," +
		" kind TEXT NOT NULL, key TEXT NOT NULL, count INTEGER NOT NULL," +
		" PRIMARY KEY (agent_id, recorder, start, seconds, kind, key))",
	"CREATE TABLE IF NOT EXISTS ranking_slots (" +
		" agent_id TEXT NOT NULL, recorder TEXT NOT NULL, start INTEGER NOT NULL," +
		" PRIMARY KEY (agent_id, recorder, start))",
}

// ErrSlotIncomplete is SlotIncomplete("unexpected answer"): a slot could not
// be read in full; it is asked for again in a later round.
var ErrSlotIncomplete = errors.New("unexpected answer")

// Counts is an insertion-ordered {key: count} of one ranking kind.
type Counts struct {
	Keys   []string
	Values map[string]int64
}

// NewCounts builds Counts from key, count pairs.
func NewCounts(pairs ...any) *Counts {
	c := &Counts{Values: map[string]int64{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		c.Add(pairs[i].(string), int64(pairs[i+1].(int)))
	}
	return c
}

// Add is counts[key] = counts.get(key, 0) + count.
func (c *Counts) Add(key string, count int64) {
	if _, ok := c.Values[key]; !ok {
		c.Keys = append(c.Keys, key)
	}
	c.Values[key] += count
}

func (c *Counts) clone() *Counts {
	out := &Counts{Keys: append([]string(nil), c.Keys...), Values: make(map[string]int64, len(c.Values))}
	for k, v := range c.Values {
		out.Values[k] = v
	}
	return out
}

// Slot is one stored 15-minute slot: records, blocked (nil when unknown),
// partial and the counts per kind (Rows holds only the kinds present, in
// KINDS order).
type Slot struct {
	Records       int64
	Blocked       *int64
	Partial       bool
	Rows          map[string]*Counts
	RetentionDays any
}

func (s *Slot) kinds() []string {
	var out []string
	for _, kind := range RankingKinds {
		if _, ok := s.Rows[kind]; ok {
			out = append(out, kind)
		}
	}
	return out
}

type answerKey struct {
	agentID, recorder string
	since, until      int64
}

type keptAnswer struct {
	version int64
	answer  *HistoryAnswer
}

// RankingStore is RankingHistory(data_dir).
type RankingStore struct {
	Path string

	mu             sync.Mutex
	version        int64 // bumped by every change of the stored data
	answers        map[answerKey]keptAnswer
	order          []answerKey
	maintainedHour *int64

	caughtMu sync.Mutex
	// caughtUp: Agent id → the slot end it was last brought up to.
	caughtUp map[string]int64

	// queryHook replaces _query (tests count the sums).
	queryHook func(agentID, recorder string, since, until int64) (*HistoryAnswer, error)
}

// NewRankingStore is RankingHistory(data_dir).
func NewRankingStore(dataDir string) *RankingStore {
	return &RankingStore{Path: filepath.Join(dataDir, "metrics", HistoryFilename),
		answers: map[answerKey]keptAnswer{}, caughtUp: map[string]int64{}}
}

// CaughtUp is caught_up.get(agent_id).
func (s *RankingStore) CaughtUp(agentID string) (int64, bool) {
	s.caughtMu.Lock()
	defer s.caughtMu.Unlock()
	end, ok := s.caughtUp[agentID]
	return end, ok
}

func (s *RankingStore) setCaughtUp(agentID string, end int64, set bool) {
	s.caughtMu.Lock()
	defer s.caughtMu.Unlock()
	if set {
		s.caughtUp[agentID] = end
	} else {
		delete(s.caughtUp, agentID)
	}
}

func (s *RankingStore) exists() bool {
	_, err := os.Stat(s.Path)
	return err == nil
}

// rankConn is one sqlite3 connection with Python's implicit transactions.
type rankConn struct {
	db   *sql.DB
	conn *sql.Conn
	inTx bool
}

var rankCtx = context.Background()

// connect is _connect().
func (s *RankingStore) connect() (*rankConn, error) {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o777); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(s.Path)
	if err != nil {
		return nil, err
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(rankCtx)
	if err != nil {
		db.Close()
		return nil, err
	}
	c := &rankConn{db: db, conn: conn}
	statements := append([]string{"PRAGMA busy_timeout=" + strconv.Itoa(rankingTimeoutMs), "PRAGMA journal_mode=WAL"}, rankingSchema...)
	for _, statement := range statements {
		if _, err := c.conn.ExecContext(rankCtx, statement); err != nil {
			c.close()
			return nil, err
		}
	}
	return c, nil
}

func (c *rankConn) close() {
	c.conn.Close()
	c.db.Close()
}

// write runs an INSERT/UPDATE/DELETE, beginning the implicit transaction
// first; returns the cursor's rowcount.
func (c *rankConn) write(statement string, args ...any) (int64, error) {
	if !c.inTx {
		if _, err := c.conn.ExecContext(rankCtx, "BEGIN"); err != nil {
			return 0, err
		}
		c.inTx = true
	}
	result, err := c.conn.ExecContext(rankCtx, statement, args...)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}

// writeMany is executemany: the transaction begins even without rows.
func (c *rankConn) writeMany(statement string, rows [][]any) error {
	if !c.inTx {
		if _, err := c.conn.ExecContext(rankCtx, "BEGIN"); err != nil {
			return err
		}
		c.inTx = true
	}
	if len(rows) == 0 {
		return nil
	}
	stmt, err := c.conn.PrepareContext(rankCtx, statement)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(rankCtx, row...); err != nil {
			return err
		}
	}
	return nil
}

func (c *rankConn) query(statement string, args []any, scan func(*sql.Rows) error) error {
	rows, err := c.conn.QueryContext(rankCtx, statement, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// with is `with self._connect() as connection:`: body, then COMMIT when a
// transaction is open, ROLLBACK when the body failed; the connection is
// closed afterwards.
func (s *RankingStore) with(body func(c *rankConn) error) error {
	c, err := s.connect()
	if err != nil {
		return err
	}
	defer c.close()
	if err := body(c); err != nil {
		if c.inTx {
			c.conn.ExecContext(rankCtx, "ROLLBACK")
		}
		return err
	}
	if c.inTx {
		if _, err := c.conn.ExecContext(rankCtx, "COMMIT"); err != nil {
			return err
		}
	}
	return nil
}

// Fetched is fetched(agent_id, recorder, starts): the slot starts among
// starts already stored.
func (s *RankingStore) Fetched(agentID, recorder string, starts []int64) (map[int64]bool, error) {
	done := map[int64]bool{}
	if len(starts) == 0 || !s.exists() {
		return done, nil
	}
	lo, hi := starts[0], starts[0]
	wanted := map[int64]bool{}
	for _, start := range starts {
		lo, hi = min(lo, start), max(hi, start)
		wanted[start] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.with(func(c *rankConn) error {
		return c.query("SELECT start FROM ranking_slots WHERE agent_id = ? AND recorder = ? AND start BETWEEN ? AND ?",
			[]any{agentID, recorder, lo, hi}, func(rows *sql.Rows) error {
				var start int64
				if err := rows.Scan(&start); err != nil {
					return err
				}
				if wanted[start] {
					done[start] = true
				}
				return nil
			})
	})
	return done, err
}

// AddSlot is add_slot(agent_id, recorder, start, slot): add one 15-minute
// slot to its hour; false when the slot was stored before.
func (s *RankingStore) AddSlot(agentID, recorder string, start int64, slot *Slot) (bool, error) {
	hour := start - floorMod(start, HistoryHour)
	s.mu.Lock()
	defer s.mu.Unlock()
	added := false
	err := s.with(func(c *rankConn) error {
		inserted, err := c.write("INSERT OR IGNORE INTO ranking_slots (agent_id, recorder, start) VALUES (?, ?, ?)",
			agentID, recorder, start)
		if err != nil || inserted == 0 {
			return err
		}
		s.version++
		blocked, blockedSlots := int64(0), int64(0)
		if slot.Blocked != nil {
			blocked, blockedSlots = *slot.Blocked, 1
		}
		partial := int64(0)
		if slot.Partial {
			partial = 1
		}
		if _, err := c.write("INSERT INTO ranking_periods (agent_id, recorder, start, seconds, records, blocked_records,"+
			" blocked_slots, slots, partial_slots) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)"+
			" ON CONFLICT (agent_id, recorder, start, seconds) DO UPDATE SET"+
			" records = records + excluded.records, blocked_records = blocked_records + excluded.blocked_records,"+
			" blocked_slots = blocked_slots + excluded.blocked_slots, slots = slots + 1,"+
			" partial_slots = partial_slots + excluded.partial_slots",
			agentID, recorder, hour, HistoryHour, slot.Records, blocked, blockedSlots, partial); err != nil {
			return err
		}
		var rows [][]any
		for _, kind := range slot.kinds() {
			counts := slot.Rows[kind]
			for _, key := range counts.Keys {
				rows = append(rows, []any{agentID, recorder, hour, HistoryHour, kind, key, counts.Values[key]})
			}
		}
		if err := c.writeMany("INSERT INTO ranking_rows (agent_id, recorder, start, seconds, kind, key, count) VALUES (?, ?, ?, ?, ?, ?, ?)"+
			" ON CONFLICT (agent_id, recorder, start, seconds, kind, key) DO UPDATE SET count = count + excluded.count", rows); err != nil {
			return err
		}
		added = true
		return nil
	})
	return added, err
}

// nowSeconds is int(time.time()) (a variable for tests).
var nowSeconds = func() int64 { return time.Now().Unix() }

func nowOr(now *int64) int64 {
	if now != nil {
		return *now
	}
	return nowSeconds()
}

// Maintain is maintain(agent_ids, now): merge old hours into days, drop
// expired data and removed Agents; at most once per hour.
func (s *RankingStore) Maintain(agentIDs []string, now *int64) error {
	if !s.exists() {
		return nil
	}
	t := nowOr(now)
	hour := t - floorMod(t, HistoryHour)
	if s.maintainedHour != nil && *s.maintainedHour == hour {
		return nil // merging and expiry only move by the hour
	}
	s.maintainedHour = &hour
	hourlyBefore := t - floorMod(t, HistoryDay) - RankingHourlyDays*HistoryDay
	cutoff := t - HistoryRetentionDay*HistoryDay
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.with(func(c *rankConn) error {
		known := map[string]bool{}
		for _, id := range agentIDs {
			known[id] = true
		}
		var stored []string
		if err := c.query("SELECT DISTINCT agent_id FROM ranking_slots", nil, func(rows *sql.Rows) error {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			stored = append(stored, id)
			return nil
		}); err != nil {
			return err
		}
		for _, id := range stored {
			if !known[id] {
				for _, table := range []string{"ranking_periods", "ranking_rows", "ranking_slots"} {
					if _, err := c.write("DELETE FROM "+table+" WHERE agent_id = ?", id); err != nil {
						return err
					}
				}
			}
		}
		type old struct {
			agentID, recorder string
			day               int64
		}
		var olds []old
		if err := c.query("SELECT DISTINCT agent_id, recorder, start - start % ? FROM ranking_periods WHERE seconds = ? AND start < ?",
			[]any{HistoryDay, HistoryHour, hourlyBefore}, func(rows *sql.Rows) error {
				var o old
				if err := rows.Scan(&o.agentID, &o.recorder, &o.day); err != nil {
					return err
				}
				olds = append(olds, o)
				return nil
			}); err != nil {
			return err
		}
		for _, o := range olds {
			if err := mergeDay(c, o.agentID, o.recorder, o.day); err != nil {
				return err
			}
		}
		for _, table := range []string{"ranking_periods", "ranking_rows"} {
			if _, err := c.write("DELETE FROM "+table+" WHERE start < ?", cutoff); err != nil {
				return err
			}
		}
		if _, err := c.write("DELETE FROM ranking_slots WHERE start < ?", cutoff); err != nil {
			return err
		}
		var changes int64
		if err := c.conn.QueryRowContext(rankCtx, "SELECT total_changes()").Scan(&changes); err != nil {
			return err
		}
		if changes != 0 {
			s.version++
		}
		return nil
	})
}

func nullInt(v sql.NullInt64) int64 {
	if v.Valid {
		return v.Int64
	}
	return 0
}

// mergeDay is _merge_day: one UTC day row from its hours, with the
// DAILY_TOP entries per kind.
func mergeDay(c *rankConn, agentID, recorder string, day int64) error {
	hours := []any{agentID, recorder, HistoryHour, day, day + HistoryDay}
	var totals [5]sql.NullInt64
	if err := c.conn.QueryRowContext(rankCtx,
		"SELECT SUM(records), SUM(blocked_records), SUM(blocked_slots), SUM(slots), SUM(partial_slots)"+
			" FROM ranking_periods WHERE agent_id = ? AND recorder = ? AND seconds = ? AND start >= ? AND start < ?",
		hours...).Scan(&totals[0], &totals[1], &totals[2], &totals[3], &totals[4]); err != nil {
		return err
	}
	if _, err := c.write("INSERT INTO ranking_periods (agent_id, recorder, start, seconds, records, blocked_records, blocked_slots,"+
		" slots, partial_slots) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"+
		" ON CONFLICT (agent_id, recorder, start, seconds) DO UPDATE SET"+
		" records = records + excluded.records, blocked_records = blocked_records + excluded.blocked_records,"+
		" blocked_slots = blocked_slots + excluded.blocked_slots, slots = slots + excluded.slots,"+
		" partial_slots = partial_slots + excluded.partial_slots",
		agentID, recorder, day, HistoryDay, nullInt(totals[0]), nullInt(totals[1]), nullInt(totals[2]),
		nullInt(totals[3]), nullInt(totals[4])); err != nil {
		return err
	}
	for _, kind := range RankingKinds {
		existing := &Counts{Values: map[string]int64{}}
		var summed []struct {
			key   string
			total int64
		}
		if err := c.query("SELECT key, SUM(count) AS total FROM ranking_rows WHERE agent_id = ? AND recorder = ? AND seconds = ?"+
			" AND start >= ? AND start < ? AND kind = ? GROUP BY key", append(append([]any{}, hours...), kind), func(rows *sql.Rows) error {
			var key string
			var total int64
			if err := rows.Scan(&key, &total); err != nil {
				return err
			}
			summed = append(summed, struct {
				key   string
				total int64
			}{key, total})
			return nil
		}); err != nil {
			return err
		}
		if err := c.query("SELECT key, count FROM ranking_rows WHERE agent_id = ? AND recorder = ? AND start = ? AND seconds = ?"+
			" AND kind = ?", []any{agentID, recorder, day, HistoryDay, kind}, func(rows *sql.Rows) error {
			var key string
			var count int64
			if err := rows.Scan(&key, &count); err != nil {
				return err
			}
			// dict(rows): a repeated key keeps the last value (keys are unique).
			if _, ok := existing.Values[key]; !ok {
				existing.Keys = append(existing.Keys, key)
			}
			existing.Values[key] = count
			return nil
		}); err != nil {
			return err
		}
		for _, row := range summed {
			existing.Add(row.key, row.total)
		}
		kept := append([]string(nil), existing.Keys...)
		sort.SliceStable(kept, func(i, j int) bool {
			a, b := existing.Values[kept[i]], existing.Values[kept[j]]
			if a != b {
				return a > b
			}
			return kept[i] < kept[j]
		})
		if len(kept) > RankingDailyTop {
			kept = kept[:RankingDailyTop]
		}
		if _, err := c.write("DELETE FROM ranking_rows WHERE agent_id = ? AND recorder = ? AND start = ?"+
			" AND seconds = ? AND kind = ?", agentID, recorder, day, HistoryDay, kind); err != nil {
			return err
		}
		rows := make([][]any, len(kept))
		for i, key := range kept {
			rows[i] = []any{agentID, recorder, day, HistoryDay, kind, key, existing.Values[key]}
		}
		if err := c.writeMany("INSERT INTO ranking_rows (agent_id, recorder, start, seconds, kind, key, count) VALUES (?, ?, ?, ?, ?, ?, ?)", rows); err != nil {
			return err
		}
	}
	for _, table := range []string{"ranking_periods", "ranking_rows"} {
		if _, err := c.write("DELETE FROM "+table+" WHERE agent_id = ? AND recorder = ? AND seconds = ?"+
			" AND start >= ? AND start < ?", hours...); err != nil {
			return err
		}
	}
	return nil
}

// HasAny is has_any(agent_id, recorder): whether any slot of this recorder
// is stored, without adding anything up.
func (s *RankingStore) HasAny(agentID, recorder string) (bool, error) {
	if !s.exists() {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	err := s.with(func(c *rankConn) error {
		return c.query("SELECT 1 FROM ranking_slots WHERE agent_id = ? AND recorder = ? LIMIT 1", []any{agentID, recorder},
			func(*sql.Rows) error { found = true; return nil })
	})
	return found, err
}

// Query is query(agent_id, recorder, since, until): totals and the top
// entries of the stored periods starting in [since, until). Answers are
// kept until the stored data changes; the returned answer is shared and
// must not be modified.
func (s *RankingStore) Query(agentID, recorder string, since, until int64) (*HistoryAnswer, error) {
	key := answerKey{agentID, recorder, since, until}
	s.mu.Lock()
	if kept, ok := s.answers[key]; ok && kept.version == s.version {
		s.touch(key)
		s.mu.Unlock()
		return kept.answer, nil
	}
	version := s.version
	s.mu.Unlock()
	run := s.query
	if s.queryHook != nil {
		run = s.queryHook
	}
	answer, err := run(agentID, recorder, since, until)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if version == s.version {
		s.answers[key] = keptAnswer{version, answer}
		s.touch(key)
		for len(s.order) > RankingQueryCache {
			delete(s.answers, s.order[0])
			s.order = s.order[1:]
		}
	}
	return answer, nil
}

// touch is OrderedDict.move_to_end(key) (the key is in answers).
func (s *RankingStore) touch(key answerKey) {
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.order = append(s.order, key)
}

func emptyAnswer() *HistoryAnswer {
	return &HistoryAnswer{Rows: map[string][]HistoryRow{"clients": {}, "domains": {}, "blocked": {}}}
}

// query is _query.
func (s *RankingStore) query(agentID, recorder string, since, until int64) (*HistoryAnswer, error) {
	if !s.exists() {
		return emptyAnswer(), nil
	}
	const where = "agent_id = ? AND recorder = ? AND start >= ? AND start + seconds <= ?"
	args := []any{agentID, recorder, since, until}
	s.mu.Lock()
	defer s.mu.Unlock()
	var answer *HistoryAnswer
	err := s.with(func(c *rankConn) error {
		var totals [7]sql.NullInt64
		if err := c.conn.QueryRowContext(rankCtx,
			"SELECT SUM(records), SUM(blocked_records), SUM(blocked_slots), SUM(slots), SUM(partial_slots),"+
				" MIN(start), MAX(start + seconds) FROM ranking_periods WHERE "+where+" AND slots > 0", args...).Scan(
			&totals[0], &totals[1], &totals[2], &totals[3], &totals[4], &totals[5], &totals[6]); err != nil {
			return err
		}
		if nullInt(totals[3]) == 0 {
			answer = emptyAnswer()
			return nil
		}
		var lastSlot, firstSlot sql.NullInt64
		if err := c.conn.QueryRowContext(rankCtx,
			"SELECT MAX(start) FROM ranking_slots WHERE agent_id = ? AND recorder = ? AND start >= ? AND start < ?",
			args...).Scan(&lastSlot); err != nil {
			return err
		}
		if err := c.conn.QueryRowContext(rankCtx,
			"SELECT MIN(start) FROM ranking_slots WHERE agent_id = ? AND recorder = ? AND start >= ? AND start < ?",
			args...).Scan(&firstSlot); err != nil {
			return err
		}
		rows := map[string][]HistoryRow{}
		for _, kind := range RankingKinds {
			list := []HistoryRow{}
			if err := c.query("SELECT key, SUM(count) AS total FROM ranking_rows WHERE "+where+" AND kind = ?"+
				" GROUP BY key ORDER BY total DESC, key ASC LIMIT ?", append(append([]any{}, args...), kind, RankingTop),
				func(r *sql.Rows) error {
					var row HistoryRow
					if err := r.Scan(&row.Key, &row.Count); err != nil {
						return err
					}
					list = append(list, row)
					return nil
				}); err != nil {
				return err
			}
			rows[kind] = list
		}
		answer = &HistoryAnswer{Records: nullInt(totals[0]), BlockedRecords: nullInt(totals[1]),
			BlockedSlots: nullInt(totals[2]), Slots: nullInt(totals[3]), PartialSlots: nullInt(totals[4]), Rows: rows}
		// Hours and days hold the slots fetched; the newest is known by its own record.
		if firstSlot.Valid {
			answer.First = firstSlot.Int64
		} else {
			answer.First = nullInt(totals[5])
		}
		if lastSlot.Valid {
			answer.Last = lastSlot.Int64 + HistorySlot
		} else {
			answer.Last = nullInt(totals[6])
		}
		return nil
	})
	return answer, err
}

var (
	storesMu sync.Mutex
	stores   = map[string]*RankingStore{}
)

// GetRankingStore is get_history(data_dir): one store per data directory.
func GetRankingStore(dataDir string) *RankingStore {
	storesMu.Lock()
	defer storesMu.Unlock()
	s := stores[dataDir]
	if s == nil {
		s = NewRankingStore(dataDir)
		stores[dataDir] = s
	}
	return s
}

// ResetRankingStores forgets every store (tests: a fresh process).
func ResetRankingStores() {
	storesMu.Lock()
	stores = map[string]*RankingStore{}
	storesMu.Unlock()
}

// --- collecting -------------------------------------------------------------

// slotRanking is _slot_ranking: one window's complete counts from the Agent,
// split while it holds too many records.
func slotRanking(ctx context.Context, m Manager, agent *jsonx.Object, recorder string, since, until int64) (*Slot, error) {
	data, err := doRequest(ctx, m, agent, "rankings", query{{"recorder", recorder},
		{"since_ms", strconv.FormatInt(since*1000, 10)}, {"until_ms", strconv.FormatInt(until*1000, 10)},
		{"rows", strconv.Itoa(RankingRows)}}, HistoryAgentVersion)
	if err != nil {
		return nil, err
	}
	if data.Value("success") != true || !pyEqualStr(data.Value("recorder"), recorder) {
		return nil, ErrSlotIncomplete
	}
	sections := map[string]*jsonx.Object{}
	for _, kind := range []string{"clients", "domains"} {
		section, err := rankingSectionRows(data.Value(kind), kind, RankingRows)
		if err != nil {
			return nil, err
		}
		sections[kind] = section
	}
	blocked, ok := data.Value("blocked").(*jsonx.Object)
	if !ok || blocked == nil {
		return nil, ErrSlotIncomplete
	}
	available, ok := blocked.Value("available").(bool)
	if !ok {
		return nil, ErrSlotIncomplete
	}
	if available {
		section, err := rankingSectionRows(blocked, "domains", RankingRows)
		if err != nil {
			return nil, err
		}
		sections["blocked"] = section
	}
	atLimit := false
	for _, section := range sections {
		if section.Value("at_limit") == true {
			atLimit = true
		}
	}
	if atLimit && until-since > RankingMinSlot {
		middle := since + floorDiv(until-since, 2)
		middle -= floorMod(middle, RankingMinSlot)
		first, err := slotRanking(ctx, m, agent, recorder, since, middle)
		if err != nil {
			return nil, err
		}
		second, err := slotRanking(ctx, m, agent, recorder, middle, until)
		if err != nil {
			return nil, err
		}
		return mergeSlots(first, second), nil
	}
	slot := &Slot{Records: sections["clients"].GetInt("sample_size"), Partial: atLimit, Rows: map[string]*Counts{},
		RetentionDays: data.Value("retention_days")}
	if section, ok := sections["blocked"]; ok {
		blockedRecords := section.GetInt("sample_size")
		slot.Blocked = &blockedRecords
	}
	for _, kind := range RankingKinds {
		section, ok := sections[kind]
		if !ok {
			continue
		}
		counts := &Counts{Values: map[string]int64{}}
		for _, item := range section.GetList("rows") {
			row := item.(*jsonx.Object)
			key := row.GetString("key")
			if _, seen := counts.Values[key]; !seen {
				counts.Keys = append(counts.Keys, key)
			}
			counts.Values[key] = row.GetInt("count")
		}
		slot.Rows[kind] = counts
	}
	return slot, nil
}

// mergeSlots is _merge(first, second).
func mergeSlots(first, second *Slot) *Slot {
	rows := map[string]*Counts{}
	for _, kind := range RankingKinds {
		a, inFirst := first.Rows[kind]
		b, inSecond := second.Rows[kind]
		var merged *Counts
		if inFirst {
			merged = a.clone()
		} else {
			merged = &Counts{Values: map[string]int64{}}
		}
		if inSecond {
			for _, key := range b.Keys {
				merged.Add(key, b.Values[key])
			}
		}
		if inFirst || inSecond {
			rows[kind] = merged
		}
	}
	var blocked *int64
	if first.Blocked != nil && second.Blocked != nil {
		sum := *first.Blocked + *second.Blocked
		blocked = &sum
	}
	return &Slot{Records: first.Records + second.Records, Blocked: blocked, Partial: first.Partial || second.Partial,
		Rows: rows, RetentionDays: first.RetentionDays}
}

// LastCompleteSlotEnd is last_complete_slot_end(now).
func LastCompleteSlotEnd(now int64) int64 {
	settled := now - RankingSettleSeconds
	return settled - floorMod(settled, HistorySlot)
}

// rankingSleep is time.sleep (a variable for tests).
var rankingSleep = time.Sleep

// CollectAgent is collect_agent(history, agent, recorders, now, budget,
// retention_days, pause): fetch up to budget missing slots, newest first;
// returns the number stored. retentionDays 0 is CORE_RETENTION_DAYS.
func CollectAgent(ctx context.Context, m Manager, store *RankingStore, agent *jsonx.Object, recorders []string,
	now *int64, budget, retentionDays int, pause time.Duration) (int, error) {
	end := LastCompleteSlotEnd(nowOr(now))
	if retentionDays == 0 {
		retentionDays = RankingCoreRetentionDays
	}
	days := int64(min(HistoryRetentionDay, max(1, retentionDays)))
	var starts []int64
	for start := end - HistorySlot; start > end-days*HistoryDay-1; start -= HistorySlot {
		starts = append(starts, start)
	}
	stored := 0
	agentID := jsonx.Str(agent.Value("id"))
	store.setCaughtUp(agentID, 0, false)
	for _, recorder := range recorders {
		done, err := store.Fetched(agentID, recorder, starts)
		if err != nil {
			return stored, err
		}
		for _, start := range starts {
			if done[start] {
				continue
			}
			if budget <= 0 {
				return stored, nil
			}
			budget--
			slot, err := slotRanking(ctx, m, agent, recorder, start, start+HistorySlot)
			if err != nil {
				var monitorErr *Error
				if !errors.As(err, &monitorErr) && !errors.Is(err, ErrSlotIncomplete) {
					return stored, err
				}
				name := agentID
				if value := agent.Value("name"); jsonx.Truthy(value) {
					name = jsonx.Str(value)
				}
				reason := err.Error()
				if monitorErr != nil {
					reason = monitorErr.Code
				}
				// The device is busy or away: try again next round.
				rankingLogger.Infof("OxiDNS 排行统计暂未取到 %s 的时段（%s），下一轮再试", name, reason)
				return stored, nil
			}
			added, err := store.AddSlot(agentID, recorder, start, slot)
			if err != nil {
				return stored, err
			}
			if added {
				stored++
			}
			if pause > 0 {
				rankingSleep(pause)
			}
		}
	}
	store.setCaughtUp(agentID, end, true)
	return stored, nil
}

// RoundManager is what collect_round needs of the Agent manager.
type RoundManager interface {
	Manager
	GetAllAgents(ctx context.Context, history bool) ([]*jsonx.Object, error)
}

// RoundManagerFromAgents adapts the registry's Manager for CollectRound.
func RoundManagerFromAgents(m *agents.Manager) RoundManager { return agentsManager{m} }

// Seams of collect_round for tests (the Python tests patch _recorders,
// collect_agent and manager.is_usable).
var (
	rankingRecorders = func(ctx context.Context, m Manager, agent *jsonx.Object) ([]string, error) {
		snap, err := overviewSnapshot(ctx, m, agent, true)
		if err != nil {
			return nil, err
		}
		out := make([]string, len(snap.recorders))
		for i, r := range snap.recorders {
			out[i] = r.(string)
		}
		return out, nil
	}
	rankingCollectAgent = func(ctx context.Context, m Manager, store *RankingStore, agent *jsonx.Object, recorders []string, now *int64) error {
		_, err := CollectAgent(ctx, m, store, agent, recorders, now, RankingSlotsPerRound, RankingCoreRetentionDays,
			200*time.Millisecond)
		return err
	}
	rankingIsUsable = func(agent *jsonx.Object) bool { return agents.IsUsable(agent, nil) }
)

// historySupportedStrict is supported(agent) as collect_round evaluates it:
// a truthy version that is not a str raises (AttributeError of .split).
func historySupportedStrict(agent *jsonx.Object) (bool, error) {
	version := agent.Value("version")
	if !jsonx.Truthy(version) {
		version = "0"
	}
	text, ok := version.(string)
	if !ok {
		return false, fmt.Errorf("AttributeError: %s object has no attribute 'split'", pyTypeName(version))
	}
	return agents.CompareVersions(text, HistoryAgentVersion) >= 0, nil
}

// CollectRound is collect_round(manager, history, now): every usable OxiDNS
// Agent of at least HISTORY_AGENT_VERSION that is not caught up, then the
// maintenance. store nil is get_history(data_dir).
func CollectRound(ctx context.Context, m RoundManager, store *RankingStore, now *int64) error {
	if store == nil {
		store = GetRankingStore(m.DataDir())
	}
	list, err := m.GetAllAgents(ctx, false)
	if err != nil {
		return err
	}
	end := LastCompleteSlotEnd(nowOr(now))
	for _, agent := range list {
		if !pyEqualStr(agent.Value("service_type"), "oxidns") {
			continue
		}
		supported, err := historySupportedStrict(agent)
		if err != nil {
			return err
		}
		if !supported || !rankingIsUsable(agent) {
			continue
		}
		if caught, ok := store.CaughtUp(jsonx.Str(agent.Value("id"))); ok && caught == end {
			continue // nothing new until the next slot ends: no request at all
		}
		recorders, err := rankingRecorders(ctx, m, agent)
		if err != nil {
			var monitorErr *Error
			if errors.As(err, &monitorErr) {
				continue
			}
			return err
		}
		if err := rankingCollectAgent(ctx, m, store, agent, recorders, now); err != nil {
			return err
		}
	}
	ids := make([]string, len(list))
	for i, agent := range list {
		ids[i] = jsonx.Str(agent.Value("id"))
	}
	return store.Maintain(ids, now)
}

// rankingLoop is start_loop()'s thread: one collector across processes,
// guarded by a file lock in the data directory; a round every
// ROUND_SECONDS.
func rankingLoop(ctx context.Context) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		threadException(err)
		return
	}
	handle, err := os.OpenFile(filepath.Join(repository.DataDir(), ".oxidns-rankings.lock"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		threadException(err)
		return
	}
	defer handle.Close()
	if err := syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return
	}
	for {
		timer := time.NewTimer(RankingRoundSeconds * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		rankingRound(ctx)
	}
}

// threadException is what Python's thread does when get_repository() or
// open(lock_path, 'a') raises before the loop: the exception leaves the
// thread and threading.excepthook prints it to stderr (not to the log).
var threadException = func(err error) {
	fmt.Fprintf(os.Stderr, "Exception in thread oxidns-ranking-history:\n%v\n", err)
}

func rankingRound(ctx context.Context) {
	scope := repo.WithScope(ctx)
	defer repo.ResetConfigContext(scope)
	defer func() {
		if p := recover(); p != nil {
			rankingLogger.Exception("OxiDNS ranking collection round failed", fmt.Errorf("%v", p))
		}
	}()
	manager, err := agents.Get(scope)
	if err == nil {
		err = CollectRound(scope, RoundManagerFromAgents(manager), nil, nil)
	}
	if err != nil {
		rankingLogger.Exception("OxiDNS ranking collection round failed", err)
	}
}

// rankingStoreFactory is the history.go hook: get_history(data_dir).
func rankingStoreFactory(dataDir string) RankingHistory { return GetRankingStore(dataDir) }
