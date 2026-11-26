package discovery

import "encoding/json"

// SettingsView is the discovery editor's selected rules and eligible resources.
type SettingsView struct {
	Direct            *RulesetInfo        `json:"direct"`
	Proxy             *RulesetInfo        `json:"proxy"`
	Ignored           []string            `json:"ignored"`
	AutoProbe         bool                `json:"auto_probe"`
	AutoApply         bool                `json:"auto_apply"`
	Confidence        int                 `json:"auto_apply_min_confidence"`
	ConfidenceChoices []int               `json:"confidence_choices"`
	ProbePath         *string             `json:"probe_path"`
	RegionEnabled     bool                `json:"region_check_enabled"`
	RegionPort        int                 `json:"region_probe_port"`
	Candidates        []map[string]string `json:"candidates"`
	Policies          []string            `json:"policies"`
}

// AgentsView describes the collectors and their minimum supported versions.
type AgentsView struct {
	Agents               []AgentStatus `json:"agents"`
	MinAgentVersion      string        `json:"min_agent_version"`
	MinProbeAgentVersion string        `json:"min_probe_agent_version"`
}

// BusyView hides another profile's job while retaining the conflict response.
type BusyView struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Job     *Job   `json:"job"`
}

// AgentIdentity is the safe identity of the selected region collector.
type AgentIdentity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RegionView is the newest persisted region result and available routing targets.
type RegionView struct {
	Enabled          bool                       `json:"enabled"`
	Port             int                        `json:"port"`
	Services         json.RawMessage            `json:"services"`
	Result           json.RawMessage            `json:"result"`
	Agent            *AgentIdentity             `json:"agent"`
	Domains          map[string]json.RawMessage `json:"domains"`
	Policies         []string                   `json:"policies"`
	Ready            bool                       `json:"ready"`
	MinAgentVersion  string                     `json:"min_agent_version"`
	Job              *Job                       `json:"job"`
	RegionalRulesets map[string]*RulesetInfo    `json:"regional_rulesets"`
}
