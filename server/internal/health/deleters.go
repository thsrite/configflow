package health

import "configflow/internal/agents"

// DeletedStores returns the ordered per-Agent store deleters owned by health
// (domain_probe, region_check).
func DeletedStores() []agents.NamedDeleter {
	return []agents.NamedDeleter{
		{Name: "domain_probe", Delete: deleteProbeStore},
		{Name: "region_check", Delete: deleteRegionStore},
	}
}
