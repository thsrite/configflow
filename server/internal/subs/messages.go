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
