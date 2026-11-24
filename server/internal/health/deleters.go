package health

import (
	"context"
	"path/filepath"

	"configflow/internal/agents"
	"configflow/internal/health/check"
	"configflow/internal/lifecycle"
)

// ProbeStore returns the ProbeStore rooted at dataDir/probes.
func ProbeStore(dataDir string) *check.ProbeStore {
	return check.NewProbeStore(filepath.Join(dataDir, "probes"))
}

// RegionStore returns the RegionStore rooted at dataDir/regions.
func RegionStore(dataDir string) *check.RegionStore {
	return check.NewRegionStore(filepath.Join(dataDir, "regions"))
}

// DeletedStores returns the ordered per-Agent store deleters owned by health
// (domain_probe, region_check) rooted at dataDir.
func DeletedStores(dataDir string) []agents.NamedDeleter {
	probes := ProbeStore(dataDir)
	regions := RegionStore(dataDir)
	return []agents.NamedDeleter{
		{Name: "domain_probe", Delete: func(ctx context.Context, agentID string) error {
			return probes.Delete(lifecycle.Detach(ctx), agentID)
		}},
		{Name: "region_check", Delete: func(ctx context.Context, agentID string) error {
			return regions.Delete(lifecycle.Detach(ctx), agentID)
		}},
	}
}
