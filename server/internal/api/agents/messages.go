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

// RegistrationLocked gives the recovery path for missing or invalid bootstrap proof.
const RegistrationLocked = "安装命令已失效，请在面板重新生成安装命令或设置注册密钥。"
const msgRegistrationStore = "无法保存 Agent 注册凭据，请检查数据目录后重新生成安装命令。"
const msgGeneratedFileMissing = "生成结果中没有这个文件。"

const msgRuleSyncRequest = "规则同步设置无效"
const msgRuleSyncNotFound = "未找到这台 Agent"
const msgInstallationSize = "安装请求内容过大。"
const msgInstallationJSON = "安装请求需要 JSON 对象。"
