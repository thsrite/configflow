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
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/store"
	"configflow/internal/utilx"
	"configflow/internal/version"
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
	manager := webListener.Load()
	result, err := func() (*httpx.Response, error) {
		// Python tests request.method == 'GET': HEAD takes the POST branch.
		if c.Method() == "GET" {
			status, err := manager.Status()
			if err != nil {
				return nil, err
			}
			return httpx.JSON(status), nil
		}
		data := c.JSONObject()
		if data == nil {
			return failure(400, "请提供 Web 监听端口"), nil
		}
		port, ok := data.Get("port")
		if !ok {
			return failure(400, "请提供 Web 监听端口"), nil
		}
		status, err := manager.Update(port)
		if err != nil {
			return nil, err
		}
		payload := jsonx.Obj("success", true)
		payload.Update(status)
		return httpx.JSON(payload), nil
	}()
	var listenerErr *weblistener.Error
	if errors.As(err, &listenerErr) {
		return failure(listenerErr.Status, listenerErr.Message), nil
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		appLogger.Exception("Failed to access deployment Web listener configuration", err)
		return failure(503, "无法访问 Web 监听配置，请检查容器数据目录权限"), nil
	}
	return result, err
}

// systemSettings is config_data.get('system_config', {}).
func systemSettings(config *jsonx.Object) *jsonx.Object {
	settings, _ := config.GetOr("system_config", jsonx.NewObject()).(*jsonx.Object)
	return settings
}

// ensureSystemSettings is `if 'system_config' not in config_data:
// config_data['system_config'] = {}` (and setdefault).
func ensureSystemSettings(config *jsonx.Object) *jsonx.Object {
	value := config.SetDefault("system_config", jsonx.NewObject())
	settings, _ := value.(*jsonx.Object)
	return settings
}

// pyExceptionName is type(error).__name__ for a save failure.
func pyExceptionName(err error) string {
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
	config, err := repo.GetSystemConfig(c.Context())
	if err != nil {
		return nil, err
	}
	if c.Method() == "GET" {
		return httpx.JSON(jsonx.Obj("rule_fetch_proxy", systemSettings(config).GetOr("rule_fetch_proxy", ""))), nil
	}
	data := c.JSONObject()
	if data == nil {
		return failure(400, "请提供规则下载代理，留空可清除设置"), nil
	}
	value, ok := data.Get("rule_fetch_proxy")
	if !ok {
		return failure(400, "请提供规则下载代理，留空可清除设置"), nil
	}
	proxy, ok := normalizeRuleFetchProxy(value)
	if !ok {
		return failure(400, "规则下载代理须为有效的 HTTP/HTTPS 代理地址，或留空使用现有网络"), nil
	}
	ensureSystemSettings(config).Set("rule_fetch_proxy", proxy)
	if err := repo.SaveSystemConfig(c.Context(), config, nil); err != nil {
		appLogger.Error("Failed to update rule download proxy: exception_type=" + pyExceptionName(err))
		return failure(500, "保存规则下载代理失败"), nil
	}
	state := "cleared"
	if proxy != "" {
		state = "enabled"
	}
	appLogger.Info("Rule download proxy " + state)
	return httpx.JSON(jsonx.Obj("success", true, "rule_fetch_proxy", proxy)), nil
}

// pyStrip is str.strip() on a value that must be a str (AttributeError
// text otherwise).
func pyStrip(v any) (string, error) {
	text, ok := v.(string)
	if !ok {
		return "", errors.New("'" + httpx.PyTypeName(v) + "' object has no attribute 'strip'")
	}
	return strings.TrimFunc(text, pyIsSpace), nil
}

// requestJSONObject is `data = request.json` followed by `data.get(...)`
// inside a try block: the error message Python would put into str(e).
func requestJSONObject(c *httpx.Ctx) (*jsonx.Object, error) {
	data, err := c.JSON()
	if err != nil {
		return nil, err
	}
	object, ok := data.(*jsonx.Object)
	if !ok || object == nil {
		return nil, errors.New(httpx.NoAttributeGet(data))
	}
	return object, nil
}

