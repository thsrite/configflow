package deploy

const (
	msgSource                 = "内容来源：%s"
	msgLine                   = "位置：第 %d 行"
	msgColumn                 = "，第 %d 列"
	msgExcerpt                = "出错内容："
	msgEOF                    = "出错内容：[文件末尾]"
	msgHidden                 = "出错内容：[敏感内容已隐藏]"
	msgDiagnostic             = "原始诊断：%s"
	msgArtifact               = "%s「%s」"
	msgPreparation            = "%s准备失败：%s"
	msgFile                   = "文件：%s"
	msgBeforeConversion       = "转换前的规则内容"
	msgInvalidGeneratedYAML   = "生成配置的 YAML 格式不正确"
	msgSubscription           = "订阅"
	msgConfig                 = "配置"
	msgRules                  = "规则集"
	msgAttachment             = "附加文件"
	msgGatewayProviderInvalid = "Linux 网关无法检查订阅节点，请先修复 Provider YAML"
	msgGatewayRoutingMark     = "Linux 网关与订阅节点中的 routing-mark 冲突，请移除后重新发布"
	msgDownloadTimeout        = "下载超时，请检查网络连接或稍后重试"
	msgDownloadProxy          = "代理连接失败，请检查规则下载代理是否可用"
	msgDownloadTLS            = "无法建立 HTTPS 安全连接，请检查规则源及其证书"
	msgDownloadConnect        = "连接失败，请检查规则下载地址及网络连接"
	msgDownloadHTTP           = "下载服务器返回错误（HTTP %d），请检查规则下载地址或稍后重试"
	msgDownloadFailed         = "下载失败，请检查规则下载地址及网络连接"
	msgDownloadMirrors        = "原始地址及 GitHub 代理地址均下载失败；"
	msgRawError               = "原始错误：%s"
	msgRuleStale              = "规则集「%s」更新失败，使用上一次有效缓存：%s"
	msgRuleDownload           = "无法下载「%s」规则集：%s。本次发布已停止，设备仍使用原配置。\n文件：%s\n修复：更换该规则的下载地址，或检查「GitHub 代理域名」和「系统设置 → 规则下载代理」。\n也可手动下载后导入规则库的内容型规则，再重新发布。"
	msgUnnamed                = "未命名"
	msgPacking                = "正在打包发布文件（%d 个文件）"
	msgOxidnsUnavailable      = "OxiDNS 还在使用不可用的规则集，发布已停止，设备配置没有改动。"
	msgOxidnsMissing          = "OxiDNS 配置用到了文件 %s，但附加文件里没有它，发布已停止，设备配置没有改动。\n请在 OxiDNS 设置的「附加文件」中添加它，或在 YAML 中删除这个引用。"
)

const (
	msgGeoxMapping    = "geox-url 必须是资源名称与下载地址的映射"
	msgGeoReuse       = "复用已有 %s 资源（%s），不重复下载"
	msgGeoDownload    = "正在下载 %s 资源：%s"
	msgGeoCheck       = "正在检查 %s 下载内容与文件大小"
	msgGeoReady       = "%s 已准备（%s 字节），已计算发布用 SHA256"
	msgGeoInvalidURL  = "geox-url.%s 未填写有效下载地址"
	msgGeoInvalid     = "下载结果为空或是网页，不是 Geo 数据文件"
	msgGeoFailure     = "%s 下载失败。本次发布已停止，设备仍使用原配置。\n原因：%s\n下载地址：%s\n目标文件：%s\n修复：更换 geox-url.%s，或检查「GitHub 代理域名」和「系统设置 → 规则下载代理」。\n也可手动下载对应数据文件，放到 Agent 机器的 %s，确保 Mihomo 可读取，再重新发布。"
	msgGeoLimit       = "%s 超出发布文件大小限制"
	msgAgentConfigDir = "Agent 配置目录"
)

const (
	msgPreviewInvalid        = "生成结果无效，请重新生成。"
	msgPreviewUnavailable    = "生成结果已过期或不可用，请重新生成后推送。"
	msgPreviewLimit          = "生成结果超过临时存储上限，请减少配套文件后重试。"
	msgPreviewProfileMissing = "生成时的配置空间已不可用，请重新选择并生成。"
	msgPreviewConfigChanged  = "配置已改变，请重新生成并检查后推送。"
	msgPreviewTargetChanged  = "目标设备或绑定已改变，请重新生成并检查后推送。"
	msgUpdateForPreview      = "请先更新目标 Agent，再生成最终下发配置。"
	msgFinalPreviewFailed    = "设备无法生成最终配置，请检查 Agent 状态。"
)

const msgUnexpandedInclude = "OxiDNS 发布配置必须先展开 include 后重新推送"

const (
	msgForwardingUpgrade  = "主路由分流（完整 TUN）需要 Mihomo Agent %s 或更新版本；旧版本在移除旧网关记录时可能关闭 IPv4 转发。请先在 Agent 页更新。"
	msgGenerating         = "正在生成 %s config.yaml 并检查基础配置"
	msgSavedSubscriptions = "已保存新下载的订阅节点，正在按保存的节点重新生成 config.yaml"
	msgAlreadyFetched     = "本次发布已经尝试过下载这个订阅，不再重复下载"
	msgGeneratedYAML      = "生成配置 YAML 格式错误"
	msgGeneratedConfig    = "config.yaml（生成配置）"
	msgConfigKind         = "%s 配置"
)

const (
	msgBindingChanged           = "草稿已保存，但设备绑定已改变，请重新选择设备后发布。"
	msgUploadAccepted           = "Upload accepted; awaiting valid Agent status"
	msgUploadUnknown            = "Upload outcome unknown; query deployment status"
	msgCapabilitiesFailed       = "Unable to query Agent deployment capabilities"
	msgTransactionalUnsupported = "Agent does not support transactional deployments; upgrade the Shell-installed or Docker Agent first"
	msgReloadUpgrade            = "请升级 Agent 到 1.8.30-go 或更新版本后使用规则自动热加载"
	msgFinalTargetChanged       = "目标设备配置已改变，请重新生成最终配置后推送。"
	msgCheckingGeo              = "正在检查并准备 Mihomo Geo 资源"
	msgCheckingOxiDNS           = "正在校验 OxiDNS 配置与规则文件"
	msgUploading                = "正在上传发布包（%d 个文件，%s 字节）"
)
