package rulefetch

const (
	msgYAMLPayload       = "YAML 规则需要 payload 列表。"
	msgUniquePayload     = "YAML 规则需要唯一的 payload 列表。"
	msgDeliveryAliases   = "请先展开 YAML 锚点或别名；Mihomo 下发保留了原文。"
	msgDeliveryItem      = "payload 每项应为一行文本；复杂 YAML 内容已保留原文。"
	msgComplexMapping    = "YAML 映射键重复或过于复杂，已保留原文。"
	msgMultilineContent  = "多行 YAML 内容已保留原文，请展开为普通 payload 列表。"
	msgEditPayloadAlias  = "请将 payload 的 YAML 别名展开后再编辑。"
	msgEditPayloadItem   = "payload 每项应为一行文本；请先展开 YAML 锚点、别名或多行内容。"
	msgRemotePayload     = "声明为 YAML，但文件需要包含 payload 列表；请检查订阅链接或文件格式。"
	msgPayloadList       = "YAML payload 必须是列表。"
	msgRemoteAliases     = "远端 YAML 规则不支持锚点或别名；请使用展开后的 payload 列表。"
	msgRemoteItem        = "payload 每项必须是单行文本；不接受数字、布尔值、嵌套结构或多行内容。"
	msgUnreadablePayload = "规则集的 payload 格式无法读取，请先在规则仓库检查内容。"
	msgReadPayloadYAML   = "规则集的 payload 格式无法读取，请先在规则仓库检查内容：%v"
)
