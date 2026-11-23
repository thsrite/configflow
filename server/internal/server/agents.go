package server

import (
	"context"

	"configflow/internal/agents"
	"configflow/internal/agents/agenttasks"
	"configflow/internal/agents/dnsfollow"
	"configflow/internal/agents/dnsha"
	agentipv6 "configflow/internal/agents/ipv6"
	"configflow/internal/agents/metrics"
	"configflow/internal/agents/monitor"
	"configflow/internal/agents/rulesync"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/deploy"
	"configflow/internal/health"
	"configflow/internal/lifecycle"
	"configflow/internal/notifications"
	"configflow/internal/routers"
	"configflow/internal/tasks"
)

// wireAgents constructs the Agent Manager (when opts.Repo is provided),
// wires its collaborators and D1 two-phase PairPublisher, connects
// notifications.Service (D6), registers the SlotAgents task row builder via
// agenttasks, and registers the Agent-domain loops on rt.
func wireAgents(ctx context.Context, opts Options, rt *lifecycle.Runtime, tk *tasks.Registry, nt *notifications.Service) (*agents.Manager, error) {
	deploy.Wire()
	rulesync.WireOn(rt)
	apiagents.WireContentSyncOn(rt)
	dnsfollow.RegisterOn(rt)
	dnsha.RegisterOn(rt)
	metrics.WireOn(rt)
	metrics.WireTraffic()
	monitor.WireOn(rt)
	health.Wire()
	wireIPv6On(rt)

	var m *agents.Manager
	getManager := agents.Get
	if opts.Repo != nil {
		deletedStores := append([]agents.NamedDeleter{metrics.TrafficStoreDeleter()}, health.DeletedStores()...)
		var err error
		m, err = agents.New(ctx, opts.Repo, agents.Options{
			Metrics:        metrics.MetricsSink(),
			Publication:    monitor.PublicationObserver(),
			ReceiptMatches: deploy.ReceiptMatches,
			DeletedStores:  deletedStores,
		})
		if err != nil {
			return nil, err
		}
		ha := dnsha.ForManager(m)
		if err := m.SetPairPublisher(ha); err != nil {
			m.Close()
			return nil, err
		}
		m.MarkStarted()
		agents.UseManager(m)
		if err := nt.SetAgents(m.NotificationAgents); err != nil {
			m.Close()
			return nil, err
		}
		getManager = func(context.Context) (*agents.Manager, error) { return m, nil }
	} else {
		if err := nt.SetAgents(agents.NotificationAgents); err != nil {
			return nil, err
		}
	}

	tk.SetRowBuilder(tasks.SlotAgents, agenttasks.BuildRows(getManager, rulesync.TaskRow, dnsfollow.TaskRow))
	return m, nil
}

func wireIPv6On(rt *lifecycle.Runtime) {
	agentipv6.WireOn(rt, apiagents.ContentChecker)
	routers.IPv6HostRequest = func(ctx context.Context, agent *routers.GatewayAgent) (routers.IPv6Host, error) {
		m, err := agents.Get(ctx)
		if err != nil {
			return routers.IPv6Host{}, err
		}
		if agent == nil {
			return (&agentipv6.Service{Manager: m}).Host(ctx, nil)
		}
		return (&agentipv6.Service{Manager: m}).Host(ctx, agent.Agent)
	}
}
