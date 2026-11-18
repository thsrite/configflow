package health

const (
	// DNSError is the user-facing validation error for node-test DNS servers.
	DNSError = "测速 DNS 须为 IPv4[:端口]、https://DoH 或 tls://DoT，最多 4 个；不支持凭据、查询参数或片段"
	// BootstrapError is the user-facing validation error for bootstrap DNS servers.
	BootstrapError = "引导 DNS 须为 IPv4 或 IPv4:端口，最多 4 个"
)

const (
	msgCheckConfig       = "DNS 设置无效，请检查地址和引导 DNS"
	msgCheckDNSBootstrap = "引导 DNS 无法解析上游地址，请检查引导 DNS"
	msgCheckDNSFakeIP    = "DNS 返回了 Fake-IP，请改用返回真实 IP 的解析路径"
	msgCheckTimeout      = "DNS 查询超时，请检查网络和服务状态"
	msgCheckTLS          = "DNS TLS 验证失败，请检查上游地址和证书"
	msgCheckDNS          = "DNS 未返回有效地址，请检查解析路径"
	msgCheckSuccess      = "解析成功，返回真实 IP"

	msgSelectDNSMode          = "请选择系统 DNS 或自定义 DNS"
	msgCustomDNSMinOne        = "自定义 DNS 至少需要一个服务器地址"
	msgCustomDNSNeedsBoot     = "域名形式的 DoH / DoT 需要指定引导 DNS（IPv4 或 IPv4:端口）"
	msgProbeNotInstalled      = "独立测速组件未安装，请更新 ConfigFlow 镜像；源码部署请先构建 backend/node-probe"
	msgProbeStartFailed       = "测速组件无法启动，请检查可执行权限和 CPU 架构"
	msgVersionMismatch        = "测速组件版本不匹配，请更新 ConfigFlow 镜像；源码部署请重新构建 backend/node-probe"
	msgDNSCheckExitFailed     = "DNS 检测组件异常退出，请检查组件版本后重试"
	msgDNSCheckInvalidOutput  = "DNS 检测组件返回了无效结果，请检查组件版本"
	msgProbeInvalidSettings   = "节点测速设置无效，请检查测试地址及超时（500–10000 毫秒）"
	msgBatchPayloadTooLarge   = "本批节点配置过大，请减少每批节点数"
	msgProbeTimeout           = "测速组件执行超时，已结束本批测试，请重试"
	msgProbeBadDNS            = "节点测速 DNS 设置无效，请检查服务器地址及引导 DNS"
	msgProbeExitFailed        = "测速组件异常退出，请检查组件版本后重试"
	msgProbeInvalidOutput     = "测速组件返回了无效结果，请检查组件版本"
	msgNodeConversionFailed   = "无法解析完整节点配置，请检查格式或 Sub-Store 服务"
	msgSaveLatencyFailed      = "写入节点延迟结果失败"
	msgProbeBusy              = "已有节点测速正在进行，请稍后重试"
	msgNamesAndKeysConflict   = "names 与 keys 不能同时指定"
	msgMaxNodesExceeded       = "单次最多测试 %d 个节点，请分批测试"
	msgDuplicateNodeName      = "存在同名节点，请使用 keys 指定节点来源后重试"
	msgMaxRemoteConversions   = "单次最多测试 %d 个需要 Sub-Store 转换的节点，请分批测试"
	msgTextScalarRequired     = "字段值必须为字符串或数字"
	msgStrictStringRequired   = "字段值必须为字符串"
	msgStoredSystemCfgInvalid = "系统配置 system_config 必须是对象"
	msgStoredNodeIDInvalid    = "节点 id 必须是字符串，当前值：%s"
	msgStoredNodeNameInvalid  = "节点 %s 的 name 必须是字符串，当前值：%s"
	msgStoredSubIDInvalid     = "订阅 id 必须是字符串，当前值：%s"
	msgCachedNodeNameInvalid  = "订阅 %s 缓存节点的 name 必须是字符串，当前值：%s"
	msgCachedNodesMustBeArray = "订阅 %s 缓存的 nodes 必须是数组，当前值：%s"
)
