package rulesync

// PinnedWithheldNotice explains why a rule's new DNS choice was held back.
const PinnedWithheldNotice = "指定上游需手动发布"
const (
	msgSnapshotUnavailable   = "无法读取设备当前规则快照，请确认 Agent 已升级且没有其他操作正在进行"
	msgSnapshotVerify        = "无法验证设备当前规则快照，未继续自动应用"
	msgSnapshotLarge         = "设备规则快照超过大小限制"
	msgSnapshotExpanded      = "设备规则快照展开后超过大小限制"
	msgSnapshotInvalid       = "设备规则快照包含无效文件"
	msgSnapshotManifestLarge = "设备规则快照清单过大"
	msgSnapshotForeign       = "设备规则快照文件不属于上次发布"
	msgSnapshotIncomplete    = "设备规则快照文件不完整"
	msgSnapshotChanged       = "设备配置已改变，请等待发布完成或重新建立同步基线"
	msgManifestIncomplete    = "设备规则快照清单不完整"
	msgManifestInvalid       = "设备规则快照清单无效"
	msgSnapshotDigest        = "设备规则快照摘要不一致"
	msgMainChanged           = "设备主配置已改变，请先成功发布一次配置"
	msgService               = "规则自动同步仅支持 Mihomo 和 OxiDNS"
	msgProjectionService     = "规则自动同步仅支持 Mihomo 和 OxiDNS。"
	msgSettings              = "library 和 rules 必须分别为布尔值"
	msgRequest               = "规则同步设置无效"
	msgPairEnabling          = "DNS 高可用正在开启，请完成后再修改主备规则自动同步设置"
	msgPairChanged           = "主备关系已改变，请刷新后重试"
	msgPairIncomplete        = "DNS 高可用配对不完整，请先修复主备关系"
	msgMissingLibrary        = "规则集引用的共享来源不存在，请先修复规则引用。"
)