func handleServerDomain(c *httpx.Ctx) (*httpx.Response, error) {
	config, err := repo.GetSystemConfig(c.Context())
	if err != nil {
		return nil, err
	}
	switch c.Method() {
	case "GET":
		return httpx.JSON(jsonx.Obj("server_domain", systemSettings(config).GetOr("server_domain", ""))), nil
	case "POST":
		response, err := func() (*httpx.Response, error) {
			data, err := requestJSONObject(c)
			if err != nil {
				return nil, err
			}
			newDomain, err := pyStrip(data.GetOr("new_domain", ""))
			if err != nil {
				return nil, err
			}
			if newDomain == "" {
				return failure(400, "New domain is required"), nil
			}
			ensureSystemSettings(config).Set("server_domain", newDomain)
			appLogger.Info("Updated server domain to " + newDomain)
			if err := repo.SaveSystemConfig(c.Context(), config, nil); err != nil {
				return nil, err
			}
			return httpx.JSON(jsonx.Obj("success", true, "server_domain", newDomain)), nil
		}()
		if err != nil {
			appLogger.Error("Failed to update server domain: " + err.Error())
			return failure(500, err.Error()), nil
		}
		return response, nil
	}
	return nil, nil
}

func handleConfigToken(c *httpx.Ctx) (*httpx.Response, error) {
	config, err := repo.GetSystemConfig(c.Context())
	if err != nil {
		return nil, err
	}
	switch c.Method() {
	case "GET":
		return httpx.JSON(jsonx.Obj("config_token", systemSettings(config).GetOr("config_token", ""))), nil
	case "POST":
		response, err := func() (*httpx.Response, error) {
			data, err := requestJSONObject(c)
			if err != nil {
				return nil, err
			}
			newToken, err := pyStrip(data.GetOr("token", ""))
			if err != nil {
				return nil, err
			}
			if jsonx.Truthy(data.GetOr("generate", false)) {
				newToken = utilx.GenerateRandomToken(32)
			}
			if newToken == "" {
				return failure(400, "Token is required or set generate=true"), nil
			}
			internal, err := redact.ContainsInternalRuleProxyToken(c.Context(), newToken)
			if err != nil {
				return nil, err
			}
			if internal {
				return failure(400, "Config token must not contain an internal rule proxy token"), nil
			}
			ensureSystemSettings(config).Set("config_token", newToken)
			appLogger.Info("Updated config token")
			if err := repo.SaveSystemConfig(c.Context(), config, nil); err != nil {
				return nil, err
			}
			return httpx.JSON(jsonx.Obj("success", true, "config_token", newToken)), nil
		}()
		if err != nil {
			appLogger.Error("Failed to update config token: " + err.Error())
			return failure(500, err.Error()), nil
		}
		return response, nil
	case "DELETE":
		ensureSystemSettings(config).Set("config_token", "")
		if err := repo.SaveSystemConfig(c.Context(), config, nil); err != nil {
			appLogger.Error("Failed to delete config token: " + err.Error())
			return failure(500, err.Error()), nil
		}
		appLogger.Info("Deleted config token")
		return httpx.JSON(jsonx.Obj("success", true)), nil
	}
	return nil, nil
}

func getVersion(c *httpx.Ctx) (*httpx.Response, error) {
	return httpx.JSON(version.Info()), nil
}

var backupTextFields = []string{"webdav_url", "webdav_username", "webdav_password", "webdav_path"}
var backupFields = append(append([]string{}, backupTextFields...), "auto_backup", "keep_latest", "keep_daily")

// backupRequestData is _backup_request_data: the validated body, or the 400
// response.
func backupRequestData(c *httpx.Ctx) (*jsonx.Object, *httpx.Response) {
	data := c.JSONObject()
	message := ""
	switch {
	case data == nil || data.Len() == 0:
		message = "请求体必须是非空 JSON 对象，请使用 Content-Type: application/json"
	case !hasAny(data, backupFields):
		message = "请提供 WebDAV 备份配置字段"
	case anyNotString(data, backupTextFields):
		message = "WebDAV 地址、用户名、密码和路径必须是字符串"
	case hasKeyNotBool(data, "auto_backup"):
		message = "auto_backup 必须是布尔值"
	case badRetention(data):
		message = "保留份数须为 0–1000 的整数，每日保留天数须为 0–3650 的整数"
	}
	if message != "" {
		return nil, failure(400, message)
	}
	return data, nil
}

