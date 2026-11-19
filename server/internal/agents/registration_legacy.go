package agents

// Compatibility codecs for the old registry/API registration entry points.
// Admission and one-use consumption are implemented once in registration.go.
import (
	"context"
	"encoding/json"

	"configflow/internal/model"
)

func legacyRegistrationClaim(data *Record) registrationClaim {
	claim := registrationClaim{ServiceType: "mihomo", Port: 8080}
	raw, err := json.Marshal(data)
	if err != nil {
		claim.invalid = true
		return claim
	}
	var wire struct {
		Name             *string    `json:"name"`
		Host             *string    `json:"host"`
		ServiceType      *string    `json:"service_type"`
		DeploymentMethod *string    `json:"deployment_method"`
		ProfileID        *string    `json:"profile_id"`
		Port             *model.Int `json:"port"`
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil || members == nil {
		claim.invalid = true
		return claim
	}
	// encoding/json matches struct tags case-insensitively. Project exact keys
	// first, retaining the registry protocol's case-sensitive field names.
	selected := make(map[string]json.RawMessage)
	for _, key := range []string{"name", "host", "service_type", "deployment_method", "profile_id", "port"} {
		if value, present := members[key]; present {
			selected[key] = value
		}
	}
	payload, err := json.Marshal(selected)
	if err != nil || json.Unmarshal(payload, &wire) != nil {
		claim.invalid = true
	}
	for _, field := range []struct {
		key         string
		value       *string
		destination *string
	}{
		{"name", wire.Name, &claim.Name}, {"host", wire.Host, &claim.Host}, {"service_type", wire.ServiceType, &claim.ServiceType},
		{"deployment_method", wire.DeploymentMethod, &claim.DeploymentMethod}, {"profile_id", wire.ProfileID, &claim.ProfileID},
	} {
		if field.value != nil {
			*field.destination = *field.value
		} else if _, present := members[field.key]; present {
			claim.invalid = true
		}
	}
	if wire.Port != nil {
		claim.Port = *wire.Port
	}
	if raw, present := members["port"]; present {
		var number *float64
		if json.Unmarshal(raw, &number) != nil || number == nil {
			claim.invalid = true
		}
	}
	return claim
}

// RegisterAuthorizedAgent enforces first-registration policy inside the registry transaction.
func (m *Manager) RegisterAuthorizedAgent(ctx context.Context, data *Record, existingToken any, admission RegistrationAuthorization) (*Result, error) {
	return m.registerAgent(ctx, data, existingToken, nil, false, &admission)
}

func (m *Manager) proofMatches(ctx context.Context, system *model.System, data *Record, admission *RegistrationAuthorization) bool {
	return m.matchesRegistrationClaim(ctx, system, legacyRegistrationClaim(data), *admission)
}

func (m *Manager) admitRegistration(ctx context.Context, system *model.System, data *Record, admission *RegistrationAuthorization) error {
	profile, err := m.admitRegistrationClaim(ctx, system, legacyRegistrationClaim(data), *admission)
	if err == nil && profile != "" {
		data.Set("profile_id", profile)
	}
	return err
}
