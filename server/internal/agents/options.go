package agents

import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/model"
)

// ErrPairPublisherAlreadySet rejects a second SetPairPublisher call on a Manager.
var ErrPairPublisherAlreadySet = errors.New("agents: pair publisher is already set")

// ErrManagerStarted rejects two-phase setter calls after Manager startup wiring completes.
var ErrManagerStarted = errors.New("agents: manager already started")

// MetricsSink receives reported heartbeat metrics and cleans up deleted Agent histories.
type MetricsSink interface {
	AddMetricsJSON(ctx context.Context, m *Manager, agentID string, sample json.RawMessage) error
	DeleteMetricsHistory(ctx context.Context, m *Manager, agentID string) error
	DeleteTrafficHistory(ctx context.Context, m *Manager, agentID string) error
}

// PublicationObserver tracks OxiDNS publication checkpoints and completions.
type PublicationObserver interface {
	PublicationComplete(ctx context.Context, m *Manager, agentID, deploymentID string, status any) error
	PublicationCheckpoint(ctx context.Context, m *Manager, agentID, deploymentID string, configSHA256 any) error
}

// PairPublisher handles OxiDNS HA pair publication state, activation, and
// service operation fencing. When nil on a Manager, active pair publications
// fail closed with ErrHAOwnerMissing.
type PairPublisher interface {
	PairDeployment(ctx context.Context, agentID, deploymentID string) (*model.Deployment, error)
	ActivatePair(ctx context.Context, agentID, deploymentID string) (*model.Deployment, error)
	ServiceOperationJSON(ctx context.Context, agentID string, operation func() (json.RawMessage, error), uninstall bool) (json.RawMessage, error)
}

// NamedDeleter clears a per-Agent auxiliary store after Agent deletion.
type NamedDeleter struct {
	Name   string
	Delete func(ctx context.Context, agentID string) error
}

// Options configures collaborators on a Manager. Nil fields preserve the exact
// fail-closed or default behavior (nil Pair -> ErrHAOwnerMissing on active HA;
// nil ReceiptMatches -> false; nil Metrics/Publication -> no-op).
type Options struct {
	Metrics        MetricsSink
	Publication    PublicationObserver
	ReceiptMatches func(*model.Agent, *model.Deployment) bool
	Pair           PairPublisher
	DeletedStores  []NamedDeleter
	Client         Client
	RuntimeState   *RuntimeState
}

// SetPairPublisher installs the HA pair publisher in the two-phase server
// construction step (O17 D1). It may be called at most once and only before MarkStarted.
func (m *Manager) SetPairPublisher(p PairPublisher) error {
	m.optsMu.Lock()
	defer m.optsMu.Unlock()
	if m.started {
		logger.Errorf("agents.SetPairPublisher: %v", ErrManagerStarted)
		return ErrManagerStarted
	}
	if m.pairSet {
		logger.Errorf("agents.SetPairPublisher: %v", ErrPairPublisherAlreadySet)
		return ErrPairPublisherAlreadySet
	}
	m.pair = p
	m.pairSet = p != nil
	return nil
}

// MarkStarted seals two-phase setters on m.
func (m *Manager) MarkStarted() {
	m.optsMu.Lock()
	m.started = true
	m.optsMu.Unlock()
}

func (m *Manager) pairPublisher() PairPublisher {
	m.optsMu.RLock()
	defer m.optsMu.RUnlock()
	return m.pair
}

// DeletedStores returns the ordered per-Agent auxiliary store deleters wired
// onto m.
func (m *Manager) DeletedStores() []NamedDeleter {
	if m == nil || len(m.deletedStores) == 0 {
		return nil
	}
	out := make([]NamedDeleter, len(m.deletedStores))
	copy(out, m.deletedStores)
	return out
}

// NotificationAgents returns the listing records (live heartbeat overlay and
// offline status included) without deployment/IPv6 histories for notifications.
func (m *Manager) NotificationAgents(ctx context.Context) ([]model.Agent, error) {
	return SnapshotAgents(ctx, m)
}

// UseManager installs m as the process Manager returned by Get(ctx) when the
// context's repository matches m.Repository().
func UseManager(m *Manager) {
	singletonMu.Lock()
	old := singleton
	singleton = m
	singletonMu.Unlock()
	if old != nil && old != m {
		old.Close()
	}
}
