package routers

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/model"
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

// RouterSettings are the stored settings the UI also sees.
type RouterSettings struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Type                string    `json:"type"`
	Endpoint            string    `json:"endpoint"`
	Username            string    `json:"username"`
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
}

// Router is one stored router (profile routers.items[]). Every other stored
// field (last_probe, last_release, ros_policy_state, dns_guard_state,
// ipv6_*_state, unknown ones) is kept in Extra and written back unchanged.
type Router struct {
	source   json.RawMessage // original stored member presence for publication CAS
	stored   *model.Router
	baseline json.RawMessage
	explicit map[string]bool
	RouterSettings
	Password string                     `json:"password"`
	Extra    map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON reads a stored record; absent settings take the defaults
// the Python reader used (verify_tls on, 60-minute interval). Like the
// repository's R5 rule, a member of the wrong type reads as absent (its
// default) instead of failing the record; the repository already logs
// 「配置字段类型不匹配，已按缺省值解码」 for it on load. The stored bytes stay in
// source, so an unchanged record is written back as it was.
func (r *Router) UnmarshalJSON(data []byte) error {
	var stored model.Router
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}
	*r = routerFromModel(stored)
	r.source = bytes.Clone(data)

	return nil
}

// MarshalJSON writes the known fields and Extra.
func (r Router) MarshalJSON() ([]byte, error) {
	if r.stored != nil && len(r.source) > 0 {
		stored := *r.stored
		var before, after map[string]json.RawMessage
		_ = json.Unmarshal(r.baseline, &before)
		encoded, err := json.Marshal(struct {
			RouterSettings
			Password string `json:"password"`
		}{r.RouterSettings, r.Password})
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal(encoded, &after)
		for key, value := range after {
			if r.explicit[key] || !jsonEqual(value, before[key]) {
				if err = stored.SetJSON(key, value); err != nil {
					return nil, err
				}
			}
		}
		var original map[string]json.RawMessage
		_ = json.Unmarshal(r.source, &original)
		for key := range original {
			if key == "id" || key == "password" || slices.Contains(EditableFields, key) {
				continue
			}
			if _, kept := r.Extra[key]; kept {
				continue
			}
			if stored.Fields.Has(key) {
				stored.Fields.Delete(key)
			} else if err = stored.Clear(key); err != nil {
				return nil, err
			}
		}
		for key, value := range r.Extra {
			if !jsonEqual(value, original[key]) {
				if err = stored.SetJSON(key, value); err != nil {
					return nil, err
				}
			}
		}
		return model.Marshal(&stored)
	}
	type plain Router
	known, err := json.Marshal(plain(r))
	if err != nil || len(r.Extra) == 0 {
		return known, err
	}
	extra, err := json.Marshal(r.Extra)
	return append(append(known[:len(known)-1], ','), extra[1:]...), err
}

// State is a stored device-state field (Extra), nil when absent or falsy.
func (r *Router) State(key string) json.RawMessage {
	if v := r.Extra[key]; hasStoredState(v) {
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
	RouterSettings
	LastProbe   json.RawMessage `json:"last_probe"`
	LastRelease json.RawMessage `json:"last_release"`
	HasPassword bool            `json:"has_password"`
}

// View is the router as returned to the UI; absent guards show their defaults.
func (r *Router) View() RouterView {
	v := RouterView{RouterSettings: r.RouterSettings, LastProbe: r.Extra["last_probe"],
		LastRelease: r.Extra["last_release"], HasPassword: r.Password != ""}
	v.IPRuleSets, v.RosPolicyRuleSets = r.StaticRuleSets(), append([]string{}, r.RosPolicyRuleSets...)
	if v.DNSGuard == nil {
		guard := DefaultDNSGuard()
		v.DNSGuard = &guard
	}
	if v.IPv6PDGuard == nil {
		v.IPv6PDGuard = &PDGuard{}
	}
	return v
}

// hasStoredState keeps opaque nonempty ownership evidence fail-closed, including
// malformed records. Legacy empty/null/false/zero receipts carry no ownership.
func hasStoredState(raw json.RawMessage) bool {
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		return false
	}
	switch s := compact.String(); s {
	case "", "null", "false", `""`, "[]", "{}":
		return false
	default:
		f, err := strconv.ParseFloat(s, 64)
		return err != nil || f != 0
	}
}

// storedText uses the shared lossless text codec for display text.
func storedText(raw json.RawMessage) string {
	var text model.String
	_ = json.Unmarshal(raw, &text)
	return string(text)
}

// requestString is strict validation of a user identity, never a coercion.
func requestString(raw json.RawMessage) (string, bool) {
	var text *string
	if json.Unmarshal(raw, &text) != nil || text == nil {
		return "", false
	}
	return *text, true
}

