package routersplit

// Message codes (the keys of MESSAGES).
const (
	CodeFullTun        = "ROUTER_SPLIT_FULL_TUN"
	CodeLegacy         = "ROUTER_SPLIT_LEGACY"
	CodeRestoreBlocked = "ROUTER_SPLIT_RESTORE_BLOCKED"
	CodeLegacyTeardown = "ROUTER_SPLIT_LEGACY_TEARDOWN"
	CodeInvalid        = "ROUTER_SPLIT_INVALID"
)

// MessageCodes lists the MESSAGES keys in Python's order.
var MessageCodes = []string{CodeFullTun, CodeLegacy, CodeRestoreBlocked, CodeLegacyTeardown, CodeInvalid}

// Messages is MESSAGES.
var Messages = map[string]string{
	CodeFullTun: "此配置空间使用 Mihomo + OxiDNS 分流（完整 TUN），主路由转来的连接已全部交给 Mihomo，" +
		"不能再开启 Linux 网关的选择性接管。",
	CodeLegacy: "这是旧版主路由分流：Mihomo 靠 Linux 网关接管主路由转来的连接。" +
		"关闭 Linux 网关会同时去掉 TUN 并还原转发，主路由转来的连接会中断。",
	CodeRestoreBlocked: "主路由分流的配置空间不能恢复试用前快照：快照中没有完整 TUN，恢复会让分流中断。",
	CodeLegacyTeardown: "撤销会还原 TUN 接管与 IPv4 转发，主路由转来的连接将中断。确认后重试。",
	CodeInvalid:        "主路由分流标记 mihomo.router_split 无效，已停止生成和网关操作。请从备份恢复，或重新运行首次使用向导。",
}

const (
	// LegacyRestoreBlocked is ROUTER_SPLIT_RESTORE_BLOCKED for a legacy
	// profile whose snapshot has the gateway off.
	LegacyRestoreBlocked = "恢复后 Linux 网关为关闭状态，主路由转来的连接会中断，已阻止。"
	// FullTunNoRestorePoint / FullTunRestoreOverwrites are
	// ROUTER_SPLIT_RESTORE_BLOCKED for a full TUN profile, by what the
	// restore point holds.
	FullTunNoRestorePoint    = "没有可恢复的试用前快照；主路由分流（完整 TUN）不使用 Linux 网关试用，无需恢复。"
	FullTunRestoreOverwrites = "主路由分流的配置空间不能恢复试用前快照：恢复会用快照覆盖当前的 Mihomo 与 OxiDNS 设置，可能让分流中断。"
	// GeneratedTunMissing is GENERATED_TUN_MISSING.
	GeneratedTunMissing = "主路由分流需要完整 TUN，但生成的 Mihomo 配置未启用（基础 YAML 缺少 tun 或被修改）。" +
		"请在基础 YAML 中恢复 tun 后再发布，否则主路由转来的连接会回环。"
	// GeneratedTunFiltered is GENERATED_TUN_FILTERED with "{keys}" in place.
	GeneratedTunFiltered = "主路由分流的完整 TUN 不能带 tun.{keys} 过滤：未进入 TUN 的连接会在主路由与 Linux 之间回环。" +
		"请在 Mihomo 基础 YAML 中移除后再发布。"
)

const (
	messageBaseParse      = "Mihomo 基础 YAML 无法解析，请先修正基础配置"
	messageBaseObject     = "Mihomo 基础配置必须是 YAML 对象"
	messageBaseText       = "Mihomo 基础配置必须为 YAML 文本"
	messageTunObject      = "Mihomo tun 必须是对象"
	messageTunFilter      = "主路由分流的完整 TUN 与现有 tun.%s 过滤冲突：未进入 TUN 的连接会在主路由与 Linux 之间回环。请先在 Mihomo 基础 YAML 中移除该过滤"
	messageDocumentObject = "Mihomo 配置必须是对象"
)

const messageBindingsInvalid = "主路由分流的 Agent 绑定身份无效"

const messageTopologyIdentity = "主路由分流的拓扑身份必须是非空文本"
