package agents

import (
	"strings"
	"unicode/utf8"

	"configflow/internal/model"
	"configflow/internal/redact"
)

// DNSReport contains only a bounded, sanitized report. Nil means absent; a
// pointer to an empty list means the Agent explicitly reported no checks.
type DNSReport struct {
	Checks    *[]model.DeploymentDNSCheck `json:"dns_checks,omitempty"`
	CheckedAt *string                     `json:"dns_checked_at,omitempty"`
}

// SanitizeDNSReport bounds and redacts a remote report. Invalid individual
// checks (missing text, unknown level) add one integrity warning; unknown
// fields are never copied into the public result.
func SanitizeDNSReport(payload *model.Deployment, secrets redact.Secrets) (DNSReport, error) {
	var report DNSReport
	if payload == nil {
		return report, nil
	}
	if items := payload.DNSChecks; items != nil {
		checks := make([]model.DeploymentDNSCheck, 0, min(len(items), DNSCheckLimit))
		incomplete := len(items) > DNSCheckLimit
		for _, input := range items[:min(len(items), DNSCheckLimit)] {
			level := input.Level
			if level != "pass" && level != "warn" && level != "error" {
				incomplete = true
				continue
			}
			name := redact.DiagnosticText(input.Name, secrets, DNSNameLimit)
			message := redact.DiagnosticText(input.Message, secrets, DNSMessageLimit)
			if name == "" || message == "" {
				incomplete = true
				continue
			}
			check := model.DeploymentDNSCheck{Name: name, Level: level, Message: message}
			if input.Fix != nil {
				if fix := redact.DiagnosticText(*input.Fix, secrets, DNSFixLimit); fix != "" {
					check.Fix = new(fix)
				}
			}
			checks = append(checks, check)
		}
		if incomplete {
			if len(checks) > DNSCheckLimit-1 {
				checks = checks[:DNSCheckLimit-1]
			}
			checks = append(checks, model.DeploymentDNSCheck{Name: msgDNSIntegrityName, Level: "warn", Message: msgDNSIntegrityMessage, Fix: new(msgDNSIntegrityFix)})
		}
		report.Checks = &checks
	}
	if payload.DNSCheckedAt != nil {
		checkedAt := *payload.DNSCheckedAt
		if utf8.RuneCountInString(checkedAt) <= 64 && strings.Contains(checkedAt, "T") {
			if _, naive, ok := fromISO(strings.ReplaceAll(checkedAt, "Z", "+00:00")); ok && !naive {
				report.CheckedAt = new(checkedAt)
			}
		}
	}
	return report, nil
}

// applyDNSReport changes only the report members explicitly supplied by an Agent.
func applyDNSReport(state *model.Deployment, report DNSReport) {
	if report.Checks != nil {
		state.DNSChecks = *report.Checks
	}
	if report.CheckedAt != nil {
		state.DNSCheckedAt = new(*report.CheckedAt)
	}
}
