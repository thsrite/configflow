package routers

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/routerdoc"
	"configflow/internal/urlx"
)

// Port of backend/routers/store.py: profile-scoped router definitions
// (profile["routers"]["items"]).

// RouterTypes is ROUTER_TYPES.
var RouterTypes = []string{"ikuai", "routeros"}

// EditableFields is EDITABLE_FIELDS.
var EditableFields = []string{"name", "type", "endpoint", "username", "verify_tls", "proxy_gateway_agent_id",
	"dns_agent_id", "ip_rule_sets", "manage_return_routing", "ros_policy_rule_sets", "auto_rule_sync",
	"rule_sync_interval", "dns_guard", "ipv6_pd_guard"}

// RoutersOf is store.routers_of(config).
func RoutersOf(config *jsonx.Object) []*jsonx.Object { return routerdoc.RoutersOf(config) }

// FindRouter is store.find_router(config, router_id).
func FindRouter(config *jsonx.Object, routerID string) *jsonx.Object {
	return routerdoc.FindRouter(config, routerID)
}

// newRouterID is f'router_{secrets.token_hex(6)}' (replaceable in tests).
var newRouterID = func() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "router_" + hex.EncodeToString(b)
}

// StaticRuleSets is static_rule_sets(router): ip_rule_sets without the
// RouterOS policy selections (legacy overlapping records).
func StaticRuleSets(router *jsonx.Object) ([]any, error) {
	policy := newPySet()
	if jsonx.Equal(router.Value("type"), "routeros") {
		var err error
		if policy, err = setOf(router.Value("ros_policy_rule_sets")); err != nil {
			return nil, err
		}
	}
	items, err := iterOr(router.Value("ip_rule_sets"))
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, item := range items {
		in, err := policy.has(item)
		if err != nil {
			return nil, err
		}
		if !in {
			out = append(out, item)
		}
	}
	return out, nil
}

// PublicView is public_view(router): the router as returned to the UI,
// never with the password. Values are shared with router, like Python's.
func PublicView(router *jsonx.Object) (*jsonx.Object, error) {
	result := jsonx.NewObject()
	keys := append(append([]string{"id"}, EditableFields...), "last_probe", "last_release")
	for _, key := range keys {
		result.Set(key, router.Value(key))
	}
	result.Set("manage_return_routing", isTrue(router.Value("manage_return_routing")))
	result.Set("auto_rule_sync", isTrue(router.Value("auto_rule_sync")))
	result.Set("rule_sync_interval", router.GetOr("rule_sync_interval", jsonx.Int(60)))
	policy, err := iterOr(router.Value("ros_policy_rule_sets"))
	if err != nil {
		return nil, err
	}
	result.Set("ros_policy_rule_sets", append([]any{}, policy...))
	static, err := StaticRuleSets(router)
	if err != nil {
		return nil, err
	}
	result.Set("ip_rule_sets", static)
	guard, err := GuardSettings(router)
	if err != nil {
		return nil, err
	}
	result.Set("dns_guard", guard)
	pd, err := PDGuardSettingsOf(router)
	if err != nil {
		return nil, err
	}
	result.Set("ipv6_pd_guard", pd)
	result.Set("has_password", jsonx.Truthy(router.Value("password")))
	return result, nil
}

// urlparsePath is urlparse(url).path: urlsplit's path without ;params.
func urlparsePath(scheme, path string) string {
	usesParams := map[string]bool{"": true, "ftp": true, "hdl": true, "prospero": true, "http": true, "imap": true,
		"https": true, "shttp": true, "rtsp": true, "rtspu": true, "sip": true, "sips": true, "mms": true, "sftp": true,
		"tel": true}
	if !usesParams[scheme] || !strings.Contains(path, ";") {
		return path
	}
	i := -1
	if slash := strings.LastIndex(path, "/"); slash >= 0 {
		if j := strings.Index(path[slash:], ";"); j >= 0 {
			i = slash + j
		}
		if i < 0 {
			return path
		}
	} else {
		i = strings.Index(path, ";")
	}
	return path[:i]
}

