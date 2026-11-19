package agents

import (
	"encoding/json"
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

// SanitizeDNSReport reads a remote report without trusting R5 scalar projections.
// Invalid individual checks add one integrity warning; unknown fields are never
// copied into the public result or used to establish a successful DNS check.
func SanitizeDNSReport(payload *model.Deployment, secrets redact.Secrets) (DNSReport, error) {
	var report DNSReport
	if payload == nil {
		return report, nil
	}
	raw, present, err := payload.MemberJSON("dns_checks")
	if err != nil {
		return report, err
	}
	if present {
		var items *[]json.RawMessage
		if json.Unmarshal(raw, &items) == nil && items != nil {
			checks := make([]model.DeploymentDNSCheck, 0, min(len(*items), DNSCheckLimit))
			incomplete := len(*items) > DNSCheckLimit
			for _, raw := range (*items)[:min(len(*items), DNSCheckLimit)] {
				var input model.DeploymentDNSCheck
				if json.Unmarshal(raw, &input) != nil {
					incomplete = true
					continue
				}
				level, levelOK := storedText(input.MemberJSON("level"))
				name, nameOK := storedText(input.MemberJSON("name"))
				message, messageOK := storedText(input.MemberJSON("message"))
				if !levelOK || !nameOK || !messageOK || level != "pass" && level != "warn" && level != "error" {
					incomplete = true
					continue
				}
				name = redact.DiagnosticText(name, secrets, DNSNameLimit)
				message = redact.DiagnosticText(message, secrets, DNSMessageLimit)
				if name == "" || message == "" {
					incomplete = true
					continue
				}
				check := model.DeploymentDNSCheck{Name: name, Level: level, Message: message}
				if fix, valid := storedText(input.MemberJSON("fix")); valid {
					if fix = redact.DiagnosticText(fix, secrets, DNSFixLimit); fix != "" {
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
	}
	if checkedAt, valid := storedText(payload.MemberJSON("dns_checked_at")); valid && utf8.RuneCountInString(checkedAt) <= 64 && strings.Contains(checkedAt, "T") {
		if _, naive, ok := fromISO(strings.ReplaceAll(checkedAt, "Z", "+00:00")); ok && !naive {
			report.CheckedAt = new(checkedAt)
		}
	}
	return report, nil
}

// applyDNSReport changes only the report members explicitly supplied by an Agent.
func applyDNSReport(state *model.Deployment, report DNSReport) error {
	if report.Checks != nil {
		raw, err := json.Marshal(report.Checks)
		if err != nil {
			return err
		}
		if err := state.SetJSON("dns_checks", raw); err != nil {
			return err
		}
	}
	if report.CheckedAt != nil {
		state.DNSCheckedAt = new(*report.CheckedAt)
	}
	return nil
}
