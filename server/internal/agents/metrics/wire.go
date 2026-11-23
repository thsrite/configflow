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

// RegisterStartup registers history loading on the default runtime.
func RegisterStartup() { RegisterStartupOn(lifecycle.Current()) }

// RegisterStartupOn registers history loading as a synchronous startup step on rt.
func RegisterStartupOn(rt *lifecycle.Runtime) {
	if rt == nil {
		rt = lifecycle.Current()
	}
	rt.RegisterStartup(historyStartup())
}

func historyStartup() lifecycle.Startup {
	return lifecycle.Startup{Name: "metrics-load-history", Run: func(ctx context.Context) error {
		manager, err := agents.Get(ctx)
		if err != nil {
			return err
		}
		LoadSavedHistory(ctx, manager) // LoadHistory owns and logs its ordinary failures.
		return nil
	}}
}

func addMetricsJSON(ctx context.Context, m *agents.Manager, agentID string, sample json.RawMessage) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	history, err := historyContext(ctx, m.Repository().DataDir())
	if err != nil {
		return err
	}
	_, err = history.AddSample(ctx, agentID, sample)
	return err
}
func historyContext(ctx context.Context, dataDir string) (*History, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	historiesMu.Lock()
	history := histories[dataDir]
	if history == nil {
		var err error
		history, err = newHistoryContext(ctx, filepath.Join(dataDir, "metrics"), MaxPointsPerAgent)
		if err != nil {
			historiesMu.Unlock()
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			historiesMu.Unlock()
			return nil, err
		}
		histories[dataDir] = history
	}
	historiesMu.Unlock()
	return history, nil
}

func deleteHistoryContext(ctx context.Context, m *agents.Manager, id string) error {
	ctx = lifecycle.Detach(ctx)
	history, err := historyContext(ctx, m.Repository().DataDir())
	if err != nil {
		return err
	}
	_, err = history.DeleteAgentHistoryContext(ctx, id)
	return err
}

// Wire installs metrics hooks, task row/runner, and lifecycle loops on the default runtime.
func Wire() { WireOn(lifecycle.Current()) }

// WireOn installs metrics hooks, task row/runner, and lifecycle loops on rt.
func WireOn(rt *lifecycle.Runtime) {
	if rt == nil {
		rt = lifecycle.Current()
	}
	agents.RegisterHooks(agents.Hooks{AddMetricsJSON: addMetricsJSON, DeleteMetricsHistoryContext: deleteHistoryContext})
	tasks.SetRowBuilder(tasks.SlotMetrics, BuildRows)
	tasks.SetRunner(TaskKey, runNow)
	tasks.SetIntervalEditor(TaskKey, func(ctx context.Context, value *int64) error {
		if value == nil {
			return errInterval
		}
		return SetFlushInterval(ctx, *value)
	})
	rt.RegisterPeriodic(flushPeriodic())
	rt.RegisterShutdown(lifecycle.ShutdownHook{Name: "metrics-history-flush-first", Phase: lifecycle.PhaseStopSignal, Run: func(ctx context.Context) { shutdownFlush(ctx, true) }})
	rt.RegisterShutdown(lifecycle.ShutdownHook{Name: "metrics-history-flush-last", Phase: lifecycle.PhaseAfterDrain, Run: func(ctx context.Context) { shutdownFlush(ctx, false) }})
}

// shutdownFlush runs one save in its own repository scope. The stop-signal
// phase records idle shutdowns; the after-drain phase records only new data or
// failure, matching the two Python signal/application exit saves.
func shutdownFlush(ctx context.Context, recordIdle bool) {
	scope := repo.WithScope(ctx)
	defer repo.ResetConfigContext(scope)
	SaveOnShutdown(scope, recordIdle)
}
