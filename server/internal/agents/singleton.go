package agents

// Port of backend/common/agent_manager.py (the process-wide manager) and the
// wiring of the registry into the HTTP shell.

import (
	"context"
	"sync"

	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

var (
	singletonMu sync.Mutex
	singleton   *Manager
)

// Get is get_agent_manager(): the manager of the current repository (a new
// one when the repository changed).
func Get(ctx context.Context) (*Manager, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	singletonMu.Lock()
	var old *Manager
	if singleton == nil || singleton.repository != repository {
		manager, err := New(ctx, repository)
		if err != nil {
			singletonMu.Unlock()
			return nil, err
		}
		old = singleton
		singleton = manager
	}
	current := singleton
	singletonMu.Unlock()
	old.Close()
	return current, nil
}

// Init is init_agent_manager(): a fresh manager, then the OnManagerInit work
// (dns_follow, DNS HA publication and IPv6 verification loops of #28, #29,
// #31).
func Init(ctx context.Context) (*Manager, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	manager, err := New(ctx, repository)
	if err != nil {
		return nil, err
	}
	singletonMu.Lock()
	old := singleton
	singleton = manager
	singletonMu.Unlock()
	old.Close()
	for _, fn := range managerInitHooks() {
		fn(ctx, manager)
	}
	return manager, nil
}

// AgentRowFunc builds the /api/tasks row of one Agent for one category, nil
// when the Agent has none (statuses is the loaded task-status document).
type AgentRowFunc func(ctx context.Context, agent *model.Agent, statuses taskstatus.Snapshot) (*tasks.Task, error)

var (
	rowsMu       sync.Mutex
	ruleSyncRow  AgentRowFunc
	dnsFollowRow AgentRowFunc
)

// SetRuleSyncRow installs the agent_rule_sync row of routes/tasks._agent_rows
// (#27). SetDNSFollowRow installs its dns_follow row (#28). The "agent" task
// slot is provided once both are set: the registry itself owns no rows.
func SetRuleSyncRow(fn AgentRowFunc) { setAgentRow(&ruleSyncRow, fn) }

// SetDNSFollowRow: see SetRuleSyncRow.
func SetDNSFollowRow(fn AgentRowFunc) { setAgentRow(&dnsFollowRow, fn) }

func setAgentRow(slot *AgentRowFunc, fn AgentRowFunc) {
	rowsMu.Lock()
	*slot = fn
	complete := ruleSyncRow != nil && dnsFollowRow != nil
	rowsMu.Unlock()
	if complete {
		tasks.SetRowBuilder(tasks.SlotAgents, agentRows)
	}
}

// agentRows is routes/tasks._agent_rows(get_agent_manager(), statuses).
func agentRows(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	manager, err := Get(ctx)
	if err != nil {
		return nil, err
	}
	rowsMu.Lock()
	builders := []AgentRowFunc{ruleSyncRow, dnsFollowRow}
	rowsMu.Unlock()
	records, err := snapshotAgents(ctx, manager)
	if err != nil {
		return nil, err
	}
	// Credentials and system secrets are read once per pass, not per row.
	// Agent credentials come from the stored records with their histories,
	// as the rows collected them before O10; the read is cached per store head.
	agentSecrets, agentsErr := redact.AgentRecordCredentials(ctx, nil)
	sysSecrets, sysErr := SystemSecrets(ctx, manager.repository)
	rowCtx := withRowSecrets(ctx, agentSecrets, agentsErr, sysSecrets, sysErr)
	rows := []*tasks.Task{}
	for i := range records {
		agent := &records[i]
		for _, build := range builders {
			row, err := build(rowCtx, agent, statuses)
			if err != nil {
				return nil, err
			}
			if row != nil {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

// snapshotAgents returns the listing records (live heartbeat overlay and
// offline status included) without deployment/IPv6 histories, which neither
// task rows nor notifications read. Within one notification snapshot
// (tasks.WithSnapshotCache) the registry is read once and shared; callers
// must treat the shared records as read-only.
func snapshotAgents(ctx context.Context, manager *Manager) ([]model.Agent, error) {
	if cached, ok := tasks.CachedAgents(ctx); ok {
		return cached, nil
	}
	records, err := manager.ListAgents(ctx, false)
	if err != nil {
		return nil, err
	}
	tasks.CacheAgents(ctx, records)
	return records, nil
}

// Wire is the registry's entry in internal/server wirePackages: the
// InitAgentManager repository hook (load_config) and the notification list's
// Agent source. GET /api/notifications registers only once every task-row
// slot is provided as well (api/notifications.ListComplete), so the source
// is installed unconditionally.
func Wire() {
	repo.RegisterHooks(repo.Hooks{InitAgentManager: func() {
		if _, err := Init(context.Background()); err != nil {
			logger.Errorf("初始化 Agent 管理器失败：%s", err)
		}
	}})
	notifications.SetInputs(notifications.Inputs{Agents: NotificationAgents})
}

// NotificationAgents is the notification list's Agent source:
// get_agent_manager().get_all_agents(), without the deployment and IPv6
// verification histories no notification reads. One notification snapshot
// shares the read with the Agent task rows (see snapshotAgents).
func NotificationAgents(ctx context.Context) ([]model.Agent, error) {
	manager, err := Get(ctx)
	if err != nil {
		return nil, err
	}
	return snapshotAgents(ctx, manager)
}
