package server

import (
	"context"

	"configflow/internal/agents"
	"configflow/internal/agents/metrics"
	"configflow/internal/auth"
	"configflow/internal/configexport"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
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
// loops, mounts every route package, and installs the authentication gate.
func New(ctx context.Context, opts Options) (*App, error) {
	registerStartup()
	if opts.Repo != nil {
		webdav.WireTasksAndStartup()
		webdav.Subscribe(opts.Repo)
	} else {
		configexport.Wire()
		wireCore()
		webdav.Wire()
	}
	for _, wire := range wirePackages {
		wire()
	}
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
		Runtime: opts.Runtime,
	}, nil
}

// registerStartup installs the synchronous startup steps in their required
// order before wirePackages runs.
func registerStartup() {
	lifecycle.RegisterStartup(lifecycle.Startup{Name: "load-config", Run: func(ctx context.Context) error {
		_, err := repo.PrepareConfig(ctx)
		return err
	}})
	substore.RegisterStartup()
	lifecycle.RegisterStartup(lifecycle.Startup{Name: "agent-manager-init", Run: func(ctx context.Context) error {
		_, err := agents.Init(ctx)
		return err
	}})
	metrics.RegisterStartup()
}
