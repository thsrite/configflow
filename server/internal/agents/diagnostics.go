package agents

// Port of backend/agents/diagnostics.py: bounded, readable failure details
// shared by Agent response paths.

import (
	"encoding/json"
	"strings"

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
	payload, err := response.JSON()
	result := jsonx.Obj("success", false)
	object, isObject := payload.(*jsonx.Object)
	if err == nil && isObject {
		for _, key := range []string{"message", "error", "error_details", "rollback_error", "rollback_error_details", "failed_stage"} {
			if text, ok := object.Value(key).(string); ok {
				result.Set(key, FailureText(text, secrets, 0))
			}
		}
	}
	if !jsonx.Truthy(result.Value("message")) {
		message := result.Value("error")
		if !jsonx.Truthy(message) {
			message = fallback
		}
		result.Set("message", message)
	}
	// Plain text is useful for older clients; HTML proxy error pages are not diagnostics.
	if !(err == nil && isObject) {
		contentType, _, _ := strings.Cut(response.contentType(), ";")
		if contentType == "text/plain" {
			if detail := FailureText(response.Text(), secrets, 0); detail != "" {
				result.Set("error_details", detail)
			}
		}
	}
	if response.StatusCode >= 400 {
		result.Set("http_status", jsonx.Int(response.StatusCode))
	}
	return result
}
