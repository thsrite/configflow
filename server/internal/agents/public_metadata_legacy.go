package agents

import (
	"configflow/internal/jsonx"
	"configflow/internal/redact"
	"encoding/json"
)

// SanitizePublicMetadata adapts native redacted JSON for the remaining legacy
// Agent records. TODO(N8-10): remove this bridge with the Agent record migration.
func SanitizePublicMetadata(record *jsonx.Object, secrets redact.Secrets) *jsonx.Object {
	if record == nil {
		return nil
	}
	var object *jsonx.Object
	err := json.Unmarshal(redact.SanitizeCredentials(record, secrets), &object)
	if err != nil {
		return nil
	}
	return object
}
