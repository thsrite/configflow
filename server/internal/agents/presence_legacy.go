package agents

// Old presence callers pass only selected metadata to the native transition
// owner. This codec never encodes or decodes a complete Agent/configuration.
import (
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/redact"
)

// ObservePresence adapts the legacy registry observation API.
func ObservePresence(records []*jsonx.Object) {
	observations := make([]PresenceObservation, 0, len(records))
	tokens := make([]string, 0, len(records))
	for _, record := range records {
		id, _ := record.Value("id").(string)
		status, _ := record.Value("status").(string)
		name := presenceDisplayJSON(record.Value("name"))
		heartbeat := presenceDisplayJSON(record.Value("last_heartbeat"))
		token, _ := record.Value("token").(string)
		tokens = append(tokens, token)
		observations = append(observations, PresenceObservation{id, status, name, heartbeat})
	}
	ObserveAgentPresence(observations, redact.NewSecrets(tokens))
}

// Rich legacy display metadata is JSON, never a private Python repr renderer.
func presenceDisplayJSON(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	raw, err := jsonresp.EncodeUTF8(value)
	if err != nil {
		return redact.Redacted
	}
	return string(raw)
}
