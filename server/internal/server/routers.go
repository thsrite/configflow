package server

import (
	"context"
	"encoding/json"

	"configflow/internal/agents"
	agentipv6 "configflow/internal/agents/ipv6"
	"configflow/internal/lifecycle"
	"configflow/internal/notifications"
	"configflow/internal/routers"
	rosipv6 "configflow/internal/routers/ipv6"
	routerrulesync "configflow/internal/routers/rulesync"
	"configflow/internal/routers/spec"
	"configflow/internal/rulesource"
	"configflow/internal/tasks"
)

type routerServices struct {
	routers  *routers.Service
	rulesync *routerrulesync.RuleSyncService
}

// wireRouters constructs the routers orchestration Service and router rule-sync
// Service with explicit collaborators (IPv6Hosts, Rules, ManualBaseline,
// Notifications), registers the router-rule-sync periodic loop on rt and the
// SlotRouters task row builder and runner on tk.
func wireRouters(opts Options, rt *lifecycle.Runtime, tk *tasks.Registry, nt *notifications.Service, ag *agentServices, rs *rulesource.Service) *routerServices {
	if rs == nil {
		rs = rulesource.Default()
	}
	ipv6Hosts := routerIPv6HostFetcher(ag)
	rulesLoader := func(ctx context.Context, profile json.RawMessage, item spec.RuleSet, library *spec.LibraryRule) (string, error) {
		var libRaw json.RawMessage
		if library != nil {
			libRaw = library.Raw
		}
		return rs.ReadOfflineJSON(ctx, profile, item.Raw, libRaw)
	}
	var agentSource routers.AgentSource
	if ag != nil && ag.manager != nil {
		agentSource = routers.ManagerAgentSource{Manager: ag.manager}
	}
	rr := routerrulesync.New(opts.Repo, routerrulesync.Options{
		Agents:    agentSource,
		IPv6Hosts: ipv6Hosts,
	})
	rtr := routers.New(opts.Repo, routers.Options{
		IPv6Hosts:      ipv6Hosts,
		Rules:          rulesLoader,
		ManualBaseline: rr.CaptureManual,
		Notifications:  nt,
	})
	routers.UseService(rtr)
	routerrulesync.UseService(rr)

	tk.SetRowBuilder(tasks.SlotRouters, rr.TaskRows)
	tk.SetRunner(tasks.SlotRouters, rr.TaskRunner())
	rt.RegisterPeriodic(rr.Loop())

	return &routerServices{
		routers:  rtr,
		rulesync: rr,
	}
}

func routerIPv6HostFetcher(ag *agentServices) routers.IPv6HostFetcher {
	if ag != nil && ag.ipv6 != nil {
		ip := ag.ipv6
		return func(ctx context.Context, agent *routers.GatewayAgent) (rosipv6.IPv6Host, error) {
			if agent == nil {
				return ip.Host(ctx, nil)
			}
			return ip.Host(ctx, agent.Agent)
		}
	}
	return func(ctx context.Context, agent *routers.GatewayAgent) (rosipv6.IPv6Host, error) {
		m, err := agents.Get(ctx)
		if err != nil {
			return rosipv6.IPv6Host{}, err
		}
		if agent == nil {
			return (&agentipv6.Service{Manager: m}).Host(ctx, nil)
		}
		return (&agentipv6.Service{Manager: m}).Host(ctx, agent.Agent)
	}
}
