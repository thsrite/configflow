package aggregations

import (
	"context"

	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

// ServiceOptions configures an aggregations Service instance.
type ServiceOptions struct {
	Provider *provider.Service
	Repo     *repo.Repository
	SubStore *substore.Service
}

// Service owns aggregation provider rendering and generation for one server
// instance.
type Service struct {
	provider *provider.Service
	repo     *repo.Repository
	substore *substore.Service
}

// New constructs an aggregations Service with explicit dependencies.
func New(opts ServiceOptions) *Service {
	return &Service{provider: opts.Provider, repo: opts.Repo, substore: opts.SubStore}
}

// RenderProvider prepares one aggregation artifact against a frozen
// configuration. It satisfies provider.AggregationRenderer.
func (s *Service) RenderProvider(ctx context.Context, agg model.Aggregation, config *model.Config, main *yamlx.Node, snapshot *provider.DeliverySnapshot, fetch refresh.FetchSourceFn) (*provider.Artifact, error) {
	return renderProviderWith(ctx, s.repo, s.substore, agg, config, main, snapshot, fetch)
}

// GenerateConfig renders or publishes one aggregation against config.
func (s *Service) GenerateConfig(ctx context.Context, agg model.Aggregation, config *model.Config, options Options) (*Result, error) {
	return GenerateConfig(ctx, agg, config, options)
}
