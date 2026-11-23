package provider

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/store"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

// ErrAggregationsAlreadySet rejects a second SetAggregations call (O17 D6).
var ErrAggregationsAlreadySet = errors.New("provider: aggregation renderer is already set")

// ErrServiceStarted rejects two-phase setter calls after startup wiring completes.
var ErrServiceStarted = errors.New("provider: service already started")

// Options configures a provider Service instance.
type Options struct {
	Repo            *repo.Repository
	SubStore        *substore.Service
	Locations       Locations
	Aggregations    AggregationRenderer
	DeliveryEnv     *DeliveryEnv
	ResponseSeconds *float64
}

// Service owns provider delivery collaborators, two-phase aggregation wiring
// (O17 D6), and response timeout configuration for one server instance.
type Service struct {
	mu              sync.RWMutex
	repo            *repo.Repository
	substore        *substore.Service
	locations       Locations
	aggregations    AggregationRenderer
	aggregationsSet bool
	started         bool
	env             *DeliveryEnv
	responseSeconds float64
}

// New constructs a provider Service with explicit dependencies. Missing
// Locations default to PassthroughLocations; missing Aggregations fail closed
// with ErrAggregationProviderMissing.
func New(opts Options) *Service {
	loc := opts.Locations
	if loc == nil {
		loc = PassthroughLocations{}
	}
	agg := opts.Aggregations
	aggSet := agg != nil
	if agg == nil {
		agg = missingAggregationProvider
	}
	respSec := float64(ResponseSeconds)
	if opts.ResponseSeconds != nil {
		respSec = *opts.ResponseSeconds
	}
	return &Service{
		repo:            opts.Repo,
		substore:        opts.SubStore,
		locations:       loc,
		aggregations:    agg,
		aggregationsSet: aggSet,
		env:             opts.DeliveryEnv,
		responseSeconds: respSec,
	}
}

var (
	defaultSvcMu   sync.RWMutex
	defaultService = New(Options{})
)

// Default returns the process default provider Service (transitional helper
// until W10/W11 remove package-level service locators).
func Default() *Service {
	defaultSvcMu.RLock()
	defer defaultSvcMu.RUnlock()
	return defaultService
}

// UseService installs svc as the process default provider Service.
func UseService(svc *Service) {
	if svc == nil {
		return
	}
	defaultSvcMu.Lock()
	defaultService = svc
	defaultSvcMu.Unlock()
}

// SetAggregations installs the aggregation renderer in the two-phase
// construction step (O17 D6). It may be called at most once and only before
// MarkStarted. A nil renderer keeps the fail-closed default.
func (s *Service) SetAggregations(fn AggregationRenderer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return ErrServiceStarted
	}
	if s.aggregationsSet {
		return ErrAggregationsAlreadySet
	}
	if fn == nil {
		s.aggregations = missingAggregationProvider
		return nil
	}
	s.aggregations = fn
	s.aggregationsSet = true
	return nil
}

// MarkStarted seals two-phase setters on s.
func (s *Service) MarkStarted() {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
}

// Validate checks that required two-phase dependencies are installed on s.
func (s *Service) Validate() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.aggregationsSet {
		return ErrAggregationProviderMissing
	}
	return nil
}

// Locations returns the diagnostic location renderer configured on s.
func (s *Service) Locations() Locations {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.locations == nil {
		return PassthroughLocations{}
	}
	return s.locations
}

// AggregationRenderer returns the aggregation renderer configured on s,
// defaulting to the fail-closed sentinel.
func (s *Service) AggregationRenderer() AggregationRenderer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.aggregations == nil {
		return missingAggregationProvider
	}
	return s.aggregations
}

// DeliveryEnv returns an effective DeliveryEnv snapshot for s.
func (s *Service) DeliveryEnv() *DeliveryEnv {
	s.mu.RLock()
	env := s.env
	sub := s.substore
	s.mu.RUnlock()
	fetch := defaultFetch
	if sub != nil {
		fetch = fetchFromSubStore(sub)
	}
	e := &DeliveryEnv{Fetch: fetch, SubscriptionFetch: fetch, AggregationFetch: fetch, NodeID: randomNodeID}
	if sub != nil {
		e.Converter = sub.Client
	}
	if env != nil {
		if env.Fetch != nil {
			e.Fetch = env.Fetch
		}
		if env.SubscriptionFetch != nil {
			e.SubscriptionFetch = env.SubscriptionFetch
		}
		if env.AggregationFetch != nil {
			e.AggregationFetch = env.AggregationFetch
		}
		if env.Converter != nil {
			e.Converter = env.Converter
		}
		if env.NodeID != nil {
			e.NodeID = env.NodeID
		}
	}
	return e
}

// ResponseSeconds returns the configured Mihomo provider response deadline.
func (s *Service) ResponseSeconds() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.responseSeconds
}

// PrepareBundle prepares a validated provider bundle using s's collaborators.
func (s *Service) PrepareBundle(ctx context.Context, config *model.Config, main *yamlx.Node, options DeliveryOptions) ([]*Artifact, error) {
	return PrepareBundle(ctx, config, main, options)
}

// Serve serves a provider response using s's response settings.
func (s *Service) Serve(c *httpx.Ctx, kind, itemID, profileID string, head *store.Head, render Render) (*httpx.Response, error) {
	return Serve(c, kind, itemID, profileID, head, render)
}

func (s *Service) setLocations(l Locations) func() {
	if l == nil {
		l = PassthroughLocations{}
	}
	s.mu.Lock()
	prev := s.locations
	s.locations = l
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.locations = prev
		s.mu.Unlock()
	}
}

func (s *Service) setAggregationRenderer(fn AggregationRenderer) func() {
	s.mu.Lock()
	prev := s.aggregations
	prevSet := s.aggregationsSet
	if fn == nil {
		s.aggregations = missingAggregationProvider
		s.aggregationsSet = false
	} else {
		s.aggregations = fn
		s.aggregationsSet = true
	}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.aggregations = prev
		s.aggregationsSet = prevSet
		s.mu.Unlock()
	}
}

func (s *Service) setDeliveryEnv(e *DeliveryEnv) func() {
	s.mu.Lock()
	prev := s.env
	s.env = e
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.env = prev
		s.mu.Unlock()
	}
}

func (s *Service) setResponseSeconds(seconds float64) func() {
	s.mu.Lock()
	prev := s.responseSeconds
	s.responseSeconds = seconds
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.responseSeconds = prev
		s.mu.Unlock()
	}
}

func fetchFromSubStore(sub *substore.Service) refresh.FetchSourceFn {
	return func(ctx context.Context, subID, url string) (*refresh.SourceResult, error) {
		result, err := sub.Client.Fetch(ctx, subID, url, "", substore.FetchOptions{})
		if err != nil || result == nil {
			return nil, err
		}
		var traffic *refresh.Traffic
		if result.Traffic != nil {
			data, err := model.Marshal(result.Traffic)
			if err != nil {
				return nil, err
			}
			traffic = &refresh.Traffic{}
			if err := json.Unmarshal(data, traffic); err != nil {
				return nil, err
			}
		}
		return &refresh.SourceResult{Text: result.Text, Source: result.Source, FetchedAt: result.FetchedAt, Traffic: traffic}, nil
	}
}
