package metrics

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

var (
	historiesMu sync.Mutex
	// histories holds one History per repository data directory (Python:
	// one MetricsHistory per AgentManager).
	histories = map[string]*History{}
)

// For returns the History of a repository data directory
// (os.path.join(repository.data_dir, 'metrics')), creating it on first use.
func For(dataDir string) *History {
	historiesMu.Lock()
	defer historiesMu.Unlock()
	h := histories[dataDir]
	if h == nil {
		h = NewHistory(filepath.Join(dataDir, "metrics"), MaxPointsPerAgent)
		histories[dataDir] = h
	}
	return h
}

// ForManager is manager.metrics_history.
func ForManager(m *agents.Manager) *History { return For(m.Repository().DataDir()) }

// Reset forgets every History and the flush state (tests: a fresh process).
func Reset() {
	historiesMu.Lock()
	histories = map[string]*History{}
	historiesMu.Unlock()
	ResetFlushState()
}

// RegisterStartup registers history loading as a synchronous startup step.
// Main calls it in startup order; Wire's repeated registration retains its slot.
func RegisterStartup() {
	lifecycle.RegisterStartup(historyStartup())
}

func historyStartup() lifecycle.Startup {
	return lifecycle.Startup{Name: "metrics-load-history", Run: func(ctx context.Context) error {
		manager, err := agents.Get(ctx)
		if err != nil {
			return err
		}
		LoadHistory(ctx, manager) // LoadHistory owns and logs its ordinary failures.
		return nil
	}}
}

func addMetrics(m *agents.Manager, agentID string, metrics any) error {
	_, err := ForManager(m).AddMetrics(agentID, metrics)
	return err
}

func addMetricsJSON(ctx context.Context, m *agents.Manager, agentID string, sample json.RawMessage) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	dataDir := m.Repository().DataDir()
	historiesMu.Lock()
	history := histories[dataDir]
	if history == nil {
		var err error
		history, err = newHistoryContext(ctx, filepath.Join(dataDir, "metrics"), MaxPointsPerAgent)
		if err != nil {
			historiesMu.Unlock()
			return err
		}
		if err := ctx.Err(); err != nil {
			historiesMu.Unlock()
			return err
		}
		histories[dataDir] = history
	}
	historiesMu.Unlock()
	_, err := history.AddSample(ctx, agentID, sample)
	return err
}

func deleteHistory(m *agents.Manager, agentID string) {
	ForManager(m).DeleteAgentHistory(agentID)
}

// Wire is A04's entry in internal/server wirePackages: the registry hooks
// (heartbeat metrics, Agent deletion), synchronous saved-history Startup,
// the task-list row with "run now" and the interval edit, the flush loop,
// and the two phased saves at shutdown.
func Wire() {
	agents.RegisterHooks(agents.Hooks{AddMetrics: addMetrics, AddMetricsJSON: addMetricsJSON, DeleteMetricsHistory: deleteHistory})
	RegisterStartup()
	tasks.SetRows(tasks.SlotMetrics, Rows)
	tasks.SetRunner(TaskKey, runNow)
	tasks.SetIntervalSetter(TaskKey, SetInterval)
	lifecycle.RegisterPeriodic(flushPeriodic())
	lifecycle.RegisterShutdown(lifecycle.ShutdownHook{Name: "metrics-history-flush-first", Phase: lifecycle.PhaseStopSignal, Run: func(ctx context.Context) { shutdownFlush(ctx, true) }})
	lifecycle.RegisterShutdown(lifecycle.ShutdownHook{Name: "metrics-history-flush-last", Phase: lifecycle.PhaseAfterDrain, Run: func(ctx context.Context) { shutdownFlush(ctx, false) }})
}

// shutdownFlush runs one save in its own repository scope. The stop-signal
// phase records idle shutdowns; the after-drain phase records only new data or
// failure, matching the two Python signal/application exit saves.
func shutdownFlush(ctx context.Context, recordIdle bool) {
	scope := repo.WithScope(ctx)
	defer repo.ResetConfigContext(scope)
	FlushOnShutdown(scope, recordIdle)
}
