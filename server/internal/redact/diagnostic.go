package redact

import "configflow/internal/agents/diag"

// DiagnosticText removes credentials and bounds readable diagnostic text. The
// shared leaf formatter also owns terminal/control cleanup and URL handling.
func DiagnosticText(text string, secrets Secrets, limit int) string {
	return diag.DiagnosticText(text, secrets, limit)
}

// EventText removes download addresses and credentials from a bounded timeline
// message. All new native Agent producers enter through this redaction owner.
func EventText(text string, secrets Secrets) string {
	return diag.EventText(text, secrets...)
}
