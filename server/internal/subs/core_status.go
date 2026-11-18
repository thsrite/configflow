package subs

import (
	"configflow/internal/model"
	"encoding/json"
	"strings"
)

// CoreAgent is the consumed discovery binding. A missing service type defaults
// to Mihomo; an explicitly empty service type does not select a core.
type CoreAgent struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	ProfileID        string  `json:"profile_id"`
	ServiceType      *string `json:"service_type"`
	DiscoveryEnabled *bool   `json:"domain_discovery_enabled"`
	Fields           model.Fields
	Raw              model.Raw
}

// UnmarshalJSON reads discovery bindings with the shared stored-record policy.
func (a *CoreAgent) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, a, &a.Raw) }

// CoreStatus contains the reported core version, without retaining traffic samples.
type CoreStatus struct {
	MihomoVersion string `json:"mihomo_version"`
	Fields        model.Fields
	Raw           model.Raw
}

// UnmarshalJSON reads reported status with the shared stored-record policy.
func (s *CoreStatus) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, s, &s.Raw) }

// CoreStatusLoader reads one agent's status using its owning store.
type CoreStatusLoader func(agentID string) CoreStatus

// CoreReport identifies a profile's agent running an unsupported Mihomo version.
type CoreReport struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// AgentCoreVersion returns a discovery-enabled Mihomo agent's current core version.
func AgentCoreVersion(agent CoreAgent, statusOf CoreStatusLoader) string {
	if (agent.ServiceType != nil && *agent.ServiceType != "mihomo") || agent.DiscoveryEnabled == nil || !*agent.DiscoveryEnabled || agent.ID == "" || statusOf == nil {
		return ""
	}
	return strings.TrimSpace(statusOf(agent.ID).MihomoVersion)
}

// OldCoreAgents returns unsupported reported core versions in discovery order.
func OldCoreAgents(agents []CoreAgent, profileID, defaultID string, statusOf CoreStatusLoader) []CoreReport {
	found := []CoreReport{}
	for _, agent := range agents {
		pid := agent.ProfileID
		if pid == "" {
			pid = defaultID
		}
		if pid != profileID {
			continue
		}
		version := AgentCoreVersion(agent, statusOf)
		if version != "" && IsCoreTooOld(version) {
			name := agent.Name
			if name == "" {
				name = agent.ID
			}
			found = append(found, CoreReport{name, version})
		}
	}
	return found
}

// DecodeCoreStatus decodes a traffic owner's raw JSON boundary without loading configuration.
func DecodeCoreStatus(data json.RawMessage) (CoreStatus, error) {
	var out CoreStatus
	err := json.Unmarshal(data, &out)
	return out, err
}