// idList decodes a list of non-empty rule-set ids, de-duplicated (falsy means none).
func idList(raw json.RawMessage) ([]string, bool) {
	var items []*string
	if len(raw) > 0 && json.Unmarshal(raw, &items) != nil || slices.Contains(items, nil) {
		return nil, false
	}
	out := []string{}
	for _, item := range items {
		if *item == "" {
			return nil, false
		} else if !slices.Contains(out, *item) {
			out = append(out, *item)
		}
	}
	return out, true
}

// boolField is fields[key] when it is a JSON bool, fallback when absent.
func boolField(fields map[string]json.RawMessage, key string, fallback bool) (bool, bool) {
	v, present := fields[key]
	if !present {
		return fallback, true
	}
	return string(v) == "true", string(v) == "true" || string(v) == "false"
}

// NormalizeDNSGuard validates the forced-DNS setting; raw nil or null means never enabled.
func NormalizeDNSGuard(raw json.RawMessage, routerType, dnsAgentID string) (DNSGuard, error) {
	guard := DefaultDNSGuard()
	if raw == nil || string(raw) == "null" {
		return guard, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return guard, invalid("强制 DNS 设置格式无效")
	}
	for key := range fields {
		if !slices.Contains([]string{"enabled", "block_dot", "failover", "bypass"}, key) {
			return guard, invalid("强制 DNS 设置格式无效")
		}
	}
	for key, dst := range map[string]*bool{"enabled": &guard.Enabled, "block_dot": &guard.BlockDoT, "failover": &guard.Failover} {
		var ok bool
		if *dst, ok = boolField(fields, key, *dst); !ok {
			return guard, invalid("强制 DNS 的开关必须为布尔值")
		}
	}
	var items []*string
	if v, ok := fields["bypass"]; ok && (json.Unmarshal(v, &items) != nil || items == nil || slices.Contains(items, nil)) {
		return guard, invalid("放行设备必须是 IPv4 地址或网段列表")
	}
	for _, item := range items {
		network, ok := netipx.ParseNetwork4(strings.TrimSpace(*item), false)
		if !ok {
			return guard, invalid("放行设备「" + *item + "」不是有效的 IPv4 地址或网段")
		}
		if text := network.String(); !slices.Contains(guard.Bypass, text) {
			guard.Bypass = append(guard.Bypass, text)
		}
	}
	switch {
	case len(items) > 64:
		return guard, invalid("放行设备最多 64 项")
	case guard.Enabled && routerType != "routeros":
		return guard, invalid("强制局域网 DNS 走 OxiDNS 仅支持 RouterOS")
	case guard.Enabled && dnsAgentID == "":
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
	if ok {
		guard.Enabled, ok = boolField(fields, "enabled", false)
	}
	if !ok || len(fields) > 1 || len(fields) == 1 && fields["enabled"] == nil {
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
// returns the record to store, with Python's checks in Python's order. An
// empty password keeps the stored one. Errors are *ValidationError.
func Normalize(body []byte, existing *Router) (*Router, error) {
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil || payload == nil {
		return nil, invalid("路由器参数必须是对象")
	}
	// m is the merged record: the stored settings (ip_rule_sets without the
	// policy selections), then the edited fields of the payload.
	r, m := &Router{Extra: map[string]json.RawMessage{}}, map[string]json.RawMessage{}
	if existing != nil {
		*r = *existing
		data, _ := json.Marshal(existing.RouterSettings)
		_ = json.Unmarshal(data, &m)
		m["ip_rule_sets"], _ = json.Marshal(existing.StaticRuleSets())
	}
	r.explicit = map[string]bool{}
	for _, key := range EditableFields {
		if v, ok := payload[key]; ok {
			r.explicit[key] = true
			m[key] = v
		}
	}
	for _, key := range []string{"type", "endpoint", "username", "password", "proxy_gateway_agent_id", "dns_agent_id"} {
		if raw, present := payload[key]; present {
			if _, valid := requestString(raw); !valid {
				return nil, invalid(messageRouterFieldType)
			}
		}
	}
	if s, ok := requestString(payload["password"]); ok && s != "" {
		r.Password = s
		r.explicit["password"] = true
	}
	r.Name, r.Type, r.Username = strings.TrimSpace(storedText(m["name"])), storedText(m["type"]), strings.TrimSpace(storedText(m["username"]))
	r.Endpoint = strings.TrimRight(strings.TrimSpace(storedText(m["endpoint"])), "/")
	r.VerifyTLS = true
	if raw, present := m["verify_tls"]; present {
		var ok bool
		r.VerifyTLS, ok = boolField(m, "verify_tls", true)
		if !ok {
			return nil, invalid(messageRouterFieldType)
		}
		_ = raw
	}
	r.ProxyGatewayAgentID, r.DNSAgentID = storedText(m["proxy_gateway_agent_id"]), storedText(m["dns_agent_id"])
	manageOK, autoOK, idsOK, policyOK := false, false, false, false
	r.ManageReturnRouting, manageOK = boolField(m, "manage_return_routing", false)
	r.AutoRuleSync, autoOK = boolField(m, "auto_rule_sync", false)
	r.RuleSyncInterval = 60
	if v, present := m["rule_sync_interval"]; present {
		var interval model.Int
		if json.Unmarshal(v, &interval) == nil {
			r.RuleSyncInterval = int(interval)
		} else {
			r.RuleSyncInterval = 0
		}
	}
	r.IPRuleSets, idsOK = idList(m["ip_rule_sets"])
	r.RosPolicyRuleSets, policyOK = idList(m["ros_policy_rule_sets"])
	guardRaw, guardSet := m["dns_guard"]
	guard, guardErr := NormalizeDNSGuard(guardRaw, r.Type, r.DNSAgentID)
	pdRaw, pdSet := m["ipv6_pd_guard"]
	pd, pdErr := NormalizePDGuard(pdRaw, r.Type)
	// Objects a feature still owns on the device pin the type and endpoint.
	moved := existing != nil && (r.Type != existing.Type || r.Endpoint != existing.Endpoint)
	owned := func(state string) bool { return moved && existing.State(state) != nil }
	for _, check := range []struct {
		failed  bool
		message string
	}{
		{!manageOK, "自动配置回流必须为布尔值"},
		{!autoOK || r.AutoRuleSync && r.Type != "routeros", "远程规则自动同步仅支持 RouterOS，开关必须为布尔值"},
		{!slices.Contains(RuleSyncIntervals, r.RuleSyncInterval), "请选择有效的规则检查间隔"},
		{!idsOK, "IP 路由规则集必须是规则集 ID 列表"},
		{!policyOK, "策略路由规则集必须是规则集 ID 列表"},
		{len(r.RosPolicyRuleSets) > 0 && r.Type != "routeros", "按来源策略路由仅支持 RouterOS"},
		{slices.ContainsFunc(r.RosPolicyRuleSets, func(id string) bool { return slices.Contains(r.IPRuleSets, id) }),
			"同一规则集只能选择静态路由或策略路由中的一种方式"},
		{owned("ros_policy_state"), "请先清空策略路由选择并发布撤销，再修改路由器类型或管理地址"},
		{guardErr != nil, fmt.Sprint(guardErr)},
		{owned("dns_guard_state"), "请先关闭强制 DNS 并发布撤销，再修改路由器类型或管理地址"},
		{pdErr != nil, fmt.Sprint(pdErr)},
		{owned("ipv6_pd_guard_state"), "请先关闭 IPv6 前缀变化保护并发布撤回，再修改路由器类型或管理地址"},
		{owned("ipv6_fakeip_state"), "请先关闭 IPv6 Fake-IP 并发布撤销，再修改路由器类型或管理地址"},
		{owned("ipv6_routes_state"), "请先撤回 IPv6 规则集路由并发布，再修改路由器类型或管理地址"},
		{r.Name == "", "请填写名称"},
		{!slices.Contains(RouterTypes, r.Type), "路由器类型只支持 iKuai 4.x 或 RouterOS"},
		{!validEndpoint(r.Endpoint), "管理地址格式应为 http(s)://主机[:端口]"},
		{r.Username == "", "请填写用户名"},
		{r.Password == "", "请填写密码"},
	} {
		if check.failed {
			return nil, invalid(check.message)
		}
	}
	r.DNSGuard, r.IPv6PDGuard = nil, nil
	if guard.Enabled || guardSet {
		r.DNSGuard = &guard
	}
	if pd.Enabled || pdSet {
		r.IPv6PDGuard = &pd
	}
	r.ID = cmp.Or(r.ID, NewRouterID())
	return r, nil
}

// validEndpoint: http(s)://host[:port] with an optional trailing slash or ;params.
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
	var raw []json.RawMessage
	if json.Unmarshal(document, &routers) != nil || routers == nil {
		return document, nil
	}
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
			if hasStoredState(item[key]) {
				item[key] = json.RawMessage(`"[REDACTED]"`)
			}
		}
		items = append(items, item)
	}
	routers["items"], _ = json.Marshal(items)
	return json.Marshal(routers)
}
