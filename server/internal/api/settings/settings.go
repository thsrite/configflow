// Package settings holds the routes of backend/routes/settings.py
// (blueprint "settings", prefix /api) that do not wait for another package:
// web port, rule download proxy, server domain, config token, version,
// WebDAV backup settings, the subscription aggregation switch, and the
// third-party dependency routes (GET /api/settings/dependencies, Sub-Store
// update and mode; internal/substore, C13).
//
// The WebDAV client, retention and post-commit backup worker live in internal/webdav.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/logx"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/store"
	"configflow/internal/utilx"
	"configflow/internal/version"
	"configflow/internal/webdav"
	"configflow/internal/weblistener"
)

// appLogger is Flask's current_app.logger (backend/app.py).
var appLogger = logx.Get("backend.app")

var webListener atomic.Pointer[weblistener.Manager]

// SetWebListener installs the manager of the web front end's sockets (nil:
// the deployment does not serve the web UI itself, as without nginx).
func SetWebListener(m *weblistener.Manager) { webListener.Store(m) }

// Register adds the settings routes available now.
func Register(r *httpx.Router) {
	r.POST("/api/backup/test", "settings.test_backup", auth.Require(testBackup))
	r.POST("/api/backup/now", "settings.backup_now", auth.Require(backupNow))
	r.Handle([]string{"GET", "POST"}, "/api/settings/web-port", "settings.handle_web_port", auth.Require(handleWebPort))
	r.Handle([]string{"GET", "POST"}, "/api/settings/rule-fetch-proxy", "settings.handle_rule_fetch_proxy", auth.Require(handleRuleFetchProxy))
	r.Handle([]string{"GET", "POST"}, "/api/server-domain", "settings.handle_server_domain", auth.Require(handleServerDomain))
	r.Handle([]string{"GET", "POST", "DELETE"}, "/api/config-token", "settings.handle_config_token", auth.Require(handleConfigToken))
	r.GET("/api/version", "settings.get_version", getVersion)
	r.Handle([]string{"GET", "POST"}, "/api/backup/config", "settings.backup_config", auth.Require(backupConfig))
	r.Handle([]string{"GET", "POST"}, "/api/settings/subscription-aggregation", "settings.handle_subscription_aggregation", auth.Require(handleSubscriptionAggregation))
	r.GET("/api/settings/dependencies", "settings.list_third_party_dependencies", auth.Require(listThirdPartyDependencies))
	r.POST("/api/settings/dependencies/sub-store/update", "settings.update_sub_store_dependency", auth.Require(updateSubStoreDependency))
	r.POST("/api/settings/dependencies/sub-store/mode", "settings.set_sub_store_mode", auth.Require(setSubStoreMode))
}

func failure(status int, message string) *httpx.Response {
	return httpx.Message(status, false, message)
}

func handleWebPort(c *httpx.Ctx) (*httpx.Response, error) {
	m := webListener.Load()
	out, err := func() (*httpx.Response, error) {
		if c.Method() == "GET" || c.Method() == "HEAD" {
			status, err := m.InformationFor(c.Context())
			return httpx.JSON(status), err
		}
		var data settingsRequest
		if !c.BindSilent(&data) || len(data.Port) == 0 {
			return failure(400, msgWebPortRequired), nil
		}
		var port int
		if json.Unmarshal(data.Port, &port) != nil || string(data.Port) == "null" {
			return failure(400, msgWebPortInvalid), nil
		}
		status, err := m.ChangePort(c.Context(), port)
		if err != nil {
			return nil, err
		}
		return httpx.JSON(struct {
			Success bool `json:"success"`
			*weblistener.Information
		}{true, status}), nil
	}()
	var e *weblistener.Error
	if errors.As(err, &e) {
		return failure(e.Status, e.Message), nil
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		appLogger.Exception("Failed to access deployment Web listener configuration", err)
		return failure(503, msgWebPortAccess), nil
	}
	return out, err
}

