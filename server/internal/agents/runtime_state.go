package agents

// Port of backend/agents/runtime_state.py: Agent heartbeat state kept in
// process memory.
//
// A heartbeat's liveness, service status and metrics are only worth showing
// while they are fresh, so they live here and are laid over the stored Agent
// on read; a restart forgets them and each Agent reappears with its next
// heartbeat. The reported version is the exception: the manager writes it
// into the configuration document whenever it changes.
//
// Whichever side holds the newer heartbeat wins: registration writes the
// document, so a re-registered Agent shows its new version until its next
// heartbeat. Every Manager for the same data directory shares one state.

import (
	"bytes"
	"configflow/internal/model"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"modernc.org/sqlite"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/repo"
)

// RuntimeFields are RUNTIME_FIELDS: what a heartbeat may lay over a record.
var RuntimeFields = []string{"version", "service_status", "system_metrics", "cpu", "memory", "disk", "network"}

var metricFields = []string{"cpu", "memory", "disk", "network"}

// LegacyDatabase is LEGACY_DATABASE, written by #369; read once, then removed.
const LegacyDatabase = "runtime.sqlite3"

var runtimeLogger = logx.Get("backend.agents.runtime_state")

var (
	statesMu      sync.Mutex
	states        = map[string]*RuntimeState{}
	legacyChecked = map[string]bool{}
)

// timestampOf is _timestamp(value): the POSIX time of an ISO text, -Inf
// for anything unparsable or ahead of the clock.
func timestampOf(value any) float64 {
	text, ok := value.(string)
	if !ok {
		return math.Inf(-1)
	}
	// ISOTimestamp: also the ValueError of a naive time whose resolution
	// needs a local time outside the years 1–9999 (A15 review).
	ts, ok := ISOTimestamp(text)
	if !ok {
		return math.Inf(-1)
	}
	// A registration time ahead of the clock (time zone changed, clock stepped
	// back) must not outrank every later heartbeat.
	if ts <= nowTimestamp() {
		return ts
	}
	return math.Inf(-1)
}

// heartbeatEntry is an immutable, owned runtime projection. It never contains
// deployment history, credentials or the configuration document.
type heartbeatEntry struct {
	HeartbeatAt   float64
	LastHeartbeat string
	Fields        model.Agent
}

// RuntimeState owns live heartbeats for a data directory.
type RuntimeState struct {
	mu        sync.Mutex
	entries   map[string]heartbeatEntry
	deletions int64
	deleted   map[string]int64
}

// NewRuntimeState returns an empty in-memory heartbeat store.
func NewRuntimeState() *RuntimeState {
	return &RuntimeState{entries: map[string]heartbeatEntry{}, deleted: map[string]int64{}}
}

func (s *RuntimeState) newerModel(agent *model.Agent) (heartbeatEntry, bool) {
	entry, ok := s.entries[agent.ID]
	return entry, ok && entry.HeartbeatAt > timestampOf(agent.LastHeartbeat)
}

