// Package metrics is the port of backend/agents/metrics_history.py (Agent
// 监控数据历史存储) and backend/agents/metrics_flush.py (系统设置 → 定时任务
// 「监控历史保存」).
//
// Every Agent's points of the last 24 hours are kept in memory; heartbeats
// only append to memory. Each point is kept as the JSON line written to the
// file and parsed again when read. The flush task appends the new points to
// one JSON Lines file per Agent on the configured interval; when a file has
// clearly more lines than the points kept in memory it is rewritten from
// memory into a temporary file and atomically replaced. At service start the
// files are read back (tolerating a half-written last line); the old
// <agent_id>_metrics.json format is read, rewritten in the new format and
// deleted.
//
// The files live in DATA_DIR/metrics. Python kept one MetricsHistory per
// AgentManager; the Go hooks are process-wide, so the state is kept per
// data directory (For / ForManager), which is the same thing for the one
// manager of a data directory and keeps tests on separate data directories
// independent.
//
// Not ported (maintainer's decision): metrics_history.LEGACY_DIRS and
// adopt_legacy_dirs() (moving files out of the old default directories
// /opt/configflow/data/metrics and backend/data/metrics).
package metrics

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/taskstatus"
)

// fromISOFormat is datetime.fromisoformat (naive texts read as UTC).
var fromISOFormat = taskstatus.FromISOFormat

var isValidUTF8 = utf8.Valid

var historyLogger = logx.Get("backend.agents.metrics_history")

const (
	// RetentionHours is RETENTION_HOURS.
	RetentionHours = 24
	// MaxPointsPerAgent is MAX_POINTS_PER_AGENT: about 2880 points in 24
	// hours at one heartbeat per 30 s; unusually frequent heartbeats keep
	// only the newest points.
	MaxPointsPerAgent = 4000
	// CompactRatio is COMPACT_RATIO: a file is compacted when it has more
	// than 1.5 times the lines of the points kept in memory.
	CompactRatio = 1.5
	// FileSuffix is FILE_SUFFIX.
	FileSuffix = "_metrics.jsonl"
	// LegacySuffix is LEGACY_SUFFIX.
	LegacySuffix = "_metrics.json"
)

// agentIDPattern is _AGENT_ID; agentIDMatch adds Python's `$`, which also
// matches before one trailing newline.
var agentIDPattern = regexp.MustCompile(`\A[A-Za-z0-9_.-]+\z`)

func agentIDMatch(name string) bool {
	return agentIDPattern.MatchString(strings.TrimSuffix(name, "\n"))
}

var errnoText = map[syscall.Errno]string{
	syscall.ENOSPC: "磁盘空间不足",
	syscall.EDQUOT: "超出磁盘配额",
	syscall.EACCES: "没有写入权限",
	syscall.EPERM:  "没有写入权限",
	syscall.EROFS:  "文件系统只读",
}

// FlushError is MetricsFlushError: one or more Agents' new points could
// not be written; they stay in memory.
type FlushError struct{ Message string }

func (e *FlushError) Error() string { return e.Message }

