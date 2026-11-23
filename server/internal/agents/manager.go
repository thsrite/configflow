package agents

// Port of backend/agents/manager.py (AgentManager): registration,
// re-registration with the existing token, heartbeats and the in-memory
// runtime state, Agent records in the system document, profile binding and
// the HTTP calls to Agents. The deployment bookkeeping is in deployments.go.

import (
	"context"
	"crypto/subtle"
	"errors"
	"sync"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
)

var logger = logx.Get("backend.agents.manager")

// HeartbeatFreshSeconds is HEARTBEAT_FRESH_SECONDS: an Agent heartbeats every
// 30–60 seconds; one silent for longer than this is offline.
const HeartbeatFreshSeconds = 120

// OxidnsHALocked is OXIDNS_HA_LOCKED.
const OxidnsHALocked = "DNS 高可用已开启，请先停用 DNS 高可用"

// StateRestoredMessage refuses changes to, and automatic pushes from or to, an
// Agent restored from a backup without its run records until it is
// published again (design/o17-runtime-state.md 2.5.1).
const StateRestoredMessage = "运行记录未包含在备份里，请先重新发布此 Agent"

// StateRestored reports an Agent restored from a backup without its run
// records: what its device runs is unknown until the next successful
// publication clears the mark.
func StateRestored(a *model.Agent) bool { return a != nil && a.StateRestoredAt != nil }

// DeploymentConflict is AgentDeploymentConflict (refused like a ValidationError): a binding or
// registration cannot change during an unfinished operation (409).
type DeploymentConflict struct{ Message string }

func (e *DeploymentConflict) Error() string { return e.Message }

// ErrUnauthorized refuses a re-registration without the existing Agent's
// token (401).
var ErrUnauthorized = errors.New("Unauthorized")

// ValidationError refuses a registry change or Agent request whose value is
// invalid (answered as a client error with Message).
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

var errNothingToSave = errors.New("nothing to save")

// deploymentResults is _DEPLOYMENT_RESULTS.
var deploymentResults = map[string]struct {
	level logx.Level
	text  string
}{
	"succeeded":       {logx.Info, "配置发布成功"},
	"failed":          {logx.Warning, "配置发布失败"},
	"rolled_back":     {logx.Warning, "配置发布未生效，已回滚到原配置"},
	"rollback_failed": {logx.Error, "配置发布失败，且回滚失败"},
}

// ConstantTimeASCIIEqual is _constant_time_ascii_equal (manager.py and
// routes/agents.py): hmac.compare_digest of the ASCII bytes; non-strings and
// non-ASCII text are simply unequal.
func ConstantTimeASCIIEqual(provided, expected any) bool {
	p, ok1 := provided.(string)
	e, ok2 := expected.(string)
	if !ok1 || !ok2 || !isASCII(p) || !isASCII(e) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(p), []byte(e)) == 1
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// FreshHeartbeat applies the registry freshness rule to a typed timestamp.
func FreshHeartbeat(text string, now *time.Time) bool {
	heartbeat, naive, ok := fromISO(text)
	if !ok {
		return false
	}
	var elapsed int64 // microseconds
	if naive {
		current := clock()
		if now != nil {
			current = *now
		}
		wall, _, _ := taskstatus.ParseISO(text) // the naive fields, read as UTC
		elapsed = wallMicros(current.In(localZone)) - wallMicros(wall)
	} else {
		if now != nil {
			return false // can't subtract offset-naive and offset-aware datetimes
		}
		elapsed = unixMicros(clock()) - unixMicros(heartbeat)
	}
	return elapsed <= HeartbeatFreshSeconds*1_000_000
}

// wallMicros is the wall clock of t (its own fields, whatever the zone) in
// microseconds, as naive datetime arithmetic sees it.
func wallMicros(t time.Time) int64 {
	return unixMicros(time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC))
}

func unixMicros(t time.Time) int64 { return t.Unix()*1_000_000 + int64(t.Nanosecond()/1000) }

// Manager is AgentManager: the registry of one repository.
type Manager struct {
	closeMu    sync.Mutex
	closed     bool
	closeHooks []func()

	repository *repo.Repository
	runtime    *RuntimeState
	client     Client

	// presence are the OnPresence observers (wake.go).
	presence presenceWatchers

	deploymentMu sync.Mutex
	preparations map[[2]string]*preparationLease

	// Optional test seams. A nil sleep uses the Runtime Clock and honours ctx.
	sleep              func(time.Duration)
	monotonic          func() float64
	preparationRunning func(agentID, deploymentID string) (bool, error)
}

// processStart is the origin of the Manager's monotonic seconds.
var processStart = time.Now()

// New is AgentManager(repository): it shares the runtime state of the data
// directory and imports a legacy runtime.sqlite3 once.
func New(ctx context.Context, repository *repo.Repository) (*Manager, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m := &Manager{
		repository:   repository,
		runtime:      RuntimeFor(repository.DataDir()),
		preparations: map[[2]string]*preparationLease{},
		monotonic:    func() float64 { return time.Since(processStart).Seconds() },
	}
	m.preparationRunning = m.defaultPreparationRunning
	if err := importLegacyDatabase(ctx, repository, m.runtime, m.saveReportedModels); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// Repository is the repository the manager works on.
func (m *Manager) Repository() *repo.Repository { return m.repository }

// Runtime is the in-memory heartbeat state.
func (m *Manager) Runtime() *RuntimeState { return m.runtime }

// SetClient makes this manager use client instead of DefaultClient().
func (m *Manager) SetClient(client Client) { m.client = client }

// Client is the client this manager sends Agent requests with (its own or
// DefaultClient()); for packages that build their own Agent URLs
// (oxidns_monitor's validated origin).
func (m *Manager) Client() Client { return m.httpClient() }

func (m *Manager) httpClient() Client {
	if m.client != nil {
		return m.client
	}
	return DefaultClient()
}

func (m *Manager) defaultProfileID(ctx context.Context) (string, error) {
	return m.repository.AgentDefaultProfileID(ctx)
}

var editableFields = []string{"name", "host", "port", "enabled", "service_type", "profile_id"}
var guardedFields = []string{"host", "port", "enabled", "service_type", "profile_id"}

// testFailUpdate makes editRegistry fail (tests; Python patched
// repository.update_system_transaction).
var testFailUpdate func() error

var testRecordHeartbeat func(ctx context.Context, m *Manager, source heartbeatSource, payload heartbeatPayload, mark int64) (bool, error)
