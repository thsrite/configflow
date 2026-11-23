package server

import (
	"context"

	"configflow/internal/agents"
	"configflow/internal/agents/metrics"
	"configflow/internal/aggregations"
	apinotifications "configflow/internal/api/notifications"
	apirules "configflow/internal/api/rules"
	apitasks "configflow/internal/api/tasks"
	"configflow/internal/auth"
	"configflow/internal/configexport"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/discovery"
	"configflow/internal/gateway"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/notifications"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/routers"
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
}

// New wires the application, registers ordered startup steps and background
// loops on the Runtime, mounts every route package, validates task and
// notification slot completeness, and installs the authentication gate.
func New(ctx context.Context, opts Options) (*App, error) {
	rt := opts.Runtime
	if rt == nil {
		rt = lifecycle.Current()
	} else if err := lifecycle.UseRuntime(rt); err != nil {
		return nil, err
	}
	tk := tasks.NewRegistry(opts.Repo, rt)
	nt := notifications.New(notifications.Options{Repo: opts.Repo, Tasks: tk})
	tasks.UseRegistry(tk)
	notifications.UseService(nt)
	m, err := wireAgents(ctx, opts, rt, tk, nt)
	if err != nil {
		return nil, err
	}
	registerStartup(rt, m)
	if opts.Repo != nil {
		webdav.WireTasksAndStartupOn(rt)
		webdav.Subscribe(opts.Repo)
	} else {
		configexport.Wire()
		wireCore()
		webdav.WireTasksAndStartupOn(rt)
		webdav.Wire()
	}
	wireOtherSubsystems(rt)
	if err := tk.Validate(); err != nil {
		return nil, err
	}
	if err := nt.Validate(); err != nil {
		return nil, err
	}
	nt.MarkStarted()
	httpApp := httpx.New()
	for _, register := range routePackages {
		register(httpApp.Router)
	}
	apitasks.NewHandler(tk).Register(httpApp.Router)
	apinotifications.NewHandler(tk, nt, opts.Repo).Register(httpApp.Router)
	auth.Install(httpApp)
	return &App{
		HTTP:          httpApp,
		Repo:          opts.Repo,
		Runtime:       rt,
		Tasks:         tk,
		Notifications: nt,
		Agents:        m,
	}, nil
}

func wireOtherSubsystems(rt *lifecycle.Runtime) {
	aggregations.Wire()
	substore.WireOn(rt)
	refresh.WireOn(rt)
	discovery.WireOn(rt)
	mihomoenv.Wire()
	routers.Wire() // routers R01: repo.Hooks.RedactRouters + router-rule-sync (W9)
	wireSetupContent()
	wireSetupOnboarding()
	wireSetupInstallation()
	wireSetupRouter()
	apirules.Wire()
	gateway.Wire()
}

// registerStartup installs the synchronous startup steps in their required
// order before webdav-worker-init runs.
func registerStartup(rt *lifecycle.Runtime, m *agents.Manager) {
	rt.RegisterStartup(lifecycle.Startup{Name: "load-config", Run: func(ctx context.Context) error {
		_, err := repo.PrepareConfig(ctx)
		return err
	}})
	substore.RegisterStartupOn(rt)
	rt.RegisterStartup(lifecycle.Startup{Name: "agent-manager-init", Run: func(ctx context.Context) error {
		if m != nil {
			agents.RunInitHooks(ctx, m)
			return nil
		}
		_, err := agents.Init(ctx)
		return err
	}})
	metrics.RegisterStartupOn(rt)
}
