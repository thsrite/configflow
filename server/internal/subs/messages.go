package subs

const (
	msgListenerListRequired    = "Mihomo listeners 必须是列表，无法确认入口的固定策略引用"
	msgListenerObjectRequired  = "Mihomo listeners[%d] 必须是对象"
	msgListenerTargetRequired  = "Mihomo listeners[%d].proxy 必须是策略名称或留空"
	msgListenerBaseParse       = "Mihomo 基础配置无法解析，未修改策略组或入口引用"
	msgListenerBaseParseDetail = msgListenerBaseParse + "：%v"
	msgListenerBaseObject      = "Mihomo 基础配置必须是对象，未修改策略组或入口引用"
	msgListenerVariable        = "Mihomo listeners[%d].proxy 含未解析变量，无法安全联动此策略组；原配置已保留"
	msgListenerUnsafeEdit      = "无法安全保留 Mihomo 基础配置的注释与引用，原配置已保留"
	msgListenerMissingPolicy   = "Mihomo listeners[%d].proxy 引用了不存在或未启用的策略「%s」"
)

const msgHostsKeyShape = "YAML hosts 的键必须是标量（第 %d 行）"

const (
	msgFilterType    = "节点筛选正则必须是字符串"
	msgFilterInvalid = "节点筛选正则无效: "
	msgFilterIgnored = "订阅 '%s' 的节点筛选正则无效，已忽略: %s"
)

const (
	msgDNSOldCore         = "Mihomo 内核 %s 早于 %s，不认识 proxy-server-nameserver-policy，所以没有加入；升级内核后重新推送，映射才对节点生效"
	msgDNSPolicyConflict  = "你已为 %s 写了 proxy-server-nameserver-policy，保留你的设置；这条映射对该节点不生效"
	msgDNSPolicyCovers    = "你写的 proxy-server-nameserver-policy 条目 %s 覆盖了 %s，保留你的设置；这条映射对该节点不生效"
	msgDNSMatchers        = "你写的 proxy-server-nameserver-policy 里有 geosite / rule-set 条目，ConfigFlow 无法检查；Mihomo 按顺序先匹配它们，匹配到的节点域名仍按你的设置解析"
	msgDNSWhitelistCovers = "fake-ip-filter-mode 为 whitelist，名单覆盖了映射目标 %s，改走本机 DNS 监听会让 %s 拿到假地址，所以没有改动这条"
	msgDNSWhitelist       = "fake-ip-filter-mode 为 whitelist：只有名单内的域名返回假地址，映射目标不在名单内，无需改动 fake-ip-filter"
	msgDNSUnchecked       = "；名单里的 geosite / rule-set 条目无法检查"
	msgDNSUnknownVersion  = "需要 Mihomo %s 或更新：更旧的内核会忽略 proxy-server-nameserver-policy，这些节点照旧连接左侧域名自己的地址"
)

const (
	msgSubscriptionCollaborator = "订阅协作者未接线：%s"
	msgHostsConflict            = "订阅「%s」给了不同的值，按订阅列表顺序采用「%s」"
	msgHostsInvalidEntry        = "条目无效"
	msgHostsLog                 = "订阅 hosts：合并 %d 条（其中域名映射 %d 条），忽略 %d 条%s"
)

const (
	msgSubscriptionList = "订阅 %s 必须是列表"
	msgDialerSkipped    = "订阅「%s」跳过 %d 个链式节点：%s"
)

const (
	msgDialerNameConflict = "名称「%s」已被使用，与订阅「%s」自动生成的中转分组冲突，请改名后重新生成"
	msgDialerMissing      = "订阅链式节点：%d 个节点的中转分组未能补上（%s）"
)

const (
	msgChainMissing       = "代理链资源「%s」不存在"
	msgChainProviderNames = "订阅与聚合 Provider 名称必须唯一"
	msgChainFollowMissing = "跟随策略组依赖不存在或存在循环"
	msgChainFollowCycle   = "跟随策略组依赖存在循环"
	msgChainEndpoints     = "代理链 chain 必须包含 entry 和 exit"
	msgChainBuiltinExit   = "代理链落地组不能包含 DIRECT/REJECT 等非代理候选"
)

const (
	msgChainFieldInvalid = "代理链字段 %s 类型无效"
	msgChainNodeType     = "节点 type"
	msgChainGroupType    = "策略组 type"
	msgChainIdentityType = "%s 必须是字符串，当前值：%s"
)
