package monitor

// Port of backend/agents/oxidns_dashboard_state.py: small, durable DNS
// dashboard baselines in DATA_DIR/dns-monitor/<sha256(agent id)>.json;
// never query records.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// MaxStateBytes, MaxRows, LockTimeout are MAX_STATE_BYTES, MAX_ROWS and
// LOCK_TIMEOUT.
const (
	MaxStateBytes = 4 * 1024 * 1024
	MaxRows       = 10_000
)

// LockTimeout is LOCK_TIMEOUT (a variable for tests).
var LockTimeout = 5 * time.Second

var reasons = map[string]bool{"initial": true, "manual": true, "core_changed": true, "configuration_changed": true,
	"target_changed": true, "counters_changed": true, "publication": true}

// stateFields are _FIELDS: per family the label keys and the number keys.
var stateFields = []struct {
	family  string
	labels  []string
	numbers map[string]bool
}{
	{"servers", []string{"tag", "protocol"}, map[string]bool{"requests": true, "failed": true, "inflight": true, "latency_sum_ms": true, "latency_count": true}},
	{"caches", []string{"tag"}, map[string]bool{"lookups": true, "fresh_hits": true, "stale_hits": true, "misses": true, "expired": true, "entries": true}},
	{"upstreams", []string{"tag", "upstream"}, map[string]bool{"requests": true, "failed": true, "latency_sum_ms": true, "latency_count": true}},
}

// StateError is DashboardStateError: the previous statistics boundary must
// not be silently discarded.
type StateError struct{ Message string }

func (e *StateError) Error() string { return e.Message }

// ErrStateConflict is DashboardStateConflict: a concurrent reset superseded
// this network snapshot.
var ErrStateConflict = errors.New("DashboardStateConflict")

var (
	hex32        = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex64        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	deploymentRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

const maxInteger = 1<<53 - 1

// DashboardState is a durable DNS statistics boundary; schema 2 adds publication carry-over.
type DashboardState struct {
	Schema           int64
	Generation       string
	StartedAtMS      int64
	Reason           string
	Target           string
	InstanceID       string
	SourceRevision   string
	RuntimeRevision  string
	Baseline         *Observations
	Carried          *Observations
	CarriedPartial   bool
	PublicationCount int64
	Pending          *PendingPublication
}

// PendingPublication pins a checkpoint to one deployment and target revision.
type PendingPublication struct {
	DeploymentID     string        `json:"deployment_id"`
	ExpectedRevision string        `json:"expected_revision"`
	CollectedAtMS    int64         `json:"collected_at_ms"`
	Totals           *Observations `json:"totals"`
	Partial          bool          `json:"partial"`
}
type stateBase struct {
	Schema          int64         `json:"schema"`
	Generation      string        `json:"generation"`
	StartedAtMS     int64         `json:"started_at_ms"`
	Reason          string        `json:"reason"`
	Target          string        `json:"target"`
	InstanceID      string        `json:"instance_id"`
	SourceRevision  string        `json:"source_revision"`
	RuntimeRevision string        `json:"runtime_revision"`
	Baseline        *Observations `json:"baseline"`
}

// MarshalJSON writes precisely the members of this schema version.
func (s DashboardState) MarshalJSON() ([]byte, error) {
	base := stateBase{s.Schema, s.Generation, s.StartedAtMS, s.Reason, s.Target, s.InstanceID, s.SourceRevision, s.RuntimeRevision, s.Baseline}
	if s.Schema != 2 {
		return json.Marshal(base)
	}
	return json.Marshal(struct {
		stateBase
		Carried          *Observations       `json:"carried"`
		CarriedPartial   bool                `json:"carried_partial"`
		PublicationCount int64               `json:"publication_count"`
		Pending          *PendingPublication `json:"pending"`
	}{base, s.Carried, s.CarriedPartial, s.PublicationCount, s.Pending})
}
func exactFields(fields model.Fields, keys ...string) bool {
	if fields.Len() != len(keys) {
		return false
	}
	for _, key := range keys {
		if !fields.Has(key) {
			return false
		}
	}
	return true
}
func stateInvalid() error { return &StateError{"Invalid DNS statistics state"} }
func stateMember(fields model.Fields, key string, target any) error {
	raw, present := fields.Get(key)
	if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return stateInvalid()
	}
	if err := jsondecode.Unmarshal(raw, target); err != nil {
		return stateInvalid()
	}
	return nil
}

