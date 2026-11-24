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
	"configflow/internal/agents/trafficstore"
	"configflow/internal/agents/upgrades"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/deploy"
	"configflow/internal/health"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/tasks"
)

type agentServices struct {
	manager   *agents.Manager
	traffic   *trafficstore.Service
	metrics   *metrics.Service
	monitor   *monitor.Service
	upgrades  *upgrades.Service
	dnsha     *dnsha.Service
	dnsfollow *dnsfollow.Service
	rulesync  *rulesync.Service
	ipv6      *agentipv6.Service
}

// wireAgents constructs the Agent Manager and Agent-domain services (when
// opts.Repo is provided), wires their collaborators and D1 two-phase setters
// (PairPublisher on Manager, HARules on monitor.Service), connects
// notifications.Service (D6), registers the SlotAgents task row builder via
// agenttasks, and registers the Agent-domain loops on rt.
func wireAgents(ctx context.Context, opts Options, rt *lifecycle.Runtime, tk *tasks.Registry, nt *notifications.Service, gen *generateServices) (*agentServices, error) {
	apiagents.WireContentSyncWith(rt, gen.substore)

	dataDir := opts.Repo.DataDir()
	ts := trafficstore.NewService(dataDir)
	mt := metrics.New(metrics.Options{Repo: opts.Repo, TrafficStore: ts, Notifications: nt, Tasks: tk})
	mon := monitor.New(dataDir)

	deletedStores := append([]agents.NamedDeleter{mt.TrafficStoreDeleter()}, health.DeletedStores(dataDir)...)
	m, err := agents.New(ctx, opts.Repo, agents.Options{
		Metrics:        mt,
		Publication:    mon,
		ReceiptMatches: deploy.ReceiptMatches,
		DeletedStores:  deletedStores,
	})
	if err != nil {
		return nil, err
	}

	ha := dnsha.New(m, dnsha.Options{})
	if err := m.SetPairPublisher(ha); err != nil {
		m.Close()
		return nil, err
	}
	if err := mon.SetHARules(ha.PairRulesJSON); err != nil {
		m.Close()
		return nil, err
	}
	mon.MarkStarted()
	m.MarkStarted()

	if err := nt.SetAgents(m.NotificationAgents); err != nil {
		m.Close()
		return nil, err
	}

	up := upgrades.New(m)
	follow := dnsfollow.New(m, dnsfollow.Options{Notifications: nt})
	rsync := rulesync.New(m, nil, rulesync.Options{
		Monitor:       mon,
		Notifications: nt,
		Pair: rulesync.PairHooks{
			Advance:        func(ctx context.Context, id string) { _, _ = ha.Advance(ctx, id) },
			FailureMessage: ha.PairFailureMessage,
		},
	})

	gen.refresh.OnRulesRefreshed(rsync.WakeLoop)

	ip := agentipv6.NewService(m, apiagents.ContentChecker)

	gen.deploy = newDeployService(deploy.Options{
		Pair: func(ctx context.Context, _ *agents.Manager, id string, b *deploy.Bundle, o deploy.PublishOptions) (*model.Deployment, bool, error) {
			return ha.PublishPair(ctx, id, b, o)
		},
		IPv6:     ip.EnrollSource,
		SubStore: gen.substore,
		Provider: gen.provider,
		Refresh:  gen.refresh,
	})
	ha.Deploy = gen.deploy
	follow.Deploy = gen.deploy
	rsync.Deploy = gen.deploy

	rt.RegisterPeriodic(rsync.Loop())
	rt.RegisterPeriodic(follow.Loop())
	rt.RegisterPeriodic(ha.Loop())
	rt.RegisterPeriodic(mt.Loop())
	for _, hook := range mt.ShutdownHooks() {
		rt.RegisterShutdown(hook)
	}
	mt.RegisterTasks(tk)
	rt.RegisterPeriodic(mon.Loop(m))
	rt.RegisterPeriodic(ip.Loop())

	tk.SetRunner("dns_follow", follow.Runner())
	tk.SetRowBuilder(tasks.SlotAgents, agenttasks.BuildRows(
		func(context.Context) (*agents.Manager, error) { return m, nil },
		rsync.TaskRow,
		follow.TaskRow,
	))

	return &agentServices{
		manager:   m,
		traffic:   ts,
		metrics:   mt,
		monitor:   mon,
		upgrades:  up,
		dnsha:     ha,
		dnsfollow: follow,
		rulesync:  rsync,
		ipv6:      ip,
	}, nil
}
