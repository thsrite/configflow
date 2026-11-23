package aggregations

import (
	"context"

	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

// ServiceOptions configures an aggregations Service instance.
type ServiceOptions struct {
	Provider *provider.Service
	Repo     *repo.Repository
}

// Service owns aggregation provider rendering and generation for one server
// instance.
type Service struct {
	provider *provider.Service
	repo     *repo.Repository
}

// New constructs an aggregations Service with explicit dependencies.
func New(opts ServiceOptions) *Service {
	return &Service{provider: opts.Provider, repo: opts.Repo}
}

// RenderProvider prepares one aggregation artifact against a frozen
// configuration. It satisfies provider.AggregationRenderer.
func (s *Service) RenderProvider(ctx context.Context, agg model.Aggregation, config *model.Config, main *yamlx.Node, snapshot *provider.DeliverySnapshot, fetch refresh.FetchSourceFn) (*provider.Artifact, error) {
	return RenderProvider(ctx, agg, config, main, snapshot, fetch)
}

// GenerateConfig renders or publishes one aggregation against config.
func (s *Service) GenerateConfig(ctx context.Context, agg model.Aggregation, config *model.Config, options Options) (*Result, error) {
	return GenerateConfig(ctx, agg, config, options)
}
