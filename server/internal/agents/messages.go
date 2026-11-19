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

const msgDomainDiscoveryMihomo = "域名发现只支持 mihomo Agent"

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

const msgHAPairIncomplete = "DNS 高可用配对不完整，请先修复主备关系"
const installationIdentityChanged = "设备身份、地址、服务类型或配置空间已改变，请检查本次安装目标。"

const (
	msgHAConflict  = "DNS 主备正在同步，请等待两台发布完成"
	msgHAUninstall = "请先停用 DNS 高可用，再卸载成员"
)

const msgMetricsHistoryFailure = "Agent %s 的监控历史保存失败：%s"

const (
	msgTrafficHistoryMihomo   = "流量记录只支持 mihomo Agent"
	msgAgentEditingUpgrade    = "Agent update is in progress; wait for confirmation before editing it"
	msgAgentEditingDNSFollow  = "DNS 联动发布正在执行，请等待结果后再修改 Agent"
	msgAgentEditingDeployment = "Agent has an unfinished deployment; finish or recover it before changing profile"
)

const (
	msgAgentDeletingUpgrade        = "Agent 正在更新或等待恢复"
	msgAgentDeletingDNSFollow      = "DNS 联动发布正在执行，请等待结果后再删除 Agent"
	msgAgentDeleted                = "已删除 Agent %s"
	msgTrafficHistoryDeleteFailure = "删除 Agent %s 的流量记录失败：%s"
)

const (
	msgInstallationProfileChanged = "Installation Agent profile changed; prepare a new installation"
	msgRegistrationDeploymentBusy = "Agent has an unfinished deployment or upgrade; recover it before changing profile"
	msgRegistrationDNSFollowBusy  = "DNS 联动发布正在执行，请等待结果后再改变 Agent 注册信息"
)

const (
	msgRebindOtherProfile = "请选择其他配置空间"
	msgRebindReviewedIDs  = "请提供已确认的 Agent ID 列表"
	msgRebindChanged      = "Agent 绑定已变化，请刷新列表后重新确认"
	msgRebindBusy         = "以下 Agent 正在发布、更新或等待恢复，请完成后再改绑："
)

const (
	msgDNSIntegrityName    = "DNS 报告完整性"
	msgDNSIntegrityMessage = "部分 DNS 检测结果格式无效或数量过多，未完整显示。"
	msgDNSIntegrityFix     = "请减少重复监听配置，或检查 Agent 返回的 DNS 检测结果。"
)

const (
	msgPreparationOwnershipChanged = "Deployment ownership changed during preparation"
	msgInvalidValidationReceipt    = "Invalid deployment validation receipt"
)

const (
	msgInvalidDeploymentID       = "Invalid deployment ID"
	msgAgentNotFound             = "Agent not found"
	msgDeploymentDNSFollowTarget = "DNS 联动发布正在执行，请等待结果后再发布 OxiDNS"
	msgDeploymentDNSFollowSource = "绑定的 OxiDNS 正在同步，请等待结果后再发布 Mihomo"
	msgDeploymentUpgradeBusy     = "Agent update is in progress"
	msgDeploymentUnfinished      = "Agent already has an unfinished deployment"
	msgDeploymentPreparationBusy = "Deployment preparation already running"
	msgDeploymentStarted         = "开始向 Agent %s 发布配置（任务 %s）"
)

const (
	msgDeploymentStatusShape = "Agent 返回的发布状态格式不正确，请检查 Agent 版本和日志。"
	msgDeploymentNotFound    = "Deployment not found"
	msgDeploymentOutcome     = "Agent %s %s（任务 %s）%s"
)

const (
	msgAgentEndpoint           = "Agent 连接信息或凭据格式不正确，请重新注册设备。"
	msgDeploymentOtherProfile  = "Deployment belongs to a different profile"
	msgDeploymentOtherService  = "Deployment belongs to a different service type"
	msgDeploymentOwnerChanged  = "Deployment profile or service type changed; activation refused"
	msgDeploymentIdentity      = "Deployment identity does not match its stored task"
	msgDeploymentPreparingLost = "Preparation interrupted before upload; current service unchanged"
	msgDeploymentPending       = "Deployment status temporarily unavailable"
	msgRollbackAwaiting        = "Rollback submission awaiting Agent confirmation"
	msgRollbackJournalMissing  = "Rollback journal unavailable; restore Agent records before retrying"
	msgDeploymentReceiving     = "设备正在接收发布，等待确认"
	msgDeploymentRemoteMissing = "Agent has no record of the uploaded deployment"
	msgCandidatePending        = "尚未确认 Agent 候选状态，请稍后重试激活"
	msgCandidateChanged        = "候选状态已变化，请刷新后重试"
	msgDeploymentNotReady      = "Deployment must be ready before activation"
	msgActivationAwaiting      = "Activation sent; awaiting Agent status"
	msgRollbackPair            = "主备发布不能单独回滚一台；请将目标配置重新发布到主备"
	msgRollbackMember          = "请将目标配置重新发布到主备，不能单独回滚成员"
	msgRollbackFollow          = "DNS 联动发布尚未确认结束，请等待结果后再回滚"
	msgRollbackLatest          = "Only the latest deployment of this profile can be rolled back"
	msgRollbackSuccessful      = "Only a successful deployment can be rolled back"
	msgRollbackSubmitting      = "正在向 Agent 提交回滚请求"
	msgRollbackUnknown         = "Rollback outcome unknown; awaiting Agent status"
	msgRecoveryRequired        = "只有回滚失败的发布需要重新恢复"
	msgRecoverySubmitting      = "正在请求 Agent 重新恢复原配置"
	msgRecoveryUnknown         = "Recovery outcome unknown; awaiting Agent status"
	msgRecoveryUnsupported     = "此 Agent 版本不支持在 ConfigFlow 中重新恢复。请升级 Agent 到 1.8.57-go 或更新版本，或在设备上依次执行：停止 configflow-agent 服务，运行 /usr/local/bin/configflow-agent -config /opt/configflow-agent/config-%s.json -recover-and-exit，再启动 configflow-agent 服务。"
	msgRecoveryRejected        = "Agent 拒绝了恢复请求"
	msgRecoveryNotRun          = "恢复请求未执行："
	msgRollbackRejected        = "Rollback was rejected by Agent"
	msgRollbackNotRun          = "回滚请求被拒绝，回滚未执行，原配置继续运行"
)

const (
	msgPresenceOnline  = "Agent %s 已恢复在线"
	msgPresenceOffline = "Agent %s 已离线：超过 2 分钟没有心跳（最后心跳 %s）"
)

const msgInvalidDevicePayload = "Out of range float values are not JSON compliant"

const msgAgentReplyShape = "Agent 返回的数据格式不正确，请检查 Agent 版本和日志。"
