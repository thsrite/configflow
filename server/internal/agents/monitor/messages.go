package monitor

import (
	"fmt"
	"strings"
)

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

// Messages is MESSAGES.
var Messages = map[string]string{
	"UPGRADE_REQUIRED":        "请将此 Agent 升级到 1.7.0-go 或更新版本后使用 DNS 监控。",
	"API_NOT_CONFIGURED":      "OxiDNS 管理 API 未启用。请在 OxiDNS 设置的「API / 日志」中开启，保存后发布到此 Agent。",
	"RECORDER_NOT_CONFIGURED": "查询记录未启用，请在 OxiDNS 设置中开启查询记录并推送配置。",
	"TLS_UNSUPPORTED":         "此监控连接暂不支持当前 OxiDNS API 的 TLS 配置。",
	"CORE_UNREACHABLE":        "暂时无法连接 OxiDNS 管理 API，请检查核心运行状态。",
	"CORE_AUTH_FAILED":        "OxiDNS 管理 API 认证失败，请检查核心配置中的认证设置。",
	"AGENT_UNREACHABLE":       "暂时无法连接 Agent，请检查网络和 Agent 运行状态。",
	"AGENT_AUTH_FAILED":       "Agent 认证失败，请检查注册状态。",
	"INVALID_RESPONSE":        "监控数据格式异常，请检查 Agent 和 OxiDNS 版本。",
	"INVALID_QUERY":           "查询条件无效，请检查筛选内容。",
	"RECORD_NOT_FOUND":        "这条查询记录已不存在，可能已超过保留期限。",
	"DASHBOARD_STATE_ERROR":   "无法读取或保存 DNS 面板统计起点，请检查数据目录并刷新面板确认状态。",
	"DASHBOARD_CHANGED":       "设备或统计起点已改变，请重新载入 DNS 面板。",
	"SNAPSHOT_CHANGED":        "采集期间核心或配置发生变化，请稍后刷新。",
	"CONFIG_UNSUPPORTED":      "当前核心 API 配置无法用于监控，请检查监听地址与认证设置。",
}

// PanelMessages is MESSAGES of mihomo_panel.py.
var PanelMessages = map[string]string{
	"CONTROLLER_UNAVAILABLE": "Agent 未找到 Mihomo 控制接口，请确认配置里启用了 external-controller。",
	"CORE_UNREACHABLE":       "暂时无法连接 Mihomo 控制接口，请检查 Mihomo 运行状态。",
	"CORE_AUTH_FAILED":       "Mihomo 控制接口拒绝了 Agent 的 secret，请检查配置。",
	"AGENT_UNREACHABLE":      "暂时无法连接 Agent，请检查网络和 Agent 运行状态。",
	"AGENT_AUTH_FAILED":      "Agent 认证失败，请检查注册状态。",
	"INVALID_RESPONSE":       "Mihomo 返回的数据格式异常。",
	"INVALID_REQUEST":        "请求参数无效。",
}

var panelResourceFeature = map[string]string{"providers/proxy/delay": "订阅节点测速", "flow": "实时流量", "logs": "实时日志"}

// CacheMessages is MESSAGES of oxidns_cache.py.
var CacheMessages = map[string]string{
	"UPGRADE_REQUIRED":        "请将此 Agent 升级到 " + CacheMinAgentVersion + " 或更新版本后清理缓存。",
	"AGENT_NOT_FOUND":         "未找到这台 Agent。",
	"WRONG_SERVICE":           "清理缓存仅适用于 OxiDNS Agent。",
	"AGENT_UNREACHABLE":       "暂时无法连接 Agent，请检查网络后重试。",
	"AGENT_AUTH_FAILED":       "Agent 认证失败，请检查注册状态。",
	"INVALID_REQUEST":         "清理缓存请求无效。",
	"INVALID_RESPONSE":        "缓存操作结果格式异常，请检查 Agent 和 OxiDNS 版本。",
	"API_NOT_CONFIGURED":      "OxiDNS 管理 API 未启用，请在设置中启用并发布一次。",
	"TLS_UNSUPPORTED":         "清理缓存暂不支持当前 OxiDNS API 的 TLS 配置。",
	"CONFIG_UNSUPPORTED":      "当前 OxiDNS 配置无法用于清理缓存，请检查管理 API 配置。",
	"CACHE_UNKNOWN":           "设备当前配置中没有这个缓存实例，请刷新面板后重试。",
	"CACHE_FLUSH_UNSUPPORTED": "当前 OxiDNS 核心不支持清理缓存接口，请升级核心后重试。",
	"CORE_UNREACHABLE":        "暂时无法连接 OxiDNS 管理 API，请检查核心运行状态。",
	"CORE_AUTH_FAILED":        "OxiDNS 管理 API 认证失败，请检查核心配置。",
	"CORE_BUSY":               "OxiDNS 缓存正忙，请稍后重试。",
	"SERVICE_BUSY":            "此设备正在发布、重启或更新，请等待当前操作结束。",
	"CACHE_FLUSH_FAILED":      "清理缓存失败，请检查 OxiDNS 日志后重试。",
}

