package nodes

const (
	msgBodyObject      = "请求体必须是 JSON 对象"
	msgNodeObject      = "节点请求必须是 JSON 对象"
	msgNodeNonNull     = "节点请求必须为非 null JSON 对象"
	msgSharedDialer    = "共享节点不保存拨号引用，请在策略组中创建代理链"
	msgNodeControlChar = "节点字段「%s」不能包含换行、回车或控制字符"
	msgImmutableID     = "节点 ID 不可修改"
	msgLines           = "lines 必须是字符串数组"
	msgLinesLimit      = "单次最多解析 500 行"
	msgUnknownNode     = "无法识别的节点格式"
	msgMissingNodeIDs  = "以下节点 id 不存在: %s"
	msgSelectNodes     = "请选择要删除的节点"
)
