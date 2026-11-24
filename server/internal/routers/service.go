package routers

import (
	"context"
	"encoding/json"
	"sync"

	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/spec"
)

// RuleContentLoader reads one rule set's offline content for a profile.
type RuleContentLoader func(ctx context.Context, profile json.RawMessage, item spec.RuleSet, library *spec.LibraryRule) (string, error)

// IPv6HostFetcher queries an IPv6 gateway Agent's host addresses.
type IPv6HostFetcher func(ctx context.Context, agent *GatewayAgent) (ipv6.IPv6Host, error)

// ManualBaselineCapturer records a verified explicit router publication as the
// automatic rule-sync baseline.
type ManualBaselineCapturer func(ctx context.Context, profile string, config json.RawMessage, r *spec.Router, d Driver, desired *spec.Desired) error

// Options configures a routers Service instance. Nil callbacks fail closed in
// their respective operations (IPv6Hosts -> messageIPv6HostUnavailable,
// Rules -> device.ErrNotPorted when a rule set is selected, ManualBaseline ->
// device.ErrNotPorted logged as capture warning).
type Options struct {
	IPv6Hosts      IPv6HostFetcher
	Rules          RuleContentLoader
	ManualBaseline ManualBaselineCapturer
	Notifications  *notifications.Service
	NewDriver      func(*spec.Router) Driver
	NewRouterID    func() string
	PendingCache   *PendingCache
}

// Service owns router orchestration dependencies for one server instance.
type Service struct {
	repo    *repo.Repository
	opts    Options
	pending *PendingCache
}

// New constructs a routers Service with explicit dependencies.
func New(r *repo.Repository, opts Options) *Service {
	cache := opts.PendingCache
	if cache == nil {
		cache = NewPendingCache(nil, nil)
	}
	return &Service{
		repo:    r,
		opts:    opts,
		pending: cache,
	}
}

var (
	defaultSvcMu   sync.RWMutex
	defaultService = New(nil, Options{})
)

// Default returns the process default routers Service (transitional bridge for
// handlers not yet converted to explicit constructors before W10/W11).
func Default() *Service {
	defaultSvcMu.RLock()
	defer defaultSvcMu.RUnlock()
	return defaultService
}

// UseService installs svc as the process default routers Service.
func UseService(svc *Service) {
	if svc == nil {
		return
	}
	defaultSvcMu.Lock()
	defaultService = svc
	defaultSvcMu.Unlock()
}

// HasIPv6Hosts reports whether the IPv6 host fetcher is wired.
func (s *Service) HasIPv6Hosts() bool { return s != nil && s.opts.IPv6Hosts != nil }

// HasRules reports whether the offline rule content loader is wired.
func (s *Service) HasRules() bool { return s != nil && s.opts.Rules != nil }

// HasManualBaseline reports whether the manual baseline capturer is wired.
func (s *Service) HasManualBaseline() bool { return s != nil && s.opts.ManualBaseline != nil }

// IPv6Host queries the wired IPv6 host fetcher, failing closed when unset.
func (s *Service) IPv6Host(ctx context.Context, agent *GatewayAgent) (ipv6.IPv6Host, error) {
	if s == nil || s.opts.IPv6Hosts == nil {
		return fetchIPv6Host(ctx, nil, agent)
	}
	return s.opts.IPv6Hosts(ctx, agent)
}

// Management returns a ManagementService bound to db (or s.repo when db is nil)
// with the Service's driver and ID factories.
func (s *Service) Management(db *repo.Repository) *ManagementService {
	if db == nil && s != nil {
		db = s.repo
	}
	ms := &ManagementService{Repo: db}
	if s != nil {
		ms.NewDriver = s.opts.NewDriver
		ms.NewRouterID = s.opts.NewRouterID
	}
	return ms
}

// Inspect reads device and Agent state using the Service's wired collaborators.
func (s *Service) Inspect(ctx context.Context, req InspectRequest) (*Inspection, error) {
	if s != nil {
		if req.Repo == nil {
			req.Repo = s.repo
		}
		if req.IPv6Hosts == nil {
			req.IPv6Hosts = s.opts.IPv6Hosts
		}
		if req.Rules == nil {
			req.Rules = s.opts.Rules
		}
	}
	return Inspect(ctx, req)
}

// Publish rebuilds the preview and executes it using the Service's wired
// collaborators.
func (s *Service) Publish(ctx context.Context, req PublishRequest) PublishOutcome {
	req = s.fillPublish(req)
	return Publish(ctx, req)
}

// StartPublish starts a tracked publication owner using the Service's wired
// collaborators.
func (s *Service) StartPublish(ctx context.Context, req PublishRequest, unlock func()) (<-chan PublishOutcome, error) {
	return StartPublish(ctx, s.fillPublish(req), unlock)
}

func (s *Service) fillPublish(req PublishRequest) PublishRequest {
	if s == nil {
		return req
	}
	if req.Repo == nil {
		req.Repo = s.repo
	}
	if req.IPv6Hosts == nil {
		req.IPv6Hosts = s.opts.IPv6Hosts
	}
	if req.Rules == nil {
		req.Rules = s.opts.Rules
	}
	if req.ManualBaseline == nil {
		req.ManualBaseline = s.opts.ManualBaseline
	}
	if req.Notifications == nil {
		req.Notifications = s.opts.Notifications
	}
	if req.NewDriver == nil {
		req.NewDriver = s.opts.NewDriver
	}
	return req
}

// AgentRouters computes upstream-router reminders using the Service's wired
// collaborators and pending cache.
func (s *Service) AgentRouters(ctx context.Context, req PendingRequest) (*PendingResponse, error) {
	if s != nil {
		if req.Repo == nil {
			req.Repo = s.repo
		}
		if req.Cache == nil {
			req.Cache = s.pending
		}
		if req.IPv6Hosts == nil {
			req.IPv6Hosts = s.opts.IPv6Hosts
		}
		if req.Rules == nil {
			req.Rules = s.opts.Rules
		}
		if req.Notifications == nil {
			req.Notifications = s.opts.Notifications
		}
		if req.NewDriver == nil {
			req.NewDriver = s.opts.NewDriver
		}
	}
	return AgentRouters(ctx, req)
}
