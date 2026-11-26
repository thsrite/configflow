package host

import "fmt"

const (
	msgWrongService     = "Linux 网关只能应用到 Mihomo Agent。"
	msgEnvironment      = "自动网络配置适用于 Linux Shell 安装，请选择原生安装的 Mihomo Agent。"
	msgUpgrade          = "请先将 Mihomo Agent 更新到 %s 或更新版本。"
	msgNotAllowed       = "不支持这个网关操作。"
	msgMissing          = "未找到这台 Agent。"
	msgProfileChanged   = "设备绑定的配置空间已变化，请重新打开网关设置。"
	msgTooLarge         = "Agent 返回的网关数据过大。"
	msgEndpointUpgrade  = "设备尚未提供网关接口，请更新到包含本功能的 %s 或更新版本。"
	msgAuth             = "Agent 认证失败，请检查设备注册状态。"
	msgParse            = "Agent 返回的网关数据无法解析，请刷新设备状态。"
	msgShape            = "Agent 返回的网关数据格式异常。"
	msgIncomplete       = "网关操作未完成，请刷新状态后查看检查结果。"
	msgUncertain        = "未能确认网关操作结果，请先刷新设备网络状态，勿重复应用。"
	msgForwardUncertain = "未能确认转发持久化结果，请先刷新设备状态，勿重复操作。"
	msgConnect          = "暂时无法连接 Agent，请检查设备连接。"
	msgSpecFields       = "请填写网卡、Linux IPv4、LAN 网段和主路由 IPv4。"
	msgSpecText         = "网关转发的网络参数必须是有效的短文本。"
	msgInterface        = "请选择实际的 LAN 网卡，不能使用回环网卡。"
	msgIPv4             = "需要合法 IPv4 地址和规范 LAN 网段，例如 192.168.1.0/24。"
	msgLAN              = "Linux 和主路由需要不同的可用 IPv4 地址，且位于同一个 /8 至 /30 LAN 网段。"
	msgDisabled         = "请先启用并保存当前配置空间的 Linux 网关。"
	msgRemoveBody       = "撤销网关转发不接受自定义命令或网络参数。"
	msgPrepareBody      = "开启转发持久化只接受 prepare_tun 参数。"
	msgProxyBody        = "为代理网关开启转发持久化不接受额外参数。"
	msgSplitRequired    = "仅主路由分流（完整 TUN）的配置空间，或「上游路由」中作为代理网关的 Mihomo Agent 可开启转发持久化。"
	msgStandalone       = "单独安装的 Mihomo Agent 需更新到 1.8.41-go 或更新版本才能开启并持久化 IPv4 转发。"
	msgForwardNotKept   = "设备上仍有旧版 Linux 网关记录，且 IPv4 转发尚未持久化：%sAgent 撤销该记录时会把 IPv4 转发还原为关闭，主路由转来的连接会中断。请先在 Linux 网关安装向导的「检查转发与 TUN」中开启并持久化 IPv4 转发。"
	msgOffNotKept       = "关闭 Linux 网关会让 Agent 撤销「%s」上的旧版网关记录，并把这台 Linux 的 IPv4 转发还原为关闭，主路由分流转来的连接会中断。这台设备尚未持久化 IPv4 转发，已阻止；请保持 Linux 网关开启。如这台设备是路由器的代理网关，可在 Agent 页这台设备的卡片上开启并持久化 IPv4 转发后再操作。"
	msgOffUpgrade       = "关闭 Linux 网关前需要确认「%s」已持久化 IPv4 转发，请先将 Mihomo Agent 更新到 1.8.40-go 或更新版本。"
	msgOffUnknown       = "无法读取「%s」的 IPv4 转发状态，已阻止关闭 Linux 网关：%s。请确认设备在线后重试。"
	msgLegacyRemove     = "撤销会删除旧版网关记录，并把这台 Linux 的 IPv4 转发还原为关闭，主路由分流转来的连接会中断。这台设备尚未持久化 IPv4 转发，已阻止；请保持网关转发。如这台设备是路由器的代理网关，可在 Agent 页这台设备的卡片上开启并持久化 IPv4 转发后再操作。"
	msgLegacyUpgrade    = "撤销前需要确认这台 Linux 已持久化 IPv4 转发，请先将 Mihomo Agent 更新到 1.8.40-go 或更新版本。"
	msgPublishUnknown   = "无法确认设备上是否仍有旧版 Linux 网关记录，已停止发布：%s"
	msgPublishAction    = "发布完整 TUN 后，"
	msgPublishOff       = "待发布的 Mihomo 配置关闭了 Linux 网关：发布后 Agent 会撤销设备上的旧版网关记录，并把 IPv4 转发还原为关闭，主路由分流转来的连接会中断。这台设备尚未持久化 IPv4 转发，已停止发布。请在 Linux 网关中重新开启并保存后再发布。"
	msgLinkedMissing    = "关联的 Agent 已删除，请在路由器设置中重新选择代理网关。"
	msgLinkedService    = "代理网关不是 Mihomo Agent。"
	msgLinkedDocker     = "Docker 部署的 Agent 不管理宿主机网络，请在宿主机上开启并持久化 IPv4 转发。"
	msgOffline          = "Agent 离线。"
	msgOfflineWrite     = "Agent 离线，无法开启转发。"
	msgLinkedUpgrade    = "请先将 Mihomo Agent 更新到 1.8.41-go 或更新版本，才能在此开启并持久化 IPv4 转发。"
	msgConfigBlocked    = "IPv4 转发持久化文件 /etc/sysctl.d/99-configflow-aio-forward.conf 已被修改或不是 ConfigFlow 写入的内容，请先在设备上核对。"
	msgHookBlocked      = "IPv4 转发启动脚本已被修改，请先在设备上核对。"
)

var removedMessages = map[string]string{
	"full_tun":      "旧网关记录已清理，IPv4 转发保持开启。",
	"full_tun_none": "设备上没有旧网关记录，无需清理；IPv4 转发保持不变。",
	"legacy":        "网关转发已撤销，主路由转来的连接已中断。请删除主路由上指向这台 Linux 的静态路由，或重新应用网关转发。",
}

// ForwardingNotKept returns the shared publish/remove forwarding warning.
func ForwardingNotKept(action string) string { return fmt.Sprintf(msgForwardNotKept, action) }
