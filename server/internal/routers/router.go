package routers

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/netipx"
)

// RouterTypes are the supported vendors.
var RouterTypes = []string{"ikuai", "routeros"}

// EditableFields are the settings a client may change.
var EditableFields = []string{"name", "type", "endpoint", "username", "verify_tls", "proxy_gateway_agent_id",
	"dns_agent_id", "ip_rule_sets", "manage_return_routing", "ros_policy_rule_sets", "auto_rule_sync",
	"rule_sync_interval", "dns_guard", "ipv6_pd_guard"}

// RuleSyncIntervals are the allowed automatic rule check intervals (minutes).
var RuleSyncIntervals = []int{15, 30, 60, 180, 360, 720, 1440}

// DNSGuard is the forced-DNS switch (RouterOS only).
type DNSGuard struct {
	Enabled  bool     `json:"enabled"`
	BlockDoT bool     `json:"block_dot"`
	Failover bool     `json:"failover"`
	Bypass   []string `json:"bypass"` // IPv4 hosts/networks exempt from the hijack
}

// DefaultDNSGuard is the guard of a router that never enabled it.
func DefaultDNSGuard() DNSGuard { return DNSGuard{Failover: true, Bypass: []string{}} }

// PDGuard is the IPv6 prefix-change guard switch (RouterOS only).
type PDGuard struct {
	Enabled bool `json:"enabled"`
}

// Router is one stored router (profile routers.items[]). Fields this
// package does not know (and the device state written by later modules) are
// kept in Extra and written back unchanged.
type Router struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Type                string    `json:"type"`
	Endpoint            string    `json:"endpoint"`
	Username            string    `json:"username"`
	Password            string    `json:"password"`
	VerifyTLS           bool      `json:"verify_tls"`
	ProxyGatewayAgentID string    `json:"proxy_gateway_agent_id"`
	DNSAgentID          string    `json:"dns_agent_id"`
	IPRuleSets          []string  `json:"ip_rule_sets"`
	ManageReturnRouting bool      `json:"manage_return_routing"`
	RosPolicyRuleSets   []string  `json:"ros_policy_rule_sets"`
	AutoRuleSync        bool      `json:"auto_rule_sync"`
	RuleSyncInterval    int       `json:"rule_sync_interval"`
	DNSGuard            *DNSGuard `json:"dns_guard,omitempty"`
	IPv6PDGuard         *PDGuard  `json:"ipv6_pd_guard,omitempty"`

	// Extra holds every other stored field: last_probe, last_release,
	// ros_policy_state, dns_guard_state, ipv6_*_state, …
	Extra map[string]json.RawMessage `json:"-"`
}

var routerFields = func() map[string]bool {
	m := map[string]bool{"id": true, "password": true}
	for _, f := range EditableFields {
		m[f] = true
	}
	return m
}()

// UnmarshalJSON reads a stored record; absent settings take the defaults
// the Python reader used (verify_tls on, 60-minute interval).
func (r *Router) UnmarshalJSON(data []byte) error {
	type plain Router
	p := plain{VerifyTLS: true, RuleSyncInterval: 60}
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	p.Extra = map[string]json.RawMessage{}
	for k, v := range all {
		if !routerFields[k] {
			p.Extra[k] = v
		}
	}
	*r = Router(p)
	return nil
}

// MarshalJSON writes the known fields and Extra.
func (r Router) MarshalJSON() ([]byte, error) {
	type plain Router
	known, err := json.Marshal(plain(r))
	if err != nil || len(r.Extra) == 0 {
		return known, err
	}
	extra, err := json.Marshal(r.Extra)
	if err != nil {
		return nil, err
	}
	return append(append(known[:len(known)-1], ','), extra[1:]...), nil
}

// State is a stored device-state field (Extra), nil when absent or null.
func (r *Router) State(key string) json.RawMessage {
	if v := r.Extra[key]; truthy(v) {
		return v
	}
	return nil
}

// StaticRuleSets is ip_rule_sets without the RouterOS policy selections
// (old records could select a rule set both ways).
func (r *Router) StaticRuleSets() []string {
	out := []string{}
	for _, id := range r.IPRuleSets {
		if r.Type != "routeros" || !slices.Contains(r.RosPolicyRuleSets, id) {
			out = append(out, id)
		}
	}
	return out
}

