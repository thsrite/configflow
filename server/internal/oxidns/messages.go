package oxidns

var dnsSourceLabels = map[string]string{"local": "直连", "remote": "代理", "fallback": "备用"}

const (
	messageMergeSources      = "YAML 合并字段必须为对象或对象数组"
	messageUnsupportedFormat = "不支持的校验格式：%s"
	// These complete messages preserve the existing user-facing null-type diagnostics.
	messageNullRuleType             = "所选规则类型暂不能转换为 OxiDNS 规则：None"
	messageNullRulesetType          = "「%s」的类型被改成了 None：请改回原来的类型"
	messageNullMainType             = "主处理链 sequence_main 的类型是 None，不是 sequence"
	messageNullForwardType          = "本地上游 forward_local 的类型是 None，不是 forward"
	messageDNSUnknownKind           = "该类型"
	messageDNSUnsupportedKind       = "%s 规则不参与 DNS 分流"
	messageDNSIPUnsupported         = "IP 规则仅用于连接分流，不参与 DNS 处理"
	messageDNSCachedHead            = "自定义匹配位于规则之前且含缓存，拦截结果可能被缓存"
	messageDNSInvalidChoice         = "不支持的 DNS 处理：%s"
	messageDNSMissingRule           = "规则不存在：%s，请刷新后重试"
	messageDNSAssignmentUnsupported = "%s 规则不参与 DNS 分流，请把 DNS 设为默认上游"
	messageDNSLinkedUpstream        = "已绑定 DNS 联动，请改选直连 DNS"
	messageDNSStaleUpstream         = "所选上游已不在 OxiDNS 上游设置中，请刷新后重选"
	messageDNSUnchanged             = "OxiDNS 分流含自定义结构，DNS 未修改：%s"
	messageDNSInvalidResult         = "写入后 OxiDNS 配置校验失败：%s"
	messageDNSUpstreamKeyChanged    = "上游标识已更新，请刷新后重新选择；已有完整配置请在编辑器按当前上游重新生成分流"
)

const (
	messageNativePlugins             = "插件结构尚未完成，请先填写插件 tag 和参数"
	messageNativeDuplicateTags       = "插件 tag 重复，请先使用不同名称再联动修改"
	messageNativeSharedDelete        = "不能自动删除共享插件，请先在完整参数中调整引用"
	messageNativeRenameMapping       = "重命名映射必须对应唯一的原插件和新增 tag，不能覆盖现有插件"
	messageNativeStandardList        = "standard_add 必须为不重复的标准功能 tag 数组"
	messageNativeStandardEntry       = "标准功能新增必须对应唯一、类型正确且尚不存在的标准插件"
	messageNativeStandardRename      = "标准功能新增不能同时作为插件重命名目标"
	messageNativeRenameTag           = "重命名需要有效的非空 tag，并保持插件类型；请继续在完整参数中填写"
	messageNativeEmptyTag            = "请先填写非空的新插件 tag，再同步重命名或删除"
	messageNativeIncludeRenameReason = "include 中可能仍引用原 tag"
	messageNativeExternalRename      = "重命名前请先确认外部或环境变量中的引用"
	messageNativeCustomRename        = "存在无法安全自动重命名的自定义引用"
	messageNativeRenamed             = "已将插件「%s」的明确引用同步为「%s」"
	messageNativeParkedRename        = "暂存参数结构无效，不能记录插件重命名"
	messageNativeAliasRecord         = "插件重命名记录结构无效，请在完整参数中修正"
	messageNativeDeleteAndEnable     = "同一次修改不能删除标准功能的依赖并重新启用该功能，请分步调整"
	messageNativeDependencyRemoved   = "新增功能仍需要本次删除的依赖"
	messageNativeIncludeDelete       = "配置包含 include，不能自动修改其依赖链"
	messageNativeIncludeDeleteReason = "请先在完整参数或附加文件中调整共享引用"
	messageNativeRemoved             = "已同步停用关联功能、处理链和受管资源；可恢复参数已保留"
	messageNativePruned              = "已清理 %d 条不再被引用的受管规则下载；自定义和共享资源已保留"
	messageNativeStillReferenced     = "删除后仍有插件引用，请在完整参数中调整"
	messageNativeIncludeAdd          = "配置包含 include，不能自动接入标准功能"
	messageNativeIncludeAddReason    = "附加文件中可能存在共享引用"
	messageNativeSharedAdd           = "不能自动接入共享或自定义处理链，请先调整引用"
	messageNativeParked              = "暂存参数结构无效，请先在完整参数中修正"
	messageNativeUpdateNeedsSource   = "规则定时更新需要当前配置引用至少一个可下载订阅"
	messageNativeAddSourceReason     = "请先添加当前使用的订阅源"
	messageNativeCustomUpdate        = "规则更新包含自定义步骤，不能自动接入定时任务"
	messageNativeCustomUpdateReason  = "请在执行序列表单配置更新调用"
	messageNativeWithdrawnDownload   = "新增下载插件仍包含已撤回的受管资源，请先删除旧下载条目"
	messageNativeWithdrawnReason     = "下载目标已经不再被当前配置引用"
	messageNativeFallbackDraft       = "备用 DNS 地址尚未填写，已保留表单草稿，未接入代理回退链"
	messageNativeHostsResponse       = "Hosts 使用了自定义返回处理，请在完整参数中调整"
	messageNativeHostsResponseReason = "Hosts 调用后需要明确的响应处理"
	messageNativeAdded               = "已接入新增的标准功能，并同步恢复所需依赖和处理链"
	messageNativeCacheDumpOff        = "已停用缓存持久化并移除保存路径和间隔；缓存处理链保持不变，参数已暂存"
	messageNativeIncludeHosts        = "配置包含 include，不能自动移除 Hosts 处理链"
	messageNativeIncludeHostsReason  = "请先确认附加文件中的共享引用"
	messageNativeHostsOff            = "Hosts 来源已清空，已移除空插件及标准调用；已撤回的来源不会在恢复时重新加入"
)

const (
	messageNativeRenamesShape = "renames 必须为原 tag 到新 tag 的文本映射"
	// NativeConfigTextMessage is the native-editor text-field rejection.
	NativeConfigTextMessage = "native_config 必须为 YAML 文本"
)

const (
	messageArchiveState   = "OxiDNS 运行数据 state/ 不能包含在配置包中"
	messageArchiveFormat  = "OxiDNS 配置格式无效"
	messageArchiveInclude = "OxiDNS 完整配置包不支持外部 include"
)

const (
	// SettingsObjectMessage rejects a non-object whole-settings request.
	SettingsObjectMessage = "OxiDNS 设置必须是对象"
	// SettingsChangedMessage explains a stale whole-object editor save.
	SettingsChangedMessage = "OxiDNS 设置已在其他页面修改（例如策略规则的 DNS），请重新加载后再保存"
)
