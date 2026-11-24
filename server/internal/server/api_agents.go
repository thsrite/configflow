package server

import (
	apiagents "configflow/internal/api/agents"
	"configflow/internal/discovery"
)

// newAgentAPIHandler constructs the /api/agents Handler from the wired
// application services. When opts.Repo is nil (legacy NewApp test path),
// zero-value fields keep the transitional locator fallbacks until W11.
func newAgentAPIHandler(opts Options, ag *agentServices, gen *generateServices, disc *discovery.Service) *apiagents.Handler {
	if opts.Repo == nil {
		return apiagents.NewHandler(apiagents.Deps{Discovery: disc})
	}
	return apiagents.NewHandler(apiagents.Deps{
		Repo:      opts.Repo,
		Agents:    ag.manager,
		Traffic:   ag.traffic,
		Metrics:   ag.metrics,
		Monitor:   ag.monitor,
		Upgrades:  ag.upgrades,
		DNSHA:     ag.dnsha,
		DNSFollow: ag.dnsfollow,
		RuleSync:  ag.rulesync,
		Deploy:    gen.deploy,
		Discovery: disc,
	})
}
