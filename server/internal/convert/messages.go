package convert

// AfterMatchNote explains why a rule after MATCH is omitted from Surge/Loon.
const AfterMatchNote = "仅 Mihomo 生效（位于 MATCH 之后，只匹配节点不支持 UDP 而跳过的连接），已忽略"

const (
	msgIPv6Boolean           = "ipv6 必须为 true 或 false，不能是字符串或拼写错误的值"
	msgDNSIPv6Boolean        = "dns 下的 ipv6 必须为 true 或 false，不能是字符串或拼写错误的值"
	msgCustomYAMLInvalid     = "mihomo.custom_config YAML 解析失败: "
	msgCustomYAMLMapping     = "mihomo.custom_config 不是 mapping，得到 "
	msgProxyYAMLInvalid      = "代理 YAML 校验失败"
	msgProxyYAMLNode         = "代理 YAML 节点无效"
	msgProxyYAMLScalar       = "代理 YAML 标量无法转换"
	msgProxyTypeString       = "代理 type 必须是非空字符串"
	msgNodeParamsObject      = "节点 params 必须是对象，不能为 null"
	msgProxyConverterMissing = "节点转换器不可用"
	msgProxyLocalLog         = "节点 '%s' 为 JSON/YAML 格式，本地解析"
	msgProxyRemoteLog        = "节点 '%s' 为 URI/base64 格式，使用 Sub-Store 的转换结果"
)
