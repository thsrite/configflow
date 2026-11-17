package substore

const (
	msgInvalidYAML       = "订阅 YAML 无法解析：%w"
	msgUpdateNoBuiltin   = "当前环境没有内置 Sub-Store，无法在线更新"
	msgUpdateBusy        = "已有更新任务在进行中"
	msgUpdateUnavailable = "Sub-Store 正忙或服务不可用，请稍后重试"
	msgLatestUnavailable = "无法获取最新版本"
	msgAlreadyLatest     = "当前已是最新版本 %s"
	msgPreparingUpdate   = "准备更新到 %s…"
	msgStartingUpdate    = "开始更新到 %s"
)

const msgRuntimeBusy = "Sub-Store 正忙或正在更新，请稍后重试"