// stateChanged is `existing and existing.get(state) and any(router.get(key)
// != existing.get(key) for key in ('type', 'endpoint'))`.
func stateChanged(router, existing *jsonx.Object, state string) bool {
	if existing == nil || existing.Len() == 0 || !jsonx.Truthy(existing.Value(state)) {
		return false
	}
	for _, key := range []string{"type", "endpoint"} {
		if !jsonx.Equal(router.Value(key), existing.Value(key)) {
			return true
		}
	}
	return false
}

func uniqueStrings(list []any) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, item := range list {
		text := item.(string)
		if !seen[text] {
			seen[text] = true
			out = append(out, text)
		}
	}
	return out
}

func idList(value any) ([]any, bool) {
	if !jsonx.Truthy(value) {
		return []any{}, true
	}
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	for _, item := range list {
		if text, ok := item.(string); !ok || text == "" {
			return nil, false
		}
	}
	return list, true
}

// Normalize is normalize(payload, existing): the validated router record.
// Errors: *RouterValidationError (the user messages), or the Python
// exception a malformed value raises (TypeError, urlparse's ValueError).
func Normalize(payload any, existing *jsonx.Object) (*jsonx.Object, error) {
	input, ok := payload.(*jsonx.Object)
	if !ok || input == nil {
		return nil, validation("路由器参数必须是对象")
	}
	hasExisting := existing != nil && existing.Len() > 0
	router := jsonx.NewObject()
	if hasExisting {
		router = existing.Clone()
		static, err := StaticRuleSets(existing)
		if err != nil {
			return nil, err
		}
		router.Set("ip_rule_sets", static)
	}
	for _, key := range EditableFields {
		if value, ok := input.Get(key); ok {
			router.Set(key, value)
		}
	}
	if password, ok := input.Value("password").(string); ok && password != "" {
		router.Set("password", password)
	} else if !hasExisting {
		router.Set("password", "")
	}
	router.Set("name", pystr.Strip(orEmptyStr(router.Value("name"))))
	router.Set("type", orEmptyStr(router.Value("type")))
	router.Set("endpoint", strings.TrimRight(pystr.Strip(orEmptyStr(router.Value("endpoint"))), "/"))
	router.Set("username", pystr.Strip(orEmptyStr(router.Value("username"))))
	router.Set("verify_tls", !isFalse(router.Value("verify_tls")))
	manageReturn := router.GetOr("manage_return_routing", false)
	if _, ok := manageReturn.(bool); !ok {
		return nil, validation("自动配置回流必须为布尔值")
	}
	router.Set("manage_return_routing", manageReturn)
	routerType := router.GetString("type")
	autoSync, isBool := router.GetOr("auto_rule_sync", false).(bool)
	if !isBool || (autoSync && routerType != "routeros") {
		return nil, validation("远程规则自动同步仅支持 RouterOS，开关必须为布尔值")
	}
	interval := router.GetOr("rule_sync_interval", jsonx.Int(60))
	validInterval := false
	if n, ok := interval.(jsonx.Number); ok && n.IsInt() {
		if v, ok := n.Int64(); ok {
			switch v {
			case 15, 30, 60, 180, 360, 720, 1440:
				validInterval = true
			}
		}
	}
	if !validInterval {
		return nil, validation("请选择有效的规则检查间隔")
	}
	router.Set("auto_rule_sync", autoSync)
	router.Set("rule_sync_interval", interval)
	for _, key := range []string{"proxy_gateway_agent_id", "dns_agent_id"} {
		router.Set(key, orEmptyStr(router.Value(key)))
	}
	ruleSets, ok := idList(router.Value("ip_rule_sets"))
	if !ok {
		return nil, validation("IP 路由规则集必须是规则集 ID 列表")
	}
	router.Set("ip_rule_sets", uniqueStrings(ruleSets))
	policySets, ok := idList(router.Value("ros_policy_rule_sets"))
	if !ok {
		return nil, validation("策略路由规则集必须是规则集 ID 列表")
	}
	if len(policySets) > 0 && routerType != "routeros" {
		return nil, validation("按来源策略路由仅支持 RouterOS")
	}
	selected := map[string]bool{}
	for _, item := range ruleSets {
		selected[item.(string)] = true
	}
	for _, item := range policySets {
		if selected[item.(string)] {
			return nil, validation("同一规则集只能选择静态路由或策略路由中的一种方式")
		}
	}
	router.Set("ros_policy_rule_sets", uniqueStrings(policySets))
	if stateChanged(router, existing, "ros_policy_state") {
		return nil, validation("请先清空策略路由选择并发布撤销，再修改路由器类型或管理地址")
	}
	guard, err := NormalizeDNSGuardSettings(router.Value("dns_guard"), routerType, router.GetString("dns_agent_id"))
	if err != nil {
		return nil, validation(err.Error())
	}
	if guard.GetBool("enabled") || router.Has("dns_guard") {
		router.Set("dns_guard", guard)
	}
	if stateChanged(router, existing, "dns_guard_state") {
		return nil, validation("请先关闭强制 DNS 并发布撤销，再修改路由器类型或管理地址")
	}
	pdGuard, err := NormalizePDGuardSettings(router.Value("ipv6_pd_guard"), routerType)
	if err != nil {
		return nil, validation(err.Error())
	}
	if pdGuard.GetBool("enabled") || router.Has("ipv6_pd_guard") {
		router.Set("ipv6_pd_guard", pdGuard)
	}
	if stateChanged(router, existing, "ipv6_pd_guard_state") {
		return nil, validation("请先关闭 IPv6 前缀变化保护并发布撤回，再修改路由器类型或管理地址")
	}
	if stateChanged(router, existing, "ipv6_fakeip_state") {
		return nil, validation("请先关闭 IPv6 Fake-IP 并发布撤销，再修改路由器类型或管理地址")
	}
	if stateChanged(router, existing, "ipv6_routes_state") {
		return nil, validation("请先撤回 IPv6 规则集路由并发布，再修改路由器类型或管理地址")
	}

	if router.GetString("name") == "" {
		return nil, validation("请填写名称")
	}
	if routerType != "ikuai" && routerType != "routeros" {
		return nil, validation("路由器类型只支持 iKuai 4.x 或 RouterOS")
	}
	parsed, err := urlx.Split(router.GetString("endpoint"))
	if err != nil {
		return nil, pyErr("ValueError", err.Error())
	}
	path := urlparsePath(parsed.Scheme, parsed.Path)
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || (path != "" && path != "/") {
		return nil, validation("管理地址格式应为 http(s)://主机[:端口]")
	}
	if router.GetString("username") == "" {
		return nil, validation("请填写用户名")
	}
	if !jsonx.Truthy(router.Value("password")) {
		return nil, validation("请填写密码")
	}
	if !router.Has("id") {
		router.Set("id", newRouterID())
	}
	return router, nil
}

