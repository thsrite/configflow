package agents

// Port of backend/agents/diagnostics.py: bounded, readable failure details
// shared by Agent response paths.

import (
	"configflow/internal/agents/diag"
)

// Limits of diagnostics.py (UTF-8 bytes).
const (
	DetailLimit     = diag.DetailLimit
	DNSCheckLimit   = 32
	DNSNameLimit    = 128
	DNSMessageLimit = 2048
	DNSFixLimit     = 1024
)
