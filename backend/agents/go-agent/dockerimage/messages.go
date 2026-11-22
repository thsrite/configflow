package dockerimage

// Messages printed by docker-config.
const (
	msgUsage   = "用法：configflow-agent docker-config agent <服务> <名称> <端口> <配置路径> | services [-macvlan-ha] <核心>[=<配置路径>] ... | companion"
	msgBadCore = "无效的核心参数 %q：只支持 mihomo、mosdns、oxidns，每个最多一次，配置路径须为绝对路径"
	msgFailed  = "容器启动准备失败：%v"
)
