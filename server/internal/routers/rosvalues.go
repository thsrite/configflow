package routers

import (
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pyip"
)

// ComparableValues is ros_values.comparable_values(path, row): equivalent
// RouterOS wire representations, scoped to known resource fields (a shallow
// copy; the row is not changed).
func ComparableValues(path string, row *jsonx.Object) *jsonx.Object {
	result := row.Clone()
	if result == nil {
		result = jsonx.NewObject()
	}
	field := map[string]string{"/routing/table": "fib", "/ip/firewall/mangle": "passthrough"}[path]
	if field != "" {
		if value, ok := result.Get(field); ok {
			text, isText := value.(string)
			if isTrue(value) || (isText && (text == "true" || text == "yes")) || (field == "fib" && isText && text == "") {
				result.Set(field, "true")
			} else if isFalse(value) || (isText && (text == "false" || text == "no")) {
				result.Set(field, "false")
			}
		}
	}
	if path == "/ip/route" {
		if gateway, ok := result.Value("gateway").(string); ok {
			gateway = strings.TrimSuffix(gateway, "@main")
			if address, ok := pyip.ParseIPv4Address(gateway); ok {
				result.Set("gateway", address.String())
			}
		}
	}
	return result
}
