package nodes

const (
	msgBodyObject       = "请求体必须是 JSON 对象"
	msgNodeObject       = "节点请求必须是 JSON 对象"
	msgNodeNonNull      = "节点请求必须为非 null JSON 对象"
	msgSharedDialer     = "共享节点不保存拨号引用，请在策略组中创建代理链"
	msgImmutableID      = "节点 ID 不可修改"
	msgLines            = "lines 必须是字符串数组"
	msgLinesLimit       = "单次最多解析 500 行"
	msgUnknownNode      = "无法识别的节点格式"
	msgMissingNodeIDs   = "以下节点 id 不存在: %s"
	msgSelectNodes      = "请选择要删除的节点"
	msgNodesGone        = "部分节点已不存在，请刷新节点列表"
	msgGroupLabel       = "配置「%s」的策略组「%s」"
	msgChainBlocker     = "%s是代理链，请先更换或移除对应的前置／落地节点。"
	msgRuleBlocker      = "配置「%s」的规则「%s」直接使用节点「%s」，请先更换规则策略。"
	msgDialerBlocker    = "节点「%s」的 dialer-proxy 使用了待删除节点，请先修改拨号代理。"
	msgAggregationLabel = "聚合「%s」"
)
