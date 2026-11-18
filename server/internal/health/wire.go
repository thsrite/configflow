package health

import (
	"context"
	"path/filepath"

	"configflow/internal/agents"
	"configflow/internal/health/check"
	"configflow/internal/repo"
)

// GetProbeStore returns the ProbeStore rooted at DATA_DIR/probes.
func GetProbeStore(ctx context.Context) (*check.ProbeStore, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return check.NewProbeStore(filepath.Join(repository.DataDir(), "probes")), nil
}

// GetRegionStore returns the RegionStore rooted at DATA_DIR/regions.
func GetRegionStore(ctx context.Context) (*check.RegionStore, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return check.NewRegionStore(filepath.Join(repository.DataDir(), "regions")), nil
}

func deleteProbeStore(ctx context.Context, agentID string) error {
	store, err := GetProbeStore(ctx)
	if err != nil {
		return err
	}
	return store.Delete(agentID)
}

func deleteRegionStore(ctx context.Context, agentID string) error {
	store, err := GetRegionStore(ctx)
	if err != nil {
		return err
	}
	return store.Delete(agentID)
}

// Wire registers Agent deletion cleanup hooks for domain probes and region checks.
func Wire() {
	agents.OnAgentDeleted("domain_probe", deleteProbeStore)
	agents.OnAgentDeleted("region_check", deleteRegionStore)
}
