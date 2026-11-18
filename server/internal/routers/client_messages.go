package routers

const (
	messageClientsTraffic     = "按 2 分钟内经过路由器的流量判断在线"
	messageClientsNoLAN       = "无法读取路由器的局域网地址，没有出现的已保存设备显示为未知"
	messageClientsDHCP        = "无法读取 DHCP 租约：%s"
	messageClientsSlow        = "路由器响应太慢，读取时间已用完"
	messageClientsOptional    = "无法读取%s：%s"
	messageClientsFallback    = "未发现启用的 DHCP 服务，按带私有地址的接口（%s）判断局域网"
	messageClientsNoInterface = "无法确定局域网接口：没有启用的 DHCP 服务，也没有配置私有地址的接口"
	messageClientsIncomplete  = "部分表读取失败，没有出现的已保存设备显示为未知"
	messageClientsNoTest      = "账号缺少 test 权限，无法确认静态 IP 设备是否在线"
	messageClientsCap         = "待确认的设备较多，只主动确认了 %d 台，其余 %d 台显示为近期见过"
	messageClientsUnsettled   = "%d 台设备未能及时确认，显示为近期见过"
)