// RouterView is a router as the UI sees it (never the password).
type RouterView struct {
	ID                  string          `json:"id"`
	Name                string          `json:"name"`
	Type                string          `json:"type"`
	Endpoint            string          `json:"endpoint"`
	Username            string          `json:"username"`
	VerifyTLS           bool            `json:"verify_tls"`
	ProxyGatewayAgentID string          `json:"proxy_gateway_agent_id"`
	DNSAgentID          string          `json:"dns_agent_id"`
	IPRuleSets          []string        `json:"ip_rule_sets"`
	ManageReturnRouting bool            `json:"manage_return_routing"`
	RosPolicyRuleSets   []string        `json:"ros_policy_rule_sets"`
	AutoRuleSync        bool            `json:"auto_rule_sync"`
	RuleSyncInterval    int             `json:"rule_sync_interval"`
	DNSGuard            DNSGuard        `json:"dns_guard"`
	IPv6PDGuard         PDGuard         `json:"ipv6_pd_guard"`
	LastProbe           json.RawMessage `json:"last_probe"`
	LastRelease         json.RawMessage `json:"last_release"`
	HasPassword         bool            `json:"has_password"`
}

// View is the router as returned to the UI.
func (r *Router) View() RouterView {
	guard := DefaultDNSGuard()
	if r.DNSGuard != nil {
		guard = *r.DNSGuard
	}
	var pd PDGuard
	if r.IPv6PDGuard != nil {
		pd = *r.IPv6PDGuard
	}
	return RouterView{ID: r.ID, Name: r.Name, Type: r.Type, Endpoint: r.Endpoint, Username: r.Username,
		VerifyTLS: r.VerifyTLS, ProxyGatewayAgentID: r.ProxyGatewayAgentID, DNSAgentID: r.DNSAgentID,
		IPRuleSets: r.StaticRuleSets(), ManageReturnRouting: r.ManageReturnRouting,
		RosPolicyRuleSets: append([]string{}, r.RosPolicyRuleSets...), AutoRuleSync: r.AutoRuleSync,
		RuleSyncInterval: r.RuleSyncInterval, DNSGuard: guard, IPv6PDGuard: pd,
		LastProbe: r.Extra["last_probe"], LastRelease: r.Extra["last_release"], HasPassword: r.Password != ""}
}

// truthy is JSON truthiness: not null/false/0/""/[]/{}.
func truthy(raw json.RawMessage) bool {
	switch s := string(bytes.TrimSpace(raw)); s {
	case "", "null", "false", `""`, "[]", "{}":
		return false
	default:
		f, err := strconv.ParseFloat(s, 64)
		return err != nil || f != 0
	}
}

// text is the setting as text: strings as they are, other scalars in their
// JSON form, empty for falsy values.
func text(raw json.RawMessage) string {
	if !truthy(raw) {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	if string(raw) == "true" {
		return "True"
	}
	return string(raw)
}

// jsonString decodes a JSON string (null and other types are not strings).
func jsonString(raw json.RawMessage) (string, bool) {
	var s string
	raw = bytes.TrimSpace(raw)
	return s, len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &s) == nil
}

