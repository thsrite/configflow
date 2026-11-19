package agents

// Compatibility codecs for the old registry/API registration entry points.
// Admission and one-use consumption are implemented once in registration.go.
import (
	"context"
	"encoding/json"
)

func legacyRegistrationClaim(data *Record) registrationClaim {
	raw, err := json.Marshal(data)
	if err != nil {
		return registrationClaim{invalid: true}
	}
	return decodeRegistrationClaim(raw)
}

// RegisterAgent translates a legacy registration record into the native DTO.
func (m *Manager) RegisterAgent(ctx context.Context, data *Record, existingToken, installationID any) (*Result, error) {
	token, _ := existingToken.(string)
	installation, _ := installationID.(string)
	return m.registerLegacyIdentity(ctx, data, RegistrationOptions{ExistingToken: token, InstallationID: installation})
}

// RegisterInstallationAgent permits only the trusted exact-identity replay.
func (m *Manager) RegisterInstallationAgent(ctx context.Context, data *Record, existingToken, installationID string) (*Result, error) {
	return m.registerLegacyIdentity(ctx, data, RegistrationOptions{ExistingToken: existingToken, InstallationID: installationID, ReplayInstallation: true})
}

// RegisterAuthorizedAgent enforces first-registration policy inside the registry transaction.
func (m *Manager) RegisterAuthorizedAgent(ctx context.Context, data *Record, existingToken any, admission RegistrationAuthorization) (*Result, error) {
	token, _ := existingToken.(string)
	return m.registerLegacyIdentity(ctx, data, RegistrationOptions{ExistingToken: token, Authorization: &admission})
}

func (m *Manager) registerLegacyIdentity(ctx context.Context, data *Record, options RegistrationOptions) (*Result, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	input, err := DecodeRegistrationInput(raw)
	if err != nil {
		return nil, err
	}
	result, err := m.RegisterIdentity(ctx, input, options)
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var response Result
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	return &response, nil
}