// RuleUpdateMessages is MESSAGES of oxidns_rule_update.py.
var RuleUpdateMessages = map[string]string{
	"UPGRADE_REQUIRED":           "请将此 Agent 升级到 " + RuleUpdateMinAgentVersion + " 或更新版本后使用更新规则。",
	"AGENT_NOT_FOUND":            "未找到这台 Agent。",
	"WRONG_SERVICE":              "更新规则仅适用于 OxiDNS Agent。",
	"AGENT_UNREACHABLE":          "暂时无法连接 Agent，请检查网络后重新查询更新状态。",
	"AGENT_AUTH_FAILED":          "Agent 认证失败，请检查注册状态。",
	"INVALID_REQUEST":            "更新规则请求无效。",
	"INVALID_RESPONSE":           "规则更新状态格式异常，请检查 Agent 和 OxiDNS 版本。",
	"API_NOT_CONFIGURED":         "OxiDNS 管理 API 未启用，请在设置中启用并发布一次。",
	"TLS_UNSUPPORTED":            "规则更新暂不支持当前 OxiDNS API 的 TLS 配置。",
	"CONFIG_UNSUPPORTED":         "当前 OxiDNS 配置无法用于更新规则，请检查管理 API 和规则更新配置。",
	"RULE_UPDATE_NOT_CONFIGURED": "请先配置可下载的规则并重新发布 OxiDNS 配置，然后重试。",
	"RULE_UPDATE_TARGET_UNKNOWN": "要更新的规则文件不在设备的下载清单中，请重新发布 OxiDNS 配置后重试。",
	"RULE_UPDATE_AMBIGUOUS":      "发现多条规则更新链，无法确定更新目标，请检查 OxiDNS 规则更新配置。",
	"CORE_UPDATE_UNSUPPORTED":    "当前 OxiDNS 核心不支持原生规则更新接口，请升级核心后重试。",
	"CORE_UNREACHABLE":           "暂时无法连接 OxiDNS 管理 API，请检查核心运行状态。",
	"CORE_AUTH_FAILED":           "OxiDNS 管理 API 认证失败，请检查核心配置。",
	"SERVICE_BUSY":               "此设备正在发布、重启或更新，请等待当前操作结束。",
	"RULE_UPDATE_BUSY":           "此设备正在更新规则，请等待当前更新完成。",
	"CORE_BUSY":                  "OxiDNS 正在执行下载或重载，请等待当前操作结束。",
	"SNAPSHOT_CHANGED":           "运行中的配置与已部署文件不一致，请先确认配置已应用，再更新规则。",
	"DOWNLOAD_FAILED":            "规则下载未全部成功，本次未执行热重载，请检查 OxiDNS 日志。",
	"DOWNLOAD_TIMEOUT":           "规则下载等待超时，核心可能仍在下载；请稍后核对状态和日志。",
	"RELOAD_FAILED":              "部分规则未能热重载，已完成的更新不会自动回滚，请检查 OxiDNS 日志。",
	"RULE_UPDATE_TIMEOUT":        "规则更新等待超时，请检查 OxiDNS 运行状态和日志。",
	"RULE_UPDATE_UNKNOWN":        "暂时无法确认规则更新结果，请稍后核对状态和 OxiDNS 日志。",
	"RULE_UPDATE_FAILED":         "规则更新失败，请检查 OxiDNS 日志后重试。",
}

func rankingReason(code string) string {
	reasons := map[string]string{
		"upgrade_required":          "请将 Agent 升级到 " + MinDashboardAgentVersion + " 或更新版本以查看排行并重置统计。",
		"recorder_not_configured":   "未启用查询记录，无法计算域名和客户端排行。",
		"unsupported_configuration": "当前配置未包含可确认的拦截路径（标准 AdGuard 或规则「拒绝/黑洞」）。",
		"too_many_block_rules": fmt.Sprintf("拦截规则分组超过 %d 个，暂不统计被拦截域名排行；", BlockRankingMatcherLimit) +
			"请合并拒绝/黑洞规则后再查看。",
		"snapshot_changed": "采集期间核心或配置发生变化，请稍后刷新。",
		"rankings_paused":  fmt.Sprintf("上次统计排行超时，已暂停 %d 分钟后再试，以免影响查询记录。", rankingBackoffMinutes()),
	}
	reason, ok := reasons[code]
	if !ok {
		if reason, ok = Messages[strings.ToUpper(code)]; !ok {
			reason = "暂时无法获取排行，请稍后刷新。"
		}
	}
	return reason
}

const (
	msgDNSMonitor         = "DNS 监控"
	msgClientsFeature     = "客户端 IP 选择与筛选"
	msgDashboardFeature   = "DNS 排行与统计重置"
	msgLogLevel           = "日志等级只能是 debug、info、warning 或 error。"
	msgTestURL            = "测速地址必须是 http(s) 链接。"
	msgTestTimeout        = "测速超时需在 500–30000 毫秒之间。"
	msgGithubTimeout      = "连接 GitHub 超时"
	msgGithubHTTP         = "GitHub 返回 HTTP %d"
	msgGithubUnreachable  = "无法连接 GitHub"
	msgVersionContent     = "version.txt 内容不是版本号"
	msgCoreFetchFailure   = "无法获取最新版本（%s），可检查设置里的「规则下载代理」和「GitHub 代理域名」。"
	msgUpstreamServer     = "上游服务器"
	msgRankingUnavailable = "OxiDNS 排行统计暂未取到 %s 的时段（%s），下一轮再试"
)
