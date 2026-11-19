package agents

import (
	"encoding/json"

	"configflow/internal/model"
)

func decodeRegistrationClaim(raw json.RawMessage) registrationClaim {
	claim := registrationClaim{ServiceType: "mihomo", Port: 8080}
	var members model.Fields
	if json.Unmarshal(raw, &members) != nil {
		claim.invalid = true
		return claim
	}
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"name", &claim.Name}, {"host", &claim.Host}, {"service_type", &claim.ServiceType},
		{"deployment_method", &claim.DeploymentMethod}, {"profile_id", &claim.ProfileID},
	} {
		value, present := members.Get(field.name)
		if !present {
			continue
		}
		var text *string
		if json.Unmarshal(value, &text) != nil || text == nil {
			if field.name != "profile_id" || !storedAbsent(value, present, nil) {
				claim.invalid = true
			}
			continue
		}
		*field.target = *text
	}
	if raw, present := members.Get("port"); present {
		var number *float64
		var port *model.Int
		if json.Unmarshal(raw, &number) != nil || number == nil || json.Unmarshal(raw, &port) != nil || port == nil {
			claim.invalid = true
		} else {
			claim.Port = *port
		}
	}
	return claim
}
