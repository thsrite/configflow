package trafficstore

import (
	"context"
	"path/filepath"

	"configflow/internal/lifecycle"
)

// Service owns the traffic store rooted at a single data directory.
type Service struct {
	dataDir string
	store   *NativeStore
}

// NewService constructs a trafficstore Service for dataDir.
func NewService(dataDir string) *Service {
	return &Service{
		dataDir: dataDir,
		store:   NewNative(filepath.Join(dataDir, "traffic")),
	}
}

// Store returns the per-Agent traffic NativeStore owned by s.
func (s *Service) Store() *NativeStore { return s.store }

// DeleteAgent removes an Agent's persisted traffic store.
func (s *Service) DeleteAgent(ctx context.Context, agentID string) error {
	return s.store.Delete(lifecycle.Detach(ctx), agentID)
}
