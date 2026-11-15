package health

import (
	"context"
	"path/filepath"

	"configflow/internal/agents"
	"configflow/internal/health/check"
	"configflow/internal/repo"
)

// GetProbeStore is domain_probe.get_probe_store(): DATA_DIR/probes.
func GetProbeStore(ctx context.Context) (*check.ProbeStore, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return check.NewProbeStore(filepath.Join(repository.DataDir(), "probes")), nil
}

// GetRegionStore is region_check.get_region_store(): DATA_DIR/regions.
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

// Wire is C16's entry in internal/server wirePackages: the DELETE
// /api/agents/<id> route clears the Agent's domain probe store, then its
// region check store (get_probe_store().delete, get_region_store().delete,
// after the traffic store). C16 owns no loops and no task rows.
func Wire() {
	agents.OnAgentDeleted("domain_probe", deleteProbeStore)
	agents.OnAgentDeleted("region_check", deleteRegionStore)
}
