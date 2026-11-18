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

const (
	msgMissingSource = "请提供有效的规则来源和订阅地址"
	msgStaleCache    = "规则 %s 更新失败，继续使用上一次有效缓存：%s"
)

const (
	msgContentLine        = "规则内容第 %d 行：%s"
	msgUnnamedRuleset     = "未命名规则集"
	msgInvalidRuleContent = "规则内容无效。"
	msgRuleFile           = "文件"
	msgRuleLine           = "第 %d 行"
	msgRulePreview        = "，原文 %s"
	msgProblemCount       = "（共 %d 处错误）"
	msgRemoteRule         = "远端规则集「%s」声明为 %s/%s，%s%s：%s%s"
	msgContentText        = "规则内容必须是文本。"
	msgContentBytes       = "规则内容必须是文本或字节。"
	msgInvalidUTF8        = "内容不是有效 UTF-8 文本；请检查订阅链接、编码或是否误选了 MRS 格式。"
)

const (
	msgSourceGone      = "规则来源已不存在，请重新生成并发布配置。"
	msgSourceInline    = "规则来源已改为手写内容，请重新生成并发布配置。"
	msgSourceChanged   = "规则订阅地址已变更，请重新生成并发布配置。"
	msgSourceAmbiguous = "同一订阅地址对应多个不同规则类型或格式，无法确定当前来源；请重新生成并发布配置。"
)

const msgCacheMigrationDeferred = "规则缓存名称迁移暂缓：%s"

const (
	msgVideoService  = "视频服务"
	msgAIService     = "AI 服务"
	msgServiceDomain = "内置服务域名："
	msgPageAccess    = "访问失败："
	msgUnknownStatus = "未知"
	msgPageStatus    = "页面未成功返回（HTTP %s）"
	msgPageChallenge = "页面要求人机验证，无法识别"
	msgPageError     = "返回错误页，无法识别"
	msgPageNoName    = "未找到网站名称，可能是 API 或资源域名"
)
