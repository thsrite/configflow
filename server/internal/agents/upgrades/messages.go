package upgrades

const (
	msgCorruptState       = "Agent 操作记录损坏（%s），请修复存储记录后重试"
	msgDockerUpgrade      = "Docker Agent 请更新镜像并重建容器；网页二进制更新不适用于 Docker"
	msgCannotConnect      = "无法连接 Agent 并确认更新能力"
	msgIdentityMismatch   = "Agent 身份或安装方式与网页记录不符"
	msgLegacyMethod       = "旧 Agent 未报告 Shell 安装方式，无法安全选择更新流程"
	msgArtifactsNotReady  = "更新文件未准备就绪，请先更新 ConfigFlow 主服务镜像"
	msgSubmitting         = "正在提交 Agent %s 更新请求"
	msgDelivered          = "Agent %s 更新请求已送达，等待设备处理"
	msgHAActive           = "DNS 主备正在同步，请等待两台发布完成后升级"
	msgUpgradeChanged     = "当前更新任务已改变，请刷新后查看最新结果"
	msgUpgradeBusy        = "已有更新正在执行或等待恢复，请先查询结果"
	msgDeploymentBusy     = "存在未结束的配置发布，请先完成或恢复"
	msgRequestUncertain   = "请求结果待确认，正在查询 Agent"
	msgUpgradeIneffective = "更新未生效：Agent 仍在运行 %s，可重新更新；详情请查看 Agent 日志"
	msgUpgradeUnknown     = "升级结果未能确认，正在继续查询；请查看 Agent 日志"
	msgConfirming         = "Agent 已报告更新完成，正在确认版本、身份与迁移结果"
	msgStartLog           = "开始升级 Agent %s：%s → %s"
	msgResultLog          = "Agent %s 升级 %s → %s %s%s"
)

const (
	msgFollowHA             = "Agent %s 上报地址由 %s 变为 %s；它是 DNS 高可用成员，记录未修改"
	msgHAUnchanged          = "是 DNS 高可用成员，记录未修改"
	msgFollowCorrupt        = "Agent %s 上报地址 %s，但%s；未跟随新地址，记录未修改"
	msgFollowDuplicate      = "Agent %s 上报地址 %s 与另一条同名记录相同，记录未修改"
	msgFollowUnreachable    = "Agent %s 上报地址由 %s 变为 %s，但面板用它的凭据连不上新地址，记录未修改"
	msgUnreachableUnchanged = "面板连不上新地址，记录未修改"
	msgFollowHelp           = "面板用这台 Agent 的凭据访问 %s:%s 没有成功，所以仍按 %s 联系它。请确认面板所在的机器能访问新地址和 Agent 端口；能访问后会自动更新。"
	msgFollowConflict       = "Agent %s 的地址暂未更新为 %s：%s"
	msgFollowUpdated        = "Agent %s 的地址已由 %s 更新为 %s（Agent 上报，已用其凭据核实可连接）"
	msgAddressNoticeTitle   = " · Agent 地址已自动更新"
	msgAddressNotice        = "设备的地址由 %s 变为 %s，面板已核实能用新地址连上它并更新了记录。如果上游路由关联了这台设备（代理网关或 DHCP DNS），请重新预览并发布上游路由。"
	msgViewAgent            = "查看 Agent"
	msgDockerAddress        = "容器没有指定 AGENT_IP，这里只修改面板记录的地址（填宿主机的局域网地址）。"
	msgLegacyAddress        = "这台 Agent 版本较旧，只修改面板记录的地址；升级 Agent 后可同时写入设备上的配置文件。"
	msgCurrentUnreachable   = "面板用当前地址 %s:%s 连不上这台 Agent。填写它现在的地址，保存时会先核实能连上。"
	msgEnterIPv4            = "请输入这台设备的局域网 IPv4 地址"
	msgDuplicateAddress     = "另一条同名的 Agent 记录已经在用 "
	msgNewUnreachable       = "面板连不上 %s:%s，没有修改。请确认地址正确，且面板所在的机器能访问它。"
	msgWrongAgent           = "%s:%s 有应答，但不是这台 Agent（凭据不匹配），没有修改。"
	msgNoReply              = "无应答"
	msgAddressRejected      = "设备没有接受这个地址（%s），没有修改。"
	msgWrittenNote          = "，已写入设备配置"
	msgManualAddress        = "Agent %s 的地址已由 %s 改为 %s（手动修改，已核实可连接%s）"
	msgAddressChanged       = "地址已改为 "
	msgDeviceWritten        = "，并写入了设备上的 Agent 配置。"
	msgRecordOnly           = "（只修改了面板记录）。"
)

const (
	msgSucceeded      = "成功"
	msgFailed         = "失败"
	msgRolledBack     = "失败，已回滚到原版本"
	msgRollbackFailed = "失败，且回滚失败"
)

const (
	// HASteps explains how to reconfigure HA after renumbering.
	HASteps = "它是 DNS 高可用成员，面板没有修改记录：两台设备的主备配置和路由器的放行名单里写的都是旧地址。" +
		"请依次：1. 停用 DNS 高可用；2. 等这台设备的地址在这里自动更新（约两分钟内）；" +
		"3. 重新配对并开启高可用；4. 重新预览并发布上游路由。"
	// HAEdit explains why a paired address cannot be edited directly.
	HAEdit = "这台设备是 DNS 高可用成员，不能在这里修改地址：两台设备的主备配置和路由器的放行名单里写的都是当前地址。" +
		"请依次：1. 停用 DNS 高可用；2. 修改地址；3. 重新配对并开启高可用；4. 重新预览并发布上游路由。"
	// EnvEdit explains the container environment address owner.
	EnvEdit = "这个容器的地址由环境变量 AGENT_IP 指定，容器每次启动都会用它。请修改 AGENT_IP 后重建容器，面板记录会自动更新。"
	// RouterHint reminds operators to republish dependent routes.
	RouterHint = "如果上游路由关联了这台设备（代理网关或 DHCP DNS），请重新预览并发布上游路由。"
)
