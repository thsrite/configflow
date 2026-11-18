package model

// AgentRegistrationSession is a Go-native one-use installation capability.
// Only the SHA-256 digest is persisted; successful registration deletes it in
// the same transaction that creates the Agent.
type AgentRegistrationSession struct {
	Hash             string `json:"hash"`
	Name             string `json:"name"`
	Host             string `json:"host"`
	Port             Int    `json:"port"`
	ServiceType      string `json:"service_type"`
	DeploymentMethod string `json:"deployment_method"`
	ProfileID        string `json:"profile_id"`
	Expires          Int    `json:"expires"`
	Fields           Fields
	raw              Raw
}

// UnmarshalJSON decodes a stored capability with the shared field-preservation codec.
func (s *AgentRegistrationSession) UnmarshalJSON(data []byte) error {
	return DecodeRecord(data, s, &s.raw)
}

// MarshalJSON preserves unknown capability metadata.
func (s AgentRegistrationSession) MarshalJSON() ([]byte, error) { return EncodeRecord(&s, &s.raw) }
