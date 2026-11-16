// Package routersplit is backend/common/router_split.py (routers R02): the
// router split ("Mihomo + OxiDNS 分流") takeover with a full TUN instead of
// the Linux gateway.
//
// A profile set up for the router split keeps mihomo.router_split. Its base
// YAML carries a complete TUN, like an ordinary All-in-One install, and the
// Linux gateway's selective takeover stays disabled. Profiles from before
// this marker used the gateway; they are recognized here as "legacy" so that
// callers do not remove a takeover the main router still forwards
// connections to.
//
// Imports: jsonx, yamlx, pystr, convert (DefaultMihomoTemplate), repo and
// internal/gateway/gwbase — never internal/gateway (agents A19–A22 import
// this package). The one call upwards, setup_flow.profile_sessions (C36), is
// the ProfileSessions hook. Wire (in internal/ippresence, listed in
// server.wirePackages) registers convert.MihomoHooks.IsFullTun.
package routersplit

import (
	"context"
	"strings"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/gateway/gwbase"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

// MarkerKey is MARKER_KEY.
const MarkerKey = "router_split"

// GatewayMarker is GATEWAY_MARKER.
const GatewayMarker = "x-configflow-linux-gateway"

// FullTun is FULL_TUN (a new value on every call).
func FullTun() *jsonx.Object {
	return jsonx.Obj("enable", true, "stack", "mixed", "auto-route", true, "auto-redirect", true,
		"auto-detect-interface", true, "dns-hijack", []any{"any:53", "tcp://any:53"})
}

// RouteKeysRemoved is ROUTE_KEYS_REMOVED.
var RouteKeysRemoved = []string{"route-address", "inet4-route-address", "inet6-route-address",
	"route-address-set", "route-exclude-address-set"}

// Message codes (the keys of MESSAGES).
const (
	CodeFullTun        = "ROUTER_SPLIT_FULL_TUN"
	CodeLegacy         = "ROUTER_SPLIT_LEGACY"
	CodeRestoreBlocked = "ROUTER_SPLIT_RESTORE_BLOCKED"
	CodeLegacyTeardown = "ROUTER_SPLIT_LEGACY_TEARDOWN"
	CodeInvalid        = "ROUTER_SPLIT_INVALID"
)

// MessageCodes lists the MESSAGES keys in Python's order.
var MessageCodes = []string{CodeFullTun, CodeLegacy, CodeRestoreBlocked, CodeLegacyTeardown, CodeInvalid}

// Messages is MESSAGES.
var Messages = map[string]string{
	CodeFullTun: "此配置空间使用 Mihomo + OxiDNS 分流（完整 TUN），主路由转来的连接已全部交给 Mihomo，" +
		"不能再开启 Linux 网关的选择性接管。",
	CodeLegacy: "这是旧版主路由分流：Mihomo 靠 Linux 网关接管主路由转来的连接。" +
		"关闭 Linux 网关会同时去掉 TUN 并还原转发，主路由转来的连接会中断。",
	CodeRestoreBlocked: "主路由分流的配置空间不能恢复试用前快照：快照中没有完整 TUN，恢复会让分流中断。",
	CodeLegacyTeardown: "撤销会还原 TUN 接管与 IPv4 转发，主路由转来的连接将中断。确认后重试。",
	CodeInvalid:        "主路由分流标记 mihomo.router_split 无效，已停止生成和网关操作。请从备份恢复，或重新运行首次使用向导。",
}

const (
	// LegacyRestoreBlocked is ROUTER_SPLIT_RESTORE_BLOCKED for a legacy
	// profile whose snapshot has the gateway off.
	LegacyRestoreBlocked = "恢复后 Linux 网关为关闭状态，主路由转来的连接会中断，已阻止。"
	// FullTunNoRestorePoint / FullTunRestoreOverwrites are
	// ROUTER_SPLIT_RESTORE_BLOCKED for a full TUN profile, by what the
	// restore point holds.
	FullTunNoRestorePoint    = "没有可恢复的试用前快照；主路由分流（完整 TUN）不使用 Linux 网关试用，无需恢复。"
	FullTunRestoreOverwrites = "主路由分流的配置空间不能恢复试用前快照：恢复会用快照覆盖当前的 Mihomo 与 OxiDNS 设置，可能让分流中断。"
	// GeneratedTunMissing is GENERATED_TUN_MISSING.
	GeneratedTunMissing = "主路由分流需要完整 TUN，但生成的 Mihomo 配置未启用（基础 YAML 缺少 tun 或被修改）。" +
		"请在基础 YAML 中恢复 tun 后再发布，否则主路由转来的连接会回环。"
	// GeneratedTunFiltered is GENERATED_TUN_FILTERED with "{keys}" in place.
	GeneratedTunFiltered = "主路由分流的完整 TUN 不能带 tun.{keys} 过滤：未进入 TUN 的连接会在主路由与 Linux 之间回环。" +
		"请在 Mihomo 基础 YAML 中移除后再发布。"
)

// Marker states returned by ReadMarker.
const (
	StateAbsent  = "absent"
	StateValid   = "valid"
	StateInvalid = "invalid"
)

var markerKeys = []string{"version", "takeover", "route_rule_set_ids", "origin"}

// RouterSplitError is router_split.RouterSplitError, a LinuxGatewayError so
// existing 400 handlers still apply: errors.As(err, **gwbase.LinuxGatewayError)
// finds the embedded error.
type RouterSplitError struct {
	gwbase.LinuxGatewayError
	Code   string
	Status int
}

// NewRouterSplitError is RouterSplitError(message, code, status); status 0
// means the default 409.
func NewRouterSplitError(message, code string, status int) *RouterSplitError {
	if status == 0 {
		status = 409
	}
	return &RouterSplitError{LinuxGatewayError: gwbase.LinuxGatewayError{Message: message}, Code: code, Status: status}
}

// PythonClass is the Python exception class name.
func (e *RouterSplitError) PythonClass() string { return "RouterSplitError" }

// As lets errors.As find the embedded *gwbase.LinuxGatewayError (Python's
// isinstance(error, LinuxGatewayError)).
func (e *RouterSplitError) As(target any) bool {
	if t, ok := target.(**gwbase.LinuxGatewayError); ok {
		*t = &e.LinuxGatewayError
		return true
	}
	return false
}

func gatewayError(message string) error { return &gwbase.LinuxGatewayError{Message: message} }

// validIDs is _valid_ids: same bounds as normalize_gateway_settings.
func validIDs(ids any) bool {
	list, ok := ids.([]any)
	if !ok || len(list) > 1024 {
		return false
	}
	for _, item := range list {
		s, ok := item.(string)
		if !ok || s == "" || utf8.RuneCountInString(s) > 256 {
			return false
		}
		for _, r := range s {
			if r < 32 || r == 127 {
				return false
			}
		}
	}
	return true
}

// ReadMarker is read_marker(config): (StateAbsent | StateValid |
// StateInvalid, the normalized marker or nil).
func ReadMarker(config any) (string, *jsonx.Object) {
	var mihomo any
	if isDict(config) {
		mihomo = dictValue(config, "mihomo")
	}
	if !isDict(mihomo) || !dictHas(mihomo, MarkerKey) {
		return StateAbsent, nil
	}
	marker, ok := dictValue(mihomo, MarkerKey).(*jsonx.Object)
	if !ok || marker == nil || marker.Len() != len(markerKeys) {
		return StateInvalid, nil
	}
	for _, key := range markerKeys {
		if _, ok := marker.Get(key); !ok {
			return StateInvalid, nil
		}
	}
	version, ok := marker.Value("version").(jsonx.Number)
	if !ok || !version.IsInt() || version.String() != "1" ||
		!jsonx.Equal(marker.Value("takeover"), "full_tun") || !jsonx.Equal(marker.Value("origin"), "setup") ||
		!validIDs(marker.Value("route_rule_set_ids")) {
		return StateInvalid, nil
	}
	result := marker.DeepCopy()
	seen := map[string]bool{}
	ids := []any{}
	for _, item := range marker.Value("route_rule_set_ids").([]any) {
		if s := item.(string); !seen[s] {
			seen[s] = true
			ids = append(ids, s)
		}
	}
	result.Set("route_rule_set_ids", ids)
	return StateValid, result
}

// IsFullTun is is_full_tun(config).
func IsFullTun(config any) bool {
	state, _ := ReadMarker(config)
	return state == StateValid
}

// RouteRuleSetIDs is route_rule_set_ids(config): the IP rule sets the main
// router forwards to Linux, besides fake-IP.
func RouteRuleSetIDs(config any) []any {
	state, marker := ReadMarker(config)
	if state == StateValid {
		return marker.Value("route_rule_set_ids").([]any)
	}
	return []any{}
}

// gatewayEnabled is _gateway_enabled(config); tolerates a malformed setting.
func gatewayEnabled(config any) (bool, error) {
	value, err := chainGet(config, "mihomo", "linux_gateway")
	if err != nil {
		return false, err
	}
	enabled, ok := dictGet(value, "enabled")
	return isDict(value) && ok && enabled == true, nil
}

// TunFilters is tun_filters(tun): the TUN filter keys in use; each lets
// some forwarded connections bypass the TUN.
func TunFilters(tun any) []string {
	out := []string{}
	if !isDict(tun) {
		return out
	}
	for _, key := range gwbase.TUNFilterKeys {
		if truthy(dictValue(tun, key)) {
			out = append(out, key)
		}
	}
	return out
}

// FullTunDocument is full_tun_document(document): the base document (a
// *jsonx.Object or a *yamlx.Mapping) with the full TUN; user TUN keys are
// kept. The input is not changed.
func FullTunDocument(document any) (any, error) {
	if _, err := getter(document); err != nil {
		return nil, attributeError(document, "get")
	}
	document = deepcopy(document)
	tun := dictValue(document, "tun")
	if tun == nil {
		tun = jsonx.NewObject()
	}
	if !isDict(tun) {
		return nil, gatewayError("Mihomo tun 必须是对象")
	}
	for _, key := range TunFilters(tun) {
		return nil, gatewayError("主路由分流的完整 TUN 与现有 tun." + key + " 过滤冲突：未进入 TUN 的连接会在主路由与 Linux 之间回环。" +
			"请先在 Mihomo 基础 YAML 中移除该过滤")
	}
	for _, key := range RouteKeysRemoved {
		dictPop(tun, key)
	}
	full := FullTun()
	for key, value := range full.All() {
		if key == "stack" {
			if !dictHas(tun, key) {
				dictSet(tun, key, value)
			}
		} else {
			dictSet(tun, key, jsonx.DeepCopy(value))
		}
	}
	dictSet(document, "tun", tun)
	return document, nil
}

// loadBase is _load_base(text). text nil-able any: Python accepts anything
// and rejects non-strings.
func loadBase(text any) (any, error) {
	s, ok := text.(string)
	if !ok {
		return nil, gatewayError("Mihomo 基础配置必须为 YAML 文本")
	}
	if pystr.Strip(s) == "" {
		return convert.DefaultMihomoTemplate(), nil
	}
	document, err := yamlx.LoadWith(s, yamlx.LoadOptions{KeepNonStringKeys: true})
	if err != nil {
		if e, ok := err.(*yamlx.Error); ok && (e.IsYAMLError() || e.Class == "RecursionError") {
			return nil, gatewayError("Mihomo 基础 YAML 无法解析，请先修正基础配置")
		}
		return nil, err
	}
	if !isDict(document) {
		return nil, gatewayError("Mihomo 基础配置必须是 YAML 对象")
	}
	return document, nil
}

// dump is yaml.safe_dump(document, allow_unicode=True, sort_keys=False).
func dump(document any) (string, error) {
	return yamlx.SafeDumpUnicode(document)
}

// FullTunBaseYAML is full_tun_base_yaml(text).
func FullTunBaseYAML(text any) (string, error) {
	base, err := loadBase(text)
	if err != nil {
		return "", err
	}
	document, err := FullTunDocument(base)
	if err != nil {
		return "", err
	}
	return dump(document)
}

// fullTunShape is _full_tun_shape(doc): captures every connection, apart
// from what a TUN filter excludes.
func fullTunShape(doc any) bool {
	if !isDict(doc) || dictHas(doc, GatewayMarker) {
		return false
	}
	tun := dictValue(doc, "tun")
	if !isDict(tun) {
		return false
	}
	for _, key := range []string{"enable", "auto-route", "auto-redirect"} {
		if dictValue(tun, key) != true {
			return false
		}
	}
	hijack, ok := dictValue(tun, "dns-hijack").([]any)
	if !ok || !contains(hijack, "any:53") {
		return false
	}
	for _, key := range RouteKeysRemoved {
		if dictHas(tun, key) {
			return false
		}
	}
	return true
}

func documentFilters(doc any) []string {
	if !isDict(doc) {
		return []string{}
	}
	return TunFilters(dictValue(doc, "tun"))
}

// IsFullTunDocument is is_full_tun_document(doc): a TUN the main router can
// forward to (a filtered connection would loop back).
func IsFullTunDocument(doc any) bool {
	return fullTunShape(doc) && len(documentFilters(doc)) == 0
}

// CheckGeneration is check_generation(config): stop Mihomo generation for
// an invalid marker or a conflicting gateway (nil or *RouterSplitError, or
// the AttributeError of a malformed profile).
func CheckGeneration(config any) error {
	state, _ := ReadMarker(config)
	if state == StateInvalid {
		return NewRouterSplitError(Messages[CodeInvalid], CodeInvalid, 0)
	}
	if state == StateValid {
		enabled, err := gatewayEnabled(config)
		if err != nil {
			return err
		}
		if enabled {
			return NewRouterSplitError(Messages[CodeFullTun], CodeFullTun, 0)
		}
	}
	return nil
}

// RequireFullTunGenerated is require_full_tun_generated(main): a ValueError
// (*PyError) when the generated document lacks the full TUN.
func RequireFullTunGenerated(main any) error {
	if !fullTunShape(main) {
		return &PyError{"ValueError", GeneratedTunMissing}
	}
	if filters := documentFilters(main); len(filters) > 0 {
		return &PyError{"ValueError", strings.Replace(GeneratedTunFiltered, "{keys}", strings.Join(filters, "、tun."), 1)}
	}
	return nil
}

// ---- legacy detection (repository reads) ----

// HookMissingError is returned where a hook of another module is needed but
// not registered yet (fail closed).
type HookMissingError struct{ Hook, Provider string }

func (e *HookMissingError) Error() string {
	return "common.router_split: " + e.Hook + " is not available (provided by " + e.Provider + ")"
}

// ProfileSessionsFunc is setup_flow.profile_sessions(repository,
// profile_id): the profile's setup sessions, newest first.
type ProfileSessionsFunc func(ctx context.Context, r *repo.Repository, profileID string) ([]*jsonx.Object, error)

var profileSessions ProfileSessionsFunc

// SetProfileSessions registers setup_flow.profile_sessions (config C36) and
// returns a function restoring the previous one. Until then the session
// reads fail closed with *HookMissingError.
func SetProfileSessions(fn ProfileSessionsFunc) (restore func()) {
	previous := profileSessions
	profileSessions = fn
	return func() { profileSessions = previous }
}

// Repository reads, variables so the recorded replay can answer them.
var (
	agentBindings = func(ctx context.Context, r *repo.Repository, profileID string) (*jsonx.Object, error) {
		return r.GetProfileAgentBindings(ctx, profileID)
	}
	getProfile = func(ctx context.Context, r *repo.Repository, profileID string) (*jsonx.Object, error) {
		return r.GetProfile(ctx, profileID)
	}
	sessionsOf = func(ctx context.Context, r *repo.Repository, profileID string) ([]*jsonx.Object, error) {
		if profileSessions == nil {
			return nil, &HookMissingError{Hook: "setup_flow.profile_sessions", Provider: "config C36 (common/setup_flow.py)"}
		}
		return profileSessions(ctx, r, profileID)
	}
)

// sessionContext is _session_context: an abandoned session is no longer
// the profile's purpose, as on the Setup page.
func sessionContext(ctx context.Context, r *repo.Repository, profileID string) (*jsonx.Object, error) {
	all, err := sessionsOf(ctx, r, profileID)
	if err != nil {
		return nil, err
	}
	var sessions []*jsonx.Object
	for _, item := range all {
		if !truthy(item.Value("abandoned")) {
			sessions = append(sessions, item)
		}
	}
	out := jsonx.NewObject()
	if len(sessions) > 0 {
		latest := sessions[0] // Sorted newest first.
		out.Set("session_goal", latest.Value("goal"))
		out.Set("session_id", latest.Value("id"))
		out.Set("session_completed", truthy(latest.Value("completed")))
	} else {
		out.Set("session_goal", nil)
		out.Set("session_id", nil)
		out.Set("session_completed", false)
	}
	gateway := false
	for _, item := range sessions {
		if jsonx.Equal(item.Value("goal"), "gateway") {
			gateway = true
			break
		}
	}
	out.Set("has_gateway_session", gateway)
	return out, nil
}

// LegacyContext is legacy_context(repository, profile_id): the setup
// sessions and bound Mihomo Agents; call outside profile transactions.
func LegacyContext(ctx context.Context, r *repo.Repository, profileID string) (*jsonx.Object, error) {
	bindings, err := agentBindings(ctx, r, profileID)
	if err != nil {
		return nil, err
	}
	agents, ok := bindings.Get("agents")
	if !ok {
		return nil, &PyError{"KeyError", "'agents'"}
	}
	out, err := sessionContext(ctx, r, profileID)
	if err != nil {
		return nil, err
	}
	items, err := pyList(agents)
	if err != nil {
		return nil, err
	}
	ids := []any{}
	for _, item := range items {
		agent, err := getter(item)
		if err != nil {
			return nil, err
		}
		if jsonx.Equal(dictValue(agent, "service_type"), "mihomo") {
			id, ok := dictGet(agent, "id")
			if !ok {
				return nil, &PyError{"KeyError", "'id'"}
			}
			ids = append(ids, id)
		}
	}
	out.Set("mihomo_agent_ids", ids)
	return out, nil
}

// NeedsContext is needs_context(config): only an unmarked profile with the
// gateway on can be a legacy router split.
func NeedsContext(config any) (bool, error) {
	if state, _ := ReadMarker(config); state != StateAbsent {
		return false, nil
	}
	return gatewayEnabled(config)
}

// BaseIsFullTun is base_is_full_tun(config): whether the Mihomo base YAML
// itself carries the full TUN, without the gateway.
func BaseIsFullTun(config any) (bool, error) {
	base, err := baseDocument(config)
	if err != nil {
		return false, err
	}
	return IsFullTunDocument(base), nil
}

// baseDocument is _base_document(config): the loaded base YAML, nil when it
// cannot be parsed.
func baseDocument(config any) (any, error) {
	value, err := chainGet(config, "mihomo", "custom_config")
	if err != nil {
		return nil, err
	}
	if !truthy(value) {
		value = ""
	}
	text, ok := value.(string)
	if !ok {
		// yaml.safe_load treats anything else as a file object.
		return nil, attributeError(value, "read")
	}
	document, err := yamlx.LoadWith(text, yamlx.LoadOptions{KeepNonStringKeys: true})
	if err != nil {
		if e, ok := err.(*yamlx.Error); ok && (e.IsYAMLError() || e.Class == "RecursionError") {
			return nil, nil
		}
		return nil, err
	}
	return document, nil
}

// routersOf is `(config.get('routers') or {}).get('items') or []`, iterated.
func routersOf(config any) ([]any, error) {
	items, err := chainGet(config, "routers", "items")
	if err != nil {
		return nil, err
	}
	if !truthy(items) {
		return []any{}, nil
	}
	return pyList(items)
}

// routerIDs is _router_ids(config, agent_ids).
func routerIDs(config any, agentIDs []any) ([]any, error) {
	routers, err := routersOf(config)
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, router := range routers {
		if isDict(router) && contains(agentIDs, dictValue(router, "proxy_gateway_agent_id")) {
			out = append(out, dictValue(router, "id"))
		}
	}
	return out, nil
}

// contextAgentIDs is list(context.get('mihomo_agent_ids') or []).
func contextAgentIDs(context any) ([]any, error) {
	c, err := getter(context)
	if err != nil {
		return nil, err
	}
	value := dictValue(c, "mihomo_agent_ids")
	if !truthy(value) {
		return []any{}, nil
	}
	return pyList(value)
}

// LegacySource is legacy_source(config, context): how an unmarked profile
// matches the router split set up with the gateway: "setup", "topology" or
// nil, whatever its gateway switch and base TUN.
func LegacySource(config, context any) (any, error) {
	if state, _ := ReadMarker(config); state != StateAbsent {
		return nil, nil
	}
	c, err := getter(context)
	if err != nil {
		return nil, err
	}
	if jsonx.Equal(dictValue(c, "session_goal"), "router") {
		return "setup", nil
	}
	if truthy(dictValue(c, "has_gateway_session")) {
		return nil, nil // A gateway-goal profile may use upstream routers without the split.
	}
	ids, err := contextAgentIDs(c)
	if err != nil {
		return nil, err
	}
	routers, err := routerIDs(config, ids)
	if err != nil {
		return nil, err
	}
	if len(routers) > 0 {
		return "topology", nil
	}
	return nil, nil
}

// PublishLegacySource is publish_legacy_source(repository, profile_id,
// config, agent_id): legacy_source for a publish to one Agent; reads the
// setup sessions only if needed. profileID "" is falsy (None or ”).
func PublishLegacySource(ctx context.Context, r *repo.Repository, profileID string, config, agentID any) (any, error) {
	if state, _ := ReadMarker(config); state != StateAbsent || profileID == "" {
		return nil, nil
	}
	context, err := sessionContext(ctx, r, profileID)
	if err != nil {
		return nil, err
	}
	context.Set("mihomo_agent_ids", []any{agentID})
	return LegacySource(config, context)
}

// LegacyGatewayAgentIDs is legacy_gateway_agent_ids(config, context): the
// bound Mihomo Agents an old router split's gateway record may be on — the
// routers' proxy gateways for a topology match, every one for "setup".
func LegacyGatewayAgentIDs(config, context any) ([]any, error) {
	agentIDs, err := contextAgentIDs(context)
	if err != nil {
		return nil, err
	}
	source, err := LegacySource(config, context)
	if err != nil {
		return nil, err
	}
	if source == "topology" {
		routers, err := routersOf(config)
		if err != nil {
			return nil, err
		}
		var proxies []any
		for _, router := range routers {
			if isDict(router) {
				value := dictValue(router, "proxy_gateway_agent_id")
				if err := hashable(value); err != nil {
					return nil, err
				}
				proxies = append(proxies, value)
			}
		}
		out := []any{}
		for _, agentID := range agentIDs {
			if err := hashable(agentID); err != nil {
				return nil, err
			}
			if contains(proxies, agentID) {
				out = append(out, agentID)
			}
		}
		return out, nil
	}
	if source != nil {
		return agentIDs, nil
	}
	return []any{}, nil
}

// GatewayWasUsed is gateway_was_used(config, agent, source): whether a
// publish may meet a gateway record left by this profile's old router split.
//
// The old wizard ("setup") always turned the gateway on. Otherwise the
// editor keeps a restore point once it changes the gateway, and the
// installation wizard marks each Agent it installed.
func GatewayWasUsed(config, agent, source any) (bool, error) {
	if jsonx.Equal(source, "setup") {
		return true, nil
	}
	c, err := getter(config)
	if err != nil {
		return false, err
	}
	mihomo := orEmpty(dictValue(c, "mihomo"))
	switch value := mihomo.(type) {
	case *jsonx.Object, *yamlx.Mapping:
		if dictHas(value, gwbase.RestoreKey) {
			return true, nil
		}
	case string:
		if strings.Contains(value, gwbase.RestoreKey) {
			return true, nil
		}
	case []any:
		if contains(value, gwbase.RestoreKey) {
			return true, nil
		}
	default:
		return false, &PyError{"TypeError", "argument of type '" + pyTypeName(value) + "' is not iterable"}
	}
	a, err := getter(agent)
	if err != nil {
		return false, err
	}
	return truthy(dictValue(a, "gateway_installation_id")), nil
}

// state is _state(config, context).
func state(config, context any) (*jsonx.Object, error) {
	agentIDs, err := contextAgentIDs(context)
	if err != nil {
		return nil, err
	}
	routers, err := routerIDs(config, agentIDs)
	if err != nil {
		return nil, err
	}
	result := jsonx.Obj("mode", nil, "source", nil, "mihomo_agent_ids", agentIDs, "router_ids", routers,
		"setup_session_id", dictValue(context, "session_id"), "issues", []any{})
	marker, _ := ReadMarker(config)
	switch marker {
	case StateValid:
		result.Set("mode", "full_tun")
		result.Set("source", "marker")
		base, err := baseDocument(config)
		if err != nil {
			return nil, err
		}
		issues := []any{}
		if len(documentFilters(base)) > 0 {
			issues = append(issues, "tun_filtered")
		}
		if !fullTunShape(base) {
			issues = append(issues, "tun_missing")
		}
		enabled, err := gatewayEnabled(config)
		if err != nil {
			return nil, err
		}
		if enabled {
			issues = append(issues, "gateway_enabled")
		}
		result.Set("issues", issues)
		return result, nil
	case StateInvalid:
		result.Set("mode", "invalid")
		result.Set("source", "marker")
		return result, nil
	}
	enabled, err := gatewayEnabled(config)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return result, nil
	}
	if full, err := BaseIsFullTun(config); err != nil {
		return nil, err
	} else if full {
		// With its own full TUN, turning the gateway off keeps the takeover;
		// the editor still checks that the host keeps IPv4 forwarding then.
		return result, nil
	}
	source, err := LegacySource(config, context)
	if err != nil {
		return nil, err
	}
	if source != nil {
		result.Set("mode", "legacy")
		result.Set("source", source)
	}
	return result, nil
}

// ModeFromContext is mode_from_context(config, context): decided from the
// locked config and a context read before the transaction (nil or a
// string).
func ModeFromContext(config, context any) (any, error) {
	result, err := state(config, context)
	if err != nil {
		return nil, err
	}
	return result.Value("mode"), nil
}

// RouterSplitState is router_split_state(repository, profile_id,
// config=None, context=None): config and context nil are None (read from
// the repository).
func RouterSplitState(ctx context.Context, r *repo.Repository, profileID string, config, context *jsonx.Object) (*jsonx.Object, error) {
	var err error
	if config == nil {
		if config, err = getProfile(ctx, r, profileID); err != nil {
			return nil, err
		}
	}
	if context == nil {
		if context, err = LegacyContext(ctx, r, profileID); err != nil {
			return nil, err
		}
	}
	return state(config, context)
}
