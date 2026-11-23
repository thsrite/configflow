package repo

const (
	msgStoreRecovery       = "无法加载配置数据库 %q：%s；请先停止服务并保留原文件，再从可用备份恢复；或使用最后一个能正常读取此库的版本导出全部数据后恢复。原数据库未改动。"
	msgModelFieldIssue     = "配置字段类型不匹配，已按缺省值解码：%s（%s）"
	msgModelUnknownMember  = "配置中有未声明的字段，读取时已丢弃：%s（%d 处）"
	msgModelRowInvalid     = "字段 %s/%s/%s 无法按配置模型读取（成员名重复或不是有效 JSON）：%w"
	msgStoreDocumentObject = "配置文档必须是对象"
	msgStoreSectionObject  = "配置节 %s/ 必须是对象"
	msgStoreProfilesObject = "profiles 必须是对象"
	msgStoreProfileObject  = "配置节 profile/%s 必须是对象"
)

const msgRouterJobBusy = "路由器正在发布或同步，不能替换连接或删除记录，请稍后重试"

// Stored-document checks shared by load and write validation.
const (
	msgSchemaMismatch          = "配置存储版本不匹配"
	msgSectionMissing          = "缺少配置节："
	msgGroupNameEmpty          = "策略组名称必须非空"
	msgGroupOrderArray         = "策略组排序必须是数组"
	msgGroupOrderEntry         = "策略组排序项必须是 type/id 对象"
	msgRuleItemType            = "规则类型必须为 rule 或 ruleset"
	msgNodeDialerRef           = "共享节点不能保存拨号引用，请在策略组中创建代理链"
	msgLibraryPolicy           = "规则仓库不包含目标策略，请在规则配置中设置"
	msgDefaultProfileIDText    = "默认配置空间 ID 必须是字符串"
	msgLibraryNames            = "共享规则仓库名称必须非空且唯一"
	msgResourceName            = "资源 name 必须是非空字符串"
	msgNodeParamsObject        = "节点 params 必须是对象，不能为 null"
	msgNodeProxyString         = "节点 proxy_string 必须是字符串"
	msgNodeSubscriptionMissing = "节点引用的订阅不存在"
	msgResourceID              = "资源 id 必须是非空字符串"
	msgMemberType              = "%s 类型不正确"
	msgDefaultProfileRequired  = "必须保留默认配置"
	msgProfileIDMismatch       = "配置内容与 ID 不一致"
	msgRevisionInvalid         = "配置修订号无效"
	// Section member shapes (validateDocument and strict input, O18 T1a).
	msgMemberObject           = "%s 参数必须是对象"
	msgRoutersShape           = "routers 参数必须是包含 items 列表的对象"
	msgSystemSectionsObject   = "系统设置和备份设置必须是对象"
	msgSystemMembers          = "system 节只接受全局设置、备份与 Agent"
	msgProfileSharedCopies    = "独立配置不能保存共享资源副本"
	msgProfileImportOverrides = "独立配置不接受资源白名单或节点拨号覆盖字段"
	// List labels in itemsOf messages.
	labelProxyGroups = "策略组"
	labelRuleConfigs = "规则配置"
)

// msgCommittedHookSkipped is logged when the committed document cannot be
// handed to the ConfigurationCommitted hook.
const msgCommittedHookSkipped = "配置已保存，但无法交给自动备份：%v"

// Old-format import conversion.
const (
	msgLegacyShape             = "旧配置格式无效：资源列表与条目必须是数组和对象"
	msgLegacyRuleName          = "旧配置的规则名称必须是字符串"
	msgLegacyDialerProxyString = "旧配置节点的 proxy_string 无法添加拨号代理"
)

// msgRouterIDInvalid refuses a router record without a scalar id.
const msgRouterIDInvalid = "路由器 ID 无效"

// msgImportResultShape refuses an import builder result of the wrong shape.
const msgImportResultShape = "导入结果格式无效"

const (
	msgRouterIDImmutable     = "不能修改路由器 ID"
	msgRouterSettingsChanged = "路由器设置已变化，请刷新后重试"
)

const (
	msgInvalidUTF8         = "configuration is not valid UTF-8"
	msgImportObject        = "导入内容必须是 JSON 对象"
	msgExportHooksMissing  = "脱敏导出尚未接入（脱敏 hooks 未注册），已拒绝导出"
	msgDeletionHookMissing = "节点删除功能尚未接入（PlanNodeDeletion 未注册）"
	msgDeletionConfirm     = "请先查看并确认删除影响"
	msgDeletionChanged     = "节点或引用关系已变化，请重新查看删除影响并确认"
	msgMetadataObject      = "Profile metadata must be an object"
	msgProfileNameEmpty    = "配置名称不能为空"
	msgMetadataFields      = "只能修改配置名称和说明"
	msgMetadataStrings     = "配置名称和说明必须是字符串，名称不能为空"
	msgProfileChanged      = "配置已更新，请刷新后重试"
)

