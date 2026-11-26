package rules

const (
	msgOrderObjects = "%s 必须是对象数组"
	msgOrderIDs     = "ids 必须是数组"
	msgOrderUnique  = "id 必须是互不重复的字符串"
)

const (
	msgRulesetName      = "规则集"
	msgRuleSuffix       = " 规则"
	msgDirectRuleSource = "直接配置的规则"
	msgRulesetSource    = "规则集: "
)

const msgPolicyReference = "规则 %s 引用了不存在或未启用的策略 '%s'，请修改策略引用"

const (
	msgFilterString      = "正则筛选必须是字符串或 null"
	msgInvalidFilter     = "正则表达式无效"
	msgFilterType        = "%s 必须是字符串或 null"
	msgFilterExpression  = "%s 正则表达式无效"
	msgSourceArray       = "%s 必须是数组"
	msgFollowID          = "follow_group 必须是 ID 字符串"
	msgFollowSource      = "跟随来源必须是有效启用的普通来源组"
	msgCandidateIDs      = "候选来源必须是 ID 数组"
	msgCandidateResource = "候选来源必须引用有效启用资源"
	msgOrderPair         = "排序项必须包含且仅包含 type 和 id"
	msgOrderSource       = "排序项必须与已选择的受支持来源一致"
	msgOrderRepeated     = "排序项不能重复"
	msgOrderComplete     = "排序必须包含所有手动节点和引用策略组"
	msgNoGroupCandidates = "策略组必须选择实际消费的候选来源，不能隐式使用 DIRECT"
	msgDeleteFields      = "delete_fields 必须是无重复的健康检查字段数组"
	msgAssignDelete      = "同一字段不能同时赋值和删除"
)

const (
	msgRuleInfo        = "无法读取规则集引用，请重新打开后重试。"
	msgIPTargetChanged = "所选规则集不支持 IP、不可编辑或策略已改变，请重新打开后选择。"
	msgInactiveIPRule  = "此规则集尚未在当前配置中启用，或位于兜底规则之后；请在策略规则中设置并发布后生效。"
)

const (
	msgGroupOrderArray     = "proxies_order 必须是数组"
	msgPrivateProviderName = "%s_%s_筛选"
	msgDestination         = "请选择有效的目标 IP 或网段，IPv4 使用 IP-CIDR，IPv6 使用 IP-CIDR6。"
)
