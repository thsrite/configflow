package monitor

const msgHARulesBusy = "DNS 主备正在同步，请等待两台发布完成后更新规则"

const msgHAConfigDamaged = "OxiDNS HA 配置记录损坏，请先停用或修复主备配置。"
const msgHAPublicationDamaged = "OxiDNS HA 发布记录损坏，请先停用或修复主备配置。"

const (
	msgAgentMissing      = "未找到这台 Agent。"
	msgPanelWrongService = "Mihomo 面板仅适用于 Mihomo Agent。"
	msgCoreOperation     = "这个内核操作"
	msgMihomoPanel       = "Mihomo 面板"
	msgPanelUpgrade      = "请将此 Agent 升级到 %s 或更新版本后使用%s。"
	msgSelectNode        = "请选择要切换的节点。"
	msgPanelMode         = "代理模式只能是 rule、global 或 direct。"
	msgPanelNotAllowed   = "面板不支持这个操作。"
	msgPanelErrorShape   = "Mihomo 返回的错误代码格式不正确。"
	msgPanelHTTP         = "Mihomo 返回 HTTP %d"
)

const msgPanelEndpointUpgrade = "请将此 Agent 升级到 1.8.15-go 或更新版本后使用 Mihomo 面板。"

const msgPublicationDamaged = "OxiDNS 发布记录损坏，请先修复设备发布记录。"

const msgDNSWrongService = "DNS 监控仅适用于 OxiDNS Agent。"

const msgBlockedPath = "拦截路径"

var blockedReasons = map[string]string{
	"standard_adguard":          "标准 AdGuard 拦截路径",
	"rule_dns":                  "规则拦截（拒绝/黑洞）",
	"standard_adguard_rule_dns": "标准 AdGuard 与规则拦截",
}

const msgCheckpointUnavailable = "OxiDNS 发布前统计快照不可用，本次发布的统计可能重新开始"

const msgCompletionUnavailable = "OxiDNS 发布后统计暂未接续，将在下次查看时重试"
