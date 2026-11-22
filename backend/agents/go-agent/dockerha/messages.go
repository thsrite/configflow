package dockerha

// User-facing messages. Every message except the msgSettings/msgOldConfig/
// msgOldSetting additions is the exact text the panel's former Python
// helpers printed, so guides, docs and support answers stay valid.
const (
	// Compose bundle bootstrap (ha-bootstrap).
	msgFileNotRegular   = "安装文件不是普通文件，拒绝继续。"
	msgFileTooLarge     = "安装文件过大，拒绝继续。"
	msgFileUnreadable   = "安装文件不存在、不可读或为软链接；请重新核对安装包和持久目录。"
	msgFileCorrupt      = "安装或身份文件损坏，拒绝覆盖。"
	msgFileInvalid      = "安装或身份文件格式无效，拒绝覆盖。"
	msgIdentityPartial  = "持久 Agent 身份不完整，拒绝启动或覆盖。"
	msgOwnerInvalid     = "安装归属标记无效，请重新生成安装包。"
	msgNotPrepared      = "未完成本次安装的准备步骤，或持久目录属于另一份安装。"
	msgAgentDirInvalid  = "Agent 持久目录不存在或为软链接。"
	msgLockNotRegular   = "Agent 登记锁不是普通文件。"
	msgForeignInstall   = "持久身份属于另一份安装，拒绝接管。"
	msgIdentityMismatch = "持久身份与本次安装标记不匹配，拒绝接管或覆盖。"
	msgIdentityVerified = "已核对持久 Agent 身份；无需安装凭据或登记网络，继续启动。"
	msgURLInvalid       = "首次登记地址无效，请重新生成安装包。"
	msgTokenInvalid     = "首次登记凭据无效，请重新生成安装包。"
	msgResponseTooLarge = "安装登记响应过大，未启动服务。"
	msgEnrollRejected   = "首次登记被拒绝（HTTP %d）；不跟随地址重定向。请确认最终服务地址；凭据过期时以相同设置重新生成安装包并准备后重试。"
	msgEnrollIncomplete = "首次登记未完成；请检查 ConfigFlow 地址、证书和网络，服务尚未启动。"
	msgResponseInvalid  = "安装登记响应无效，服务尚未启动。"
	msgDifferentServer  = "服务端返回了不同的身份，拒绝覆盖。"
	msgIdentitySaved    = "本机 Agent 身份已持久保存；继续启动并等待真实心跳。"
	msgSaveFailed       = "无法安全保存安装身份；请保留现场并检查目录权限，服务尚未启动。"

	// Script install enrollment (ha-enroll).
	msgEnrollSymlink        = "Agent 身份路径是软链接，已停止安装。"
	msgEnrollForeign        = "此目录属于另一份安装命令；请保留原命令或使用迁移。"
	msgEnrollExisting       = "Agent 数据目录已有身份文件；请选择旧容器迁移，不能覆盖。"
	msgEnrollFailedDefault  = "安装登记失败"
	msgEnrollRetry          = "%s；请回到引导重新生成或重试。"
	msgEnrollUnreachable    = "无法连接 ConfigFlow 登记设备，请检查服务地址、证书和网络后重试。"
	msgEnrollInvalid        = "安装登记响应无效，未写入身份文件。"
	msgEnrollUnreadable     = "现有 Agent 身份文件不可读，已停止安装。"
	msgEnrollDiffers        = "现有 Agent 身份与本次安装不一致，已停止安装。"
	msgEnrollKept           = "已核对本次安装的 Agent 身份，保留原文件。"
	msgEnrollDone           = "本机 Agent 身份已登记；启动后等待真实心跳与高可用环境检查。"
	msgSettings             = "安装命令参数无效；请回到引导重新生成。"
	msgProofMissing         = "本次安装的身份文件或标记缺失；请保留数据并检查，拒绝覆盖"
	msgProofUnreadable      = "本次安装的身份文件不可读，拒绝覆盖"
	msgProofMismatch        = "本次安装的身份与标记不匹配，拒绝继续"
	msgProofVerified        = "已核对本次安装的本地身份；保留原凭据并继续启动。"
	msgPortUnavailable      = "宿主机的 UDP/TCP 53 端口不可用；请先排查端口占用，再重试安装。"
	msgMigrationEnvironment = "无法读取原容器环境配置"

	// Migration check (ha-migration-check).
	msgEnvFormat       = "原环境变量格式不支持自动迁移"
	msgEnvName         = "原环境变量含多行值或无效名称，请保留原 Compose 手动迁移"
	msgOldConfig       = "无法读取原 Agent 配置；未执行迁移"
	msgAgentMismatch   = "原 Agent 名称、服务器地址、端口或配置路径不匹配；未执行迁移"
	msgHAPresent       = "原容器已有 HA 配置；请先在现有高可用设置中迁移虚拟 IP、停用并确认清理后再迁移，勿删除记录"
	msgOxiDNSOnly      = "仅支持单独运行 OxiDNS 的容器"
	msgNotOxiDNS       = "原容器不是单 OxiDNS 服务，请沿用原 Compose 手动调整"
	msgHealthURL       = "原 Agent 使用自定义 HTTP 健康检查，请沿用原 Compose 手动迁移"
	msgOldSetting      = "原 Agent 运行设置格式无效，请沿用原 Compose 手动迁移"
	msgMultilineValues = "原运行设置含多行值，请沿用原 Compose 手动迁移"

	msgUsage = "用法：configflow-agent ha-bootstrap | ha-enroll | ha-enroll-check <登记地址> | ha-migration-check <Agent 名称> <服务器地址> <端口> | ha-inspect | ha-port-check"
)
