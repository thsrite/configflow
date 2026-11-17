package convert

const (
	msgIPv6Boolean       = "ipv6 必须为 true 或 false，不能是字符串或拼写错误的值"
	msgDNSIPv6Boolean    = "dns 下的 ipv6 必须为 true 或 false，不能是字符串或拼写错误的值"
	msgCustomYAMLInvalid = "mihomo.custom_config YAML 解析失败: "
	msgCustomYAMLMapping = "mihomo.custom_config 不是 mapping，得到 "
	msgProxyYAMLInvalid  = "代理 YAML 校验失败"
	msgProxyYAMLNode     = "代理 YAML 节点无效"
	msgProxyYAMLScalar   = "代理 YAML 标量无法转换"
)
