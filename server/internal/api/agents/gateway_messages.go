package agents

const (
	msgGatewayForwarded = "已为代理网关 Agent %s 开启并持久化 IPv4 转发"
	msgGatewayProfile   = "请从一个明确的配置空间打开 Linux 网关设置。"
	msgGatewayBodySize  = "网关请求内容过大。"
	msgGatewayBodyJSON  = "网关参数必须是 JSON 对象。"
	msgGatewayAgentID   = "请求只能包含 agent_id。"
	msgGatewayNotLinked = "这台 Agent 不是任何路由器的代理网关。"
)
