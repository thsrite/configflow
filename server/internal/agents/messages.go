package agents

// Ports of backend/agents/deployment_messages.py and upgrade_messages.py:
// display explanations only; protocol states and original diagnostics are
// kept (the tables are generated into messages_data.go).

import (
	"regexp"
	"strings"

	"configflow/internal/agents/diag"
	"configflow/internal/jsonx"
)

// MessageUncheckedCandidate refuses activating an OxiDNS candidate without a
// matching server validation receipt (ReceiptMatches false).
const MessageUncheckedCandidate = "候选配置缺少当前规则的校验证明，已停止激活。请重新推送配置并完成校验。"

var (
	agentReturnedHTTP = regexp.MustCompile(`^Agent returned HTTP \p{Nd}{3}\z`)
	ruleFailure       = regexp.MustCompile(`^Rule .+: .+\z`)
	han               = regexp.MustCompile(`[\x{4e00}-\x{9fff}]`)
	downloadHTTP      = regexp.MustCompile(`^Agent download HTTP (\p{Nd}{3})\z`)
	recoveryRequired  = regexp.MustCompile(`^deployment [^` + diag.PyWS + `]+ requires (rollback )?recovery\z`)
	serviceCommand    = regexp.MustCompile(`^(systemctl|rc-service|rc-update|supervise-daemon) operation failed:`)
)

func lookup(pairs []messagePair, key string) (string, bool) {
	for _, pair := range pairs {
		if pair.key == key {
			return pair.value, true
		}
	}
	return "", false
}

// DeploymentMessage is deployment_message(message): a known protocol
// failure gets a Chinese explanation followed by the exact original text;
// anything else (and non-strings) is returned unchanged.
func DeploymentMessage(message any) any {
	text, ok := message.(string)
	if !ok {
		return message
	}
	explanation, found := lookup(deploymentMessages, text)
	if !found || explanation == "" {
		explanation = ""
		for _, pair := range deploymentPrefixes {
			if strings.HasPrefix(text, pair.key) {
				explanation = pair.value
				break
			}
		}
	}
	if explanation == "" && agentReturnedHTTP.MatchString(text) {
		explanation = "Agent 请求失败，请根据下方 HTTP 状态码检查设备连接。"
	}
	if explanation == "" && ruleFailure.MatchString(text) {
		explanation = "规则集准备失败，请检查下方规则名称和具体原因。"
	}
	if explanation != "" {
		return explanation + "\n原始错误：" + text
	}
	return text
}

// PublicDeploymentMessages is public_deployment_messages(record): a shallow
// copy with the display texts explained and the events cleaned.
func PublicDeploymentMessages(record *jsonx.Object) *jsonx.Object {
	result := record.Clone()
	for _, key := range []string{"message", "error", "rollback_error", "rollback_message"} {
		if value, ok := result.Get(key); ok {
			result.Set(key, DeploymentMessage(value))
		}
	}
	if events, ok := result.Get("events"); ok {
		cleaned := CleanEvents(events, nil, nil)
		for i, item := range cleaned {
			event := item.(*jsonx.Object)
			if jsonx.Truthy(event.Value("message")) {
				event = event.Clone()
				event.Set("message", DeploymentMessage(event.Value("message")))
				cleaned[i] = event
			}
		}
		result.Set("events", cleaned)
	}
	return result
}

// pyStrip is str.strip(): Python's whitespace includes U+001C–U+001F.
func pyStrip(text string) string { return diag.PyStrip(text) }

// pyLower is str.lower() for the containment checks against the ASCII
// _SYSTEM_ERRORS keys: Python lowercases U+0130 to "i̇" (two characters),
// Go's ToLower to a plain "i".
func pyLower(text string) string {
	return strings.ToLower(strings.ReplaceAll(text, "\u0130", "i\u0307"))
}

// upgradeExplanation is _upgrade_explanation(message).
func upgradeExplanation(message any) string {
	text, ok := message.(string)
	if !ok || pyStrip(text) == "" {
		return "Agent 更新遇到异常，请查看 Agent 日志。"
	}
	text = pyStrip(text)
	if value, found := lookup(upgradeMessages, text); found {
		return value
	}
	// A failed update can also fail to restore the old version. Show both causes.
	if original, rollback, found := strings.Cut(text, "; rollback: "); found {
		return upgradeExplanation(original) + " 恢复旧版本失败：" + upgradeExplanation(rollback)
	}
	for _, pair := range upgradeCausePrefixes {
		if strings.HasPrefix(text, pair.key) {
			return pair.value + "：" + upgradeExplanation(text[len(pair.key):])
		}
	}
	for _, pair := range upgradePathPrefixes {
		if strings.HasPrefix(text, pair.key) {
			return pair.value + "。"
		}
	}
	if match := downloadHTTP.FindStringSubmatch(text); match != nil {
		return "Agent 更新下载失败，服务器返回 HTTP " + match[1] + "，请检查 ConfigFlow 的更新文件是否可用。"
	}
	if recoveryRequired.MatchString(text) {
		return "之前的配置发布尚未完成恢复，请先到发布记录中处理，再更新 Agent。"
	}
	lower := pyLower(text)
	for _, pair := range upgradeSystemErrors {
		if strings.Contains(lower, pair.key) {
			return pair.value
		}
	}
	if serviceCommand.MatchString(text) {
		return "服务管理命令执行失败，请查看 Agent 日志和设备上的服务状态。"
	}
	if han.MatchString(text) {
		return text
	}
	return "Agent 更新遇到异常，请查看 Agent 日志。"
}

// UpgradeErrorMessage is upgrade_error_message(message): a Chinese
// explanation followed by the complete original error.
func UpgradeErrorMessage(message any) string {
	if text, ok := message.(string); ok && han.MatchString(text) {
		return text
	}
	explanation := upgradeExplanation(message)
	if text, ok := message.(string); ok && pyStrip(text) != "" {
		return explanation + "\n原始错误：" + text
	}
	return explanation
}

var upgradeFailureStatuses = map[string]bool{"failed": true, "rolled_back": true, "rollback_failed": true}

// PublicUpgradeState is public_upgrade_state(state): a localized response
// copy; the durable journal is never rewritten.
func PublicUpgradeState(state *jsonx.Object) *jsonx.Object {
	result := state.Clone()
	if jsonx.Truthy(result.Value("error")) {
		result.Set("error", UpgradeErrorMessage(FailureText(result.Value("error"), nil, 0)))
	}
	if jsonx.Truthy(result.Value("message")) {
		result.Set("message", FailureText(result.Value("message"), nil, 0))
	}
	if events, ok := result.Get("events"); ok {
		cleaned := CleanEvents(events, nil, nil)
		for _, item := range cleaned {
			event := item.(*jsonx.Object)
			if upgradeFailureStatuses[event.GetString("status")] && jsonx.Truthy(event.Value("message")) {
				event.Set("message", UpgradeErrorMessage(event.Value("message")))
			}
		}
		result.Set("events", cleaned)
	}
	return result
}
