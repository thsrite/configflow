package subs

import (
	"configflow/internal/logx"
	"configflow/internal/model"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// UnrecognizedCoreVersionWarnInterval limits unrecognized core-version warnings per Agent.
const UnrecognizedCoreVersionWarnInterval = 10 * time.Minute

const maxCoreVersionWarnEntries = 4096

var (
	coreStatusLogger = logx.Get("backend.utils.proxy_server_dns")
	coreWarnMu       sync.Mutex
	coreWarnLast     = map[string]time.Time{}
)

// ResetCoreVersionWarnState clears per-Agent warning throttle state for tests.
func ResetCoreVersionWarnState() {
	coreWarnMu.Lock()
	coreWarnLast = map[string]time.Time{}
	coreWarnMu.Unlock()
}

// WarnUnrecognizedCoreVersion logs a per-Agent rate-limited warning when a
// non-empty reported Mihomo core version cannot be parsed. Callers treat
// unrecognized versions identically to an empty/unknown version.
func WarnUnrecognizedCoreVersion(agentID, agentName, version string, at time.Time) bool {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return false
	}
	if _, ok := CoreVersion(trimmed); ok {
		return false
	}
	key := agentID
	if key == "" {
		key = agentName
	}
	coreWarnMu.Lock()
	if last, seen := coreWarnLast[key]; seen && at.Sub(last) < UnrecognizedCoreVersionWarnInterval && at.Sub(last) >= 0 {
		coreWarnMu.Unlock()
		return false
	}
	if len(coreWarnLast) >= maxCoreVersionWarnEntries {
		for k, last := range coreWarnLast {
			if at.Sub(last) >= UnrecognizedCoreVersionWarnInterval || at.Sub(last) < 0 {
				delete(coreWarnLast, k)
			}
		}
		if len(coreWarnLast) >= maxCoreVersionWarnEntries {
			coreWarnMu.Unlock()
			return false
		}
	}
	coreWarnLast[key] = at
	coreWarnMu.Unlock()
	label := coreAgentLabel(agentID, agentName)
	coreStatusLogger.Warningf(msgUnrecognizedCoreVersion, label, logx.LogText(trimmed, 64))
	return true
}

func coreAgentLabel(id, name string) string {
	cleanID := logx.LogText(id, 64)
	cleanName := logx.LogText(name, 64)
	switch {
	case cleanName != "" && cleanID != "" && cleanName != cleanID:
		return cleanName + " (" + cleanID + ")"
	case cleanName != "":
		return cleanName
	case cleanID != "":
		return cleanID
	default:
		return "<unknown>"
	}
}

// CoreAgent is the consumed discovery binding. A missing service type defaults
// to Mihomo; an explicitly empty service type does not select a core.
type CoreAgent struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	ProfileID        string  `json:"profile_id"`
	ServiceType      *string `json:"service_type"`
	DiscoveryEnabled *bool   `json:"domain_discovery_enabled"`
}

type coreAgentWire CoreAgent

// UnmarshalJSON reads a discovery binding leniently.
func (a *CoreAgent) UnmarshalJSON(b []byte) error { return model.DecodeLenient(b, (*coreAgentWire)(a)) }

// CoreStatus contains the reported core version, without retaining traffic samples.
type CoreStatus struct {
	MihomoVersion string `json:"mihomo_version"`
}

type coreStatusWire CoreStatus

// UnmarshalJSON reads reported status leniently.
func (s *CoreStatus) UnmarshalJSON(b []byte) error {
	return model.DecodeLenient(b, (*coreStatusWire)(s))
}

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
