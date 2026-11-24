package routers

import "configflow/internal/routers/device"

const (
	messageReturnUpgrade         = "mihomo Agent 不支持回流占用安全检查，请先升级 Agent，或关闭自动配置回流后继续发布"
	messageReturnIncomplete      = "mihomo Agent 的回流安全检查结果不完整，无法确认规则归属"
	messageReturnUnsafe          = "mihomo Agent 未确认回流配置可安全使用，停止发布"
	messageReturnNoAllocation    = "mihomo Agent 未提供有效的回流资源自动分配结果，请先升级 Agent，或关闭自动配置回流后继续发布"
	messageReturnScopeRouter     = "标记主路由转来的全部新连接"
	messageReturnScopeDest       = "标记 %d 个网段"
	messageReturnChanged         = "回流资源分配在预览后发生变化，请重新预览后发布"
	messageReturnProtocolChanged = "mihomo Agent 不支持按来源标记的回流规则，请重新预览后发布"
	messageReleaseBlockers       = "发布计划存在阻止项：%s"
	messageDisabledWrites        = "未启用的发布阶段包含写入操作，请重新预览"
	messageReleaseUnknown        = "发布失败（未知错误）"
	messageStageNeedsRouterOS    = "发布阶段 %s 需要 RouterOS 驱动"
	messageNoReturnAgent         = "未启用 Agent 回流配置，无法执行回流操作"
	messageReturnHandshake       = "回流操作缺少安全握手或包含手工规则接管，请重新预览"
	messageReturnStillPresent    = "回读校验失败：Agent 上仍有受管的回流规则"
	messageReturnNoTable         = "Agent 上没有找到 configflow_return 表"
	messageReturnDestMismatch    = "Agent 上的回流网段与计划不一致"
	messageReturnSpecMismatch    = "Agent 上的回流路由或安全握手与计划不一致"
	messageReturnRuleMissing     = "没有找到与计划一致的回流策略路由（ip rule）"
	messageRouteVerify           = "回读校验失败：%s 没有有效且无冲突的受管主路由指向 %s"
	messagePolicyVerify          = "回读校验失败：策略路由「%s」没有按计划排除 %s 并转发到 %s"
	messageDeletedRoutePresent   = "回读校验失败：应删除的路由仍然存在"
	messageRestoredRouteMissing  = "回读校验失败：没有找到恢复的静态路由 %s → %s"
	messageDeletedPolicyPresent  = "回读校验失败：应删除的端口分流规则仍然存在"
	messageIPv6DNSNeedsDHCP      = "IPv6 DNS 下发缺少已确认的 OxiDNS DHCP 目标，请重新预览"
	messageReleaseManual         = "需要在路由器上手动导入脚本"
	messageReleaseVerified       = "回读校验通过"
	messageRollbackOK            = "已撤销：%s"
	messageRollbackFailed        = "撤销失败：%s（%s）"
	messageRollbackBatchFailed   = "撤销失败：%s"
)

var releaseEmptyMessages = map[string]string{
	device.StageIPv6FakeIP:    "回读确认 IPv6 Fake-IP 路由与计划一致",
	device.StageIPv6Routes:    "回读确认 IPv6 规则集路由与计划一致",
	device.StageIPv6DNS:       "回读确认 OxiDNS DHCP 与同一局域网的 IPv6 DNS 通告一致",
	device.StageIPv6PDGuard:   "回读确认 IPv6 前缀变化保护与模板一致",
	device.StageRoutes:        "回读确认来源策略路由与计划一致",
	device.StageReturnRouting: "回读确认受管回流配置与占用安全检查通过",
	device.StageNATBypass:     "回读确认 NAT 免转换与计划一致",
	device.StageDHCPDNS:       "回读确认 DHCP DNS 与所选服务器一致",
	device.StageDNSGuard:      "回读确认强制 DNS 规则与计划一致",
}
var releaseSkippedMessages = map[string]string{
	device.StageIPv6Routes:  "IPv6 规则集路由未能确认下一跳或归属，本次保留现状",
	device.StageIPv6DNS:     "IPv6 DNS 下发未能确认可安全修改的 LAN，本次保留现状",
	device.StageIPv6PDGuard: "IPv6 前缀变化保护未能确认，本次保留现状",
}

