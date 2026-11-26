package routers

const (
	messagePendingGenericBlocked = "差异报告中有暂时无法发布的项目，请打开差异报告查看原因。"
	messagePendingSkipped        = "本次跳过"
	messagePendingWithdraw       = "待撤回"
	messagePendingInstall        = "待安装"
	messagePendingAdopt          = "待接管"
	messagePendingAdd            = "待添加"
	messagePendingUpdate         = "待更新"
	messagePendingRemove         = "待移除"
	messagePendingClose          = "待关闭"
	messagePendingMore           = "%s（另有 %d 项）"
	messagePendingConflictDetail = "%s：%s"
	messagePendingConflict       = "%s 存在冲突"
	messagePendingRouterTimeout  = "读取超时（%g 秒），请检查路由器负载和网络后重试。"
	messagePendingReadError      = "读取路由器失败"
	messagePendingInternalError  = "读取失败：内部错误"
	messagePendingWaitTimeout    = "读取上游路由状态超时，请稍后重试。"
	messagePendingMalformed      = "无法解析上游路由差异报告，请重新检查。"
)

var pendingSectionWords = map[string]string{"missing": messagePendingAdd, "pending": messagePendingUpdate, "conflict": "冲突", "unknown": "待确认"}
