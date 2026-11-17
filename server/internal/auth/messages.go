package auth

const (
	msgExpired            = "登录已过期，请重新登录。"
	msgInvalid            = "登录凭据无效，请重新登录。原始错误："
	msgMissingCredentials = "缺少有效的登录凭据，请重新登录。"
	msgSessionInvalid     = "登录已失效，请重新登录。"
	msgLoginRequired      = "请先登录后再操作。"
)

type failure struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error"`
}
