package routerdoc

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Messages of an invalid profile client_fixed_ips document.
const (
	BindingsShapeMessage   = "客户端固定 IP 记录必须为对象，最多 2048 条。"
	BindingsInvalidMessage = "客户端固定 IP 记录缺少有效 IP、MAC、路由器或归属标记。"
)

var bindingFields = map[string]bool{"router_id": true, "vendor": true, "id": true, "ip": true, "mac": true,
	"comment": true, "server": true, "interface": true, "gateway": true}

// ValidateBindings checks a stored client_fixed_ips document (JSON object
// IPv4 -> binding): "" when valid, else the message for the user.
func ValidateBindings(document []byte) string {
	var bindings map[string]json.RawMessage
	if json.Unmarshal(document, &bindings) != nil || bindings == nil || len(bindings) > 2048 {
		return BindingsShapeMessage
	}
	for ip, raw := range bindings {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil || IPv4(ip) == "" {
			return BindingsInvalidMessage
		}
		values := map[string]string{}
		for key, v := range fields {
			var s string
			if !bindingFields[key] || len(v) == 0 || v[0] != '"' || json.Unmarshal(v, &s) != nil || utf8.RuneCountInString(s) > 512 ||
				strings.ContainsFunc(s, func(r rune) bool { return r < 32 }) {
				return BindingsInvalidMessage
			}
			values[key] = s
		}
		for _, key := range []string{"router_id", "vendor", "id", "ip", "mac", "comment"} {
			if _, ok := values[key]; !ok {
				return BindingsInvalidMessage
			}
		}
		mac := values["mac"]
		if values["ip"] != ip || (values["vendor"] != "routeros" && values["vendor"] != "ikuai") ||
			values["router_id"] == "" || values["id"] == "" || NormalizeMAC(mac) == "" || NormalizeMAC(mac) != mac ||
			!strings.HasPrefix(values["comment"], "cf-client:") {
			return BindingsInvalidMessage
		}
	}
	return ""
}
