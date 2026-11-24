package server

import (
	apiaggregations "configflow/internal/api/aggregations"
	apiauth "configflow/internal/api/auth"
	apiclientdevices "configflow/internal/api/clientdevices"
	apidiscovery "configflow/internal/api/domaindiscovery"
	apigatewayonboarding "configflow/internal/api/gatewayonboarding"
	apiipv6 "configflow/internal/api/ipv6"
	apilinuxgateway "configflow/internal/api/linuxgateway"
	apilogs "configflow/internal/api/logs"
	apinodes "configflow/internal/api/nodes"
	apinotifications "configflow/internal/api/notifications"
	apiproxygroups "configflow/internal/api/proxygroups"
	apisettings "configflow/internal/api/settings"
	apisetup "configflow/internal/api/setup"
	apistats "configflow/internal/api/stats"
	apisubscriptions "configflow/internal/api/subscriptions"
	apitasks "configflow/internal/api/tasks"
	"configflow/internal/discovery"
	"configflow/internal/gateway"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/notifications"
	"configflow/internal/rulesource"
	"configflow/internal/setup"
	"configflow/internal/tasks"
	"configflow/internal/webdav"
)

// w10cHandlers holds the HTTP handler instances constructed for the W10c
// blueprint group (everything outside W10a api/agents and W10b config/rules/
// routers/generation/DNS).
type w10cHandlers struct {
	auth              *apiauth.Handler
	clientdevices     *apiclientdevices.Handler
	nodes             *apinodes.Handler
	subscriptions     *apisubscriptions.Handler
	aggregations      *apiaggregations.Handler
	proxygroups       *apiproxygroups.Handler
	ipv6              *apiipv6.Handler
	linuxgateway      *apilinuxgateway.Handler
	gatewayonboarding *apigatewayonboarding.Handler
	settings          *apisettings.Handler
	setup             *apisetup.Handler
	logs              *apilogs.Handler
	stats             *apistats.Handler
	domaindiscovery   *apidiscovery.Handler
	tasks             *apitasks.Handler
	notifications     *apinotifications.Handler
}

func newW10cHandlers(opts Options, tk *tasks.Registry, nt *notifications.Service, ag *agentServices, gen *generateServices, rs *rulesource.Service, wd *webdav.Service, disc *discovery.Service) *w10cHandlers {
	var ruleReader gateway.RuleSourceReader
	if rs != nil {
		ruleReader = rs.ReadGateway
	}
	var onboardingHandler *apigatewayonboarding.Handler
	var setupHandler *apisetup.Handler
	if opts.Repo != nil {
		onboardingHandler = apigatewayonboarding.NewHandler(opts.Repo, onboarding.CheckDNSLoop, setup.AdoptOwnedWrite)
		owners := setup.RegisteredOwners()
		setupHandler = apisetup.NewHandler(opts.Repo, &owners)
	} else {
		onboardingHandler = &apigatewayonboarding.Handler{DNSLoop: onboarding.CheckDNSLoop}
		setupHandler = apisetup.NewHandler(nil, nil)
	}
	return &w10cHandlers{
		auth:              apiauth.NewHandler(),
		clientdevices:     apiclientdevices.NewHandler(opts.Repo, ag.manager),
		nodes:             apinodes.NewHandler(opts.Repo, gen.substore),
		subscriptions:     apisubscriptions.NewHandler(opts.Repo, gen.substore, gen.provider, gen.refresh),
		aggregations:      apiaggregations.NewHandler(opts.Repo, gen.aggregations, gen.provider, gen.substore),
		proxygroups:       apiproxygroups.NewHandler(opts.Repo, gen.substore),
		ipv6:              apiipv6.NewHandler(opts.Repo, ag.manager, ag.ipv6),
		linuxgateway:      apilinuxgateway.NewHandler(opts.Repo, ag.manager, ruleReader),
		gatewayonboarding: onboardingHandler,
		settings:          apisettings.NewHandler(opts.Repo, gen.substore, wd),
		setup:             setupHandler,
		logs:              apilogs.NewHandler(),
		stats:             apistats.NewHandler(opts.Repo),
		domaindiscovery:   apidiscovery.NewHandler(opts.Repo, disc),
		tasks:             apitasks.NewHandlerWithRepo(tk, opts.Repo),
		notifications:     apinotifications.NewHandler(tk, nt, opts.Repo),
	}
}
