package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
)

// ErrHARulesAlreadySet rejects a second SetHARules call on a monitor Service.
var ErrHARulesAlreadySet = errors.New("monitor: HA rules owner is already set")

// ErrServiceStarted rejects two-phase setter calls after startup wiring completes.
var ErrServiceStarted = errors.New("monitor: service already started")

// Service owns OxiDNS monitoring, publication statistics, and ranking history
// collection for one server instance.
type Service struct {
	mu         sync.RWMutex
	dataDir    string
	haRules    HARulesJSONFunc
	haRulesSet bool
	started    bool
}

// New constructs a monitor Service for dataDir.
func New(dataDir string) *Service {
	return &Service{dataDir: dataDir}
}

// SetHARules installs the HA pair-rule owner in the two-phase server
// construction step (O17 D1). It may be called at most once and only before MarkStarted.
func (s *Service) SetHARules(fn HARulesJSONFunc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		publicationLogger.Errorf("monitor.SetHARules: %v", ErrServiceStarted)
		return ErrServiceStarted
	}
	if s.haRulesSet {
		publicationLogger.Errorf("monitor.SetHARules: %v", ErrHARulesAlreadySet)
		return ErrHARulesAlreadySet
	}
	s.haRules = fn
	s.haRulesSet = fn != nil
	return nil
}

// MarkStarted seals two-phase setters on s.
func (s *Service) MarkStarted() {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
}

// PairRules dispatches an HA rule update, failing closed with ErrHARulesNotPorted
// when no HA rules owner was wired.
func (s *Service) PairRules(ctx context.Context, m Registry, id, method string) (json.RawMessage, int, error) {
	s.mu.RLock()
	fn := s.haRules
	s.mu.RUnlock()
	if fn == nil {
		return nil, 0, ErrHARulesNotPorted
	}
	return fn(ctx, m, id, method)
}

// PublicationComplete records publication completion statistics.
func (s *Service) PublicationComplete(ctx context.Context, m *agents.Manager, agentID, deploymentID string, status any) error {
	return completeHook(ctx, m, agentID, deploymentID, status)
}

// PublicationCheckpoint records publication checkpoint statistics.
func (s *Service) PublicationCheckpoint(ctx context.Context, m *agents.Manager, agentID, deploymentID string, configSHA256 any) error {
	return checkpointHook(ctx, m, agentID, deploymentID, configSHA256)
}

// Loop returns the periodic OxiDNS ranking history collection loop.
func (s *Service) Loop() lifecycle.Periodic { return rankingPeriodic() }