func runtimeProjection(source *model.Agent) (model.Agent, error) {
	var result model.Agent
	for _, key := range RuntimeFields {
		data, present, err := source.MemberJSON(key)
		if err != nil {
			return result, err
		}
		if present {
			if err := result.SetJSON(key, data); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

// Mark is taken before the repository lookup, preventing deletion races.
func (s *RuntimeState) Mark() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deletions
}

func (s *RuntimeState) recordModel(ctx context.Context, agent, payload *model.Agent, mark int64) (bool, error) {
	now := nowTimestamp()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.deleted[agent.ID] > mark {
		return false, nil
	}
	source := agent
	if previous, ok := s.newerModel(agent); ok {
		source = &previous.Fields
	}
	fields, err := runtimeProjection(source)
	if err != nil {
		return false, err
	}
	for _, key := range []string{"version", "service_status", "system_metrics"} {
		data, present, err := payload.MemberJSON(key)
		if err != nil {
			return false, err
		}
		if !present {
			continue
		}
		if err := fields.SetJSON(key, data); err != nil {
			return false, err
		}
		if key == "system_metrics" && payload.SystemMetrics != nil {
			var metrics model.Fields
			if err := json.Unmarshal(data, &metrics); err != nil {
				return false, err
			}
			for _, component := range metricFields {
				if raw, exists := metrics.Get(component); exists {
					if err := fields.SetJSON(component, raw); err != nil {
						return false, err
					}
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.entries[agent.ID] = heartbeatEntry{HeartbeatAt: now, LastHeartbeat: isoformat(fromTimestamp(now), false), Fields: fields}
	return true, nil
}

func (s *RuntimeState) seedModel(ctx context.Context, agentID string, heartbeatAt float64, lastHeartbeat string, fields *model.Agent) error {
	projection, err := runtimeProjection(fields)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry, ok := s.entries[agentID]; !ok || entry.HeartbeatAt < heartbeatAt {
		s.entries[agentID] = heartbeatEntry{HeartbeatAt: heartbeatAt, LastHeartbeat: lastHeartbeat, Fields: projection}
	}
	return nil
}

// Delete forgets an Agent and blocks heartbeats looked up before it.
func (s *RuntimeState) Delete(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, agentID)
	s.deletions++
	s.deleted[agentID] = s.deletions
}

func pyResolve(path string) string {
	resolved, _ := joinRealPath("", path, map[string]*string{})
	if !filepath.IsAbs(resolved) {
		if wd, err := os.Getwd(); err == nil {
			resolved = filepath.Join(wd, resolved)
		}
	}
	return filepath.Clean(resolved)
}

// joinRealPath is posixpath._joinrealpath(path, rest, strict=False, seen).
func joinRealPath(path, rest string, seen map[string]*string) (string, bool) {
	join := func(a, b string) string {
		if strings.HasPrefix(b, "/") || a == "" {
			return b
		}
		if strings.HasSuffix(a, "/") {
			return a + b
		}
		return a + "/" + b
	}
	if strings.HasPrefix(rest, "/") {
		rest, path = rest[1:], "/"
	}
	for rest != "" {
		var name string
		name, rest, _ = strings.Cut(rest, "/")
		if name == "" || name == "." {
			continue
		}
		if name == ".." {
			if path != "" {
				// path, name = posixpath.split(path)
				i := strings.LastIndex(path, "/") + 1
				head, tail := path[:i], path[i:]
				if head != "" && strings.Trim(head, "/") != "" {
					head = strings.TrimRight(head, "/")
				}
				path = head
				if tail == ".." {
					path = join(join(path, ".."), "..")
				}
			} else {
				path = ".."
			}
			continue
		}
		newPath := join(path, name)
		info, err := os.Lstat(newPath)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			path = newPath
			continue
		}
		if known, ok := seen[newPath]; ok {
			if known != nil {
				path = *known
				continue
			}
			return join(newPath, rest), false
		}
		seen[newPath] = nil
		target, err := os.Readlink(newPath)
		if err != nil {
			path = newPath
			continue
		}
		var ok bool
		path, ok = joinRealPath(path, target, seen)
		if !ok {
			return join(path, rest), false
		}
		resolved := path
		seen[newPath] = &resolved
	}
	return path, true
}

// RuntimeFor is for_data_dir(data_dir): the process-wide state of a data
// directory.
func RuntimeFor(dataDir string) *RuntimeState {
	key := pyResolve(dataDir)
	statesMu.Lock()
	defer statesMu.Unlock()
	state, ok := states[key]
	if !ok {
		state = NewRuntimeState()
		states[key] = state
	}
	return state
}

// ResetRuntime forgets every Agent's heartbeat state, as a process restart
// does (tests).
func ResetRuntime() {
	statesMu.Lock()
	states = map[string]*RuntimeState{}
	legacyChecked = map[string]bool{}
	statesMu.Unlock()
}

type legacyRow struct {
	agentID       string
	heartbeatAt   float64
	lastHeartbeat string
	fields        *jsonx.Object
}

// legacyRows is _legacy_rows(path): the decodable rows of the #369 table,
// nothing (and a warning) when the database cannot be read.
func legacyRows(path string) map[string]legacyRow {
	rows, err := readLegacyRows(path)
	if err != nil {
		runtimeLogger.Warningf("旧版 Agent 心跳状态库无法读取，已忽略：%s", err)
		return map[string]legacyRow{}
	}
	result := make(map[string]legacyRow, len(rows))
	for _, row := range rows {
		result[row.agentID] = row
	}
	return result
}

// readLegacyRows reads the rows in Python's dict order (a repeated agent_id
// keeps its first position and its last value). The error's text is
// str(sqlite3.Error).
func readLegacyRows(path string) ([]legacyRow, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String() + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, sqliteFailure(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT agent_id, heartbeat_at, last_heartbeat, fields FROM agent_runtime")
	if err != nil {
		return nil, sqliteFailure(err)
	}
	defer rows.Close()
	// fetchall(): every row is converted before any is used, so one TEXT value
	// that is not UTF-8 fails the whole read.
	var raw [][4]any
	columns := [4]string{"agent_id", "heartbeat_at", "last_heartbeat", "fields"}
	for rows.Next() {
		var row [4]any
		if err := rows.Scan(&row[0], &row[1], &row[2], &row[3]); err != nil {
			return nil, sqliteFailure(err)
		}
		for i, value := range row {
			if text, ok := value.(string); ok && !utf8.ValidString(text) {
				return nil, &textDecodeError{column: columns[i], text: text}
			}
		}
		raw = append(raw, row)
	}
	if err := rows.Err(); err != nil {
		return nil, sqliteFailure(err)
	}
	var result []legacyRow
	index := map[string]int{}
	for _, row := range raw {
		fields, ok := legacyFields(row[3])
		if !ok {
			continue
		}
		id, idOK := row[0].(string)
		last, lastOK := row[2].(string)
		var at float64
		atOK := true
		switch value := row[1].(type) {
		case int64:
			at = float64(value)
		case float64:
			at = value
		default:
			atOK = false
		}
		if !idOK || !atOK || !lastOK {
			continue
		}
		item := legacyRow{id, at, last, fields}
		if i, seen := index[id]; seen {
			result[i] = item
			continue
		}
		index[id] = len(result)
		result = append(result, item)
	}
	return result, nil
}

// legacyFields is json.loads(raw) of the fields column, ok=false where
// Python raises TypeError/ValueError or the value is not an object.
func legacyFields(raw any) (*jsonx.Object, bool) {
	var text string
	switch value := raw.(type) {
	case string:
		text = value
	case []byte:
		decoded, ok := decodeJSONBytes(value)
		if !ok {
			return nil, false
		}
		text = decoded
	default:
		return nil, false
	}
	parsed, err := jsonx.ParseString(text)
	if err != nil {
		return nil, false
	}
	fields, ok := parsed.(*jsonx.Object)
	return fields, ok && fields != nil
}

// DecodeJSONBytes is decodeJSONBytes for other packages.
func DecodeJSONBytes(b []byte) (string, bool) { return decodeJSONBytes(b) }

// decodeJSONBytes is the bytes branch of json.loads:
// s.decode(json.detect_encoding(s), 'surrogatepass'). ok=false is a
// UnicodeDecodeError. Lone surrogates become U+FFFD (they have no UTF-8
// form; the same holds for every jsonx string).
func decodeJSONBytes(b []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(b, []byte{0, 0, 0xFE, 0xFF}):
		return decodeUTF32(b[4:], binary.BigEndian)
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE, 0, 0}):
		return decodeUTF32(b[4:], binary.LittleEndian)
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return decodeUTF16(b[2:], binary.BigEndian)
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		return decodeUTF16(b[2:], binary.LittleEndian)
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		b = b[3:]
	case len(b) >= 4:
		if b[0] == 0 {
			if b[1] != 0 {
				return decodeUTF16(b, binary.BigEndian)
			}
			return decodeUTF32(b, binary.BigEndian)
		}
		if b[1] == 0 {
			if b[2] != 0 || b[3] != 0 {
				return decodeUTF16(b, binary.LittleEndian)
			}
			return decodeUTF32(b, binary.LittleEndian)
		}
	case len(b) == 2:
		if b[0] == 0 {
			return decodeUTF16(b, binary.BigEndian)
		}
		if b[1] == 0 {
			return decodeUTF16(b, binary.LittleEndian)
		}
	}
	if !utf8.Valid(b) {
		return "", false
	}
	return string(b), true
}

func decodeUTF16(b []byte, order binary.ByteOrder) (string, bool) {
	if len(b)%2 != 0 {
		return "", false
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = order.Uint16(b[2*i:])
	}
	return string(utf16.Decode(units)), true
}

func decodeUTF32(b []byte, order binary.ByteOrder) (string, bool) {
	if len(b)%4 != 0 {
		return "", false
	}
	var out strings.Builder
	for i := 0; i < len(b); i += 4 {
		r := order.Uint32(b[i:])
		if r > unicode.MaxRune {
			return "", false
		}
		out.WriteRune(rune(r))
	}
	return out.String(), true
}

// textDecodeError is the sqlite3.OperationalError Python's sqlite3 module
// raises when a TEXT value is not valid UTF-8.
type textDecodeError struct {
	column string
	text   string
}

func (e *textDecodeError) Error() string {
	var b strings.Builder
	for _, r := range e.text {
		b.WriteRune(r)
	}
	return "Could not decode to UTF-8 column '" + e.column + "' with text '" + b.String() + "'"
}

var sqliteCodeSuffix = regexp.MustCompile(` \(\d+\)$`)

// sqliteFailure gives a driver error the text of str(sqlite3.Error)
// (sqlite3_errmsg): the driver writes "errstr: errmsg (code)".
func sqliteFailure(err error) error {
	var target *sqlite.Error
	if !errors.As(err, &target) {
		return err
	}
	message := strings.TrimSuffix(target.Error(), " (SQLITE_BUSY)")
	message = sqliteCodeSuffix.ReplaceAllString(message, "")
	if _, detail, found := strings.Cut(message, ": "); found {
		message = detail
	}
	return errors.New(message)
}

func legacyFiles(path string) []string {
	files := []string{path, path + "-journal", path + "-wal", path + "-shm"}
	corrupt, _ := filepath.Glob(escapeGlob(path) + ".corrupt-*")
	sort.Strings(corrupt)
	return append(files, corrupt...)
}

func escapeGlob(path string) string {
	replacer := strings.NewReplacer("*", `\*`, "?", `\?`, "[", `\[`, `\`, `\\`)
	return replacer.Replace(path)
}

// removeFiles is _remove(paths): Path.unlink (never rmdir), a missing file
// is fine, any other failure is logged.
func removeFiles(paths []string) {
	for _, path := range paths {
		if err := syscall.Unlink(path); err != nil && !errors.Is(err, syscall.ENOENT) {
			runtimeLogger.Warningf("旧版 Agent 心跳状态文件 %s 删除失败：%s", filepath.Base(path), pyOSError(err, path))
		}
	}
}

// pyOSError is str(OSError) of a failed call on path:
// "[Errno 21] Is a directory: '/data/runtime.sqlite3-wal'".
func pyOSError(err error, path string) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		text := errno.Error()
		if text != "" {
			text = strings.ToUpper(text[:1]) + text[1:]
		}
		return fmt.Sprintf("[Errno %d] %s: %s", int(errno), text, jsonx.Repr(path))
	}
	return err.Error()
}

// exists is Path.exists(): follows symlinks, so a dangling link is missing.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// removeLegacyFile is the indirection tests use to refuse a removal.
var removeLegacyFile = removeFiles

func removeLegacyFiles(ctx context.Context, paths []string) error {
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		removeLegacyFile([]string{path})
	}
	return ctx.Err()
}

// ImportLegacyDatabase is import_legacy_database(repository, state,
// persist_versions): carry #369's on-disk heartbeat state into memory once,
// then remove it. Agents newer there than in the document are seeded, and a
// version that only reached that database is handed to persist. Nothing is
// applied unless the file could be removed; a failed version write is left to
// the Agent's next heartbeat.
func ImportLegacyDatabase(ctx context.Context, repository *repo.Repository, state *RuntimeState,
	persist func(ctx context.Context, changes map[string]*jsonx.Object) (map[string]bool, error)) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(repository.DataDir(), LegacyDatabase)
	key := pyResolve(path)
	statesMu.Lock()
	if legacyChecked[key] {
		statesMu.Unlock()
		return nil
	}
	legacyChecked[key] = true
	statesMu.Unlock()
	found := false
	for _, item := range legacyFiles(path) {
		if exists(item) {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	rows := map[string]legacyRow{}
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		rows = legacyRows(path)
	}
	// Listed after reading, which may leave a journal behind.
	if err := removeLegacyFiles(ctx, legacyFiles(path)); err != nil {
		return err
	}
	if exists(path) {
		runtimeLogger.Warningf("旧版 %s 无法删除，未导入其中的 Agent 心跳状态", LegacyDatabase)
		return nil
	}
	system, err := repository.GetSystem(ctx)
	if err != nil {
		return err
	}
	agents := map[string]*jsonx.Object{}
	for _, item := range system.GetList("agents") {
		// Legacy rows have string ids; an Agent without one never matches.
		if agent, ok := item.(*jsonx.Object); ok {
			if id, ok := agent.Value("id").(string); ok {
				agents[id] = agent
			}
		}
	}
	current := map[string]legacyRow{}
	var ids []string
	for id, row := range rows {
		if agent, ok := agents[id]; ok && row.heartbeatAt > timestampOf(agent.Value("last_heartbeat")) {
			current[id] = row
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	versions := map[string]*jsonx.Object{}
	for _, id := range ids {
		row := current[id]
		if version, ok := row.fields.Value("version").(string); ok && !jsonx.Equal(version, agents[id].Value("version")) {
			versions[id] = jsonx.Obj("version", version)
		}
	}
	for _, id := range ids {
		row := current[id]
		fields, err := agentModel(row.fields)
		if err != nil {
			continue
		}
		if err := state.seedModel(ctx, id, row.heartbeatAt, row.lastHeartbeat, fields); err != nil {
			return err
		}
	}
	if len(versions) > 0 {
		if _, err := persist(ctx, versions); err != nil {
			runtimeLogger.Warningf("旧版 Agent 心跳状态里的版本号写入配置失败，将在 Agent 下次心跳时写入：%s", err)
			versions = map[string]*jsonx.Object{}
		}
	}
	runtimeLogger.Infof("Agent 心跳状态改为只保存在内存：已从旧版 %s 恢复 %d 个 Agent 的状态（%d 个版本号写入配置），并删除该文件",
		LegacyDatabase, len(current), len(versions))
	return nil
}