// DecodeState validates exact keys, numeric token types and observation series
// before returning a typed boundary. Invalid state never resets counters silently.
func DecodeState(raw []byte) (*DashboardState, error) {
	var fields model.Fields
	if jsondecode.Unmarshal(raw, &fields) != nil {
		return nil, stateInvalid()
	}
	state := new(DashboardState)
	if stateMember(fields, "schema", &state.Schema) != nil || state.Schema != 1 && state.Schema != 2 {
		return nil, stateInvalid()
	}
	keys := []string{"schema", "generation", "started_at_ms", "reason", "target", "instance_id", "source_revision", "runtime_revision", "baseline"}
	if state.Schema == 2 {
		keys = append(keys, "carried", "carried_partial", "publication_count", "pending")
	}
	if !exactFields(fields, keys...) {
		return nil, stateInvalid()
	}
	for _, field := range []struct {
		name   string
		target any
	}{
		{"generation", &state.Generation}, {"started_at_ms", &state.StartedAtMS}, {"reason", &state.Reason}, {"target", &state.Target},
		{"instance_id", &state.InstanceID}, {"source_revision", &state.SourceRevision}, {"runtime_revision", &state.RuntimeRevision},
	} {
		if stateMember(fields, field.name, field.target) != nil {
			return nil, stateInvalid()
		}
	}
	if !hex32.MatchString(state.Generation) || !hex64.MatchString(state.Target) || !hex64.MatchString(state.SourceRevision) || !hex64.MatchString(state.RuntimeRevision) || !reasons[state.Reason] || state.StartedAtMS < 0 || state.StartedAtMS > maxInteger || runeLen(state.InstanceID) == 0 || runeLen(state.InstanceID) > 128 {
		return nil, stateInvalid()
	}
	baseline, _ := fields.Get("baseline")
	var err error
	state.Baseline, err = decodeObservations(baseline)
	if err != nil {
		return nil, stateInvalid()
	}
	if state.Schema == 1 {
		return state, nil
	}
	if stateMember(fields, "carried_partial", &state.CarriedPartial) != nil || stateMember(fields, "publication_count", &state.PublicationCount) != nil || state.PublicationCount < 0 || state.PublicationCount > maxInteger {
		return nil, stateInvalid()
	}
	carried, _ := fields.Get("carried")
	state.Carried, err = decodeObservations(carried)
	if err != nil {
		return nil, stateInvalid()
	}
	pending, _ := fields.Get("pending")
	if bytes.Equal(bytes.TrimSpace(pending), []byte("null")) {
		return state, nil
	}
	var checkpoint model.Fields
	if jsondecode.Unmarshal(pending, &checkpoint) != nil || !exactFields(checkpoint, "deployment_id", "expected_revision", "collected_at_ms", "totals", "partial") {
		return nil, stateInvalid()
	}
	p := new(PendingPublication)
	for _, field := range []struct {
		name   string
		target any
	}{{"deployment_id", &p.DeploymentID}, {"expected_revision", &p.ExpectedRevision}, {"collected_at_ms", &p.CollectedAtMS}, {"partial", &p.Partial}} {
		if stateMember(checkpoint, field.name, field.target) != nil {
			return nil, stateInvalid()
		}
	}
	if !deploymentRE.MatchString(p.DeploymentID) || !hex64.MatchString(p.ExpectedRevision) || p.CollectedAtMS < state.StartedAtMS || p.CollectedAtMS > 253402300799999 {
		return nil, stateInvalid()
	}
	totals, _ := checkpoint.Get("totals")
	p.Totals, err = decodeObservations(totals)
	if err != nil {
		return nil, stateInvalid()
	}
	state.Pending = p
	return state, nil
}

func decodeObservations(raw []byte) (*Observations, error) {
	var fields model.Fields
	if jsondecode.Unmarshal(raw, &fields) != nil || !exactFields(fields, "servers", "caches", "upstreams") {
		return nil, stateInvalid()
	}
	total := 0
	for _, family := range stateFields {
		raw, _ := fields.Get(family.family)
		var rows []json.RawMessage
		if jsondecode.Unmarshal(raw, &rows) != nil || rows == nil {
			return nil, stateInvalid()
		}
		total += len(rows)
		if total > MaxRows {
			return nil, stateInvalid()
		}
		seen := map[groupKey]bool{}
		for _, raw := range rows {
			var row model.Fields
			if jsondecode.Unmarshal(raw, &row) != nil {
				return nil, stateInvalid()
			}
			for key := range row.All() {
				if !contains(family.labels, key) && !family.numbers[key] {
					return nil, stateInvalid()
				}
			}
			var identity groupKey
			for i, key := range family.labels {
				text := fieldText(row, key)
				if runeLen(text) == 0 || runeLen(text) > 16384 {
					return nil, stateInvalid()
				}
				if i == 0 {
					identity.a = text
				} else {
					identity.b = text
				}
			}
			if seen[identity] {
				return nil, stateInvalid()
			}
			seen[identity] = true
			for key := range family.numbers {
				raw, present := row.Get(key)
				if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					continue
				}
				var n MetricNumber
				if jsondecode.Unmarshal(raw, &n) != nil || n.num.less(intNum(0)) {
					return nil, stateInvalid()
				}
				finite, err := n.isFinite()
				if err != nil || !finite {
					return nil, stateInvalid()
				}
			}
		}
	}
	var observations Observations
	if jsondecode.Unmarshal(raw, &observations) != nil {
		return nil, stateInvalid()
	}
	return &observations, nil
}

