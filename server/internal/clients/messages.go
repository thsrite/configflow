package clients

const (
	messageMode        = "DNS 策略只能是默认、智能分流或直连。"
	messageChanged     = "此设备已在其他页面修改，请重新载入后再保存。"
	messageUnsupported = "需要在当前配置接入 RouterOS 或 iKuai 才能发现设备"
	messagePartial     = "部分路由器读取失败，未出现的设备显示为未知"
	messageFailed      = "路由器读取失败，暂时无法发现设备"
	messageReadFailed  = "读取失败"
	messageCacheFailed = "路由器读取失败"
	messageTimeout     = "读取超时（%g 秒），请检查路由器负载和网络"
)
