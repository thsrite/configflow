// Package dnsfollow owns opt-in Mihomo-to-OxiDNS binding and publication
// reconciliation. It delegates rendering and device transactions to deploy.
package dnsfollow

import (
	"context"
	"encoding/json"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
)

// PollInterval is the production reconciliation interval.
const PollInterval = 5 * time.Second

// Options configures the clock, notifications service, and normal A14 preparation boundary.
type Options struct {
	Now           func() time.Time
	Prepare       func(context.Context, *agents.Manager, deploy.Target, *deploy.Source, deploy.PrepareOptions) (*deploy.Prepared, error)
	Notifications *notifications.Service
}

// Service owns follow reconciliation for one registry manager.
type Service struct {
	m             *agents.Manager
	Deploy        *deploy.Service
	notifications *notifications.Service
	now           func() time.Time
	prepare       func(context.Context, *agents.Manager, deploy.Target, *deploy.Source, deploy.PrepareOptions) (*deploy.Prepared, error)
	waker         *agents.Waker
}

func (s *Service) deployService() *deploy.Service {
	if s != nil && s.Deploy != nil {
		return s.Deploy
	}
	return deploy.New(deploy.Options{})
}

var followLocks = lockx.NewKeyed[string](lockx.LevelJob, "dns-follow")

// New constructs an owner without starting a loop or touching the network.
func New(m *agents.Manager, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	w := agents.NewWaker(PollInterval, agents.IdleFallback)
	if m != nil {
		w.Watch(m, repo.ChangeConfig|repo.ChangeState, true)
	}
	s := &Service{m: m, notifications: o.Notifications, now: o.Now, prepare: o.Prepare, waker: w}
	if s.prepare == nil {
		s.prepare = func(ctx context.Context, m *agents.Manager, t deploy.Target, src *deploy.Source, opt deploy.PrepareOptions) (*deploy.Prepared, error) {
			return s.deployService().PrepareContent(ctx, m, t, src, opt)
		}
	}
	return s
}

// Error is a safe user-facing follow refusal.
type Error struct{ Message string }

func (e *Error) Error() string     { return e.Message }
func refusal(message string) error { return &Error{message} }

// SourceView exposes only a selectable source's public identity and suggested host.
type SourceView struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	SuggestedAddress string `json:"suggested_address"`
}

// UpstreamView identifies one selectable ordinary DNS upstream.
type UpstreamView struct {
	Tag     string `json:"tag"`
	Index   int    `json:"index"`
	Address string `json:"address"`
}

// OptionsView is the binding editor; private jobs and deployment proofs are absent.
type OptionsView struct {
	Success   bool            `json:"success"`
	Binding   json.RawMessage `json:"binding"`
	State     json.RawMessage `json:"state"`
	Sources   []SourceView    `json:"sources"`
	Upstreams []UpstreamView  `json:"upstreams"`
}

// DisabledView confirms that all follow ownership has been removed.
type DisabledView struct {
	Success bool                    `json:"success"`
	Binding *model.DNSFollowBinding `json:"binding"`
	State   *model.DNSFollowState   `json:"state"`
}

// RetryView confirms a durable request to retry a failed follow.
type RetryView struct {
	Success bool                  `json:"success"`
	State   *model.DNSFollowState `json:"state"`
}

type agent struct {
	*model.Agent
	target                          deploy.Target
	bindingRaw, stateRaw            json.RawMessage
	isEnabled                       bool
	dropBinding, dropState, dropJob bool
}

func terminal(status string) bool {
	return status == "succeeded" || status == "failed" || status == "rolled_back"
}
func running(status string) bool { return status == "preparing" || status == "publishing" }
func value[T any](p *T) (z T) {
	if p != nil {
		return *p
	}
	return
}
func latest(a *agent) model.Deployment {
	if a.LatestDeployment != nil {
		return *a.LatestDeployment
	}
	return model.Deployment{}
}
func state(a *agent) model.DNSFollowState {
	if a.DNSFollowState != nil {
		return *a.DNSFollowState
	}
	return model.DNSFollowState{}
}
func name(a *agent) string {
	if a.Name != "" {
		return a.Name
	}
	return a.ID
}
func selected(list []*agent, id string) *agent {
	for _, a := range list {
		if a.ID == id {
			return a
		}
	}
	return nil
}
func enabled(a *agent) bool { return a != nil && a.isEnabled }
func listed(list []*agent, id, service string) (*agent, error) {
	a := selected(list, id)
	if !enabled(a) || a.ServiceType != service {
		return nil, refusal(service + msgMissingAgent)
	}
	return a, nil
}

type statePatch struct {
	sourceID, targetID, address *string
	clearTarget                 bool
	attempt                     *model.Int
	retry, draftSynced          *bool
}