// strerror is the C library's strerror(errno), which Python puts in
// OSError.strerror: Go's texts are glibc's with a lower-case first letter.
func strerror(errno syscall.Errno) string {
	if errno == syscall.ESTALE {
		return "Stale file handle"
	}
	text := errno.Error()
	if text == "" {
		return ""
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

// DescribeOSError is describe_os_error: a short, readable reason for
// 定时任务; never includes file contents.
func DescribeOSError(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if text, ok := errnoText[errno]; ok {
			return text
		}
		if text := strerror(errno); text != "" {
			return text
		}
	}
	return err.Error()
}

type point struct {
	at   time.Time // naive local wall clock, held in UTC
	line string
}

// History is MetricsHistory: the monitoring history of the Agents of one
// data directory.
type History struct {
	// DataDir is the directory of the files (DATA_DIR/metrics, or the
	// temporary fallback when it cannot be created).
	DataDir   string
	maxPoints int

	// mu guards memory; flushMu serializes saves, compaction and deletion so
	// that writing the disk never blocks heartbeats.
	mu      sync.Mutex
	flushMu sync.Mutex
	// agent_id -> points (oldest first; at most maxPoints)
	points map[string][]point
	// how many of each Agent's newest points are not in the file yet
	pending map[string]int
	// lines in the file (unparseable ones included), to decide compaction
	fileLines map[string]int
	// Agents whose file failed to write and is not trusted: rewritten from
	// memory at the next save
	rewrite map[string]bool
	// old files a migration could not rewrite yet; deleted once the new file
	// is written
	legacy map[string]string
}

// NewHistory is MetricsHistory(data_dir, max_points): it creates dataDir;
// without permission it falls back to a directory under the temporary
// directory, as Python does.
func NewHistory(dataDir string, maxPoints int) *History {
	h := &History{DataDir: dataDir, maxPoints: maxPoints, points: map[string][]point{}, pending: map[string]int{},
		fileLines: map[string]int{}, rewrite: map[string]bool{}, legacy: map[string]string{}}
	if err := os.MkdirAll(h.DataDir, 0o777); err != nil {
		if errors.Is(err, os.ErrPermission) {
			h.DataDir = filepath.Join(os.TempDir(), "configflow_metrics")
			if err := os.MkdirAll(h.DataDir, 0o777); err != nil {
				historyLogger.Warningf("监控历史目录创建失败：%s", DescribeOSError(err))
			}
			historyLogger.Warningf("监控历史目录没有写入权限，改用临时目录 %s", h.DataDir)
		} else {
			// Python's constructor raised here (the AgentManager could not be
			// built); Go keeps going and reports the error at the next save.
			historyLogger.Warningf("监控历史目录创建失败：%s", DescribeOSError(err))
		}
	}
	return h
}

func (h *History) agentFile(agentID string) string  { return h.DataDir + "/" + agentID + FileSuffix }
func (h *History) legacyFile(agentID string) string { return h.DataDir + "/" + agentID + LegacySuffix }

// now is the clock (tests replace it).
var now = time.Now

// naiveNow is datetime.now(): the local wall clock with microseconds, held
// as a UTC time so that comparisons ignore zone changes like Python's naive
// datetimes.
func naiveNow() time.Time {
	t := now()
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond()/1000*1000, time.UTC)
}

// isoformat is datetime.isoformat() of a naive datetime.
func isoformat(t time.Time) string {
	text := fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d", t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second())
	if micro := t.Nanosecond() / 1000; micro != 0 {
		text += fmt.Sprintf(".%06d", micro)
	}
	return text
}

// OverflowError is Python's OverflowError (its text is str(error)).
type OverflowError struct{ Message string }

func (e *OverflowError) Error() string { return e.Message }

const maxDeltaDays = 999999999

var (
	bigHour   = big.NewInt(3600)
	bigDay    = big.NewInt(86400)
	bigMaxInt = big.NewInt(1<<31 - 1)
	bigMinInt = big.NewInt(-1 << 31)
)

// cutoff is _cutoff(hours): datetime.now() - timedelta(hours=hours), with
// Python's OverflowErrors for a huge hours value.
func cutoff(hours *big.Int) (time.Time, error) {
	seconds := new(big.Int).Mul(hours, bigHour)
	days, rest := new(big.Int).DivMod(seconds, bigDay, new(big.Int))
	if days.Cmp(bigMaxInt) > 0 || days.Cmp(bigMinInt) < 0 {
		return time.Time{}, &OverflowError{"Python int too large to convert to C int"}
	}
	if d := days.Int64(); d > maxDeltaDays || d < -maxDeltaDays {
		return time.Time{}, &OverflowError{fmt.Sprintf("days=%d; must have magnitude <= %d", d, maxDeltaDays)}
	}
	result := naiveNow().AddDate(0, 0, -int(days.Int64())).Add(-time.Duration(rest.Int64()) * time.Second)
	if result.Year() < 1 || result.Year() > 9999 {
		return time.Time{}, &OverflowError{"date value out of range"}
	}
	return result, nil
}

func retentionCutoff() time.Time {
	t, _ := cutoff(big.NewInt(RetentionHours))
	return t
}

