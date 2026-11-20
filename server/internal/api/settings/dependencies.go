package settings

import (
	"encoding/json"

	"configflow/internal/httpx"
	"configflow/internal/substore"
)

// listThirdPartyDependencies is GET /api/settings/dependencies: version,
// latest release and update state of the bundled Sub-Store.
func listThirdPartyDependencies(c *httpx.Ctx) (*httpx.Response, error) {
	refresh, _ := c.Arg("refresh")
	force := refresh == "1" || refresh == "true"
	dependencies, err := substore.Default().Dependencies.ListDependencies(c.Context(), force)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(map[string]any{"dependencies": dependencies}), nil
}

// updateSubStoreDependency is POST /api/settings/dependencies/sub-store/update:
// the online update runs in the background; progress is polled through the
// dependency list.
func updateSubStoreDependency(c *httpx.Ctx) (*httpx.Response, error) {
	ok, message := substore.Default().Dependencies.StartSubStoreUpdate(c.Context())
	if ok {
		appLogger.Info("Sub-Store 在线更新：" + message)
		return httpx.JSONStatus(202, map[string]any{"success": true, "message": message}), nil
	}
	appLogger.Warning("Sub-Store 在线更新：" + message)
	return httpx.JSONStatus(409, map[string]any{"success": false, "message": message}), nil
}

// setSubStoreMode is POST /api/settings/dependencies/sub-store/mode.
func setSubStoreMode(c *httpx.Ctx) (*httpx.Response, error) {
	var request settingsRequest
	ok := c.BindSilent(&request)
	var mode string
	if err := json.Unmarshal(request.Mode, &mode); err != nil {
		ok = false
	}
	if !ok || (mode != "on_demand" && mode != "always") {
		return failure(400, "请选择按需启动或常驻运行"), nil
	}
	if err := substore.Default().Runtime.SetMode(c.Context(), mode); err != nil {
		if substore.IsRuntimeUnavailable(err) {
			return failure(409, err.Error()), nil
		}
		return nil, err
	}
	label := "按需启动"
	if mode == "always" {
		label = "常驻运行"
	}
	appLogger.Info("Sub-Store 运行模式已切换为" + label)
	return httpx.JSON(map[string]any{"success": true, "mode": mode}), nil
}
