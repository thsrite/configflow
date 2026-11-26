package auth

const (
	msgJWTSecretUnset       = "未设置"
	msgJWTSecretPlaceholder = "使用示例占位值"
	msgJWTSecretShort       = "少于 32 字节"
	msgJWTSecretRandom      = "JWT_SECRET_KEY %s，已生成本次启动的随机密钥；重启后需重新登录。请设置至少 32 字节的随机 JWT_SECRET_KEY 并在更新时保留。"
	msgExpired              = "登录已过期，请重新登录。"
	msgInvalid              = "登录凭据无效，请重新登录。原始错误："
	msgMissingCredentials   = "缺少有效的登录凭据，请重新登录。"
	msgSessionInvalid       = "登录已失效，请重新登录。"
	msgLoginRequired        = "请先登录后再操作。"
)

type failure struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error"`
}