// prune is _prune: drop points older than 24 hours; the caller holds mu.
func (h *History) prune(agentID string) []point {
	points, ok := h.points[agentID]
	if !ok {
		return nil
	}
	limit := retentionCutoff()
	drop := 0
	for drop < len(points) && points[drop].at.Before(limit) {
		drop++
	}
	if drop > 0 {
		points = points[drop:]
		h.points[agentID] = points
	}
	if h.pending[agentID] > len(points) {
		h.pending[agentID] = len(points)
	}
	return points
}

// ---- 启动加载与迁移 ---------------------------------------------------------

// parseNaiveISO is datetime.fromisoformat(value) for a naive text; aware
// texts give aware=true (Python then fails to compare them with the naive
// cutoff: TypeError, and the line is skipped).
func parseNaiveISO(value string) (t time.Time, aware bool, ok bool) {
	parsed, ok := fromISOFormat(value)
	if !ok {
		return time.Time{}, false, false
	}
	_, withOffset := fromISOFormat(value + "+00:00")
	_, dateOnly := fromISOFormat(value + "T00:00:00+00:00")
	if !withOffset && !dateOnly {
		return parsed, true, true
	}
	return parsed, false, true
}

// pyIsSpace is str.isspace() for one character.
func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// splitUniversal splits text like iterating a file opened in text mode
// (universal newlines: \n, \r and \r\n end a line; the line keeps "\n").
func splitUniversal(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			lines = append(lines, text[start:i]+"\n")
			start = i + 1
		case '\r':
			lines = append(lines, text[start:i]+"\n")
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// parseLines is _parse_lines: (points, line count). Unparseable lines (an
// append cut short by a crash) and duplicate timestamps are skipped.
func parseLines(lines []string, limit time.Time) ([]point, int) {
	var points []point
	seen := map[string]bool{}
	count := 0
	for _, raw := range lines {
		if strings.TrimFunc(raw, pyIsSpace) == "" {
			continue
		}
		count++
		value, err := jsonx.ParseString(raw)
		if err != nil {
			continue
		}
		object, ok := value.(*jsonx.Object)
		if !ok || object == nil {
			continue
		}
		stamp, present := object.Get("timestamp")
		if !present {
			continue
		}
		text, ok := stamp.(string)
		if !ok {
			continue
		}
		at, aware, ok := parseNaiveISO(text)
		if !ok || aware || at.Before(limit) || seen[text] {
			continue
		}
		line, err := jsonx.Compact(object)
		if err != nil {
			continue
		}
		seen[text] = true
		points = append(points, point{at, string(line)})
	}
	return points, count
}

// decodeReplace is bytes.decode('utf-8', errors='replace').
func decodeReplace(data []byte) string {
	return strings.ToValidUTF8(string(data), "�")
}

func (h *History) readFile(agentID string, limit time.Time) ([]point, int, error) {
	data, err := os.ReadFile(h.agentFile(agentID))
	if err != nil {
		return nil, 0, err
	}
	points, count := parseLines(splitUniversal(decodeReplace(data)), limit)
	return points, count, nil
}

// legacyFormatError is a ValueError (json or UTF-8) of _read_legacy.
type legacyFormatError struct{ error }

// PyTypeName is type(value).__name__ of a JSON value.
func PyTypeName(value any) string { return pyTypeName(value) }

func pyTypeName(value any) string {
	switch v := value.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case jsonx.Number:
		if v.IsInt() {
			return "int"
		}
		return "float"
	case []any:
		return "list"
	case *jsonx.Object:
		if v == nil {
			return "NoneType"
		}
		return "dict"
	}
	return fmt.Sprintf("%T", value)
}

// TypeError is Python's TypeError (its text is str(error)).
type TypeError struct{ Message string }

func (e *TypeError) Error() string { return e.Message }

