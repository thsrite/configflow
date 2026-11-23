package server

import (
	"context"

	"configflow/internal/agents"
	"configflow/internal/agents/dnsfollow"
	"configflow/internal/agents/dnsha"
	agentipv6 "configflow/internal/agents/ipv6"
	"configflow/internal/agents/metrics"
	"configflow/internal/agents/monitor"
	"configflow/internal/agents/rulesync"
	"configflow/internal/aggregations"
	apiagents "configflow/internal/api/agents"
	apirules "configflow/internal/api/rules"
	"configflow/internal/auth"
	"configflow/internal/configexport"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/discovery"
	"configflow/internal/gateway"
	"configflow/internal/health"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/substore"
	"configflow/internal/webdav"
)

// Options configures server construction. Zero-value fields fall back to the
// process defaults during the incremental W1-W11 wiring migration.
type Options struct {
	Repo    *repo.Repository
	Runtime *lifecycle.Runtime
}

// App owns the wired HTTP application and its core runtime dependencies.
type App struct {
	HTTP    *httpx.App
	Repo    *repo.Repository
	Runtime *lifecycle.Runtime
}

// New wires the application, registers ordered startup steps and background
// loops on the Runtime, mounts every route package, and installs the
// authentication gate.
func New(ctx context.Context, opts Options) (*App, error) {
	rt := opts.Runtime
	if rt == nil {
		rt = lifecycle.Current()
	} else if err := lifecycle.UseRuntime(rt); err != nil {
		return nil, err
	}
	registerStartup(rt)
	if opts.Repo != nil {
		webdav.WireTasksAndStartupOn(rt)
		webdav.Subscribe(opts.Repo)
	} else {
		configexport.Wire()
		wireCore()
		webdav.WireTasksAndStartupOn(rt)
		webdav.Wire()
	}
	wireSubsystems(rt)
	httpApp := httpx.New()
	for _, register := range routePackages {
		register(httpApp.Router)
	}
	for _, register := range lateRoutePackages {
		register(httpApp.Router)
	}
	auth.Install(httpApp)
	return &App{
		HTTP:    httpApp,
		Repo:    opts.Repo,
		Runtime: rt,
	}, nil
}

func wireSubsystems(rt *lifecycle.Runtime) {
	deploy.Wire()
	rulesync.WireOn(rt)
	apiagents.WireContentSyncOn(rt)
	aggregations.Wire()
	agents.Wire()
	dnsfollow.RegisterOn(rt)
	dnsha.RegisterOn(rt)
	metrics.WireOn(rt)
	metrics.WireTraffic()
	monitor.WireOn(rt)
	substore.WireOn(rt)
	refresh.WireOn(rt)
	health.Wire()
	discovery.WireOn(rt)
	mihomoenv.Wire()
	routers.Wire() // routers R01: repo.Hooks.RedactRouters + router-rule-sync (W9)
	wireSetupContent()
	wireSetupOnboarding()
	wireSetupInstallation()
	wireSetupRouter()
	apirules.Wire()
	gateway.Wire()
	wireIPv6On(rt)
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

// registerStartup installs the synchronous startup steps in their required
// order before subsystem wiring runs.
func registerStartup(rt *lifecycle.Runtime) {
	rt.RegisterStartup(lifecycle.Startup{Name: "load-config", Run: func(ctx context.Context) error {
		_, err := repo.PrepareConfig(ctx)
		return err
	}})
	substore.RegisterStartupOn(rt)
	rt.RegisterStartup(lifecycle.Startup{Name: "agent-manager-init", Run: func(ctx context.Context) error {
		_, err := agents.Init(ctx)
		return err
	}})
	metrics.RegisterStartupOn(rt)
}
