// Package dnsha owns DNS HA pairing, immutable pair publications and VIP handoff.
// Device transactions and configuration rendering remain owned by deploy.
package dnsha

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// Protocol and production election/handoff deadlines match the Agent contract.
const (
	Protocol       = 1
	SettleTime     = 10 * time.Second
	HandoffTimeout = 30 * time.Second
	PollInterval   = 2 * time.Second
)

// Options supplies the clock and content materializer without changing device timeouts.
type Options struct {
	Now       func() time.Time
	Monotonic func() float64
	Prepare   func(context.Context, *agents.Manager, deploy.Target, *deploy.Source, deploy.PrepareOptions) (*deploy.Prepared, error)
}

// Service is the owner for one manager. All owners of a DATA_DIR share locks.
type Service struct {
	m       *agents.Manager
	now     func() time.Time
	prepare func(context.Context, *agents.Manager, deploy.Target, *deploy.Source, deploy.PrepareOptions) (*deploy.Prepared, error)
	rounds  *agents.IdleRounds
}

var haLocks = lockx.NewKeyed[string](lockx.LevelJob, "oxidns-ha")
var pairLocks = lockx.NewKeyed[string](lockx.LevelJob, "oxidns-ha-pair")

// New constructs an owner without starting work.
func New(m *agents.Manager, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Prepare == nil {
		o.Prepare = deploy.PrepareContent
	}
	return &Service{m: m, now: o.Now, prepare: o.Prepare, rounds: agents.NewIdleRounds(o.Monotonic)}
}

// Error is a safe refusal optionally accompanied by preflight findings.
type Error struct {
	Message  string
	Blockers []string
	CanForce bool
}

func (e *Error) Error() string { return e.Message }

// Unwrap makes all HA safety refusals preparation failures for deploy callers.
func (e *Error) Unwrap() error { return &deploy.Refusal{Message: e.Message} }
func refuse(m string) error    { return &Error{Message: m} }

// IsUserError identifies errors mapped to the HA route's 409 response.
func IsUserError(err error) bool {
	var e *Error
	return errors.As(err, &e) || deploy.IsPreparationFailure(err) || repo.IsValueError(err) || errors.Is(err, agents.ErrPairIncomplete)
}

// HandoffPending keeps the reservation until an uncertain device state is proved.
type HandoffPending struct {
	Message string
	Unknown bool
}

func (e *HandoffPending) Error() string { return e.Message }

// ErrStatusUnavailable keeps an uncertain health probe retryable.
var ErrStatusUnavailable = errors.New(msgStatusUnavailable)
var errHALockBusy = errors.New("HA lock busy")

type agent struct {
	*model.Agent
	target     deploy.Target
	identity   model.HAIdentity
	haRaw      json.RawMessage
	enabled    bool
	hasHAState bool
	dropHA     bool
}

func name(a *agent) string {
	if a == nil {
		return "Agent"
	}
	if a.Name != "" {
		return a.Name
	}
	if a.ID != "" {
		return a.ID
	}
	return "Agent"
}
func haEnabled(a *agent) bool { return a != nil && a.OxiDNSHA != nil && yes(a.OxiDNSHA.Enabled) }
func paired(a, b *agent) bool {
	return haEnabled(a) && haEnabled(b) && a.OxiDNSHA.GroupID != "" && a.OxiDNSHA.GroupID == b.OxiDNSHA.GroupID && a.OxiDNSHA.PeerID == b.ID && b.OxiDNSHA.PeerID == a.ID
}
func yes(p *bool) bool  { return p != nil && *p }
func ptr[T any](v T) *T { return &v }
func childTerminal(st string) bool {
	switch st {
	case "succeeded", "failed", "rolled_back", "rollback_failed":
		return true
	}
	return false
}
func terminal(st string) bool { return st == "succeeded" || st == "failed" || st == "rollback_failed" }
func active(a *agent) bool {
	return a != nil && a.OxiDNSHAPublication != nil && string(rawObject(a.OxiDNSHAPublication)) != "{}" && !terminal(a.OxiDNSHAPublication.Status)
}
func selected(list []*agent, id string) *agent {
	for _, a := range list {
		if a.ID == id {
			return a
		}
	}
	return nil
}