func (h *History) readLegacy(agentID string, limit time.Time) ([]point, error) {
	data, err := os.ReadFile(h.legacyFile(agentID))
	if err != nil {
		return nil, err
	}
	if !isValidUTF8(data) {
		return nil, legacyFormatError{errors.New("invalid utf-8")}
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil, legacyFormatError{err}
	}
	var metrics any = []any{}
	if object, ok := value.(*jsonx.Object); ok && object != nil {
		if v, present := object.Get("metrics"); present {
			metrics = v
		}
	}
	var items []any
	switch v := metrics.(type) {
	case []any:
		items = v
	case *jsonx.Object:
		if v == nil {
			return nil, &TypeError{"'NoneType' object is not iterable"}
		}
		// iterating a dict gives its keys (strings): none is a point
	case string:
		// iterating a string gives characters: none is a point
	default:
		// Python's TypeError is not caught by load(): it ends the whole load.
		return nil, &TypeError{fmt.Sprintf("'%s' object is not iterable", pyTypeName(metrics))}
	}
	var lines []string
	for _, item := range items {
		if object, ok := item.(*jsonx.Object); ok && object != nil {
			line, err := jsonx.Compact(object)
			if err != nil {
				continue
			}
			lines = append(lines, string(line))
		}
	}
	points, _ := parseLines(lines, limit)
	return points, nil
}

// LoadResult is the counts load() returns.
type LoadResult struct{ Loaded, Migrated, Removed int }

// Object is the dict Python returns.
func (r LoadResult) Object() *jsonx.Object {
	return jsonx.Obj("loaded", r.Loaded, "migrated", r.Migrated, "removed", r.Removed)
}

// Load is load(agent_ids): read back the history of existing Agents at
// service start and delete the files of deleted Agents. The old whole-JSON
// format is read, rewritten as JSON Lines and deleted.
//
// Points a heartbeat added before Load ran (the Go server listens before
// its loops start; Python loaded before serving) are kept after the loaded
// ones instead of being replaced.
//
// The error is the TypeError Python's load() raised for an old file whose
// "metrics" is not iterable (load_history logs it and gives up).
func (h *History) Load(agentIDs []string) (LoadResult, error) {
	wanted := map[string]bool{}
	for _, id := range agentIDs {
		wanted[id] = true
	}
	result := LoadResult{}
	entries, err := os.ReadDir(h.DataDir)
	if err != nil {
		historyLogger.Warningf("读取监控历史目录失败：%s", DescribeOSError(err))
		return result, nil
	}
	type kinds struct{ file, legacy bool }
	found := map[string]*kinds{}
	var order []string
	for _, entry := range entries {
		name := entry.Name()
		path := h.DataDir + "/" + name
		if strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".tmp") {
			// 整理文件时中断留下的临时文件
			h.remove(path, false)
			continue
		}
		for _, suffix := range []string{FileSuffix, LegacySuffix} {
			agentID := strings.TrimSuffix(name, suffix)
			if strings.HasSuffix(name, suffix) && agentIDMatch(agentID) {
				k := found[agentID]
				if k == nil {
					k = &kinds{}
					found[agentID] = k
					order = append(order, agentID)
				}
				if suffix == FileSuffix {
					k.file = true
				} else {
					k.legacy = true
				}
				break
			}
		}
	}

	limit := retentionCutoff()
	var migrate []string
	h.flushMu.Lock()
	for _, agentID := range order {
		k := found[agentID]
		if !wanted[agentID] {
			for _, item := range []struct {
				present bool
				suffix  string
			}{{k.file, FileSuffix}, {k.legacy, LegacySuffix}} {
				if item.present && h.remove(h.DataDir+"/"+agentID+item.suffix, false) == removed {
					result.Removed++
				}
			}
			continue
		}
		var points []point
		lines := 0
		migrated := false
		if k.file {
			points, lines, err = h.readFile(agentID, limit)
			if err != nil {
				historyLogger.Warningf("读取 Agent %s 的监控历史失败：%s", agentID, DescribeOSError(err))
				continue
			}
			if k.legacy {
				// 新文件由旧文件改写而来，旧文件是迁移中断留下的
				h.remove(h.legacyFile(agentID), false)
			}
		} else {
			points, err = h.readLegacy(agentID, limit)
			if err != nil {
				var typeError *TypeError
				if errors.As(err, &typeError) {
					h.flushMu.Unlock()
					return result, err
				}
				reason := "文件格式错误"
				var format legacyFormatError
				if !errors.As(err, &format) {
					reason = DescribeOSError(err)
				}
				historyLogger.Warningf("读取 Agent %s 的旧版监控历史失败，已忽略：%s", agentID, reason)
				h.remove(h.legacyFile(agentID), false)
				continue
			}
			migrate = append(migrate, agentID)
			migrated = true
		}
		if len(points) > h.maxPoints {
			points = points[len(points)-h.maxPoints:]
		}
		h.mu.Lock()
		earlier := h.points[agentID]
		merged := append(append([]point{}, points...), earlier...)
		if len(merged) > h.maxPoints {
			merged = merged[len(merged)-h.maxPoints:]
		}
		h.points[agentID] = merged
		h.fileLines[agentID] = lines
		if migrated {
			h.pending[agentID] = len(merged)
			h.legacy[agentID] = h.legacyFile(agentID)
			h.rewrite[agentID] = true
		} else if h.pending[agentID] > len(merged) {
			h.pending[agentID] = len(merged)
		}
		h.mu.Unlock()
		result.Loaded++
	}
	var errs []agentError
	if len(migrate) > 0 {
		errs = h.flushAgents(migrate).errors
	}
	h.flushMu.Unlock()
	result.Migrated = len(migrate) - len(errs)
	for _, item := range errs {
		historyLogger.Warningf("Agent %s 的旧版监控历史改写失败，下次保存时重试：%s", item.agentID, item.reason)
	}
	if result.Migrated > 0 || result.Removed > 0 {
		historyLogger.Infof("监控历史已加载：迁移旧格式 %d 个，删除已删除 Agent 的文件 %d 个", result.Migrated, result.Removed)
	}
	return result, nil
}

