package server

import (
	"errors"

	"configflow/internal/aggregations"
	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/gateway"
	"configflow/internal/lifecycle"
	"configflow/internal/notifications"
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

func newGenerateServices(r *repo.Repository, rt *lifecycle.Runtime, tk *tasks.Registry, nt *notifications.Service) (*generateServices, error) {
	var subEnv *substore.Env
	if r != nil {
		subEnv = substore.NewEnvFor(r)
	} else {
		subEnv = substore.NewEnv()
	}
	sub := substore.NewService(subEnv)

	rf := refresh.New(refresh.Options{
		Repo:          r,
		SubStore:      sub,
		Runtime:       rt,
		Tasks:         tk,
		Notifications: nt,
	})

	pv := provider.New(provider.Options{
		Repo:      r,
		SubStore:  sub,
		Refresh:   rf,
		Locations: deploy.Locations{},
	})
	agg := aggregations.New(aggregations.ServiceOptions{
		Provider: pv,
		Repo:     r,
		SubStore: sub,
		Refresh:  rf,
	})
	if err := pv.SetAggregations(agg.RenderProvider); err != nil {
		return nil, err
	}
	if err := pv.Validate(); err != nil {
		return nil, err
	}
	pv.MarkStarted()

	return &generateServices{
		substore:     sub,
		refresh:      rf,
		provider:     pv,
		aggregations: agg,
	}, nil
}

// newDeployService constructs the deploy Service once the HA pair publisher
// and IPv6 enroller exist (wireAgents), so both are injected directly.
// Nil options keep the fail-closed defaults.
func newDeployService(opts deploy.Options) *deploy.Service {
	return deploy.New(opts)
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
