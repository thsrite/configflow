package agents

import "errors"

// InstallationError is gateway_installation.InstallationError (#30).
type InstallationError struct {
	Message string
	Status  int
}

func (e *InstallationError) Error() string { return e.Message }

// ErrNotPorted is returned where the registry would call a later package
// whose collaborator is not wired.
var ErrNotPorted = errors.New("agents: the package providing this operation is not ported yet")

// AgentDeletedStores are the per-Agent stores DELETE /api/agents/<id> clears
// after the registry deletes the Agent, in order:
//
//	traffic_store  get_traffic_store().delete   A05 (traffic history and traffic store)
//	domain_probe   get_probe_store().delete     C16 (domain probe)
//	region_check   get_region_store().delete    C16 (region check)
var AgentDeletedStores = []string{"traffic_store", "domain_probe", "region_check"}
