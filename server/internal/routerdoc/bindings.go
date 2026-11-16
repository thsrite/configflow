package routerdoc

import (
	"strings"
	"unicode/utf8"

	"configflow/internal/jsonx"
)

// BindingFields is client_fixed_ip.BINDING_FIELDS.
var BindingFields = []string{"router_id", "vendor", "id", "ip", "mac", "comment", "server", "interface", "gateway"}

var bindingRequired = []string{"router_id", "vendor", "id", "ip", "mac", "comment"}

// Messages of client_fixed_ip.validate_bindings (an OxidnsSettingsError).
const (
	BindingsShapeMessage   = "客户端固定 IP 记录必须为对象，最多 2048 条。"
	BindingsInvalidMessage = "客户端固定 IP 记录缺少有效 IP、MAC、路由器或归属标记。"
)

// ValidateBindings is client_fixed_ip.validate_bindings(value): "" when
// valid, else the message of the OxidnsSettingsError Python raises (the
// caller wraps it in its own error type).
func ValidateBindings(value any) string {
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil || object.Len() > 2048 {
		return BindingsShapeMessage
	}
	for ip, item := range object.All() {
		if !validBinding(ip, item) {
			return BindingsInvalidMessage
		}
	}
	return ""
}

func validBinding(ip string, item any) bool {
	binding, ok := item.(*jsonx.Object)
	if !ok || binding == nil {
		return false
	}
	for key := range binding.All() {
		known := false
		for _, field := range BindingFields {
			known = known || key == field
		}
		if !known {
			return false
		}
	}
	for _, key := range bindingRequired {
		if !binding.Has(key) {
			return false
		}
	}
	if IPv4(ip) == "" || !jsonx.Equal(binding.Value("ip"), ip) {
		return false
	}
	if vendor, _ := binding.Value("vendor").(string); vendor != "routeros" && vendor != "ikuai" {
		return false
	}
	for _, v := range binding.All() {
		text, ok := v.(string)
		if !ok || utf8.RuneCountInString(text) > 512 {
			return false
		}
		for _, r := range text {
			if r < 32 {
				return false
			}
		}
	}
	mac := binding.GetString("mac")
	return binding.GetString("router_id") != "" && binding.GetString("id") != "" &&
		NormalizeMAC(mac) != "" && NormalizeMAC(mac) == mac &&
		strings.HasPrefix(binding.GetString("comment"), "cf-client:")
}
