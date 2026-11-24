package server

import (
	apiconfig "configflow/internal/api/config"
	apigenerate "configflow/internal/api/generate"
	apimosdns "configflow/internal/api/mosdns"
	apioxidns "configflow/internal/api/oxidns"
	apiprofiles "configflow/internal/api/profiles"
	apirouters "configflow/internal/api/routers"
	apirulelibrary "configflow/internal/api/rulelibrary"
	apirules "configflow/internal/api/rules"
	"configflow/internal/rulesource"
)

// w10bHandlers holds the explicitly constructed HTTP handlers for the W10b
// route packages (routers, rules, rulelibrary, config, profiles, generate,
// mosdns, oxidns).
type w10bHandlers struct {
	rules       *apirules.Handler
	rulelibrary *apirulelibrary.Handler
	routers     *apirouters.Handler
	config      *apiconfig.Handler
	generate    *apigenerate.Handler
	profiles    *apiprofiles.Handler
	mosdns      *apimosdns.Handler
	oxidns      *apioxidns.Handler
}

func newW10bHandlers(opts Options, ag *agentServices, gen *generateServices, rs *rulesource.Service, rtrSvc *routerServices) w10bHandlers {
	return w10bHandlers{
		rules: apirules.New(apirules.Deps{
			Repo:       opts.Repo,
			RuleSource: rs,
		}),
		rulelibrary: apirulelibrary.New(apirulelibrary.Deps{
			Repo: opts.Repo,
		}),
		routers: apirouters.New(apirouters.Deps{
			Repo:     opts.Repo,
			Agents:   ag.manager,
			Routers:  rtrSvc.routers,
			RuleSync: rtrSvc.rulesync,
		}),
		config: apiconfig.New(apiconfig.Deps{
			Repo: opts.Repo,
		}),
		generate: apigenerate.New(apigenerate.Deps{
			Repo:   opts.Repo,
			Agents: ag.manager,
			Deploy: gen.deploy,
		}),
		profiles: apiprofiles.New(apiprofiles.Deps{
			Repo:   opts.Repo,
			Agents: ag.manager,
		}),
		mosdns: apimosdns.New(apimosdns.Deps{
			Repo: opts.Repo,
		}),
		oxidns: apioxidns.New(apioxidns.Deps{
			Repo: opts.Repo,
		}),
	}
}