// idList decodes a list of non-empty rule-set ids (falsy means none).
func idList(raw json.RawMessage) ([]string, bool) {
	if !truthy(raw) {
		return []string{}, true
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil, false
	}
	out := []string{}
	for _, item := range items {
		s, ok := jsonString(item)
		if !ok || s == "" {
			return nil, false
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out, true
}

func isBool(raw json.RawMessage) (bool, bool) {
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// NormalizeDNSGuard validates the forced-DNS setting; raw nil means never
// enabled.
func NormalizeDNSGuard(raw json.RawMessage, routerType, dnsAgentID string) (DNSGuard, error) {
	guard := DefaultDNSGuard()
	if raw == nil || string(raw) == "null" {
		return guard, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return guard, invalid("强制 DNS 设置格式无效")
	}
	for k := range fields {
		if k != "enabled" && k != "block_dot" && k != "failover" && k != "bypass" {
			return guard, invalid("强制 DNS 设置格式无效")
		}
	}
	for key, dst := range map[string]*bool{"enabled": &guard.Enabled, "block_dot": &guard.BlockDoT, "failover": &guard.Failover} {
		if v, ok := fields[key]; ok {
			b, ok := isBool(v)
			if !ok {
				return guard, invalid("强制 DNS 的开关必须为布尔值")
			}
			*dst = b
		}
	}
	var bypass []json.RawMessage
	if v, ok := fields["bypass"]; ok && (json.Unmarshal(v, &bypass) != nil || bypass == nil) {
		return guard, invalid("放行设备必须是 IPv4 地址或网段列表")
	}
	var items []string
	for _, v := range bypass {
		s, ok := jsonString(v)
		if !ok {
			return guard, invalid("放行设备必须是 IPv4 地址或网段列表")
		}
		items = append(items, s)
	}
	for _, item := range items {
		network, ok := netipx.ParseNetwork4(strings.TrimSpace(item), false)
		if !ok {
			return guard, invalid("放行设备「" + item + "」不是有效的 IPv4 地址或网段")
		}
		if text := network.String(); !slices.Contains(guard.Bypass, text) {
			guard.Bypass = append(guard.Bypass, text)
		}
	}
	if len(items) > 64 {
		return guard, invalid("放行设备最多 64 项")
	}
	if guard.Enabled && routerType != "routeros" {
		return guard, invalid("强制局域网 DNS 走 OxiDNS 仅支持 RouterOS")
	}
	if guard.Enabled && dnsAgentID == "" {
		return guard, invalid("请先选择 DHCP DNS 服务器（OxiDNS Agent），强制 DNS 使用同一台设备")
	}
	return guard, nil
}

// NormalizePDGuard validates the IPv6 prefix-change guard switch.
func NormalizePDGuard(raw json.RawMessage, routerType string) (PDGuard, error) {
	var guard PDGuard
	if raw == nil || string(raw) == "null" {
		return guard, nil
	}
	var fields map[string]json.RawMessage
	ok := json.Unmarshal(raw, &fields) == nil && fields != nil
	for k, v := range fields {
		b, isB := isBool(v)
		ok = ok && k == "enabled" && isB
		guard.Enabled = b
	}
	if !ok {
		return guard, invalid("IPv6 前缀变化保护的开关必须为布尔值")
	}
	if guard.Enabled && routerType != "routeros" {
		return guard, invalid("IPv6 前缀变化保护仅支持 RouterOS")
	}
	return guard, nil
}

// NewRouterID returns a fresh router id (replaceable in tests).
var NewRouterID = func() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "router_" + hex.EncodeToString(b)
}

// Normalize validates a create (existing nil) or update request body and
// returns the record to store. An empty password keeps the stored one.
// Errors are *ValidationError with the message for the user.
func Normalize(body []byte, existing *Router) (*Router, error) {
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil || payload == nil {
		return nil, invalid("路由器参数必须是对象")
	}
	r := &Router{VerifyTLS: true, RuleSyncInterval: 60, Extra: map[string]json.RawMessage{}}
	if existing != nil {
		copied := *existing
		r = &copied
		r.IPRuleSets = existing.StaticRuleSets()
	}
	set := func(key string, apply func(json.RawMessage)) {
		if v, ok := payload[key]; ok {
			apply(v)
		}
	}
	set("name", func(v json.RawMessage) { r.Name = text(v) })
	set("type", func(v json.RawMessage) { r.Type = text(v) })
	set("endpoint", func(v json.RawMessage) { r.Endpoint = text(v) })
	set("username", func(v json.RawMessage) { r.Username = text(v) })
	set("verify_tls", func(v json.RawMessage) { r.VerifyTLS = string(bytes.TrimSpace(v)) != "false" })
	set("proxy_gateway_agent_id", func(v json.RawMessage) { r.ProxyGatewayAgentID = text(v) })
	set("dns_agent_id", func(v json.RawMessage) { r.DNSAgentID = text(v) })
	if v, ok := payload["password"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			r.Password = s
		}
	}
	r.Name, r.Username = strings.TrimSpace(r.Name), strings.TrimSpace(r.Username)
	r.Endpoint = strings.TrimRight(strings.TrimSpace(r.Endpoint), "/")

	if v, ok := payload["manage_return_routing"]; ok {
		b, ok := isBool(v)
		if !ok {
			return nil, invalid("自动配置回流必须为布尔值")
		}
		r.ManageReturnRouting = b
	}
	if v, ok := payload["auto_rule_sync"]; ok {
		b, ok := isBool(v)
		if !ok {
			return nil, invalid("远程规则自动同步仅支持 RouterOS，开关必须为布尔值")
		}
		r.AutoRuleSync = b
	}
	if r.AutoRuleSync && r.Type != "routeros" {
		return nil, invalid("远程规则自动同步仅支持 RouterOS，开关必须为布尔值")
	}
	if v, ok := payload["rule_sync_interval"]; ok {
		n, err := strconv.Atoi(string(bytes.TrimSpace(v)))
		if err != nil || !slices.Contains(RuleSyncIntervals, n) {
			return nil, invalid("请选择有效的规则检查间隔")
		}
		r.RuleSyncInterval = n
	}
	if v, ok := payload["ip_rule_sets"]; ok {
		if r.IPRuleSets, ok = idList(v); !ok {
			return nil, invalid("IP 路由规则集必须是规则集 ID 列表")
		}
	}
	if v, ok := payload["ros_policy_rule_sets"]; ok {
		if r.RosPolicyRuleSets, ok = idList(v); !ok {
			return nil, invalid("策略路由规则集必须是规则集 ID 列表")
		}
	}
	if r.IPRuleSets == nil {
		r.IPRuleSets = []string{}
	}
	if r.RosPolicyRuleSets == nil {
		r.RosPolicyRuleSets = []string{}
	}
	if len(r.RosPolicyRuleSets) > 0 && r.Type != "routeros" {
		return nil, invalid("按来源策略路由仅支持 RouterOS")
	}
	for _, id := range r.RosPolicyRuleSets {
		if slices.Contains(r.IPRuleSets, id) {
			return nil, invalid("同一规则集只能选择静态路由或策略路由中的一种方式")
		}
	}
	moved := existing != nil && (r.Type != existing.Type || r.Endpoint != existing.Endpoint)
	owns := func(state string) bool { return moved && existing.State(state) != nil }
	if owns("ros_policy_state") {
		return nil, invalid("请先清空策略路由选择并发布撤销，再修改路由器类型或管理地址")
	}
	guardRaw, guardSet := payload["dns_guard"]
	if !guardSet && r.DNSGuard != nil {
		guardRaw, _ = json.Marshal(r.DNSGuard)
	}
	guard, err := NormalizeDNSGuard(guardRaw, r.Type, r.DNSAgentID)
	if err != nil {
		return nil, err
	}
	if guard.Enabled || guardSet || r.DNSGuard != nil {
		r.DNSGuard = &guard
	}
	if owns("dns_guard_state") {
		return nil, invalid("请先关闭强制 DNS 并发布撤销，再修改路由器类型或管理地址")
	}
	pdRaw, pdSet := payload["ipv6_pd_guard"]
	if !pdSet && r.IPv6PDGuard != nil {
		pdRaw, _ = json.Marshal(r.IPv6PDGuard)
	}
	pd, err := NormalizePDGuard(pdRaw, r.Type)
	if err != nil {
		return nil, err
	}
	if pd.Enabled || pdSet || r.IPv6PDGuard != nil {
		r.IPv6PDGuard = &pd
	}
	for _, check := range [][2]string{
		{"ipv6_pd_guard_state", "请先关闭 IPv6 前缀变化保护并发布撤回，再修改路由器类型或管理地址"},
		{"ipv6_fakeip_state", "请先关闭 IPv6 Fake-IP 并发布撤销，再修改路由器类型或管理地址"},
		{"ipv6_routes_state", "请先撤回 IPv6 规则集路由并发布，再修改路由器类型或管理地址"},
	} {
		if owns(check[0]) {
			return nil, invalid(check[1])
		}
	}
	if r.Name == "" {
		return nil, invalid("请填写名称")
	}
	if !slices.Contains(RouterTypes, r.Type) {
		return nil, invalid("路由器类型只支持 iKuai 4.x 或 RouterOS")
	}
	if !validEndpoint(r.Endpoint) {
		return nil, invalid("管理地址格式应为 http(s)://主机[:端口]")
	}
	if r.Username == "" {
		return nil, invalid("请填写用户名")
	}
	if r.Password == "" {
		return nil, invalid("请填写密码")
	}
	if r.ID == "" {
		r.ID = NewRouterID()
	}
	return r, nil
}

// validEndpoint: http(s)://host[:port] with an optional trailing slash or
// ;params.
func validEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.Opaque != "" {
		return false
	}
	path, _, _ := strings.Cut(u.Path, ";")
	return path == "" || path == "/"
}

// RedactRouters is the desensitized export of a profile's routers document:
// credentials, management endpoints and device state are removed.
func RedactRouters(document []byte) ([]byte, error) {
	var routers map[string]json.RawMessage
	if json.Unmarshal(document, &routers) != nil || routers == nil {
		return document, nil
	}
	var raw []json.RawMessage
	_ = json.Unmarshal(routers["items"], &raw)
	items := []map[string]json.RawMessage{}
	for _, entry := range raw {
		var item map[string]json.RawMessage
		if json.Unmarshal(entry, &item) != nil || item == nil {
			continue
		}
		for _, key := range []string{"password", "last_probe", "last_release", "ros_policy_state", "dns_guard_state",
			"ipv6_fakeip_state", "ipv6_routes_state", "ipv6_pd_guard_state"} {
			delete(item, key)
		}
		for _, key := range []string{"username", "endpoint"} {
			if truthy(item[key]) {
				item[key] = json.RawMessage(`"[REDACTED]"`)
			}
		}
		items = append(items, item)
	}
	routers["items"], _ = json.Marshal(items)
	return json.Marshal(routers)
}
