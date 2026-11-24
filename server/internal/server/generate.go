package server

import (
	"errors"

	"configflow/internal/aggregations"
	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/gateway"
	"configflow/internal/lifecycle"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/tasks"
)

// generateServices holds the instantiated generation-pipeline services owned by
// one server App (O17 W7). deploy is filled by wireAgents (newDeployService).
type generateServices struct {
	substore     *substore.Service
	refresh      *refresh.Service
	provider     *provider.Service
	aggregations *aggregations.Service
	deploy       *deploy.Service
}

func newGenerateServices(r *repo.Repository, rt *lifecycle.Runtime, tk *tasks.Registry) (*generateServices, error) {
	var subEnv *substore.Env
	if r != nil {
		subEnv = substore.NewEnvFor(r)
	} else {
		subEnv = substore.NewEnv()
	}
	sub := substore.NewService(subEnv)

	rf := refresh.New(refresh.Options{
		Repo:     r,
		SubStore: sub,
		Runtime:  rt,
		Tasks:    tk,
	})
	refresh.UseService(rf)

	pv := provider.New(provider.Options{
		Repo:      r,
		SubStore:  sub,
		Locations: deploy.Locations{},
	})
	agg := aggregations.New(aggregations.ServiceOptions{
		Provider: pv,
		Repo:     r,
		SubStore: sub,
	})
	if err := pv.SetAggregations(agg.RenderProvider); err != nil {
		return nil, err
	}
	if err := pv.Validate(); err != nil {
		return nil, err
	}
	pv.MarkStarted()
	provider.UseService(pv)

	return &generateServices{
		substore:     sub,
		refresh:      rf,
		provider:     pv,
		aggregations: agg,
	}, nil
}

// newDeployService constructs the deploy Service once the HA pair publisher
// and IPv6 enroller exist (wireAgents), so both are injected directly instead
// of through the package-level SetPairPublication/SetIPv6Enroll bridges. The
// instance is also installed as deploy.Default() for callers not yet migrated
// (api/agents until W10). Nil options keep the fail-closed defaults.
func newDeployService(opts deploy.Options) *deploy.Service {
	dp := deploy.New(opts)
	deploy.UseService(dp)
	return dp
}

func wireConvertOnce(r *repo.Repository, gen *generateServices) error {
	if err := mihomoenv.WireWith(r, gen.substore, gen.provider); err != nil && !errors.Is(err, convert.ErrMihomoEnvironmentAlreadySet) {
		return err
	}
	if err := convert.SetMihomoIntegrations(gateway.Hooks()); err != nil && !errors.Is(err, convert.ErrMihomoIntegrationsAlreadySet) {
		return err
	}
	return nil
}
