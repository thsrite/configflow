// Package auth holds the routes of backend/routes/auth.py (blueprint "auth",
// prefix /api/auth): status, login, verify. The global gate lives in
// internal/auth.
package auth

import (
	"fmt"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/logx"
)

var logger = logx.Get("backend.routes.auth")

// Handler serves the /api/auth routes.
type Handler struct{}

// NewHandler constructs a Handler for the auth blueprint.
func NewHandler() *Handler { return &Handler{} }

// Register adds the auth blueprint routes to r.
func (h *Handler) Register(r *httpx.Router) {
	r.GET("/api/auth/status", "auth.auth_status", h.authStatus)
	r.POST("/api/auth/login", "auth.login", h.login)
	r.GET("/api/auth/verify", "auth.verify", auth.Require(h.verify))
}

// Register adds the auth blueprint.
func Register(r *httpx.Router) { NewHandler().Register(r) }

func (h *Handler) authStatus(c *httpx.Ctx) (*httpx.Response, error) {
	return httpx.JSON(map[string]bool{"authEnabled": auth.IsAuthEnabled()}), nil
}

func (h *Handler) login(c *httpx.Ctx) (*httpx.Response, error) {
	fail := func(message string) (*httpx.Response, error) {
		return httpx.Message(500, false, fmt.Sprintf(msgLoginFailure, message)), nil
	}
	var body loginRequest
	if err := c.Bind(&body); err != nil {
		return nil, err
	}
	username, password := body.credentials()
	if !auth.IsAuthEnabled() {
		return httpx.Message(400, false, msgLoginDisabled), nil
	}
	config := auth.Current()
	if username == config.AdminUsername && password == config.AdminPassword {
		name := username
		logger.Info("登录成功：用户 " + logx.LogText(username, 120) + "，来源 " + logx.ClientIP(c.Request))
		token, err := auth.GenerateToken(name)
		if err != nil {
			return fail(err.Error())
		}
		return httpx.JSON(map[string]any{"success": true, "token": token, "username": name, "expiresIn": auth.ExpirationHours * 3600}), nil
	}
	logger.Warning("登录失败：用户名或密码错误，用户 " + logx.LogText(username, 120) + "，来源 " + logx.ClientIP(c.Request))
	return httpx.Message(401, false, msgInvalidCredentials), nil
}

func (h *Handler) verify(c *httpx.Ctx) (*httpx.Response, error) {
	return httpx.Message(200, true, "登录状态有效。"), nil
}
