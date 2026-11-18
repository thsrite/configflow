package agents

const (
	msgClientDevice       = "请选择有效的 Mihomo / OxiDNS 设备。"
	msgClientBinding      = "设备绑定的配置空间已改变，请重新打开后再保存。"
	msgClientNoteBody     = "设备备注请求格式无效。"
	msgClientNoteConflict = "此设备的备注已在其他页面修改，请重新打开后再保存。"
	msgConnectionBody     = "连接规则请求格式无效。"
	msgConnectionTarget   = "请选择有效的规则集和代理或直连目标。"
	msgConnectionDefaults = "默认规则集已改变，请重新打开后再保存。"
	msgDiscoveryBoolean   = "enabled 必须是布尔值"
	msgPolicyMissing      = "Agent 不存在。"
	msgPolicyService      = "设备策略仅支持 OxiDNS Agent。"
	msgPolicyBody         = "设备策略请求格式无效。"
	msgPolicyBinding      = "Agent 绑定的配置空间已改变，请重新载入。"
	msgPolicyConflict     = "配置或设备绑定已更新，请重新载入后再保存。"
)

// RegistrationLocked explains why ordinary installers cannot register while
// the panel's optional global registration-key policy is enabled.
const RegistrationLocked = "Invalid registration key：面板已启用注册锁定，普通 Shell 和 Docker Agent 安装不支持传入注册密钥。请使用 OxiDNS HA enrollment 安装，或联系管理员调整 AGENT_REGISTRATION_KEY 注册策略后重试。"
const msgGeneratedFileMissing = "生成结果中没有这个文件。"

const msgRuleSyncRequest = "规则同步设置无效"
const msgRuleSyncNotFound = "未找到这台 Agent"
