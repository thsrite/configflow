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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	_ "modernc.org/sqlite"

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
func heartbeatTimestamp(text string) float64 {
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
	Fields        runtimeFields
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
	return entry, ok && entry.HeartbeatAt > heartbeatTimestamp(agent.LastHeartbeat)
}

// runtimeFields owns only the seven JSON values exposed by live overlays.
// Published slices are immutable; replacing one value cannot mutate an older
// entry or retain a whole stored Agent, deployment history or profile.
type runtimeFields [7]json.RawMessage

func runtimeProjection(source *model.Agent) (runtimeFields, error) {
	var result runtimeFields
	for i, key := range RuntimeFields {
		raw, present, err := source.MemberJSON(key)
		if err != nil {
			return result, err
		}
		if present {
			result[i] = raw
		}
	}
	return result, nil
}

type heartbeatSource struct {
	id, name, lastHeartbeat string
	fields                  runtimeFields
}

type heartbeatPayload struct {
	Version       json.RawMessage `json:"version,omitempty"`
	ConfigVersion json.RawMessage `json:"config_version,omitempty"`
	ServiceStatus json.RawMessage `json:"service_status,omitempty"`
	SystemMetrics json.RawMessage `json:"system_metrics,omitempty"`
}

func reportedHeartbeat(payload *model.Agent) (heartbeatPayload, error) {
	var result heartbeatPayload
	if payload == nil {
		return result, nil
	}
	for _, item := range []struct {
		name   string
		target *json.RawMessage
	}{
		{"version", &result.Version}, {"config_version", &result.ConfigVersion},
		{"service_status", &result.ServiceStatus}, {"system_metrics", &result.SystemMetrics},
	} {
		raw, present, err := payload.MemberJSON(item.name)
		if err != nil {
			return result, err
		}
		if present {
			*item.target = raw
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
	fields, err := runtimeProjection(agent)
	if err != nil {
		return false, err
	}
	reported, err := reportedHeartbeat(payload)
	if err != nil {
		return false, err
	}
	return s.recordFields(ctx, heartbeatSource{agent.ID, agent.Name, agent.LastHeartbeat, fields}, reported, mark)
}

func (s *RuntimeState) recordFields(ctx context.Context, agent heartbeatSource, payload heartbeatPayload, mark int64) (bool, error) {
	// Decode a sample once, before the runtime lock. Components are matched by
	// exact name (Fields, not a case-folding struct) and owned by this call;
	// explicit nulls, invalid types and number spelling are retained.
	// Non-object samples remain visible as reported but have no components.
	var components [4]json.RawMessage
	if sample := bytes.TrimSpace(payload.SystemMetrics); len(sample) > 0 && sample[0] == '{' {
		var metrics model.Fields
		if err := json.Unmarshal(sample, &metrics); err == nil {
			for i, name := range metricFields {
				if raw, ok := metrics.Get(name); ok {
					components[i] = raw
				}
			}
		}
	}
	now := nowTimestamp()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.deleted[agent.id] > mark {
		return false, nil
	}
	fields := agent.fields
	if previous, ok := s.entries[agent.id]; ok && previous.HeartbeatAt > heartbeatTimestamp(agent.lastHeartbeat) {
		fields = previous.Fields
	}
	if payload.Version != nil {
		fields[0] = payload.Version
	}
	if payload.ServiceStatus != nil {
		fields[1] = payload.ServiceStatus
	}
	if payload.SystemMetrics != nil {
		fields[2] = payload.SystemMetrics
		for i, raw := range components {
			if raw != nil {
				fields[i+3] = raw
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.entries[agent.id] = heartbeatEntry{HeartbeatAt: now, LastHeartbeat: isoformat(fromTimestamp(now), false), Fields: fields}
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
func (s *RuntimeState) Delete(agentID string) { _ = s.deleteContext(context.Background(), agentID) }

func (s *RuntimeState) deleteContext(ctx context.Context, agentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	delete(s.entries, agentID)
	s.deletions++
	s.deleted[agentID] = s.deletions
	return nil
}

// runtimeDirectory uses the repository's shared physical-directory identity.
// Repositories refuse symlink loops; a direct diagnostic caller that supplies
// an unusable path still receives a stable absolute fallback key.
func runtimeDirectory(path string) string {
	if resolved, err := repo.ResolveDataDir(path); err == nil {
		return resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return filepath.Clean(path)
}

// RuntimeFor is for_data_dir(data_dir): the process-wide state of a data
// directory.
func RuntimeFor(dataDir string) *RuntimeState {
	key := runtimeDirectory(dataDir)
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
	fields        *model.Agent
}

// legacyRows is _legacy_rows(path): the decodable rows of the #369 table,
// nothing (and a warning) when the database cannot be read.
func legacyRows(ctx context.Context, path string) map[string]legacyRow {
	rows, err := readLegacyRowsContext(ctx, path)
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
func readLegacyRowsContext(ctx context.Context, path string) ([]legacyRow, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String() + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, fmt.Errorf("读取旧版 Agent 心跳状态库: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT agent_id, heartbeat_at, last_heartbeat, fields FROM agent_runtime")
	if err != nil {
		return nil, fmt.Errorf("读取旧版 Agent 心跳状态库: %w", err)
	}
	defer rows.Close()
	// fetchall(): every row is converted before any is used, so one TEXT value
	// that is not UTF-8 fails the whole read.
	var raw [][4]any
	columns := [4]string{"agent_id", "heartbeat_at", "last_heartbeat", "fields"}
	for rows.Next() {
		var row [4]any
		if err := rows.Scan(&row[0], &row[1], &row[2], &row[3]); err != nil {
			return nil, fmt.Errorf("读取旧版 Agent 心跳状态库: %w", err)
		}
		for i, value := range row {
			if text, ok := value.(string); ok && !utf8.ValidString(text) {
				return nil, fmt.Errorf("%w: %s", ErrLegacyTextEncoding, columns[i])
			}
		}
		raw = append(raw, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取旧版 Agent 心跳状态库: %w", err)
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
func legacyFields(raw any) (*model.Agent, bool) {
	var text string
	switch value := raw.(type) {
	case string:
		text = value
	case []byte:
		decoded, ok := DecodeJSONBytes(value)
		if !ok {
			return nil, false
		}
		text = decoded
	default:
		return nil, false
	}
	// The retired SQLite column allowed duplicate keys. Standard map decoding
	// retains their last values once during migration; the live Agent protocol
	// keeps its strict duplicate-free Fields boundary. Only runtime/version data
	// is consumed from this historical envelope, never ownership evidence.
	var members map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &members) != nil || members == nil {
		return nil, false
	}
	data, err := json.Marshal(members)
	if err != nil {
		return nil, false
	}
	var fields model.Agent
	if json.Unmarshal(data, &fields) != nil {
		return nil, false
	}
	return &fields, true
}

// ErrLegacyTextEncoding rejects non-UTF-8 SQLite TEXT without logging its data.
var ErrLegacyTextEncoding = errors.New(msgLegacyTextEncoding)

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
			runtimeLogger.Warningf("旧版 Agent 心跳状态文件 %s 删除失败：%s", filepath.Base(path), fmt.Errorf("unlink %q: %w", path, err))
		}
	}
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
func importLegacyDatabase(ctx context.Context, repository *repo.Repository, state *RuntimeState,
	persist func(context.Context, map[string]model.Fields) (map[string]bool, error)) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(repository.DataDir(), LegacyDatabase)
	key := runtimeDirectory(path)
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
		rows = legacyRows(ctx, path)
	}
	// Listed after reading, which may leave a journal behind.
	if err := removeLegacyFiles(ctx, legacyFiles(path)); err != nil {
		return err
	}
	if exists(path) {
		runtimeLogger.Warningf("旧版 %s 无法删除，未导入其中的 Agent 心跳状态", LegacyDatabase)
		return nil
	}
	agents := map[string]model.Agent{}
	if err := repository.ViewAgents(ctx, func(records []model.Agent) error {
		for i := range records {
			a := &records[i]
			if agentIDProven(a) {
				agents[a.ID] = *a
			}
		}
		return nil
	}); err != nil {
		return err
	}
	current := map[string]legacyRow{}
	var ids []string
	for id, row := range rows {
		if a, ok := agents[id]; ok && row.heartbeatAt > heartbeatTimestamp(a.LastHeartbeat) {
			current[id] = row
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	versions := map[string]model.Fields{}
	for _, id := range ids {
		row := current[id]
		raw, present, err := row.fields.MemberJSON("version")
		if _, ok := storedText(raw, present, err); ok {
			a := agents[id]
			old, exists, e := a.MemberJSON("version")
			if e != nil {
				return e
			}
			if !sameStoredJSON(old, exists, raw) {
				var patch model.Fields
				if err := patch.Set("version", raw); err != nil {
					return err
				}
				versions[id] = patch
			}
		}
	}
	for _, id := range ids {
		row := current[id]
		if err := state.seedModel(ctx, id, row.heartbeatAt, row.lastHeartbeat, row.fields); err != nil {
			return err
		}
	}
	if len(versions) > 0 {
		if _, err := persist(ctx, versions); err != nil {
			runtimeLogger.Warningf("旧版 Agent 心跳状态里的版本号写入配置失败，将在 Agent 下次心跳时写入：%s", err)
			versions = map[string]model.Fields{}
		}
	}
	runtimeLogger.Infof("Agent 心跳状态改为只保存在内存：已从旧版 %s 恢复 %d 个 Agent 的状态（%d 个版本号写入配置），并删除该文件",
		LegacyDatabase, len(current), len(versions))
	return nil
}

// RestoreHeartbeat imports a native saved heartbeat without replacing a newer
// live entry. It preserves cancellation at the shared runtime commit point.
func (s *RuntimeState) RestoreHeartbeat(ctx context.Context, id string, at float64, heartbeat string, fields *model.Agent) error {
	return s.seedModel(ctx, id, at, heartbeat, fields)
}
