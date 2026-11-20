package agents

// Port of backend/agents/diagnostics.py: bounded, readable failure details
// shared by Agent response paths.

import (
	"configflow/internal/agents/diag"
	"configflow/internal/redact"
)

// Limits of diagnostics.py (UTF-8 bytes).
const (
	DetailLimit     = diag.DetailLimit
	DNSCheckLimit   = 32
	DNSNameLimit    = 128
	DNSMessageLimit = 2048
	DNSFixLimit     = 1024
)

// FailureText is failure_text(value, secrets, limit); see diag.FailureText.
// limit <= 0 is DetailLimit.
func FailureText(value any, secrets []any, limit int) string {
	text, _ := value.(string)
	return redact.DiagnosticText(text, legacySecrets(secrets), limit)
}

func legacySecrets(values []any) redact.Secrets {
	var secrets []string
	for _, value := range values {
		if text, ok := value.(string); ok {
			secrets = append(secrets, text)
		}
	}
	return redact.NewSecrets(secrets)
}
