package routers

const (
	messageWorkspaceStale        = "配置已在其他页面更新，请重新载入后再保存。"
	messageManagementChanged     = "路由器设置已变化，请刷新后重试"
	messageManagementMissing     = "路由器已不存在"
	messageDeletePolicy          = "请先清空策略路由选择并发布撤回，再删除路由器记录"
	messageDeleteIPv6Fake        = "请先关闭 IPv6 Fake-IP 并发布撤销，再删除路由器记录"
	messageDeleteIPv6Routes      = "请先关闭 Mihomo IPv6 或取消相关规则集，并发布撤回 IPv6 规则集路由，再删除路由器记录"
	messageDeleteDNS             = "请先关闭强制 DNS 并发布撤销，再删除路由器记录"
	messageDeletePD              = "请先关闭 IPv6 前缀变化保护并发布撤回，再删除路由器记录"
	messageRoutePreviewStale     = "操作内容或路由状态已变化，请重新预览后确认"
	messageManagementCreated     = "已添加主路由 %s（%s）"
	messageManagementDeleted     = "已删除主路由记录 %s"
	messageManagementUpdated     = "已修改主路由 %s 的设置：%s"
	messageManagementNoChange    = "无变化"
	messageManagementRoute       = "主路由 %s：%s%s"
	messageManagementUnchanged   = "（没有变化）"
	messageManagementRouteFailed = "主路由 %s 静态路由变更失败：%s"
)
