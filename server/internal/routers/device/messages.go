package device

// Cross-package user-facing Chinese messages shared by multiple router subpackages.
const (
	MessageEditBusy              = "此路由器正在发布或同步，请稍后重试"
	MessageEditFormat            = "路由操作参数格式错误"
	MessageOperationFormat       = "RouterOS 操作格式无法识别"
	MessagePolicyChanged         = "策略路由对象在操作期间发生变化，保留现场：%s/%s"
	MessagePolicyDynamic         = "动态策略对象不能修改"
	MessagePolicyStillPresent    = "策略路由回读校验失败：对象仍然存在"
	MessageROSListFormat         = "RouterOS %s 返回的列表格式无法识别"
	MessageRollbackDeletePresent = "回滚删除后对象仍存在"
	MessageUncertainRouteWrite   = "RouterOS %s 已提交但没有返回可信的新对象 ID；无法确认新增对象归属，已保留现场，请检查路由器"
	MessageUnknown               = "未知"
	MessageUnsupportedOperation  = "不支持的操作 %s"
)
