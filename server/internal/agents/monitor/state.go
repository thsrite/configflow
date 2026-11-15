package monitor

// Port of backend/agents/oxidns_dashboard_state.py: small, durable DNS
// dashboard baselines in DATA_DIR/dns-monitor/<sha256(agent id)>.json;
// never query records.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
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

// finite is _finite: None, or a non-negative finite int/float (not bool).
func finite(value any) bool {
	if value == nil {
		return true
	}
	n, ok := asNum(value)
	if !ok || n.isNaN() || n.less(intNum(0)) {
		return false
	}
	f, err := n.isFinite()
	return err == nil && f
}

var (
	hex32        = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex64        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	deploymentRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

func hasExactKeys(o *jsonx.Object, keys ...string) bool {
	if o.Len() != len(keys) {
		return false
	}
	for _, key := range keys {
		if !o.Has(key) {
			return false
		}
	}
	return true
}

func strMatches(value any, re *regexp.Regexp) bool {
	s, ok := value.(string)
	return ok && re.MatchString(s)
}

const maxInteger = 1<<53 - 1

// ValidateState is validate_state.
func ValidateState(value any) bool {
	state, ok := value.(*jsonx.Object)
	if !ok || state == nil {
		return false
	}
	keys := []string{"schema", "generation", "started_at_ms", "reason", "target", "instance_id",
		"source_revision", "runtime_revision", "baseline"}
	if pyNumberEqual(state.Value("schema"), jsonx.Int(2)) {
		keys = append(keys, "carried", "carried_partial", "publication_count", "pending")
	}
	if !hasExactKeys(state, keys...) {
		return false
	}
	if !pyIntIn(state.Value("schema"), 1, 2) {
		return false
	}
	if !strMatches(state.Value("generation"), hex32) || !strMatches(state.Value("target"), hex64) ||
		!strMatches(state.Value("source_revision"), hex64) || !strMatches(state.Value("runtime_revision"), hex64) {
		return false
	}
	reason, _ := state.Value("reason").(string)
	instance, isStr := state.Value("instance_id").(string)
	if !pyIntIn(state.Value("started_at_ms"), 0, maxInteger) || !reasons[reason] || !isStr ||
		runeLen(instance) == 0 || runeLen(instance) > 128 {
		return false
	}
	if !ValidateObservations(state.Value("baseline")) {
		return false
	}
	if pyInt64(state.Value("schema")) == 1 {
		return true
	}
	if _, ok := state.Value("carried_partial").(bool); !ok || !ValidateObservations(state.Value("carried")) ||
		!pyIntIn(state.Value("publication_count"), 0, maxInteger) {
		return false
	}
	pendingValue := state.Value("pending")
	if pendingValue == nil {
		return true
	}
	pending, ok := pendingValue.(*jsonx.Object)
	if !ok || !hasExactKeys(pending, "deployment_id", "expected_revision", "collected_at_ms", "totals", "partial") {
		return false
	}
	if !strMatches(pending.Value("deployment_id"), deploymentRE) || !strMatches(pending.Value("expected_revision"), hex64) {
		return false
	}
	if !bigIntBetween(pending.Value("collected_at_ms"), state.Value("started_at_ms"), 253402300799999) {
		return false
	}
	_, isBool := pending.Value("partial").(bool)
	return isBool && ValidateObservations(pending.Value("totals"))
}

// bigIntBetween is type(v) is int and lo <= v <= hi (lo an int).
func bigIntBetween(v, lo any, hi int64) bool {
	n, ok := v.(jsonx.Number)
	if !ok || !n.IsInt() {
		return false
	}
	i, ok := n.BigInt()
	l, ok2 := lo.(jsonx.Number).BigInt()
	return ok && ok2 && i.Cmp(l) >= 0 && i.Cmp(big.NewInt(hi)) <= 0
}

// ValidateObservations is validate_observations.
func ValidateObservations(value any) bool {
	baseline, ok := value.(*jsonx.Object)
	if !ok || baseline == nil || !hasExactKeys(baseline, "servers", "caches", "upstreams") {
		return false
	}
	total := 0
	for _, f := range stateFields {
		rows, ok := baseline.Value(f.family).([]any)
		if !ok {
			return false
		}
		total += len(rows)
		if total > MaxRows {
			return false
		}
		seen := map[string]bool{}
		for _, item := range rows {
			row, ok := item.(*jsonx.Object)
			if !ok || row == nil {
				return false
			}
			for _, key := range row.Keys() {
				isLabel := false
				for _, label := range f.labels {
					isLabel = isLabel || key == label
				}
				if !isLabel && !f.numbers[key] {
					return false
				}
			}
			for _, key := range f.labels {
				s, ok := row.Value(key).(string)
				if !ok || runeLen(s) == 0 || runeLen(s) > 16_384 {
					return false
				}
			}
			id := identity(row, f.labels)
			if seen[id] {
				return false
			}
			for key := range f.numbers {
				if !finite(row.Value(key)) {
					return false
				}
			}
			seen[id] = true
		}
	}
	return true
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

// locks are _LOCKS: a fixed set of in-process locks (here semaphores so the
// wait can time out like RLock.acquire(timeout=...)).
var locks = func() []chan struct{} {
	out := make([]chan struct{}, 64)
	for i := range out {
		out[i] = make(chan struct{}, 1)
	}
	return out
}()

func lockFor(path string) chan struct{} {
	if abs, err := filepath.Abs(path); err == nil {
		if resolved, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
			abs = filepath.Join(resolved, filepath.Base(abs))
		}
		path = abs
	}
	digest := sha256.Sum256([]byte(path))
	n := new(big.Int).SetBytes(digest[:])
	return locks[new(big.Int).Mod(n, big.NewInt(int64(len(locks)))).Int64()]
}

// locked is _locked: the in-process lock, then flock on <digest>.lock,
// both waiting at most LockTimeout. OSErrors become "Cannot access DNS
// statistics state", like the except OSError around the yield.
func (s *StateStore) locked(path string, body func() error) (err error) {
	lock := lockFor(path)
	select {
	case lock <- struct{}{}:
	case <-time.After(LockTimeout):
		return &StateError{"DNS statistics state is busy"}
	}
	defer func() { <-lock }()
	if err := os.MkdirAll(s.Directory, 0o700); err != nil {
		return &StateError{"Cannot access DNS statistics state"}
	}
	lockPath := path[:len(path)-len(filepath.Ext(path))] + ".lock"
	handle, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return &StateError{"Cannot access DNS statistics state"}
	}
	defer handle.Close()
	deadline := time.Now().Add(LockTimeout)
	for {
		err := syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return &StateError{"Cannot access DNS statistics state"}
		}
		if !time.Now().Before(deadline) {
			return &StateError{"DNS statistics state is busy"}
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer syscall.Flock(int(handle.Fd()), syscall.LOCK_UN)
	err = body()
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return &StateError{"Cannot access DNS statistics state"}
	}
	return err
}

