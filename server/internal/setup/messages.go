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
	msgSessionID           = "向导记录不存在"
	msgMissingSession      = "向导记录不存在，请重新开始"
	msgBadBody             = "请提交有效 JSON 对象"
	msgLargeBody           = "向导请求过大"
	msgAwaitInstall        = "等待设备安装模块"
	msgGlobalDirect        = "🎯 全球直连"
	msgProxyFirst          = "优先代理"
	msgNodeSelection       = "✈️ 节点选择"
	msgUnmatched           = "🐟 漏网之鱼"
	msgBuiltinNameSuffix   = "（内置）"
	msgBuiltinNumberedName = "%s（内置 %d）"
)