const messageNATNotRequiredVerified = "回读确认无需额外 NAT 免转换"
const (
	messageIkuaiIPv6Skipped     = "所选规则集含 %d 个 IPv6 网段；iKuai 只发布 IPv4 路由，IPv6 网段未处理"
	messageDHCPReadFailed       = "无法读取 DHCP DNS 配置：%s"
	messageIPv6ForeignGateway   = "代理网关不属于当前配置空间，请重新关联后预览。"
	messageIPv6InvalidGateway   = "代理网关不是可用的 Mihomo 设备，请核对关联。"
	messageReturnDisabled       = "未启用自动配置回流；本次不写入回流规则。"
	messageReturnCleanupPending = "未启用自动配置回流；mihomo 上仍有 ConfigFlow 写入的回流规则，发布时最后移除。"
	messageReturnUnlinked       = "未关联 mihomo Agent"
	messageReturnOffline        = "Agent %s 离线"
)
const (
	messageAdvisoryRow       = "第 %d 条"
	messageAdvisoryLabel     = "「%s」（ID %s）"
	messageAdvisoryID        = "（ID %s）"
	messageAdvisoryRemaining = "在放行已建立连接的规则 %s 后 %s 其余流量"
	messageReturnAdvisory    = "RouterOS forward 规则 %s：%s，可能作用于局域网代理转发。当前未开启自动配置回流，主路由只能看到去程，状态过滤可能丢弃或拒绝后续包。同类 drop 规则的 TCP 实测中，每个新连接约 6 秒后才通（syn-sent 超时），短超时请求会失败。建议开启「自动配置回流」，让回包先经过主路由。本提示不会自动开启回流，也不阻止发布。"
)
const (
	messageRouterChangedWaiting      = "路由器设置在等待发布期间发生变化，请重新预览"
	messageStaleFingerprint          = "路由器、Agent 或期望状态在预览后发生了变化，请重新预览"
	messagePublishFailedStage        = "阶段「%s」失败，%s"
	messageReleaseRollbackIncomplete = "回滚未完成，请检查发布记录"
	messageRollbackComplete          = "已按逆序撤销已完成的变更"
	messageReleaseAborted            = "服务正在停止，发布在当前操作完成后中止；已完成的变更保留，请重新预览后发布"
	messageReleaseAbortedProgress    = "（已完成 %d/%d 项操作）"
	messageServerStopping            = "服务正在停止，请稍后重试"
	messagePublishStopped            = "主路由 %s 发布中止：%s"
	messageCaptureFailed             = "主路由 %s 自动同步基线记录失败：%s"
	messagePublishBlocked            = "主路由 %s 发布被阻止：%s"
	messagePublishStale              = "主路由 %s 发布被拒绝：预览后状态已变化，需要重新预览"
	messagePublishStart              = "开始发布到主路由 %s：%d 项变更"
	messagePublishEmpty              = "主路由 %s 发布：没有需要写入的变更"
	messagePublishHint               = "主路由 %s 发布提示（%s）：%s"
	messagePublishComplete           = "主路由 %s 发布完成：%d 项变更已写入并通过回读校验"
	messagePublishLogFailed          = "主路由 %s 发布失败：阶段「%s」%s"
	messagePublishRollbackFailed     = "主路由 %s 回滚未完成：%s"
	messagePublishRolledBack         = "主路由 %s 已逆序撤销 %d 项已完成的变更"
	// MessagePublishPanic logs a recovered panic in a router publish or sync worker.
	MessagePublishPanic = "主路由 %s 发布任务异常终止，发布结果未记录"
)

const messageIPv6HostUnavailable = "无法读取代理网关的 IPv6 状态（功能未就绪）"