// RedactRouters is redact_routers(routers): the desensitized export drops
// credentials, management endpoints and device state. It is
// repo.Hooks.RedactRouters (a value Python cannot iterate panics with
// *PyError; stored documents always hold a list).
func RedactRouters(routers any) any {
	object, ok := routers.(*jsonx.Object)
	if !ok || object == nil {
		return routers
	}
	itemsRaw, err := iterOr(object.Value("items"))
	if err != nil {
		panic(err)
	}
	dropped := map[string]bool{"password": true, "last_probe": true, "last_release": true, "ros_policy_state": true,
		"dns_guard_state": true, "ipv6_fakeip_state": true, "ipv6_routes_state": true, "ipv6_pd_guard_state": true}
	items := []any{}
	for _, raw := range itemsRaw {
		item, ok := raw.(*jsonx.Object)
		if !ok || item == nil {
			continue
		}
		clean := jsonx.NewObject()
		for key, value := range item.All() {
			if !dropped[key] {
				clean.Set(key, value)
			}
		}
		if jsonx.Truthy(clean.Value("username")) {
			clean.Set("username", "[REDACTED]")
		}
		if jsonx.Truthy(clean.Value("endpoint")) {
			clean.Set("endpoint", "[REDACTED]")
		}
		items = append(items, clean)
	}
	result := object.Clone()
	result.Set("items", items)
	return result
}
