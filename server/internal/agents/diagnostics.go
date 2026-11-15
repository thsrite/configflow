package agents

// Port of backend/agents/diagnostics.py: bounded, readable failure details
// shared by Agent response paths.

import (
	"strings"
	"unicode/utf8"

	"configflow/internal/agents/diag"
	"configflow/internal/jsonx"
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
	return diag.FailureText(value, secrets, limit)
}

// tokenSecrets is the (agent.get('token'),) tuple.
func tokenSecrets(agent *jsonx.Object) []any { return []any{agent.Value("token")} }

// DNSReportFields is dns_report_fields(payload, secrets): bounded, safe DNS
// results, without inventing a result for old Agents.
func DNSReportFields(payload *jsonx.Object, secrets []any) *jsonx.Object {
	result := jsonx.NewObject()
	if checks, ok := payload.Value("dns_checks").([]any); ok {
		safeChecks := []any{}
		incomplete := len(checks) > DNSCheckLimit
		limit := min(len(checks), DNSCheckLimit)
		for _, item := range checks[:limit] {
			check, isObject := item.(*jsonx.Object)
			level := check.Value("level")
			name, nameOK := check.Value("name").(string)
			message, messageOK := check.Value("message").(string)
			if !isObject || check == nil || (level != "pass" && level != "warn" && level != "error") || !nameOK || !messageOK {
				incomplete = true
				continue
			}
			name = FailureText(name, secrets, DNSNameLimit)
			message = FailureText(message, secrets, DNSMessageLimit)
			if name == "" || message == "" {
				incomplete = true
				continue
			}
			safe := jsonx.Obj("name", name, "level", level, "message", message)
			if fix, ok := check.Value("fix").(string); ok {
				if fix = FailureText(fix, secrets, DNSFixLimit); fix != "" {
					safe.Set("fix", fix)
				}
			}
			safeChecks = append(safeChecks, safe)
		}
		if incomplete {
			// A partial report must not look like an exhaustive all-pass result.
			if len(safeChecks) > DNSCheckLimit-1 {
				safeChecks = safeChecks[:DNSCheckLimit-1]
			}
			safeChecks = append(safeChecks, jsonx.Obj("name", "DNS 报告完整性", "level", "warn",
				"message", "部分 DNS 检测结果格式无效或数量过多，未完整显示。",
				"fix", "请减少重复监听配置，或检查 Agent 返回的 DNS 检测结果。"))
		}
		result.Set("dns_checks", safeChecks)
	}
	if checkedAt, ok := payload.Value("dns_checked_at").(string); ok && utf8.RuneCountInString(checkedAt) <= 64 && strings.Contains(checkedAt, "T") {
		if _, naive, ok := fromISO(strings.ReplaceAll(checkedAt, "Z", "+00:00")); ok && !naive {
			result.Set("dns_checked_at", checkedAt)
		}
	}
	return result
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