// Spec is the ownership-checked Agent keepalived request.
type Spec struct {
	SafetyVersion int    `json:"safety_version"`
	VIP           string `json:"vip"`
	VRID          int    `json:"vrid"`
	Priority      int    `json:"priority"`
	SelfIP        string `json:"self_ip"`
	PeerIP        string `json:"peer_ip"`
}

func spec(vip string, vrid int, role string, a, b *agent) Spec {
	p := 100
	if role == "primary" {
		p = 150
	}
	return Spec{Protocol, vip, vrid, p, a.Host, b.Host}
}

// MemberView reports only public runtime facts, never managed secrets.
type MemberView struct {
	AgentID string   `json:"agent_id"`
	Name    string   `json:"name"`
	Host    string   `json:"host"`
	Online  bool     `json:"online"`
	Role    *string  `json:"role_config"`
	Runtime *Runtime `json:"runtime"`
	Error   string   `json:"error"`
}

// Runtime is the Agent's current HA health. Extra runtime facts survive public reads.
type Runtime struct {
	VIPPresent bool `json:"vip_present"`
	Running    bool `json:"keepalived_running"`
	Healthy    bool `json:"healthy"`
	VIPHealthy bool `json:"vip_healthy"`
	raw        json.RawMessage
}

// MarshalJSON retains additional Agent runtime facts in the public status response.
func (r Runtime) MarshalJSON() ([]byte, error) {
	if r.raw != nil {
		return r.raw, nil
	}
	type plain Runtime
	return json.Marshal(plain(r))
}

// StatusView omits live health for an unconfigured Agent, as Python does.
type StatusView struct {
	Success     bool                        `json:"success"`
	Config      *model.HASettings           `json:"config"`
	State       *model.HAState              `json:"state,omitempty"`
	Members     []MemberView                `json:"members,omitempty"`
	Health      string                      `json:"health,omitempty"`
	Publication *model.HAPublicationSummary `json:"publication,omitempty"`
	Warnings    []string                    `json:"warnings,omitempty"`
	Routers     []model.HARouter            `json:"routers,omitempty"`
}

// ConfigureView is the durable reservation returned before the pair loop proceeds.
type ConfigureView struct {
	Success     bool                        `json:"success"`
	Config      *model.HASettings           `json:"config"`
	Publication *model.HAPublicationSummary `json:"publication"`
	Warnings    []string                    `json:"warnings"`
	Routers     []model.HARouter            `json:"routers"`
}

// DisableView records any manual cleanup warnings after forced removal.
type DisableView struct {
	Success  bool              `json:"success"`
	Config   *model.HASettings `json:"config"`
	Warnings []string          `json:"warnings"`
	Routers  []model.HARouter  `json:"routers"`
}

// EnvironmentView is a bounded, sanitized live capability projection.
type EnvironmentView struct {
	Success            bool              `json:"success"`
	AgentID            string            `json:"agent_id,omitempty"`
	ServiceType        string            `json:"service_type,omitempty"`
	Message            string            `json:"message,omitempty"`
	Protocols          []int             `json:"oxidns_ha_protocols,omitempty"`
	ContainerProtocols []int             `json:"oxidns_ha_container_protocols,omitempty"`
	HandoffProtocols   []int             `json:"oxidns_ha_handoff_protocols,omitempty"`
	Environment        *EnvironmentProof `json:"oxidns_ha_environment,omitempty"`
}

// EnvironmentProof contains only installation-guide fields.
type EnvironmentProof struct {
	Kind         string   `json:"kind"`
	Ready        bool     `json:"ready"`
	Network      string   `json:"network"`
	HostID       string   `json:"host_id"`
	HostIDSource string   `json:"host_id_source"`
	HostBootID   string   `json:"host_boot_id"`
	Interface    string   `json:"interface"`
	Blockers     []string `json:"blockers"`
}

type payload struct {
	ID         string          `json:"id"`
	Source     json.RawMessage `json:"source_snapshot"`
	Resource   json.RawMessage `json:"resource_config"`
	Revision   json.RawMessage `json:"config_revision,omitempty"`
	BaseURL    string          `json:"base_url"`
	BaselineID string          `json:"baseline_deployment_id,omitempty"`
	Bundle     *bundleMeta     `json:"bundle,omitempty"`
}
type bundleMeta struct {
	Manifest      deploy.Manifest `json:"manifest"`
	SHA256        string          `json:"sha256"`
	ConfigVersion string          `json:"config_version"`
	Comparison    *deploy.File    `json:"comparison_config"`
}
