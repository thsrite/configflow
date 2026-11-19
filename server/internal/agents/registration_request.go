package agents

import (
	"configflow/internal/model"
	"encoding/json"
)

func decodeRegistrationClaim(raw json.RawMessage) registrationClaim {
	claim := registrationClaim{ServiceType: "mihomo", Port: 8080}
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
		} else if raw, present := members[field.key]; present {
			if field.key != "profile_id" || !storedAbsent(raw, true, nil) {
				claim.invalid = true
			}
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
