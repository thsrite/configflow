package metrics

import (
	"context"
	"encoding/json"
	"path/filepath"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/lifecycle"
	"configflow/internal/tasks"
)

// Service owns metrics and traffic history for a single data directory and
// implements agents.MetricsSink.
type Service struct {
	dataDir      string
	history      *History
	trafficStore *trafficstore.Service
}

// New constructs a metrics Service rooted at dataDir.
func New(dataDir string, ts *trafficstore.Service) *Service {
	h := NewHistory(filepath.Join(dataDir, "metrics"), MaxPointsPerAgent)
	historiesMu.Lock()
	histories[dataDir] = h
	historiesMu.Unlock()
	if ts == nil {
		ts = trafficstore.NewService(dataDir)
	}
	return &Service{
		dataDir:      dataDir,
		history:      h,
		trafficStore: ts,
	}
}

// History returns the in-memory metrics History owned by s.
func (s *Service) History() *History { return s.history }

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
func (s *Service) DeleteTrafficHistory(ctx context.Context, m *agents.Manager, agentID string) error {
	return deleteTrafficHistoryContext(ctx, m, agentID)
}

// TrafficStoreDeleter returns the NamedDeleter clearing an Agent's traffic store.
func (s *Service) TrafficStoreDeleter() agents.NamedDeleter {
	return agents.NamedDeleter{Name: "traffic_store", Delete: s.trafficStore.DeleteAgent}
}

// Startup returns the synchronous metrics history load step for m.
func (s *Service) Startup(m *agents.Manager) lifecycle.Startup {
	return lifecycle.Startup{Name: "metrics-load-history", Run: func(ctx context.Context) error {
		manager := m
		if manager == nil {
			var err error
			manager, err = agents.Get(ctx)
			if err != nil {
				return err
			}
		}
		LoadSavedHistory(ctx, manager)
		return nil
	}}
}

// Loop returns the periodic metrics flush loop.
func (s *Service) Loop() lifecycle.Periodic { return flushPeriodic() }

// ShutdownHooks returns the two phased metrics flush shutdown hooks.
func (s *Service) ShutdownHooks() []lifecycle.ShutdownHook {
	return []lifecycle.ShutdownHook{
		{Name: "metrics-history-flush-first", Phase: lifecycle.PhaseStopSignal, Run: func(ctx context.Context) { shutdownFlush(ctx, true) }},
		{Name: "metrics-history-flush-last", Phase: lifecycle.PhaseAfterDrain, Run: func(ctx context.Context) { shutdownFlush(ctx, false) }},
	}
}

// RegisterTasks installs the metrics task row builder, runner, and interval editor on tk.
func (s *Service) RegisterTasks(tk *tasks.Registry) {
	if tk == nil {
		tk = tasks.Default()
	}
	tk.SetRowBuilder(tasks.SlotMetrics, BuildRows)
	tk.SetRunner(TaskKey, runNow)
	tk.SetIntervalEditor(TaskKey, func(ctx context.Context, value *int64) error {
		if value == nil {
			return errInterval
		}
		return SetFlushInterval(ctx, *value)
	})
}
