package config

const (
	msgYAMLText       = "config 必须是 YAML 文本"
	msgMihomoInterval = "Mihomo 订阅同步间隔"
	msgYAMLSyntax     = "YAML 语法错误，未保存: %v"
	msgEasyTier       = "EasyTier 设置未保存：%v"
	msgObject         = "请求数据必须是 JSON 对象"
	msgReset          = "配置已重置为默认值"
)

// msgImportRestored follows an import of a backup without Agent run records (O17).
const msgImportRestored = "全部数据已导入。运行记录未包含在备份里，各 Agent 需要重新发布一次"
