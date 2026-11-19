package agents

// Port of backend/agents/diagnostics.py: bounded, readable failure details
// shared by Agent response paths.

import (
	"encoding/json"

	"configflow/internal/agents/diag"
	"configflow/internal/jsonx"
	"configflow/internal/model"
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

// tokenSecrets is the (agent.get('token'),) tuple.
func tokenSecrets(agent *jsonx.Object) []any { return []any{agent.Value("token")} }

// DNSReportFields is dns_report_fields(payload, secrets): bounded, safe DNS
// results, without inventing a result for old Agents.
func DNSReportFields(payload *jsonx.Object, secrets []any) *jsonx.Object {
	var input model.Deployment
	if items, present := payload.Value("dns_checks").([]any); present {
		checks := make([]model.DeploymentDNSCheck, len(items))
		for i, item := range items {
			record, valid := item.(*Record)
			if !valid || record == nil {
				continue
			}
			checks[i].Name, _ = record.Value("name").(string)
			checks[i].Level, _ = record.Value("level").(string)
			checks[i].Message, _ = record.Value("message").(string)
			if fix, valid := record.Value("fix").(string); valid {
				checks[i].Fix = new(fix)
			}
		}
		raw, err := json.Marshal(checks)
		if err != nil || input.SetJSON("dns_checks", raw) != nil {
			return jsonx.NewObject()
		}
	}
	if text, present := payload.Value("dns_checked_at").(string); present {
		raw, err := json.Marshal(text)
		if err != nil || input.SetJSON("dns_checked_at", raw) != nil {
			return jsonx.NewObject()
		}
	}
	report, err := SanitizeDNSReport(&input, legacySecrets(secrets))
	if err != nil {
		return jsonx.NewObject()
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return jsonx.NewObject()
	}
	var result Record
	if json.Unmarshal(raw, &result) != nil {
		return jsonx.NewObject()
	}
	return &result
}

// ResponseFailure is response_failure(response, fallback=..., secrets=...):
// keeps a legacy client's explanation even when its HTTP status is non-200.
func ResponseFailure(response *Response, fallback string, secrets []any) *jsonx.Object {
	result, err := ResponseFailureModel(response, fallback, legacySecrets(secrets))
	if err == nil {
		if object, encodeErr := deploymentObject(result); encodeErr == nil {
			return object
		}
	}
	// The old boundary cannot return an encoding error; keep its fallback bounded.
	return jsonx.Obj("success", false, "message", redact.DiagnosticText(fallback, legacySecrets(secrets), 0))
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
