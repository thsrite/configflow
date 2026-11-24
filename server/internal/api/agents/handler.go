package agents

import (
	"context"
	"path/filepath"

	core "configflow/internal/agents"
	"configflow/internal/agents/dnsfollow"
	"configflow/internal/agents/dnsha"
	"configflow/internal/agents/metrics"
	"configflow/internal/agents/monitor"
	"configflow/internal/agents/rulesync"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/agents/upgrades"
	"configflow/internal/deploy"
	"configflow/internal/health"
	"configflow/internal/health/check"
	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// Deps holds the explicit services injected into the /api/agents HTTP handlers.
// Nil fields fall back to transitional process/context locators until W11.
type Deps struct {
	Repo      *repo.Repository
	Agents    *core.Manager
	Traffic   *trafficstore.Service
	Metrics   *metrics.Service
	Monitor   *monitor.Service
	Upgrades  *upgrades.Service
	DNSHA     *dnsha.Service
	DNSFollow *dnsfollow.Service
	RuleSync  *rulesync.Service
	Deploy    *deploy.Service
}

func (d Deps) repository(ctx context.Context) (*repo.Repository, error) {
	if d.Repo != nil {
		return d.Repo, nil
	}
	return repo.GetRepository(ctx)
}

func (d Deps) manager(ctx context.Context) (*core.Manager, error) {
	if d.Agents != nil {
		return d.Agents, nil
	}
	return core.Get(ctx)
}

func (d Deps) withRepo(ctx context.Context) context.Context {
	if d.Repo != nil {
		return repo.WithRepository(ctx, d.Repo)
	}
	return ctx
}

func (d Deps) dataDir() string {
	if d.Repo != nil {
		return d.Repo.DataDir()
	}
	return repo.DataDir()
}

func (d Deps) dnsFollowService(ctx context.Context) (*dnsfollow.Service, error) {
	if d.DNSFollow != nil {
		return d.DNSFollow, nil
	}
	m, err := d.manager(ctx)
	if err != nil {
		return nil, err
	}
	return dnsfollow.ForManager(m), nil
}

func (d Deps) dnsHAService(ctx context.Context) (*dnsha.Service, error) {
	if d.DNSHA != nil {
		return d.DNSHA, nil
	}
	m, err := d.manager(ctx)
	if err != nil {
		return nil, err
	}
	return dnsha.ForManager(m), nil
}

func (d Deps) ruleSyncService(ctx context.Context) (*rulesync.Service, error) {
	if d.RuleSync != nil {
		return d.RuleSync, nil
	}
	return rulesync.Get(ctx)
}

func (d Deps) metricsHistory(m *core.Manager) *metrics.History {
	if d.Metrics != nil {
		return d.Metrics.History()
	}
	return metrics.ForManager(m)
}

func (d Deps) trafficHistory(m *core.Manager) *metrics.TrafficHistory {
	if d.Repo != nil {
		return metrics.TrafficFor(d.Repo.DataDir())
	}
	return metrics.TrafficForManager(m)
}

func (d Deps) trafficStore(ctx context.Context) (*trafficstore.NativeStore, error) {
	if d.Traffic != nil {
		return d.Traffic.Store(), nil
	}
	return trafficstore.GetNative(ctx)
}

func (d Deps) probeStore(ctx context.Context) (*check.ProbeStore, error) {
	if d.Repo != nil {
		return check.NewProbeStore(filepath.Join(d.Repo.DataDir(), "probes")), nil
	}
	return health.GetProbeStore(ctx)
}

func (d Deps) regionStore(ctx context.Context) (*check.RegionStore, error) {
	if d.Repo != nil {
		return check.NewRegionStore(filepath.Join(d.Repo.DataDir(), "regions")), nil
	}
	return health.GetRegionStore(ctx)
}

func (d Deps) loadPreview(m *core.Manager, previewID, agentID string) (*deploy.Preview, error) {
	if d.Deploy != nil {
		return d.Deploy.LoadPreview(m, previewID, agentID)
	}
	return deploy.LoadPreview(m, previewID, agentID)
}

func (d Deps) followHost(ctx context.Context, m *core.Manager, a *model.Agent, host string) error {
	if d.Upgrades != nil {
		return d.Upgrades.FollowModel(ctx, a, host)
	}
	return upgrades.FollowModel(ctx, m, a, host)
}

func (d Deps) confirmedHost(a *model.Agent, host string) string {
	if d.Upgrades != nil {
		return d.Upgrades.ConfirmedModel(a, host)
	}
	return upgrades.ConfirmedModel(a, host)
}

// Handler serves the /api/agents blueprint routes using explicit service
// dependencies organized into focused sub-handlers.
type Handler struct {
	registry    *registryHandler
	dnsFollow   *dnsFollowHandler
	dnsHA       *dnsHAHandler
	content     *contentHandler
	deploy      *deployHandler
	ruleSync    *ruleSyncHandler
	metrics     *metricsHandler
	traffic     *trafficHandler
	oxidns      *oxidnsHandler
	clientTools *clientToolsHandler
	panel       *panelHandler
	gateway     *gatewayHandler
	installer   *installerHandler
	upgrades    *upgradesHandler
	dockerHA    *dockerHAHandler
	downloads   *downloadsHandler
	gatewayInst *gatewayInstallationHandler
}

// NewHandler constructs a Handler for the /api/agents routes.
func NewHandler(deps Deps) *Handler {
	if deps.Repo == nil && deps.Agents != nil {
		deps.Repo = deps.Agents.Repository()
	}
	content := &contentHandler{deps: deps}
	met := &metricsHandler{deps: deps}
	ox := &oxidnsHandler{deps: deps}
	return &Handler{
		registry:    &registryHandler{deps: deps, content: content},
		dnsFollow:   &dnsFollowHandler{deps: deps},
		dnsHA:       &dnsHAHandler{deps: deps},
		content:     content,
		deploy:      &deployHandler{deps: deps},
		ruleSync:    &ruleSyncHandler{deps: deps},
		metrics:     met,
		traffic:     &trafficHandler{deps: deps, metrics: met},
		oxidns:      ox,
		clientTools: &clientToolsHandler{tools: clientTools{deps: deps}},
		panel:       &panelHandler{deps: deps, oxidns: ox},
		gateway:     &gatewayHandler{deps: deps},
		installer:   &installerHandler{deps: deps},
		upgrades:    &upgradesHandler{deps: deps},
		dockerHA:    &dockerHAHandler{deps: deps},
		downloads:   &downloadsHandler{},
		gatewayInst: &gatewayInstallationHandler{deps: deps},
	}
}

// Register mounts every /api/agents route on r in Flask-blueprint order.
func (h *Handler) Register(r *httpx.Router) {
	h.registry.register(r)
	h.dnsFollow.register(r)
	h.dnsHA.register(r)
	h.content.register(r)
	h.deploy.register(r)
	h.ruleSync.register(r)
	h.metrics.register(r)
	h.traffic.register(r)
	h.oxidns.register(r)
	h.clientTools.register(r)
	h.panel.register(r)
	h.gateway.register(r)
	h.installer.register(r)
	h.upgrades.register(r)
	h.dockerHA.register(r)
	h.downloads.register(r)
	h.gatewayInst.register(r)
}
