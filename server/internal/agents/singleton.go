package agents

// Port of backend/common/agent_manager.py (the process-wide manager) and the
// wiring of the registry into the HTTP shell.

import (
	"context"
	"sync"

	"configflow/internal/model"
	"configflow/internal/notifications"
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

// RunInitHooks invokes registered OnManagerInit callbacks on m (transitional
// helper until W6 constructs dnsfollow/dnsha/ipv6 directly in server).
func RunInitHooks(ctx context.Context, m *Manager) {
	for _, fn := range managerInitHooks() {
		fn(ctx, m)
	}
}

// SnapshotAgents returns the listing records (live heartbeat overlay and
// offline status included) without deployment/IPv6 histories, which neither
// task rows nor notifications read. Within one notification snapshot
// (tasks.WithSnapshotCache) the registry is read once and shared; callers
// must treat the shared records as read-only.
func SnapshotAgents(ctx context.Context, manager *Manager) ([]model.Agent, error) {
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

// Wire is the registry's entry in the legacy NewApp wiring: the notification
// list's Agent source (server.New passes Manager.NotificationAgents instead).
func Wire() {
	notifications.SetInputs(notifications.Inputs{Agents: NotificationAgents})
}

// NotificationAgents is the notification list's Agent source:
// get_agent_manager().get_all_agents(), without the deployment and IPv6
// verification histories no notification reads. One notification snapshot
// shares the read with the Agent task rows (see SnapshotAgents).
func NotificationAgents(ctx context.Context) ([]model.Agent, error) {
	manager, err := Get(ctx)
	if err != nil {
		return nil, err
	}
	return SnapshotAgents(ctx, manager)
}
