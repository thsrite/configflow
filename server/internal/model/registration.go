package model

import ()

// AgentRegistrationSession is a Go-native one-use installation capability.
// Only the SHA-256 digest is persisted; successful registration deletes it in
// the same transaction that creates the Agent.
type AgentRegistrationSession struct {
	Hash             string `json:"hash,omitzero" stored:"secret"`
	Name             string `json:"name,omitzero"`
	Host             string `json:"host,omitzero"`
	Port             Int    `json:"port,omitzero"`
	ServiceType      string `json:"service_type,omitzero"`
	DeploymentMethod string `json:"deployment_method,omitzero"`
	ProfileID        string `json:"profile_id,omitzero"`
	Expires          Int    `json:"expires,omitzero"`
}

// Plain-record codecs (O18 T1b); see plainCodec.

type agentRegistrationSessionWire AgentRegistrationSession

func (*AgentRegistrationSession) plainRecord() {}

// UnmarshalJSON decodes stored or captured AgentRegistrationSession data leniently (DecodeStored).
func (r *AgentRegistrationSession) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*agentRegistrationSessionWire)(r))
}

func (r *AgentRegistrationSession) plainWire() any { return (*agentRegistrationSessionWire)(r) }

// MarshalJSON writes the AgentRegistrationSession members in declaration order.
func (r AgentRegistrationSession) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }
