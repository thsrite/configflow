package provider

const (
	msgSlowProvider      = "Provider %s/%s 生成超过 %d 秒，先返回上次成功内容，后台继续生成"
	msgPreparationFailed = "%s准备失败：%s"
	msgPreparationName   = "%s「%s」"
	msgContentSource     = "\n内容来源：%s"
)

const msgProviderYAMLInvalid = "Provider YAML 内容无效"

const (
	msgTopologyShape      = "拨号拓扑字段类型无效"
	msgChainConflict      = "Provider 节点名称与代理链出口冲突"
	msgProxyName          = "Provider 代理必须是含非空 name 的对象"
	msgProxyRefs          = "Provider proxies 引用必须是字符串数组"
	msgProxyType          = "Provider type 必须是字符串"
	msgProxyList          = "Provider proxies 必须是数组"
	msgProviderRoot       = "Provider 必须是 YAML 对象\n字段：文档根节点"
	msgField              = "\n字段："
	msgSubscription       = "订阅"
	msgAggregation        = "订阅聚合"
	msgNodeSource         = "节点来源"
	msgSubscriptionSource = "本次订阅返回的内容"
	msgPreparedSource     = "本次准备的文件内容"
	msgNodesList          = "nodes 必须是数组"
	msgNoSource           = "订阅没有可用缓存或远程代理"
	msgOriginalDiagnostic = "\n原始诊断：%s"
	msgUnverifiedFallback = "拨号拓扑不能使用未验证的 Provider URL fallback"
	msgProviderOrigin     = "拨号拓扑 Provider 必须使用当前配置空间的订阅或聚合来源"
	msgPreparing          = "正在准备%s「%s」"
	msgPrepared           = "%s「%s」已准备"
	msgValidating         = "正在校验节点来源与拨号引用关系（%d 个文件）"
	msgFiltersFormat      = "来源正则筛选仅支持 Surge/Loon 格式"
)