// StateStore is DashboardStateStore.
type StateStore struct {
	Directory string
}

// NewStateStore is DashboardStateStore(data_dir).
func NewStateStore(dataDir string) *StateStore {
	return &StateStore{Directory: filepath.Join(dataDir, "dns-monitor")}
}

// Path is DashboardStateStore.path(agent_id).
func (s *StateStore) Path(agentID string) string {
	digest := sha256.Sum256([]byte(agentID))
	return filepath.Join(s.Directory, hex.EncodeToString(digest[:])+".json")
}

// stateLocks serializes one physical state file across Store instances.
// Legacy Python .lock files remain untouched behind the instance boundary.
var stateLocks = lockx.NewLeafKeyed[string]("dashboard-state")

func (s *StateStore) locked(ctx context.Context, path string, body func() error) error {
	key, err := repo.ResolveDataDir(path)
	if err != nil {
		return &StateError{"Cannot access DNS statistics state"}
	}
	_, release, err := stateLocks.Lock(ctx, key, LockTimeout)
	if errors.Is(err, lockx.ErrTimeout) {
		return &StateError{"DNS statistics state is busy"}
	}
	if err != nil {
		return err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Directory, 0o700); err != nil {
		return &StateError{"Cannot access DNS statistics state"}
	}
	err = body()
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return &StateError{"Cannot access DNS statistics state"}
	}
	return err
}

// read is _read: nil when the file does not exist.
func (s *StateStore) readState(path string) (*DashboardState, error) {
	handle, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, &StateError{"Cannot read DNS statistics state"}
	}
	encoded, err := io.ReadAll(io.LimitReader(handle, MaxStateBytes+1))
	handle.Close()
	if err != nil {
		return nil, &StateError{"Cannot read DNS statistics state"}
	}
	damaged := &StateError{"DNS statistics state is damaged"}
	if len(encoded) > MaxStateBytes {
		return nil, damaged
	}
	text, ok := agents.DecodeJSONBytes(encoded)
	if !ok {
		return nil, damaged
	}
	state, err := DecodeState([]byte(text))
	if err != nil {
		return nil, damaged
	}
	return state, nil
}

// ReadState returns a validated, caller-owned dashboard boundary.
func (s *StateStore) ReadState(ctx context.Context, agentID string) (*DashboardState, error) {
	path := s.Path(agentID)
	var result *DashboardState
	err := s.locked(ctx, path, func() error {
		var err error
		result, err = s.readState(path)
		return err
	})
	return result, err
}

// CompareState is the typed generation compare-and-set: the generation is checked under the
// same leaf lock as the atomic replacement. Owners verify configuration under
// a repository read snapshot before entering this lock. A nil replacement only
// checks that a read is still current. expected ""
// is None.
func (s *StateStore) CompareState(ctx context.Context, agentID, expected string, replacement *DashboardState) (*DashboardState, error) {
	path := s.Path(agentID)
	var result *DashboardState
	err := s.locked(ctx, path, func() error {
		current, err := s.readState(path)
		if err != nil {
			return err
		}
		generation := ""
		if current != nil {
			generation = current.Generation
		}
		if generation != expected {
			return ErrStateConflict
		}
		if replacement == nil {
			result = current
			return nil
		}
		encoded, err := json.Marshal(replacement)
		if err != nil {
			return err
		}
		if _, err := DecodeState(encoded); err != nil {
			return &StateError{"Invalid DNS statistics state"}
		}
		if len(encoded) > MaxStateBytes {
			return &StateError{"DNS statistics state exceeds size limit"}
		}
		if err := writeStateAtomic(ctx, s.Directory, path, encoded); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &StateError{"Cannot save DNS statistics state"}
		}
		result = replacement
		return nil
	})
	return result, err
}

// replaceFile is os.replace (a variable so tests can make it fail).
var replaceFile = os.Rename

// writeAtomic is the mkstemp / fsync / os.replace / directory fsync of
// compare_and_set; the temporary file is removed when anything fails.
func writeStateAtomic(ctx context.Context, directory, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	handle, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	temporary := handle.Name()
	defer func() {
		if temporary != "" {
			os.Remove(temporary)
		}
	}()
	if _, err := handle.Write(data); err != nil {
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := replaceFile(temporary, path); err != nil {
		return err
	}
	temporary = ""
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