func hasAny(data *jsonx.Object, keys []string) bool {
	for _, key := range keys {
		if _, ok := data.Get(key); ok {
			return true
		}
	}
	return false
}

func anyNotString(data *jsonx.Object, keys []string) bool {
	for _, key := range keys {
		if value, ok := data.Get(key); ok {
			if _, isString := value.(string); !isString {
				return true
			}
		}
	}
	return false
}

func hasKeyNotBool(data *jsonx.Object, key string) bool {
	value, ok := data.Get(key)
	if !ok {
		return false
	}
	_, isBool := value.(bool)
	return !isBool
}

func badRetention(data *jsonx.Object) bool {
	for _, key := range retentionKeys {
		value, ok := data.Get(key)
		if !ok {
			continue
		}
		n, isNumber := value.(jsonx.Number)
		v, fits := n.Int64()
		if !isNumber || !fits || v < 0 || v > retentionLimits[key] {
			return true
		}
	}
	return false
}

func backupConfig(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := context.WithoutCancel(c.Context())
	config, err := repo.GetSystemConfig(ctx)
	if err != nil {
		return nil, err
	}
	switch c.Method() {
	case "GET":
		backup, _ := config.GetOr("backup", jsonx.NewObject()).(*jsonx.Object)
		keepLatest, keepDaily := retentionSettings(backup)
		password := ""
		if jsonx.Truthy(backup.Value("webdav_password")) {
			password = "******"
		}
		return httpx.JSON(jsonx.Obj(
			"webdav_url", backupEditorURL(backup.GetString("webdav_url")),
			"webdav_username", backup.GetOr("webdav_username", ""),
			"webdav_password", password,
			"webdav_path", backup.GetOr("webdav_path", "/config-flow-backup/"),
			"auto_backup", backup.GetOr("auto_backup", false),
			"keep_latest", keepLatest,
			"keep_daily", keepDaily,
		)), nil
	case "POST":
		data, bad := backupRequestData(c)
		if bad != nil {
			return bad, nil
		}
		backup, _ := config.SetDefault("backup", jsonx.NewObject()).(*jsonx.Object)
		var changed []string
		for _, key := range backupFields {
			value, ok := data.Get(key)
			if !ok || (key == "webdav_password" && (value == "******" || value == "")) {
				continue
			}
			if previous, _ := backup.Get(key); !jsonx.Equal(previous, value) {
				changed = append(changed, key)
			}
			backup.Set(key, value)
		}
		if err := repo.SaveSystemConfig(ctx, config, nil); err != nil {
			return nil, err
		}
		summary := strings.Join(changed, "、")
		if summary == "" {
			summary = "无变化"
		}
		state := "已关闭"
		if jsonx.Truthy(backup.Value("auto_backup")) {
			state = "已开启"
		}
		appLogger.Info("WebDAV 备份配置已保存：" + summary + "；自动备份" + state)
		return httpx.Message(200, true, "备份配置已保存"), nil
	}
	return nil, nil
}

func handleSubscriptionAggregation(c *httpx.Ctx) (*httpx.Response, error) {
	config, err := repo.GetSystemConfig(c.Context())
	if err != nil {
		return nil, err
	}
	switch c.Method() {
	case "GET":
		return httpx.JSON(jsonx.Obj("enabled", systemSettings(config).GetOr("subscription_aggregation_enabled", false))), nil
	case "POST":
		response, err := func() (*httpx.Response, error) {
			data, err := requestJSONObject(c)
			if err != nil {
				return nil, err
			}
			enabled := data.GetOr("enabled", false)
			ensureSystemSettings(config).Set("subscription_aggregation_enabled", enabled)
			appLogger.Info("Updated subscription aggregation enabled to " + jsonx.Str(enabled))
			if err := repo.SaveSystemConfig(c.Context(), config, nil); err != nil {
				return nil, err
			}
			return httpx.JSON(jsonx.Obj("success", true, "enabled", enabled)), nil
		}()
		if err != nil {
			appLogger.Error("Failed to update subscription aggregation: " + err.Error())
			return failure(500, err.Error()), nil
		}
		return response, nil
	}
	return nil, nil
}