const (
	msgResourceArray  = "%s 必须是数组"
	msgResourceObject = "资源必须是 JSON 对象"
)

const (
	msgLegacyBackupSections       = "旧多配置备份必须包含 system 索引和全部 profiles 配置内容"
	msgLegacyIndexVersion         = "不支持的旧配置索引版本；请保留原数据，不要重置"
	msgLegacyIndexLabel           = "旧配置索引"
	msgLegacyIndexDefault         = "旧配置索引缺少默认配置"
	msgLegacyIndexContents        = "旧多配置备份的索引与配置内容不一致"
	msgImportFormats              = "仅支持原版单配置、schema 2 完整多配置备份或当前格式；未修改原数据"
	msgLegacyProfileObject        = "旧配置内容必须是对象"
	msgLegacyIntermediateFields   = "旧配置包含不支持的中间格式字段；未修改原数据"
	msgSystemSettingsObject       = "系统设置必须是对象"
	defaultProfileName            = "默认配置"
	msgRawDialerName              = "原始 dialer-proxy 必须是非空名称"
	msgProfileImportReferences    = "独立配置导入只接受引用，不接受共享资源副本"
	msgProfileImportFormat        = "独立配置导入仅支持当前格式"
	msgImportUnknownFieldPrefix   = "导入结果包含未知字段："
	msgSharedChanged              = "共享资源已更新，请刷新后重试"
	msgSystemChanged              = "系统设置已更新，请刷新后重试"
	msgRuleLibraryExists          = "规则库 ID 已存在，请刷新后重试"
	msgRuleCacheExists            = "规则缓存已存在，请使用新的规则 ID"
	msgRuleLibraryUncertain       = "规则保存结果待确认，请刷新列表核对，勿重复导入"
	msgDeletionBlockedPrefix      = "无法删除："
	msgMigrationPathSymlink       = "旧配置路径不能是符号链接"
	msgMigrationPathOutside       = "旧配置路径不在数据目录内"
	msgRouterConnectionChanged    = "路由器连接已变化，无法保存发布回读记录"
	msgDefaultProfileOnly         = "只有一个配置空间时不能删除默认配置"
	msgDefaultReplacementRequired = "删除默认配置时需要指定新的默认配置空间"
	msgDefaultReplacementPrefix   = "新的默认配置空间不存在："
	msgProfileInUsePrefix         = "配置「"
	msgProfileAgentsInUseSuffix   = "」仍被以下 Agent 使用："
	msgLegacyMissingResource      = "旧配置 %s 引用的本地资源不存在：%s/%s"
	msgLegacyNodeSubscription     = "旧配置 %s 的节点引用的订阅不存在"
	msgLegacyDialerTarget         = "旧配置 %s 的原始拨号目标不存在或名称不唯一：%s"
	msgResourceObjects            = "%s 必须是资源对象数组"
	msgResourceIDRequired         = "%s 中每一项必须有 ID"
	msgDatabaseMustBeRegular      = "%s 必须是普通文件"
	msgLegacyOnlyDataDir          = "数据目录中有旧版配置文件（%s），但没有 %s。此版本不再迁移 SQLite 之前的旧格式：请先升级到最后一个 Python 版本的 ConfigFlow 并启动一次完成迁移，再升级到此版本。配置数据未改动。"
)

const msgUnknownResourceType = "未知资源类型"

const msgProfileFieldsObject = "Profile fields and baseline must be objects"

const msgProfileComposition = "资源由策略组直接引用，拨号组合请使用代理链类型"

// Storage upgrade (design/o17-runtime-state.md 2.5).
const (
	msgUpgradeBackupFailed = "升级配置数据库（存储版本 %d → %d）前的备份失败：%v。数据库未改动，服务没有启动。" +
		"请检查数据目录下 backups 目录（%s）的磁盘空间和写入权限，处理后重新启动。"
	msgUpgradeFailed = "升级配置数据库（存储版本 %d → %d）失败：%v。数据库未改动（升级前备份：%s），服务没有启动；请保留日志后重新启动重试。"
	msgUpgraded      = "配置数据库已升级到存储版本 %d：%d 条 Agent 运行记录移到独立的状态表；升级前备份 %s"
	msgBackupCheck   = "备份文件检查失败：%v"
)
