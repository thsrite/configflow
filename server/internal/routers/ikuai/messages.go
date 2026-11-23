package ikuai

// iKuai driver messages (backend/routers/ikuai.py). %s/%d placeholders are
// filled in the order the Python f-strings used.
const (
	messageIkuaiTLS             = "iKuai HTTPS 连接失败：请检查证书是否可信、是否过期，以及管理地址和 HTTPS 端口是否正确。"
	messageIkuaiConnectTimeout  = "连接 iKuai 超时（%d 秒）：请检查管理地址、端口，以及 ConfigFlow 所在主机到路由器的网络是否畅通。"
	messageIkuaiReadTimeout     = "等待 iKuai 响应超时（%s 秒）：请检查网络连接和路由器负载，稍后重试。"
	messageIkuaiConnection      = "无法连接 iKuai 管理服务：请检查地址、端口和域名解析，确认管理服务已开启，并允许 ConfigFlow 所在主机访问。"
	messageIkuaiTransport       = "与 iKuai 通信失败：请检查管理地址、端口和网络连接，稍后重试。"
	messageIkuaiEmptyReply      = "iKuai 没有返回任何内容：请求可能过大或被 iKuai 拒绝"
	messageIkuaiNotJSON         = "iKuai 返回的不是 JSON，请确认地址指向管理页面"
	messageIkuaiReplyFormat     = "iKuai 返回格式无法识别"
	messageIkuaiRedirectTarget  = "iKuai 要求跳转到 %s%s。请核对浏览器打开的管理地址，在「编辑路由器」中修改地址后重试。"
	messageIkuaiRedirect        = "iKuai 将请求转到了其他页面%s。请在浏览器打开管理页面，核对最终地址的协议和端口；若地址无误，请检查管理入口或反向代理的跳转设置。"
	messageIkuaiHTTP            = "iKuai %s%s。"
	messageIkuaiHTTPCode        = "（HTTP %d）"
	messageIkuaiHTTPServer      = "管理服务发生错误，请确认浏览器能否正常打开管理页面，稍后重试"
	messageIkuaiHTTPOther       = "请求未成功，请检查管理地址、端口和管理入口设置"
	messageIkuaiLoginFailed     = "iKuai 登录失败：%s%s"
	messageIkuaiLoginDefault    = "账号或密码错误"
	messageIkuaiErrorCode       = "（错误码 %s）"
	messageIkuai3x              = "检测到 iKuai 3.x 的响应格式，只支持 iKuai 4.x"
	messageIkuaiNoRead          = "iKuai 账号没有%s的访问权限（错误码 1001）。请在 iKuai 的用户组中授予读取权限，或改用管理员账号。"
	messageIkuaiNoWrite         = "iKuai 账号没有%s的执行权限（错误码 1002）。请在 iKuai 的用户组中授予修改权限，或改用管理员账号。"
	messageIkuaiNoFunc          = "iKuai 不提供 %s 接口（错误码 1003%s）。当前固件可能不支持这项功能，请确认 iKuai 版本。"
	messageIkuaiSession         = "iKuai 登录会话已失效（错误码 1008），重新登录后仍未恢复，请稍后重试。"
	messageIkuaiCallFormat      = "iKuai 调用 %s 返回格式无法识别"
	messageIkuaiCallFailed      = "iKuai 调用 %s 失败：%s（错误码 %s）"
	messageIkuaiUnknownError    = "未知错误"
	messageIkuaiWriteFailed     = "iKuai %s %s 失败：%s"
	messageIkuaiListIncomplete  = "iKuai %s列表读取不完整或读取期间发生变化，请重试"
	messageIkuaiListIDs         = "iKuai %s列表缺少唯一有效 ID，无法安全编辑"
	messageIkuaiVersionFormat   = "iKuai 版本信息格式无法识别，请检查固件版本和管理接口。"
	messageIkuaiRedirectFormat  = "iKuai 重定向地址格式无法识别，请在浏览器核对管理地址、协议和端口。"
	messageIkuaiVersion         = "只支持 iKuai 4.x，当前版本 %s"
	messageIkuaiOperationFormat = "iKuai 操作格式无法识别"
	messageIkuaiRowMissing      = "iKuai %s 中找不到 id=%s，可能已被手动删除"
	messageIkuaiAddUnconfirmed  = "iKuai %s 已提交但读不回新对象"
	messageIkuaiRoutesMissing   = "iKuai static_rt 已提交但读不回新对象："
	messageIkuaiAddGone         = "iKuai %s 中找不到本次新增的对象（名称 %s），可能已被手动修改或删除，请检查"
	messageIkuaiAddChanged      = "iKuai %s 中名称为 %s 的对象已被修改，未自动删除，请检查"
	messageIkuaiRouteNetwork    = "iKuai static_rt 中 id=%s 的目标网段无法识别，未删除，请检查"
	messageIkuaiRestoreReplaced = "iKuai %s 中 id=%s 的对象已被替换或改名，未自动恢复，请检查"

	messageIkuaiDHCPIncomplete    = "iKuai %s 配置读取不完整，无法安全下发 DHCP DNS"
	messageIkuaiDHCPFields        = "iKuai DHCP 字段与已验证的 4.x 格式不兼容，无法保留完整配置（缺少：%s；未知：%s）"
	messageIkuaiDHCPNone          = "无"
	messageIkuaiDHCPFieldType     = "iKuai DHCP 字段 %s 类型无法识别"
	messageIkuaiDHCPEnabled       = "iKuai DHCP 启用状态无法识别"
	messageIkuaiDHCPStatus        = "iKuai DHCP 服务未生效或无法确认运行状态"
	messageIkuaiDHCPRelay         = "iKuai DHCP 仅响应中继请求，无法确认该网段客户端的 DNS 覆盖"
	messageIkuaiDHCPInterfaces    = "iKuai DHCP 限制了物理接口，无法确认完整网段的 DNS 覆盖"
	messageIkuaiDHCPNetwork       = "iKuai DHCP 地址池、掩码或网关无法确定唯一网段"
	messageIkuaiDHCPDNSInherited  = "iKuai DHCP DNS 包含无法识别的自动或继承配置"
	messageIkuaiDHCPStaticAddress = "iKuai 静态 DHCP 绑定缺少有效地址，无法排除 DNS 覆盖"
	messageIkuaiDHCPStaticPartial = "iKuai 静态 DHCP 绑定信息不完整，无法排除 DNS 覆盖"
	messageIkuaiDHCPStaticDNS     = "该网段存在静态 DHCP 绑定的独立 DNS，请先移除绑定中的 DNS 覆盖"
	messageIkuaiDHCPStaticFields  = "iKuai 静态 DHCP 绑定含未验证字段，无法排除 DNS 覆盖"
	messageIkuaiDHCPTarget        = "iKuai DHCP 目标配置不存在或标识不唯一"
	messageIkuaiDHCPReadback      = "iKuai DHCP DNS 回读校验失败：DNS 或其他 DHCP 配置与提交值不一致"
	messageIkuaiDHCPUndoRecord    = "iKuai DHCP DNS 撤销记录不完整"
	messageIkuaiDHCPScope         = "iKuai DHCP DNS 操作缺少已预览网段"
	messageIkuaiDHCPDrift         = "iKuai DHCP 配置已变化，请重新预览"
	messageIkuaiDHCPAddress       = "iKuai DHCP DNS 需要单个有效 IPv4 地址"
	messageIkuaiDHCPRollback      = "%s；DHCP DNS 局部撤销失败：%s"

	messageIkuaiGroupOp         = "iKuai IP 分组操作无效，请重新预览"
	messageIkuaiGroupNotOurs    = "iKuai IP 分组「%s」不存在或不属于 ConfigFlow，请重新预览"
	messageIkuaiGroupNoID       = "iKuai IP 分组「%s」缺少有效 ID，无法安全编辑"
	messageIkuaiGroupUnreadable = "iKuai IP 分组 %s 已提交但读不回预期内容，无法引用"
	messageIkuaiGroupRebind     = "iKuai IP 分组 %s 不存在，无法恢复规则引用"
	messageIkuaiGroupExists     = "iKuai IP 分组「%s」已存在，请重新预览"
	messageIkuaiGroupDrift      = "iKuai IP 分组「%s」在预览后发生变化，请重新预览"
	messageIkuaiGroupInUse      = "iKuai IP 分组「%s」仍被%s引用，未删除"
	messageIkuaiGroupAddGone    = "iKuai 中找不到本次新增的 IP 分组「%s」，可能已被手动修改或删除，请检查"
	messageIkuaiGroupChanged    = "iKuai IP 分组「%s」已被修改，未自动删除，请检查"
	messageIkuaiGroupUndoNoID   = "iKuai IP 分组「%s」缺少有效 ID，未自动删除，请检查"
	messageIkuaiGroupUndoInUse  = "iKuai IP 分组「%s」仍被%s引用，未自动删除"
	messageIkuaiGroupKept       = "iKuai IP 分组「%s」已被修改，保留现状，请检查"
	messageIkuaiGroupTaken      = "iKuai IP 分组「%s」已被其他配置占用，无法恢复，请检查"
	messageIkuaiGroupUser       = "%s「%s」"

	labelIkuaiNATRule     = "NAT 规则"
	labelIkuaiPolicy      = "端口分流"
	labelIkuaiGroup       = "IP 分组"
	labelIkuaiNATBypass   = "NAT 免转换规则"
	unitIkuaiNetworks     = "个网段"
	unitIkuaiGroupRefs    = "个 IP 分组引用"
	messageIkuaiOversized = "iKuai %s「%s」需要一次写入 %d %s，请求约 %d KB，超过 iKuai 单次请求上限（约 %d KB），发布会被拒绝。请减少代理网段（例如少选规则集）后重试"
)

// iKuai HTTP status explanations (_http_error).
var ikuaiHTTPReasons = map[int]string{
	401: "身份验证未通过，请检查用户名、密码，以及管理入口是否另有登录验证",
	403: "拒绝访问，请检查账号的管理员权限和管理页面的访问限制",
	404: "未找到管理接口，请检查管理地址和端口，并确认使用的是 iKuai 4.x",
	405: "管理接口不接受此请求，请检查管理地址，以及反向代理是否允许接口请求",
	408: "等待请求超时，请检查网络连接后重试",
	429: "请求过于频繁，请稍后再试",
	502: "管理入口未能连接后端服务，请检查 iKuai 管理服务或反向代理",
	503: "管理服务暂时不可用，请稍后再试",
	504: "管理入口等待后端响应超时，请检查 iKuai 管理服务或反向代理",
}

// ikuaiListLabels name the lists in incomplete-read messages.
var ikuaiListLabels = map[string]string{"static_rt": "静态路由", "static_rt_table": "当前路由表",
	"route_object": "IP 分组", "nat_rule": "NAT 规则", "stream_ipport": "端口分流"}
