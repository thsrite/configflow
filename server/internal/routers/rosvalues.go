package routers

import (
	"maps"
	"strings"

	"configflow/internal/netipx"
)

// ComparableValues returns a copy of a RouterOS row (decoded REST JSON)
// with equivalent wire forms made equal: fib/passthrough flags as
// "true"/"false", and an /ip/route gateway "1.2.3.4@main" as "1.2.3.4".
// Unknown fields, interface gateways and other tables stay as they are.
func ComparableValues(path string, row map[string]any) map[string]any {
	result := maps.Clone(row)
	if result == nil {
		result = map[string]any{}
	}
	field := map[string]string{"/routing/table": "fib", "/ip/firewall/mangle": "passthrough"}[path]
	if value, ok := result[field]; ok && field != "" {
		switch value {
		case true, "true", "yes":
			result[field] = "true"
		case false, "false", "no":
			result[field] = "false"
		case "":
			if field == "fib" { // fib is a presence flag
				result[field] = "true"
			}
		}
	}
	if gateway, ok := result["gateway"].(string); ok && path == "/ip/route" {
		if a, ok := netipx.ParseAddr4(strings.TrimSuffix(gateway, "@main")); ok {
			result["gateway"] = a.String()
		}
	}
	return result
}
