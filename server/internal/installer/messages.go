package installer

const (
	msgRendererText          = "安装参数必须为文本。"
	msgRendererMount         = "安装挂载必须为完整挂载对象。"
	msgInstallationIdentity  = "安装记录的身份字段无效，请保留现场并重新生成安装命令。"
	msgInstallationExpiry    = "安装记录的有效期无效。"
	msgInstallationEncoding  = "安装记录包含无效 UTF-8 文本。"
	msgInstallationChanged   = "本次安装的设备信息已改变，请重新生成命令。"
	msgInstallationCollision = "该名称与地址已有 Agent；请使用旧容器迁移，不能创建或覆盖其身份。"
	msgComposeInvalid        = "Compose YAML 无效：%w"
	msgComposeMapping        = "Compose YAML 必须是对象"
)
