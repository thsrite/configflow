package model

// RuleSyncSettings selects library refresh and device-rule refresh independently.
type RuleSyncSettings struct {
	Library *bool `json:"library"`
	Rules   *bool `json:"rules"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON decodes RuleSyncSettings with stored-member preservation and R5 fallback.
func (r *RuleSyncSettings) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged RuleSyncSettings members.
func (r RuleSyncSettings) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// AgentRuleSync holds explicit rule-sync settings and the last durable worker state.
type AgentRuleSync struct {
	Settings RuleSyncSettings `json:"settings"`
	State    *RuleSyncState   `json:"state"`
	Fields   Fields
	raw      Raw
}

// UnmarshalJSON decodes AgentRuleSync with stored-member preservation and R5 fallback.
func (r *AgentRuleSync) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged AgentRuleSync members.
func (r AgentRuleSync) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// RuleSyncState preserves worker ownership and retry state; NativeTask is a task ID.
type RuleSyncState struct {
	Status       string   `json:"status"`
	Message      string   `json:"message"`
	UpdatedAt    string   `json:"updated_at"`
	DeploymentID *string  `json:"deployment_id"`
	Key          *string  `json:"key"`
	NoticedAt    *float64 `json:"noticed_at"`
	RetryAt      *float64 `json:"retry_at"`
	ForceRefresh *bool    `json:"force_refresh"`
	NativeTask   *string  `json:"native_task"`
	Fields       Fields
	raw          Raw
}

// UnmarshalJSON decodes RuleSyncState with stored-member preservation and R5 fallback.
func (r *RuleSyncState) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged RuleSyncState members.
func (r RuleSyncState) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }
