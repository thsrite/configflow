package installer

import (
	"errors"
	"strings"
)

// o17TextOnly reports a guide request refused because of the O17 ruling:
// vip, ha_gateway and container_name must be JSON strings (Python read
// booleans and integers as IPv4 addresses and treated null, false, 0, [] and
// {} as "not filled").
func o17TextOnly(request map[string]any, err error) bool {
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		return false
	}
	nonText := func(key string) bool {
		value, present := request[key]
		_, text := value.(string)
		return present && !text
	}
	switch invalid.Message {
	case msgVirtualIPv4Text:
		return request["action"] == "verify" && nonText("vip")
	case msgOriginalContainerName:
		return request["action"] == "inspect" && nonText("container_name")
	case msgHAGatewayText:
		return nonText("ha_gateway")
	}
	return false
}

// o17RareLineBreak reports text with a separator only Python's
// str.splitlines broke lines at; since O17 scripts break at \n, \r\n, \r.
func o17RareLineBreak(values map[string]any) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.ContainsAny(text, "\v\f\x1c\x1d\x1e\u0085  ") {
			return true
		}
	}
	return false
}