// exceptionName preserves the existing save-failure log labels.
func exceptionName(err error) string {
	switch {
	case errors.Is(err, repo.ErrValidation):
		return "ProfileValidationError"
	case errors.Is(err, repo.ErrInUse):
		return "ProfileInUse"
	case errors.Is(err, repo.ErrRepository):
		return "ProfileRepositoryError"
	}
	var storeErr *store.Error
	if errors.As(err, &storeErr) {
		return "ConfigStoreError"
	}
	return "Exception"
}

func handleRuleFetchProxy(c *httpx.Ctx) (*httpx.Response, error) {
	config, err := loadSystemConfig(c.Context())
	if err != nil {
		return nil, err
	}
	if c.Method() == "GET" || c.Method() == "HEAD" {
		return httpx.JSON(map[string]any{"rule_fetch_proxy": settingValue(config, "rule_fetch_proxy", json.RawMessage(`""`))}), nil
	}
	var data settingsRequest
	if !c.BindSilent(&data) || len(data.RuleFetchProxy) == 0 {
		return failure(400, msgProxyRequired), nil
	}
	var text string
	if string(data.RuleFetchProxy) == "null" || json.Unmarshal(data.RuleFetchProxy, &text) != nil {
		return failure(400, msgProxyInvalid), nil
	}
	proxy, ok := normalizeRuleFetchProxy(text)
	if !ok {
		return failure(400, msgProxyInvalid), nil
	}
	raw, _ := json.Marshal(proxy)
	if err = config.SystemConfig.SetJSON("rule_fetch_proxy", raw); err != nil {
		return nil, err
	}
	if err := saveSystemConfig(c.Context(), config); err != nil {
		appLogger.Error("Failed to update rule download proxy: exception_type=" + exceptionName(err))
		return failure(500, msgProxySave), nil
	}
	state := "cleared"
	if proxy != "" {
		state = "enabled"
	}
	appLogger.Info("Rule download proxy " + state)
	return httpx.JSON(map[string]any{"success": true, "rule_fetch_proxy": proxy}), nil
}
func requestSettings(c *httpx.Ctx) (settingsRequest, error) {
	var data settingsRequest
	err := c.Bind(&data)
	return data, err
}
func inputText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var text string
	if string(raw) == "null" || json.Unmarshal(raw, &text) != nil {
		return "", errors.New("setting must be a string")
	}
	return strings.TrimSpace(text), nil
}
func handleServerDomain(c *httpx.Ctx) (*httpx.Response, error) {
	if c.Method() == "POST" && invalidUnicode(c) {
		return failure(400, msgInvalidUnicode), nil
	}
	config, err := loadSystemConfig(c.Context())
	if err != nil {
		return nil, err
	}
	if c.Method() == "GET" || c.Method() == "HEAD" {
		return httpx.JSON(map[string]any{"server_domain": settingValue(config, "server_domain", json.RawMessage(`""`))}), nil
	}
	data, err := requestSettings(c)
	if err != nil {
		return nil, err
	}
	domain, err := inputText(data.NewDomain)
	if err != nil {
		return failure(500, err.Error()), nil
	}
	if domain == "" {
		return failure(400, "New domain is required"), nil
	}
	raw, _ := json.Marshal(domain)
	if err = config.SystemConfig.SetJSON("server_domain", raw); err != nil {
		return nil, err
	}
	if err = saveSystemConfig(c.Context(), config); err != nil {
		return failure(500, err.Error()), nil
	}
	appLogger.Info("Updated server domain")
	return httpx.JSON(map[string]any{"success": true, "server_domain": domain}), nil
}
func handleConfigToken(c *httpx.Ctx) (*httpx.Response, error) {
	if c.Method() == "POST" && invalidUnicode(c) {
		return failure(400, msgInvalidUnicode), nil
	}
	config, err := loadSystemConfig(c.Context())
	if err != nil {
		return nil, err
	}
	switch c.Method() {
	case "GET", "HEAD":
		return httpx.JSON(map[string]any{"config_token": settingValue(config, "config_token", json.RawMessage(`""`))}), nil
	case "DELETE":
		if err = config.SystemConfig.SetJSON("config_token", json.RawMessage(`""`)); err != nil {
			return nil, err
		}
		if err = saveSystemConfig(c.Context(), config); err != nil {
			return failure(500, err.Error()), nil
		}
		appLogger.Info("Deleted config token")
		return httpx.JSON(map[string]bool{"success": true}), nil
	}
	data, err := requestSettings(c)
	if err != nil {
		return nil, err
	}
	token, err := inputText(data.Token)
	if err != nil {
		return failure(500, err.Error()), nil
	}
	if enabledInput(data.Generate) {
		token = utilx.GenerateRandomToken(32)
	}
	if token == "" {
		return failure(400, "Token is required or set generate=true"), nil
	}
	internal, err := redact.ContainsInternalRuleProxyToken(c.Context(), token)
	if err != nil {
		return nil, err
	}
	if internal {
		return failure(400, "Config token must not contain an internal rule proxy token"), nil
	}
	raw, _ := json.Marshal(token)
	if err = config.SystemConfig.SetJSON("config_token", raw); err != nil {
		return nil, err
	}
	if err = saveSystemConfig(c.Context(), config); err != nil {
		return failure(500, err.Error()), nil
	}
	appLogger.Info("Updated config token")
	return httpx.JSON(map[string]any{"success": true, "config_token": token}), nil
}
func enabledInput(raw json.RawMessage) bool {
	switch string(raw) {
	case "", "null", "false", `""`, "[]", "{}":
		return false
	}
	var number json.Number
	if len(raw) > 0 && raw[0] != '"' && json.Unmarshal(raw, &number) == nil {
		value, _ := number.Float64()
		return value != 0
	}
	return true
}
func getVersion(c *httpx.Ctx) (*httpx.Response, error) { return httpx.JSON(version.Info()), nil }
func backupRequestData(c *httpx.Ctx) (*settingsRequest, *httpx.Response) {
	var data settingsRequest
	if !c.BindSilent(&data) || !data.nonempty {
		return nil, failure(400, msgBackupObject)
	}
	fields := []json.RawMessage{data.WebDAVURL, data.WebDAVUsername, data.WebDAVPassword, data.WebDAVPath, data.AutoBackup, data.KeepLatest, data.KeepDaily}
	present := false
	for _, raw := range fields {
		present = present || len(raw) > 0
	}
	if !present {
		return nil, failure(400, msgBackupFields)
	}
	for _, raw := range fields[:4] {
		if len(raw) > 0 {
			var text string
			if string(raw) == "null" || json.Unmarshal(raw, &text) != nil {
				return nil, failure(400, msgBackupStrings)
			}
		}
	}
	if len(data.AutoBackup) > 0 && string(data.AutoBackup) != "true" && string(data.AutoBackup) != "false" {
		return nil, failure(400, msgBackupBool)
	}
	for i, raw := range []json.RawMessage{data.KeepLatest, data.KeepDaily} {
		if len(raw) == 0 {
			continue
		}
		var n httpx.Number
		if json.Unmarshal(raw, &n) != nil || !n.IsIntegerLiteral() {
			return nil, failure(400, msgBackupRetention)
		}
		v, err := n.Int64()
		if err != nil || v < 0 || v > retentionLimits[retentionKeys[i]] {
			return nil, failure(400, msgBackupRetention)
		}
	}
	return &data, nil
}
func backupConfig(c *httpx.Ctx) (*httpx.Response, error) {
	config, err := loadBackup(c.Context())
	if err != nil {
		return nil, err
	}
	backup := *config.Backup
	if c.Method() == "GET" || c.Method() == "HEAD" {
		raw, err := json.Marshal(backup)
		if err != nil {
			return nil, err
		}
		var stored map[string]json.RawMessage
		if err = json.Unmarshal(raw, &stored); err != nil {
			return nil, err
		}
		var address string
		_ = json.Unmarshal(stored["webdav_url"], &address)
		username := json.RawMessage(`""`)
		if value, present := stored["webdav_username"]; present {
			username = value
		}
		path := json.RawMessage(`"/config-flow-backup/"`)
		if value, present := stored["webdav_path"]; present {
			path = value
		}
		automatic := json.RawMessage(`false`)
		if value, present := stored["auto_backup"]; present {
			automatic = value
		}
		password := ""
		if enabledInput(stored["webdav_password"]) {
			password = "******"
		}
		latest := webdav.RetentionValue(stored["keep_latest"], webdav.DefaultKeepLatest, webdav.MaxKeepLatest)
		daily := webdav.RetentionValue(stored["keep_daily"], webdav.DefaultKeepDaily, webdav.MaxKeepDaily)
		return httpx.JSON(map[string]any{"webdav_url": backupEditorURL(address), "webdav_username": username, "webdav_password": password, "webdav_path": path, "auto_backup": automatic, "keep_latest": latest, "keep_daily": daily}), nil
	}
	data, bad := backupRequestData(c)
	if bad != nil {
		return bad, nil
	}
	before, err := json.Marshal(backup)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(before, &fields); err != nil {
		return nil, err
	}
	updates := map[string]json.RawMessage{"webdav_url": data.WebDAVURL, "webdav_username": data.WebDAVUsername, "webdav_password": data.WebDAVPassword, "webdav_path": data.WebDAVPath, "auto_backup": data.AutoBackup, "keep_latest": data.KeepLatest, "keep_daily": data.KeepDaily}
	changed := []string{}
	for _, key := range []string{"webdav_url", "webdav_username", "webdav_password", "webdav_path", "auto_backup", "keep_latest", "keep_daily"} {
		raw := updates[key]
		if len(raw) == 0 || (key == "webdav_password" && (bytes.Equal(raw, []byte(`"******"`)) || bytes.Equal(raw, []byte(`""`)))) {
			continue
		}
		if !bytes.Equal(fields[key], raw) {
			changed = append(changed, key)
		}
		fields[key] = raw
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &backup); err != nil {
		return nil, err
	}
	var address, path string
	_ = json.Unmarshal(fields["webdav_url"], &address)
	_ = json.Unmarshal(fields["webdav_path"], &path)
	if err = (webdav.Settings{URL: address, Path: path}).ValidatePaths(); err != nil {
		return failure(400, err.Error()), nil
	}
	*config.Backup = backup
	if err = saveBackup(c.Context(), config); err != nil {
		return nil, err
	}
	summary := strings.Join(changed, "、")
	if summary == "" {
		summary = msgUnchanged
	}
	state := msgBackupDisabled
	if enabledInput(fields["auto_backup"]) {
		state = msgBackupEnabled
	}
	appLogger.Info(msgBackupSaved + "：" + summary + "；" + msgBackupAutomatic + state)
	return httpx.Message(200, true, msgBackupSaved), nil
}
func handleSubscriptionAggregation(c *httpx.Ctx) (*httpx.Response, error) {
	config, err := loadSystemConfig(c.Context())
	if err != nil {
		return nil, err
	}
	if c.Method() == "GET" || c.Method() == "HEAD" {
		return httpx.JSON(map[string]any{"enabled": settingValue(config, "subscription_aggregation_enabled", json.RawMessage(`false`))}), nil
	}
	data, err := requestSettings(c)
	if err != nil {
		return nil, err
	}
	raw := data.Enabled
	if len(raw) == 0 {
		raw = json.RawMessage(`false`)
	}
	if err = config.SystemConfig.SetJSON("subscription_aggregation_enabled", raw); err != nil {
		return nil, err
	}
	if err = saveSystemConfig(c.Context(), config); err != nil {
		return failure(500, err.Error()), nil
	}
	appLogger.Info("Updated subscription aggregation")
	return httpx.JSON(map[string]any{"success": true, "enabled": raw}), nil
}
