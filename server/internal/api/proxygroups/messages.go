package proxygroups

const (
	msgObject       = "策略组请求必须是 JSON 对象"
	msgDeleteCreate = "delete_fields 仅适用于更新"
	msgImmutableID  = "策略组 ID 不可修改，请保留原 ID"
	msgNameExists   = "策略组名称已存在，请使用其他名称"
	msgUsed         = "策略组仍被规则或其他策略组引用，请先修改引用后再删除或禁用"
	msgDetach       = "已保存；%d 个 Mihomo 入口已解除固定策略，恢复各自的规则路由。"
	msgRename       = "已保存；已同步更新 %d 个 Mihomo 入口的固定策略名称。"
	msgBatchObject  = "批量测速地址请求必须是 JSON 对象"
	msgBatchIDs     = "ids 必须是非空且不重复的策略组 ID 字符串数组"
	msgGroupMissing = "策略组不存在：%s"
	msgFollowHealth = "策略组「%s」跟随其他组，请修改其来源组的测速地址"
	msgNoHealth     = "策略组「%s」不支持节点测速"
	msgOrder        = "以下策略组 id 不存在: %s"
)

const (
	msgPreviewObject    = "预览请求必须是 JSON 对象"
	msgPreviewSelection = "预览来源 ID 必须是数组"
	msgRegexType        = "正则表达式必须是字符串"
	msgExcludeType      = "排除正则必须是字符串"
	msgRegexRequired    = "请输入正则表达式"
	msgRegexInvalid     = "正则表达式无效: %s"
	msgExcludeInvalid   = "排除正则无效: %s"
)
