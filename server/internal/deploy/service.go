package deploy

import (
	"context"
	"sync"

	"configflow/internal/agents"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/substore"
)

// PairPublisher handles HA pair publication when an Agent belongs to an
// enabled HA pair. A nil PairPublisher fails closed with ErrPairPublisherMissing.
type PairPublisher func(context.Context, *agents.Manager, string, *Bundle, PublishOptions) (*model.Deployment, bool, error)

// IPv6Enroller enrolls a published Mihomo deployment for IPv6 readiness checks.
type IPv6Enroller func(context.Context, *agents.Manager, string, string, *Source) error

// Options configures a deploy Service instance.
type Options struct {
	Pair     PairPublisher
	IPv6     IPv6Enroller
	SubStore *substore.Service
	Provider *provider.Service
}

// Service owns deployment publication hooks and per-Manager preview stores for
// one server instance.
type Service struct {
	mu       sync.RWMutex
	pair     PairPublisher
	ipv6     IPv6Enroller
	substore *substore.Service
	provider *provider.Service

	previewsMu sync.Mutex
	previews   map[*agents.Manager]*Previews
}

// New constructs a deploy Service with explicit dependencies. A nil Pair
// publisher fails closed with ErrPairPublisherMissing on HA pair targets.
func New(opts Options) *Service {
	return &Service{
		pair:     opts.Pair,
		ipv6:     opts.IPv6,
		substore: opts.SubStore,
		provider: opts.Provider,
		previews: map[*agents.Manager]*Previews{},
	}
}

var (
	defaultSvcMu   sync.RWMutex
	defaultService = New(Options{})
)

// Default returns the process default deploy Service (transitional helper
// until W5/W6/W11 remove package-level wiring).
func Default() *Service {
	defaultSvcMu.RLock()
	defer defaultSvcMu.RUnlock()
	return defaultService
}

// UseService installs svc as the process default deploy Service.
func UseService(svc *Service) {
	if svc == nil {
		return
	}
	defaultSvcMu.Lock()
	defaultService = svc
	defaultSvcMu.Unlock()
}

// Validate reports a required collaborator that server wiring left unset: the
// HA pair publisher (without it every HA member fails closed) and the IPv6
// enrollment (without it publications silently skip IPv6 readiness).
func (s *Service) Validate() error {
	if s.pairPublisher() == nil {
		return ErrPairPublisherMissing
	}
	if s.ipv6Enroller() == nil {
		return ErrIPv6EnrollerMissing
	}
	return nil
}

func (s *Service) pairPublisher() PairPublisher {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pair
}

func (s *Service) ipv6Enroller() IPv6Enroller {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ipv6
}

func (s *Service) setPairPublication(fn PairPublisher) func() {
	s.mu.Lock()
	old := s.pair
	s.pair = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.pair = old
		s.mu.Unlock()
	}
}

func (s *Service) setIPv6Enroll(fn IPv6Enroller) func() {
	s.mu.Lock()
	old := s.ipv6
	s.ipv6 = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.ipv6 = old
		s.mu.Unlock()
	}
}

// PreviewsFor returns the Manager-owned immutable preview store on s.
func (s *Service) PreviewsFor(m *agents.Manager) *Previews {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()
	if s.previews == nil {
		s.previews = map[*agents.Manager]*Previews{}
	}
	if p := s.previews[m]; p != nil {
		return p
	}
	p := NewPreviews(m.Repository().DataDir(), nil)
	if m.OnClose(func() {
		s.previewsMu.Lock()
		delete(s.previews, m)
		s.previewsMu.Unlock()
	}) {
		s.previews[m] = p
	}
	return p
}

// LoadPreview reads the Manager-owned immutable artifact store on s.
func (s *Service) LoadPreview(m *agents.Manager, id, agentID string) (*Preview, error) {
	return s.PreviewsFor(m).Load(id, agentID)
}

// Publish delegates pair members to s's HA pair publisher, failing closed with
// ErrPairPublisherMissing when none is installed.
func (s *Service) Publish(ctx context.Context, m *agents.Manager, id string, b *Bundle, o PublishOptions) (answer *model.Deployment, err error) {
	err = RunDetached(ctx, "agent-publish-"+id, func(ctx context.Context) error {
		nativePair := s.pairPublisher()
		if nativePair != nil {
			r, handled, e := nativePair(ctx, m, id, b, o)
			if e != nil || handled {
				answer = r
				return e
			}
		}

		t, found, e := targetByID(ctx, m, id)
		if e != nil {
			return e
		}
		// Fail closed only without an owner. An installed owner that did not
		// handle the Agent (no complete enabled pair, e.g. during a first enable)
		// leaves it to PublishDirect, whose reservation answers the HA conflict.
		if found && nativePair == nil && pairRequired(t) {
			return ErrPairPublisherMissing
		}
		answer, e = s.publishDirect(ctx, m, id, b, o)
		return e
	})
	return
}

// Push checks current target guards before choosing transactional or legacy publication.
func (s *Service) Push(ctx context.Context, m *agents.Manager, id string, in PushInput) (answer *model.Deployment, err error) {
	err = RunDetached(ctx, "agent-push-"+id, func(ctx context.Context) error {
		t, found, e := targetByID(ctx, m, id)
		if e != nil {
			return e
		}
		if !found {
			answer = result(false, "", "Agent not found", 0)
			return nil
		}
		if (in.ExpectedProfileID != nil && t.ProfileID != *in.ExpectedProfileID) || (in.ExpectedServiceType != nil && t.ServiceType != *in.ExpectedServiceType) {
			answer = result(false, "", msgBindingChanged, 409)
			return nil
		}
		if in.Bundle != nil || t.ServiceType == "mihomo" || t.ServiceType == "mosdns" || t.ServiceType == "oxidns" {
			if in.Bundle == nil {
				answer = result(false, "", "A complete deployment bundle is required", 409)
				return nil
			}
			answer, e = s.Publish(ctx, m, id, in.Bundle, in.Publish)
		} else {
			answer, e = legacyPush(ctx, m, id, in.ConfigContent, in)
		}
		return e
	})
	return
}

// PublishDirect durably submits a complete bundle before transmitting it.
func (s *Service) PublishDirect(ctx context.Context, m *agents.Manager, id string, b *Bundle, o PublishOptions) (answer *model.Deployment, err error) {
	err = RunDetached(ctx, "agent-publish-"+id, func(ctx context.Context) error {
		var e error
		answer, e = s.publishDirect(ctx, m, id, b, o)
		return e
	})
	return
}

func (s *Service) publishDirect(ctx context.Context, m *agents.Manager, id string, b *Bundle, o PublishOptions) (*model.Deployment, error) {
	return publishDirectWith(ctx, m, id, b, o, s.ipv6Enroller())
}