// ---- 心跳与查询 -------------------------------------------------------------

// AddMetrics is add_metrics(agent_id, system_metrics): append one point to
// memory (the flush task writes it to the file). The error is the
// AttributeError Python raised when system_metrics is not a dict.
func (h *History) AddMetrics(agentID string, systemMetrics any) (bool, error) {
	metrics, ok := systemMetrics.(*jsonx.Object)
	if !ok || metrics == nil {
		return false, &AttributeError{fmt.Sprintf("'%s' object has no attribute 'get'", pyTypeName(systemMetrics))}
	}
	at := naiveNow()
	get := func(key string) any {
		if value, present := metrics.Get(key); present {
			return value
		}
		return jsonx.NewObject()
	}
	dataPoint := jsonx.Obj("timestamp", isoformat(at), "cpu", get("cpu"), "memory", get("memory"),
		"disk", get("disk"), "network", get("network"))
	// 查询时解析这一行，结果与以前从文件读回的一致，也不与心跳数据共享可变对象
	line, err := jsonx.Compact(dataPoint)
	if err != nil {
		historyLogger.Warningf("Agent %s 的监控数据无法保存：%s", agentID, err)
		return false, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	points := append(h.points[agentID], point{at, string(line)})
	if len(points) > h.maxPoints {
		points = points[len(points)-h.maxPoints:]
	}
	h.points[agentID] = points
	h.pending[agentID] = min(h.pending[agentID]+1, len(points))
	h.prune(agentID)
	return true, nil
}

// AttributeError is Python's AttributeError (its text is str(error)).
type AttributeError struct{ Message string }

func (e *AttributeError) Error() string { return e.Message }

// GetMetrics is get_metrics(agent_id, hours): the points of the last hours
// hours (nil: every point kept), each a fresh parse of its line. The error
// is Python's OverflowError for an hours value timedelta cannot hold.
func (h *History) GetMetrics(agentID string, hours *big.Int) ([]any, error) {
	h.mu.Lock()
	points := h.prune(agentID)
	var lines []string
	if hours == nil {
		for _, p := range points {
			lines = append(lines, p.line)
		}
	} else {
		limit, err := cutoff(hours)
		if err != nil {
			h.mu.Unlock()
			return nil, err
		}
		for _, p := range points {
			if !p.at.Before(limit) {
				lines = append(lines, p.line)
			}
		}
	}
	h.mu.Unlock()
	out := make([]any, 0, len(lines))
	for _, line := range lines {
		value, err := jsonx.ParseString(line)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

// GetLatestMetrics is get_latest_metrics(agent_id): the newest point, nil
// when there is none.
func (h *History) GetLatestMetrics(agentID string) any {
	metrics, _ := h.GetMetrics(agentID, nil)
	if len(metrics) > 0 {
		return metrics[len(metrics)-1]
	}
	return nil
}

// PendingCount is pending_count(): the points in memory not written yet.
func (h *History) PendingCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	total := 0
	for _, n := range h.pending {
		total += n
	}
	return total
}

// DeleteAgentHistory is delete_agent_history(agent_id): drop the Agent's
// history from memory and disk; false when a file could not be removed.
func (h *History) DeleteAgentHistory(agentID string) bool {
	h.flushMu.Lock()
	defer h.flushMu.Unlock()
	h.mu.Lock()
	delete(h.points, agentID)
	delete(h.pending, agentID)
	delete(h.fileLines, agentID)
	delete(h.legacy, agentID)
	delete(h.rewrite, agentID)
	h.mu.Unlock()
	first := h.remove(h.agentFile(agentID), true)
	second := h.remove(h.legacyFile(agentID), true)
	return first != failed && second != failed
}

// RetainAgents is retain_agents(agent_ids): delete the history of every
// Agent not in agentIDs (import, WebDAV restore and reset replace the Agent
// list without deleting Agents); returns the number of Agents dropped.
func (h *History) RetainAgents(agentIDs []string) int {
	wanted := map[string]bool{}
	for _, id := range agentIDs {
		wanted[id] = true
	}
	h.mu.Lock()
	stale := map[string]bool{}
	for _, store := range []map[string]bool{keysOf(h.points), keysOf(h.fileLines), keysOf(h.legacy)} {
		for id := range store {
			if !wanted[id] {
				stale[id] = true
			}
		}
	}
	h.mu.Unlock()
	for _, id := range sortedKeys(stale) {
		h.DeleteAgentHistory(id)
	}
	return len(stale)
}

func keysOf[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- 写入文件 ---------------------------------------------------------------

type removal int

const (
	failed removal = iota - 1
	missing
	removed
)

// unlink is os.remove (unlink(2): a directory is an error, not removed).
var unlink = func(path string) error {
	if err := syscall.Unlink(path); err != nil {
		return &os.PathError{Op: "remove", Path: path, Err: err}
	}
	return nil
}

// remove is _remove: removed, missing, or failed (logged as a warning when
// report is set, at debug level otherwise).
func (h *History) remove(path string, report bool) removal {
	err := unlink(path)
	if err == nil {
		return removed
	}
	if errors.Is(err, syscall.ENOENT) {
		return missing
	}
	log := historyLogger.Debugf
	if report {
		log = historyLogger.Warningf
	}
	log("删除监控历史文件 %s 失败：%s", filepath.Base(path), DescribeOSError(err))
	return failed
}

func joinLines(lines []string) []byte {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// appendLines is _append (tests replace it).
var appendLines = func(path string, lines []string) error {
	handle, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o666)
	if err != nil {
		return err
	}
	defer handle.Close()
	size, err := handle.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	var prefix []byte
	if size > 0 {
		// 上次追加若被中断，最后一行没有换行；新行另起一行，坏行在读取时跳过
		last := make([]byte, 1)
		if _, err := handle.ReadAt(last, size-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			prefix = []byte{'\n'}
		}
	}
	if prefix != nil {
		if _, err := handle.Write(prefix); err != nil {
			return err
		}
	}
	if _, err := handle.Write(joinLines(lines)); err != nil {
		return err
	}
	return handle.Sync()
}

// replaceFile is os.replace (tests replace it).
var replaceFile = os.Rename

// replace is _replace: write the lines to a temporary file in the data
// directory and atomically replace the file; no lines removes the file.
func (h *History) replace(agentID, path string, lines []string) error {
	if len(lines) == 0 {
		h.remove(path, false)
		return nil
	}
	handle, err := os.CreateTemp(h.DataDir, "."+agentID+"*.tmp")
	if err != nil {
		return err
	}
	temporary := handle.Name()
	defer func() {
		if _, err := os.Lstat(temporary); err == nil {
			os.Remove(temporary)
		}
	}()
	if _, err := handle.Write(joinLines(lines)); err != nil {
		handle.Close()
		return err
	}
	if err := handle.Sync(); err != nil {
		handle.Close()
		return err
	}
	if err := handle.Close(); err != nil {
		return err
	}
	return replaceFile(temporary, path)
}

type agentError struct{ agentID, reason string }

type flushResult struct {
	agents, appended, compacted int
	errors                      []agentError
}

type flushJob struct {
	agentID string
	compact bool
	pending int
	lines   []string
}

// flushAgents is _flush_agents: write the given Agents; the caller holds
// flushMu.
func (h *History) flushAgents(agentIDs []string) flushResult {
	var jobs []flushJob
	h.mu.Lock()
	for _, agentID := range agentIDs {
		points := h.prune(agentID)
		pending := h.pending[agentID]
		linesAfter := h.fileLines[agentID] + pending
		compact := h.rewrite[agentID] || float64(linesAfter) > float64(len(points))*CompactRatio
		if !compact && pending == 0 {
			continue
		}
		var lines []string
		selected := points
		if !compact {
			selected = points[len(points)-pending:]
		}
		for _, p := range selected {
			lines = append(lines, p.line)
		}
		h.pending[agentID] = 0
		jobs = append(jobs, flushJob{agentID, compact, pending, lines})
		if len(points) == 0 {
			// 超过 24 小时没有心跳：删除文件，也不再保留空记录
			delete(h.points, agentID)
			delete(h.pending, agentID)
		}
	}
	h.mu.Unlock()

	result := flushResult{}
	for _, job := range jobs {
		path := h.agentFile(job.agentID)
		var err error
		if job.compact {
			err = h.replace(job.agentID, path, job.lines)
		} else {
			err = appendLines(path, job.lines)
		}
		if err != nil {
			h.mu.Lock()
			if points, ok := h.points[job.agentID]; ok {
				h.pending[job.agentID] = min(h.pending[job.agentID]+job.pending, len(points))
				// 追加可能只写了一部分；下次从内存整份重写，避免重复的点
				h.rewrite[job.agentID] = true
			}
			h.mu.Unlock()
			result.errors = append(result.errors, agentError{job.agentID, DescribeOSError(err)})
			continue
		}
		h.mu.Lock()
		if job.compact {
			delete(h.rewrite, job.agentID)
		}
		if len(job.lines) > 0 {
			if job.compact {
				h.fileLines[job.agentID] = len(job.lines)
			} else {
				h.fileLines[job.agentID] += len(job.lines)
			}
		} else {
			delete(h.fileLines, job.agentID)
		}
		legacy, hadLegacy := h.legacy[job.agentID]
		delete(h.legacy, job.agentID)
		h.mu.Unlock()
		if hadLegacy && legacy != "" {
			h.remove(legacy, true)
		}
		if job.pending > 0 {
			result.agents++
		}
		result.appended += job.pending
		if job.compact {
			result.compacted++
		}
	}
	return result
}

// FlushResult is the counts flush() returns.
type FlushResult struct{ Agents, Appended, Compacted int }

// Object is the dict Python returns.
func (r FlushResult) Object() *jsonx.Object {
	return jsonx.Obj("agents", r.Agents, "appended", r.Appended, "compacted", r.Compacted)
}

// Flush is flush(): append the new points to the files; a file clearly
// larger than the points in memory is atomically compacted. A *FlushError
// means some Agents failed; their points stay in memory for the next save.
func (h *History) Flush() (FlushResult, error) {
	h.flushMu.Lock()
	h.mu.Lock()
	ids := map[string]bool{}
	for _, store := range []map[string]bool{keysOf(h.points), keysOf(h.fileLines), keysOf(h.legacy)} {
		for id := range store {
			ids[id] = true
		}
	}
	h.mu.Unlock()
	result := h.flushAgents(sortedKeys(ids))
	h.flushMu.Unlock()
	counts := FlushResult{result.agents, result.appended, result.compacted}
	if len(result.errors) > 0 {
		reasons := map[string]bool{}
		for _, item := range result.errors {
			reasons[item.reason] = true
		}
		return counts, &FlushError{fmt.Sprintf("%d 台 Agent 的监控历史写入失败（%s），数据仍在内存中，下次保存时重试。目录：%s",
			len(result.errors), strings.Join(sortedKeys(reasons), "；"), h.DataDir)}
	}
	return counts, nil
}
