package aggregations

import (
	"context"
	"encoding/json"

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
	Refresh  *refresh.Service
}

// Service owns aggregation provider rendering and generation for one server
// instance.
type Service struct {
	provider *provider.Service
	repo     *repo.Repository
	substore *substore.Service
	refresh  *refresh.Service
}

// New constructs an aggregations Service with explicit dependencies.
func New(opts ServiceOptions) *Service {
	return &Service{provider: opts.Provider, repo: opts.Repo, substore: opts.SubStore, refresh: opts.Refresh}
}

func (s *Service) refreshSvc() *refresh.Service {
	if s != nil && s.refresh != nil {
		return s.refresh
	}
	var r *repo.Repository
	var sub *substore.Service
	if s != nil {
		r = s.repo
		sub = s.substore
	}
	return refresh.New(refresh.Options{Repo: r, SubStore: sub})
}

// RenderProvider prepares one aggregation artifact against a frozen
// configuration. It satisfies provider.AggregationRenderer.
func (s *Service) RenderProvider(ctx context.Context, agg model.Aggregation, config *model.Config, main *yamlx.Node, snapshot *provider.DeliverySnapshot, fetch refresh.FetchSourceFn) (*provider.Artifact, error) {
	return renderProviderWith(ctx, s.provider, s.repo, s.substore, s.refreshSvc(), agg, config, main, snapshot, fetch)
}

// Generate decodes the native JSON entry point and renders or publishes one aggregation.
func (s *Service) Generate(ctx context.Context, rawAggregation, rawConfig json.RawMessage, options Options) (*Result, error) {
	return s.generateJSON(ctx, rawAggregation, rawConfig, options)
}

// GenerateConfig renders or publishes one aggregation against config.
func (s *Service) GenerateConfig(ctx context.Context, agg model.Aggregation, config *model.Config, options Options) (*Result, error) {
	return s.generateConfig(ctx, agg, config, options)
}

// LoadCatalog reads an owned shared snapshot for SaveCatalog.
func (s *Service) LoadCatalog(ctx context.Context) (*model.Shared, error) {
	return loadCatalogWith(ctx, s.repo)
}

// SaveCatalog commits an edited catalog against its repository baseline.
func (s *Service) SaveCatalog(ctx context.Context, document *model.Shared) error {
	return saveCatalogWith(ctx, s.repo, document)
}

// AppendCatalog adds an aggregation in one shared repository transaction.
func (s *Service) AppendCatalog(ctx context.Context, raw json.RawMessage) error {
	return appendCatalogWith(ctx, s.repo, raw)
}
