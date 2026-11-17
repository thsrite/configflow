package yamlx

const (
	msgDuplicateKey      = "YAML 包含重复键 %q（第 %d 行）"
	msgRecursiveAlias    = "YAML 包含递归引用（第 %d 行）"
	msgExcessiveAliases  = "YAML 引用展开过多（第 %d 行）"
	msgTypeMismatch      = "YAML 类型不匹配：%v"
	msgMultipleDocuments = "YAML 只能包含一个文档（第 %d 行开始第二个文档）"
	msgEmptyTag          = "YAML 标签 !! 缺少类型名（第 %d 行）"
)
