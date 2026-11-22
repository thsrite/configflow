package model

import ()

// RuleSyncSettings selects library refresh and device-rule refresh independently.
type RuleSyncSettings struct {
	Library *bool `json:"library,omitzero"`
	Rules   *bool `json:"rules,omitzero"`
}

// AgentRuleSync holds explicit rule-sync settings and the last durable worker state.
type AgentRuleSync struct {
	Settings RuleSyncSettings `json:"settings,omitzero"`
	State    *RuleSyncState   `json:"state,omitzero"`
}

// RuleSyncState preserves worker ownership and retry state; NativeTask is a task ID.
type RuleSyncState struct {
	Status       string   `json:"status,omitzero"`
	Message      string   `json:"message,omitzero"`
	UpdatedAt    string   `json:"updated_at,omitzero"`
	DeploymentID *string  `json:"deployment_id,omitzero"`
	Key          *string  `json:"key,omitzero"`
	NoticedAt    *float64 `json:"noticed_at,omitzero"`
	RetryAt      *float64 `json:"retry_at,omitzero"`
	ForceRefresh *bool    `json:"force_refresh,omitzero"`
	NativeTask   *string  `json:"native_task,omitzero"`
}

// Plain-record codecs (O18 T1b); see plainCodec.

type ruleSyncSettingsWire RuleSyncSettings

func (*RuleSyncSettings) plainRecord() {}

// UnmarshalJSON decodes stored or captured RuleSyncSettings data leniently (DecodeStored).
func (r *RuleSyncSettings) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*ruleSyncSettingsWire)(r))
}

func (r *RuleSyncSettings) plainWire() any { return (*ruleSyncSettingsWire)(r) }

// MarshalJSON writes the RuleSyncSettings members in declaration order.
func (r RuleSyncSettings) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type agentRuleSyncWire AgentRuleSync

func (*AgentRuleSync) plainRecord() {}

// UnmarshalJSON decodes stored or captured AgentRuleSync data leniently (DecodeStored).
func (r *AgentRuleSync) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*agentRuleSyncWire)(r))
}

func (r *AgentRuleSync) plainWire() any { return (*agentRuleSyncWire)(r) }

// MarshalJSON writes the AgentRuleSync members in declaration order.
func (r AgentRuleSync) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type ruleSyncStateWire RuleSyncState

func (*RuleSyncState) plainRecord() {}

// UnmarshalJSON decodes stored or captured RuleSyncState data leniently (DecodeStored).
func (r *RuleSyncState) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*ruleSyncStateWire)(r))
}

func (r *RuleSyncState) plainWire() any { return (*ruleSyncStateWire)(r) }

// MarshalJSON writes the RuleSyncState members in declaration order.
func (r RuleSyncState) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }
