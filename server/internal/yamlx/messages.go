package yamlx

const (
	msgJSONKey           = "订阅 YAML 的对象键必须是字符串、数字、布尔值或空值（第 %d 行）"
	msgSyntax            = "YAML 语法错误"
	msgSyntaxDetail      = msgSyntax + "：%s"
	msgSyntaxAtLine      = msgSyntax + "（第 %d 行）：%s"
	msgDepthLimit        = "YAML 嵌套层数超过 %s"
	msgDuplicateKey      = "YAML 包含重复键 %q（第 %d 行）"
	msgRecursiveAlias    = "YAML 包含递归引用（第 %d 行）"
	msgExcessiveAliases  = "YAML 引用展开过多（第 %d 行）"
	msgTypeMismatch      = "YAML 类型不匹配：%v"
	msgMultipleDocuments = "YAML 只能包含一个文档（第 %d 行开始第二个文档）"
	msgEmptyTag          = "YAML 标签 !! 缺少类型名（第 %d 行）"
	msgTagShape          = "YAML 标签 %s 必须用于%s（第 %d 行）"
	msgScalarShape       = "标量"
	msgSequenceShape     = "序列"
	msgMappingShape      = "映射"
	msgMergeSource       = "YAML 合并引用 << 必须是映射或映射序列（第 %d 行）"
	msgExplicitScalar    = "YAML 标签 %s 的标量内容无效（第 %d 行）：%v"
	msgMarshalGraph      = "YAML 输出节点无效"
	msgMarshalNodeKind   = "YAML 输出节点必须设置类型"
	msgMarshalAlias      = "YAML 输出引用必须指向此前定义且名称一致的锚点（第 %d 行）"
)
