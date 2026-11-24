package server

import (
	"context"
	"encoding/json"

	"configflow/internal/agents"
	"configflow/internal/agents/dnsfollow"
	"configflow/internal/agents/dnsha"
	agentipv6 "configflow/internal/agents/ipv6"
	"configflow/internal/agents/metrics"
	"configflow/internal/agents/monitor"
	"configflow/internal/agents/rulesync"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/agents/upgrades"
	"configflow/internal/aggregations"
	apisettings "configflow/internal/api/settings"
	"configflow/internal/auth"
	"configflow/internal/configexport"
	"configflow/internal/deploy"
	"configflow/internal/discovery"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/notifications"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/routers"
	routerrulesync "configflow/internal/routers/rulesync"
	"configflow/internal/routers/spec"
	"configflow/internal/rulesource"
	"configflow/internal/substore"
	"configflow/internal/tasks"
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
	HTTP          *httpx.App
	Repo          *repo.Repository
	Runtime       *lifecycle.Runtime
	Tasks         *tasks.Registry
	Notifications *notifications.Service
	Agents        *agents.Manager
	Traffic       *trafficstore.Service
	Metrics       *metrics.Service
	Monitor       *monitor.Service
	Upgrades      *upgrades.Service
	DNSHA         *dnsha.Service
	DNSFollow     *dnsfollow.Service
	RuleSync      *rulesync.Service
	IPv6          *agentipv6.Service
	SubStore      *substore.Service
	Refresh       *refresh.Service
	Provider      *provider.Service
	Aggregations  *aggregations.Service
	Deploy        *deploy.Service
	RuleSource    *rulesource.Service
	Routers       *routers.Service
	RouterSync    *routerrulesync.RuleSyncService
	Settings      *apisettings.Handler
}

// New wires the application, registers ordered startup steps and background
// loops on the Runtime, mounts every route package, validates task and
// notification slot completeness, and installs the authentication gate.
func New(ctx context.Context, opts Options) (*App, error) { return build(ctx, opts, true) }

// build wires the application. process registers the two startup steps only
// cmd/configflow registered before W1 (load-config, agent-manager-init); the
// legacy NewApp test helper leaves them out, as before W1, so differential
// harnesses keep initializing the Agent manager on first use.
func build(ctx context.Context, opts Options, process bool) (*App, error) {
	rt := opts.Runtime
	if rt == nil {
		rt = lifecycle.Current()
	} else if err := lifecycle.UseRuntime(rt); err != nil {
		return nil, err
	}
	tk := tasks.NewRegistry(opts.Repo, rt)
	nt := notifications.New(notifications.Options{Repo: opts.Repo, Tasks: tk, Summarize: routers.NotificationSummary})
	tasks.UseRegistry(tk)
	notifications.UseService(nt)
	gen, err := newGenerateServices(opts.Repo, rt, tk)
	if err != nil {
		return nil, err
	}
	ag, err := wireAgents(ctx, opts, rt, tk, nt, gen)
	if err != nil {
		return nil, err
	}
	rs := rulesource.New(rulesource.Options{})
	rulesource.UseService(rs)
	registerStartup(rt, gen.substore, ag.manager, ag.metrics, process)
	if opts.Repo != nil {
		webdav.WireTasksAndStartupOn(rt)
		webdav.Subscribe(opts.Repo)
	} else {
		configexport.Wire()
		wireCore()
		repo.RegisterHooks(repo.Hooks{RecordHooks: repo.RecordHooks{
			RedactRoutersJSON: func(raw json.RawMessage) (json.RawMessage, error) { return spec.RedactRouters(raw) },
			JobLockRecord:     routers.JobLockRecord,
		}})
		webdav.WireTasksAndStartupOn(rt)
		webdav.Wire()
	}
	rtrSvc, err := wireOtherSubsystems(opts, rt, tk, nt, ag, gen, rs)
	if err != nil {
		return nil, err
	}
	if err := gen.deploy.Validate(); err != nil {
		return nil, err
	}
	if err := tk.Validate(); err != nil {
		return nil, err
	}
	if err := nt.Validate(); err != nil {
		return nil, err
	}
	nt.MarkStarted()
	httpApp := httpx.New()
	w10c := newW10cHandlers(opts, tk, nt, ag, gen, rs)
	registerRoutes(httpApp.Router, newAgentAPIHandler(opts, ag, gen), newW10bHandlers(opts, ag, gen, rs, rtrSvc), w10c)
	w10c.tasks.Register(httpApp.Router)
	w10c.notifications.Register(httpApp.Router)
	auth.Install(httpApp)
	return &App{
		HTTP:          httpApp,
		Repo:          opts.Repo,
		Runtime:       rt,
		Tasks:         tk,
		Notifications: nt,
		Agents:        ag.manager,
		Traffic:       ag.traffic,
		Metrics:       ag.metrics,
		Monitor:       ag.monitor,
		Upgrades:      ag.upgrades,
		DNSHA:         ag.dnsha,
		DNSFollow:     ag.dnsfollow,
		RuleSync:      ag.rulesync,
		IPv6:          ag.ipv6,
		SubStore:      gen.substore,
		Refresh:       gen.refresh,
		Provider:      gen.provider,
		Aggregations:  gen.aggregations,
		Deploy:        gen.deploy,
		RuleSource:    rs,
		Routers:       rtrSvc.routers,
		RouterSync:    rtrSvc.rulesync,
		Settings:      w10c.settings,
	}, nil
}

// wireOtherSubsystems registers the generation-pipeline loops and task rows
// (W7), routers and router rule-sync services (W9), and the subsystems not yet
// instantiated (W10). Agent-domain services are wired exactly once in wireAgents.
func wireOtherSubsystems(opts Options, rt *lifecycle.Runtime, tk *tasks.Registry, nt *notifications.Service, ag *agentServices, gen *generateServices, rs *rulesource.Service) (*routerServices, error) {
	gen.substore.Register(rt)
	gen.refresh.Register(rt, tk)
	discovery.WireOn(rt)
	if err := wireConvertOnce(gen); err != nil {
		return nil, err
	}
	rtrSvc := wireRouters(opts, rt, tk, nt, ag, rs)
	wireSetupContentWith(rs)
	wireSetupOnboarding()
	wireSetupInstallation()
	wireSetupRouter(rtrSvc.routers)
	wireRuleSource(rs)
	return rtrSvc, nil
}

// registerStartup installs the synchronous startup steps in their required
// order before webdav-worker-init runs; load-config and agent-manager-init
// only for the process (New), as before W1 the legacy NewApp test helper left
// them to cmd/configflow.
func registerStartup(rt *lifecycle.Runtime, sub *substore.Service, m *agents.Manager, mt *metrics.Service, process bool) {
	if process {
		rt.RegisterStartup(lifecycle.Startup{Name: "load-config", Run: func(ctx context.Context) error {
			_, err := repo.PrepareConfig(ctx)
			return err
		}})
	}
	sub.RegisterStartup(rt)
	if process {
		rt.RegisterStartup(lifecycle.Startup{Name: "agent-manager-init", Run: func(ctx context.Context) error {
			if m != nil {
				agents.RunInitHooks(ctx, m)
				return nil
			}
			_, err := agents.Init(ctx)
			return err
		}})
	}
	if mt != nil {
		rt.RegisterStartup(mt.Startup(m))
	} else {
		metrics.RegisterStartupOn(rt)
	}
}
