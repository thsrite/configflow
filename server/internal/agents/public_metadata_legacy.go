package agents

import (
	"configflow/internal/jsonx"
	"configflow/internal/redact"
)

// SanitizePublicMetadata adapts native redacted JSON for the remaining legacy
// Agent records. TODO(N8-10): remove this bridge with the Agent record migration.
func SanitizePublicMetadata(record *jsonx.Object, secrets redact.Secrets) *jsonx.Object {
	if record == nil {
		return nil
	}
	value, err := jsonx.Parse(redact.SanitizeCredentials(record, secrets))
	if err != nil {
		return nil
	}
	object, _ := value.(*jsonx.Object)
	return object
}
