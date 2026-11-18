package setup

import "errors"

// ErrCorruptRecord refuses unreadable ownership evidence without overwriting it.
var ErrCorruptRecord = errors.New("invalid setup session")

// Error is a wizard validation failure with its public HTTP status.
type Error struct {
	Message string
	Status  int
}

func (e *Error) Error() string                 { return e.Message }
func failure(message string, status int) error { return &Error{message, status} }

const (
	msgSessionID            = "向导记录不存在"
	msgMissingSession       = "向导记录不存在，请重新开始"
	msgBadBody              = "请提交有效 JSON 对象"
	msgLargeBody            = "向导请求过大"
	msgAwaitOwner           = "等待首次使用向导模块"
	msgGoal                 = "请选择使用方式"
	msgNameLength           = "配置名称需为 1–64 个字符"
	msgIdentityConflict     = "配置名称标识冲突，请重新开始"
	msgSessionConflict      = "该向导已创建，请继续原向导或新建一次配置"
	msgProgressFields       = "向导进度参数无效"
	msgProgressOrder        = "请先完成前面的步骤"
	msgRouterMode           = "请选择有效的路由器配置方式"
	msgRouterSelection      = "请选择当前配置空间中的路由器"
	msgSourcePrepared       = "方案已生成；修改来源请前往订阅来源或创建新的向导"
	msgSourceSelection      = "请选择有效的订阅或节点"
	msgSubscriptionFormat   = "订阅链接格式不正确"
	msgHTTPSubscription     = "请输入 HTTP(S) 订阅链接"
	msgSubscriptionName     = "订阅"
	msgSourceUnavailable    = "所选来源已删除或停用，请刷新后重选"
	msgUnlinkedSubscription = "所选订阅未填写订阅链接，请改用订阅链接添加"
	msgNeedSource           = "请添加订阅链接或选择已有节点"
	msgRouterDraftPhase     = "请在安装等待期间填写主路由信息；安装完成后使用路由器管理"
	msgRouterDraftFields    = "主路由连接参数无效"
	msgRouterDraftBinding   = "安装完成后自动关联本次设备，暂不启用规则自动同步"
	msgRouterDraftPolicy    = "新配置空间还没有设为智能分流的客户端设备，暂不能使用策略路由；完成向导后先在「客户端设备」中设置，再到上游路由中启用"
	msgGlobalDirect         = "🎯 全球直连"
	msgProxyFirst           = "优先代理"
	msgNodeSelection        = "✈️ 节点选择"
	msgUnmatched            = "🐟 漏网之鱼"
	msgBuiltinNameSuffix    = "（内置）"
	msgBuiltinNumberedName  = "%s（内置 %d）"
)
