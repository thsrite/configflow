package monitor

import (
	"context"
	"encoding/json"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func legacyAgent(a *model.Agent) *jsonx.Object {
	if a == nil {
		return nil
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return nil
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil
	}
	return value.(*jsonx.Object)
}
func typedAgent(a *jsonx.Object) (*model.Agent, error) {
	if a == nil {
		return nil, nil
	}
	out := new(model.Agent)
	return out, metricJSON(a, out)
}
func slotRanking(ctx context.Context, m Manager, a *jsonx.Object, recorder string, since, until int64) (*Slot, error) {
	record, err := typedAgent(a)
	if err != nil {
		return nil, err
	}
	return readSlot(ctx, registryFor(m), record, recorder, since, until)
}

// CollectAgent is the legacy collection codec.
func CollectAgent(ctx context.Context, m Manager, store *RankingStore, a *jsonx.Object, recorders []string, now *int64, budget, retention int, pause time.Duration) (int, error) {
	record, err := typedAgent(a)
	if err != nil {
		return 0, err
	}
	return CollectAgentHistory(ctx, registryFor(m), store, record, recorders, now, budget, retention, pause)
}

// RoundManager is what collect_round needs of the Agent manager.
type RoundManager interface {
	Manager
	GetAllAgents(ctx context.Context, history bool) ([]*jsonx.Object, error)
}

// RoundManagerFromAgents adapts the registry's Manager for CollectRound.
func RoundManagerFromAgents(m *agents.Manager) RoundManager { return agentsManager{m} }

// Seams of collect_round for tests (the Python tests patch _recorders,
// collect_agent and manager.is_usable).
var (
	rankingRecorders = func(ctx context.Context, m Manager, agent *jsonx.Object) ([]string, error) {
		snap, err := overviewSnapshot(ctx, m, agent, true)
		if err != nil {
			return nil, err
		}
		out := make([]string, len(snap.recorders))
		for i, r := range snap.recorders {
			out[i] = r.(string)
		}
		return out, nil
	}
	rankingCollectAgent = func(ctx context.Context, m Manager, store *RankingStore, agent *jsonx.Object, recorders []string, now *int64) error {
		_, err := CollectAgent(ctx, m, store, agent, recorders, now, RankingSlotsPerRound, RankingCoreRetentionDays,
			200*time.Millisecond)
		return err
	}
	rankingIsUsable = func(agent *jsonx.Object) bool { return agents.IsUsable(agent, nil) }
)

type legacyRoundRegistry struct {
	legacyRegistry
	RoundManager
}

func (m legacyRoundRegistry) WithReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	return m.RoundManager.WithReadSnapshot(ctx, fn)
}
func (m legacyRoundRegistry) DataDir() string       { return m.RoundManager.DataDir() }
func (m legacyRoundRegistry) Client() agents.Client { return m.RoundManager.Client() }
func (m legacyRoundRegistry) ReadAgents(ctx context.Context, history bool, inspect func(*model.Agent)) ([]model.Agent, error) {
	list, err := m.GetAllAgents(ctx, history)
	if err != nil {
		return nil, err
	}
	out := make([]model.Agent, 0, len(list))
	for _, raw := range list {
		a, err := typedAgent(raw)
		if err != nil {
			return nil, err
		}
		if inspect != nil {
			inspect(a)
		}
		out = append(out, *a)
	}
	return out, nil
}
func (m legacyRoundRegistry) historyUsable(a *model.Agent) bool {
	return rankingIsUsable(legacyAgent(a))
}
func (m legacyRoundRegistry) historyRecorders(ctx context.Context, a *model.Agent) ([]string, error) {
	return rankingRecorders(ctx, m.RoundManager, legacyAgent(a))
}
func (m legacyRoundRegistry) historyCollect(ctx context.Context, store *RankingStore, a *model.Agent, recorders []string, now *int64) error {
	return rankingCollectAgent(ctx, m.RoundManager, store, legacyAgent(a), recorders, now)
}

// CollectRound is the legacy collector entry point.
func CollectRound(ctx context.Context, m RoundManager, store *RankingStore, now *int64) error {
	if native, ok := m.(RoundRegistry); ok {
		return CollectHistoryRound(ctx, native, store, now)
	}
	return CollectHistoryRound(ctx, legacyRoundRegistry{legacyRegistry{m}, m}, store, now)
}

// HistorySupported is supported(agent).
func HistorySupported(agent *jsonx.Object) bool { return versionAtLeast(agent, HistoryAgentVersion) }
