package metrics

import (
	"context"
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
	// loaded marks the data directories whose saved history was read back
	// (load_history runs once per service start).
	loaded = map[string]bool{}
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
	loaded = map[string]bool{}
	historiesMu.Unlock()
	ResetFlushState()
}

// loadOnce is metrics_flush.load_history() at service start, once per data
// directory: init_agent_manager runs it (the Go server builds the manager
// at start, before it listens, where app.py loaded the history).
func loadOnce(ctx context.Context, m *agents.Manager) {
	dataDir := m.Repository().DataDir()
	historiesMu.Lock()
	done := loaded[dataDir]
	loaded[dataDir] = true
	historiesMu.Unlock()
	if !done {
		LoadHistory(ctx, m)
	}
}

func addMetrics(m *agents.Manager, agentID string, metrics any) error {
	_, err := ForManager(m).AddMetrics(agentID, metrics)
	return err
}

func deleteHistory(m *agents.Manager, agentID string) {
	ForManager(m).DeleteAgentHistory(agentID)
}

var lifecycleOnce sync.Once

// Wire is A04's entry in internal/server wirePackages: the registry hooks
// (heartbeat metrics, Agent deletion), loading the saved history when the
// Agent manager is initialized at start, the task-list row with "run now"
// and the interval edit, the flush loop, and the final save at shutdown.
func Wire() {
	agents.RegisterHooks(agents.Hooks{AddMetrics: addMetrics, DeleteMetricsHistory: deleteHistory})
	agents.OnManagerInit(loadOnce)
	tasks.SetRows(tasks.SlotMetrics, Rows)
	tasks.SetRunner(TaskKey, runNow)
	tasks.SetIntervalSetter(TaskKey, SetInterval)
	lifecycleOnce.Do(func() {
		lifecycle.Loop("metrics-history-flush", loop)
		// app.py's stop-signal handler saved with record_idle=True: a stop
		// always leaves a "shutdown" run in the task list.
		lifecycle.OnShutdown("metrics-history-flush", func(ctx context.Context) {
			scope := repo.WithScope(ctx)
			defer repo.ResetConfigContext(scope)
			FlushOnShutdown(scope, true)
		})
	})
}
