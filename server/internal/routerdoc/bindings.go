package routerdoc

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"
)

// Messages of an invalid profile client_fixed_ips document.
const (
	BindingsShapeMessage   = "客户端固定 IP 记录必须为对象，最多 2048 条。"
	BindingsInvalidMessage = "客户端固定 IP 记录缺少有效 IP、MAC、路由器或归属标记。"
)

var bindingFields = []string{"router_id", "vendor", "id", "ip", "mac", "comment", "server", "interface", "gateway"}

// ValidateBindings checks a stored client_fixed_ips document (JSON object
// IPv4 -> binding): "" when valid, else the message for the user.
func ValidateBindings(document []byte) string {
	var bindings map[string]json.RawMessage
	if json.Unmarshal(document, &bindings) != nil || bindings == nil || len(bindings) > 2048 {
		return BindingsShapeMessage
	}
	for ip, raw := range bindings {
		var b map[string]*string // every value a string, at most 512 characters, no control characters
		if json.Unmarshal(raw, &b) != nil || b == nil || IPv4(ip) == "" {
			return BindingsInvalidMessage
		}
		for key, v := range b {
			if !slices.Contains(bindingFields, key) || v == nil || utf8.RuneCountInString(*v) > 512 ||
				strings.ContainsFunc(*v, func(r rune) bool { return r < 32 }) {
				return BindingsInvalidMessage
			}
		}
		for _, key := range bindingFields[:6] { // required
			if b[key] == nil {
				return BindingsInvalidMessage
			}
		}
		if mac := *b["mac"]; *b["ip"] != ip || *b["vendor"] != "routeros" && *b["vendor"] != "ikuai" || *b["router_id"] == "" ||
			*b["id"] == "" || NormalizeMAC(mac) == "" || NormalizeMAC(mac) != mac || !strings.HasPrefix(*b["comment"], "cf-client:") {
			return BindingsInvalidMessage
		}
	}
	return ""
}