// read is _read: nil when the file does not exist.
func (s *StateStore) read(path string) (*jsonx.Object, error) {
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
	value, err := jsonx.ParseString(text)
	if err != nil || exceedsDigitLimit(value) || !ValidateState(value) {
		return nil, damaged
	}
	return value.(*jsonx.Object), nil
}

// Read is DashboardStateStore.read(agent_id).
func (s *StateStore) Read(agentID string) (*jsonx.Object, error) {
	path := s.Path(agentID)
	var result *jsonx.Object
	err := s.locked(path, func() error {
		var err error
		result, err = s.read(path)
		return err
	})
	return result, err
}

// CompareAndSet is compare_and_set: the generation is checked under the
// same lock as the atomic replacement; verify (no network) runs in between;
// a nil replacement only checks that a read is still current. expected ""
// is None.
func (s *StateStore) CompareAndSet(agentID, expected string, replacement *jsonx.Object, verify func() error) (*jsonx.Object, error) {
	path := s.Path(agentID)
	var result *jsonx.Object
	err := s.locked(path, func() error {
		current, err := s.read(path)
		if err != nil {
			return err
		}
		generation := ""
		if current != nil {
			generation, _ = current.Value("generation").(string)
		}
		if generation != expected {
			return ErrStateConflict
		}
		if verify != nil {
			if err := verify(); err != nil {
				return err
			}
		}
		if replacement == nil {
			result = current
			return nil
		}
		if !ValidateState(replacement) {
			return &StateError{"Invalid DNS statistics state"}
		}
		encoded, err := jsonx.Compact(replacement)
		if err != nil {
			return err
		}
		if len(encoded) > MaxStateBytes {
			return &StateError{"DNS statistics state exceeds size limit"}
		}
		if err := writeAtomic(s.Directory, path, encoded); err != nil {
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
func writeAtomic(directory, path string, data []byte) error {
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

// ExceedsDigitLimit is exceedsDigitLimit for the routes.
func ExceedsDigitLimit(value any) bool { return exceedsDigitLimit(value) }

// exceedsDigitLimit reports an int literal longer than 4300 digits, which
// json.loads refuses with ValueError.
func exceedsDigitLimit(value any) bool {
	switch v := value.(type) {
	case jsonx.Number:
		if v.IsInt() {
			text := v.String()
			if len(text) > 0 && text[0] == '-' {
				text = text[1:]
			}
			return len(text) > maxStrDigits
		}
	case []any:
		for _, item := range v {
			if exceedsDigitLimit(item) {
				return true
			}
		}
	case *jsonx.Object:
		for _, item := range v.Values() {
			if exceedsDigitLimit(item) {
				return true
			}
		}
	}
	return false
}
