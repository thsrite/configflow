package settings

const msgInvalidUnicode = "请求内容包含无效的 Unicode 字符"

const (
	msgWebPortRequired = "请提供 Web 监听端口"
	msgWebPortInvalid  = "Web 端口须为 1–65535 的整数"
	msgWebPortAccess   = "无法访问 Web 监听配置，请检查容器数据目录权限"
	msgProxyRequired   = "请提供规则下载代理，留空可清除设置"
	msgProxyInvalid    = "规则下载代理须为有效的 HTTP/HTTPS 代理地址，或留空使用现有网络"
	msgProxySave       = "保存规则下载代理失败"
	msgBackupObject    = "请求体必须是非空 JSON 对象，请使用 Content-Type: application/json"
	msgBackupFields    = "请提供 WebDAV 备份配置字段"
	msgBackupStrings   = "WebDAV 地址、用户名、密码和路径必须是字符串"
	msgBackupBool      = "auto_backup 必须是布尔值"
	msgBackupRetention = "保留份数须为 0–1000 的整数，每日保留天数须为 0–3650 的整数"
	msgBackupSaved     = "备份配置已保存"
)

const (
	msgUnchanged       = "无变化"
	msgBackupDisabled  = "已关闭"
	msgBackupEnabled   = "已开启"
	msgBackupAutomatic = "自动备份"
)
