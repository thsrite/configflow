package metrics

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

// Options configures a metrics Service instance.
type Options struct {
	Repo          *repo.Repository
	DataDir       string
	TrafficStore  *trafficstore.Service
	Notifications *notifications.Service
	Tasks         *tasks.Registry
}

// Service owns metrics and traffic history for a single server instance and
// implements agents.MetricsSink.
type Service struct {
	repo          *repo.Repository
	dataDir       string
	history       *History
	traffic       *TrafficHistory
	trafficStore  *trafficstore.Service
	notifications *notifications.Service
	tasks         *tasks.Registry

	// runMu is the L2 save operation, spanning history I/O and its receipt.
	runMu   *lockx.Mutex
	running atomic.Bool
	// wake coalesces interval edits without launching overlapping rounds.
	wake lifecycle.Signal

	stateMu sync.Mutex
	// startedAt is _started_at: when the loop started (nil before).
	startedAt *time.Time
	// attemptedAt is _attempted_at: when this service last started a save,
	// so that a run whose record could not be written does not rerun every tick.
	attemptedAt *time.Time
	// stopping is _stopping: the service is stopping.
	stopping bool

	// Test seams (default to production implementations).
	utcNow       func() time.Time
	recordStatus func(context.Context, string, taskstatus.Result) error
}

// New constructs a metrics Service rooted at opts.Repo (or opts.DataDir).
func New(opts Options) *Service {
	dataDir := opts.DataDir
	if dataDir == "" && opts.Repo != nil {
		dataDir = opts.Repo.DataDir()
	}
	metricsDir := filepath.Join(dataDir, "metrics")
	ts := opts.TrafficStore
	if ts == nil {
		ts = trafficstore.NewService(dataDir)
	}
	s := &Service{
		repo:          opts.Repo,
		dataDir:       dataDir,
		history:       NewHistory(metricsDir, MaxPointsPerAgent),
		traffic:       NewTrafficHistory(metricsDir),
		trafficStore:  ts,
		notifications: opts.Notifications,
		tasks:         opts.Tasks,
		runMu:         lockx.NewMutex(lockx.LevelDevice, "metrics-history-save"),
	}
	return s
}

// History returns the in-memory metrics History owned by s.
func (s *Service) History() *History { return s.history }

// Traffic returns the in-memory TrafficHistory owned by s.
func (s *Service) Traffic() *TrafficHistory { return s.traffic }

// AddMetricsJSON records one heartbeat metrics sample.
func (s *Service) AddMetricsJSON(ctx context.Context, _ *agents.Manager, agentID string, sample json.RawMessage) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.history.AddSample(ctx, agentID, sample)
	return err
}

// DeleteMetricsHistory deletes an Agent's metrics history.
func (s *Service) DeleteMetricsHistory(ctx context.Context, _ *agents.Manager, agentID string) error {
	ctx = lifecycle.Detach(ctx)
	_, err := s.history.DeleteAgentHistoryContext(ctx, agentID)
	return err
}

// DeleteTrafficHistory deletes an Agent's traffic history.
func (s *Service) DeleteTrafficHistory(ctx context.Context, _ *agents.Manager, agentID string) error {
	return s.traffic.DeleteContext(ctx, agentID)
}

// TrafficStoreDeleter returns the NamedDeleter clearing an Agent's traffic store.
func (s *Service) TrafficStoreDeleter() agents.NamedDeleter {
	return agents.NamedDeleter{Name: "traffic_store", Delete: s.trafficStore.DeleteAgent}
}

func (s *Service) flushTraffic(ctx context.Context, agentIDs []string, system *model.System) (int64, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if _, err := s.traffic.RetainAgentsContext(ctx, agentIDs); err != nil {
		return 0, err
	}
	retention, err := TrafficRetention(system)
	if err != nil {
		return 0, err
	}
	result, err := s.traffic.FlushContext(ctx, retention, nil)
	if err != nil {
		return 0, err
	}
	return result.Rows, nil
}

// Startup returns the synchronous metrics history load step.
func (s *Service) Startup() lifecycle.Startup {
	return lifecycle.Startup{Name: "metrics-load-history", Run: func(ctx context.Context) error {
		s.LoadSavedHistory(ctx)
		return nil
	}}
}

// Loop returns the periodic metrics flush loop.
func (s *Service) Loop() lifecycle.Periodic { return s.flushPeriodic() }

// ShutdownHooks returns the two phased metrics flush shutdown hooks.
func (s *Service) ShutdownHooks() []lifecycle.ShutdownHook {
	return []lifecycle.ShutdownHook{
		{Name: "metrics-history-flush-first", Phase: lifecycle.PhaseStopSignal, Run: func(ctx context.Context) { s.shutdownFlush(ctx, true) }},
		{Name: "metrics-history-flush-last", Phase: lifecycle.PhaseAfterDrain, Run: func(ctx context.Context) { s.shutdownFlush(ctx, false) }},
	}
}

func (s *Service) shutdownFlush(ctx context.Context, recordIdle bool) {
	scope := repo.WithScope(ctx)
	defer repo.ResetConfigContext(scope)
	s.SaveOnShutdown(scope, recordIdle)
}

// RegisterTasks installs the metrics task row builder, runner, and interval editor on tk.
func (s *Service) RegisterTasks(tk *tasks.Registry) {
	if tk == nil {
		return
	}
	s.tasks = tk
	tk.SetRowBuilder(tasks.SlotMetrics, s.BuildRows)
	tk.SetRunner(TaskKey, s.runNow)
	tk.SetIntervalEditor(TaskKey, func(ctx context.Context, value *int64) error {
		if value == nil {
			return errInterval
		}
		return s.SetFlushInterval(ctx, *value)
	})
}
