package difftest

import (
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"unicode"

	"configflow/internal/httpx"
)

// approvedInstallerIntegerRefusal covers only the approved native machine-number
// boundary. The original live Python response is retained and checked here;
// frozen renderer rows have separate complete archive/row SHA bindings.
func approvedInstallerIntegerRefusal(r installerRequest, python, native response) bool {
	if r.method != "GET" || native.status != 400 {
		return false
	}
	target, err := url.Parse(r.path)
	if err != nil {
		return false
	}
	query := url.Values{}
	for _, pair := range httpx.ParseQuery(target.RawQuery) {
		if _, present := query[pair[0]]; !present {
			query[pair[0]] = []string{pair[1]}
		}
	}
	message := ""
	nonASCII := false
	keys := []string{"port", "deployment_health_timeout", "mihomo_agent_port", "oxidns_agent_port", "mihomo_port", "mosdns_port", "oxidns_port"}
	for _, key := range keys {
		value := query.Get(key)
		if strings.ContainsFunc(value, func(r rune) bool { return r > 127 && unicode.IsDigit(r) }) {
			message = "Invalid installer integer"
			nonASCII = true
			break
		}
		if len(value) < 4301 && key != "deployment_health_timeout" {
			continue
		}
		text := strings.ReplaceAll(strings.TrimSpace(value), "_", "")
		number, ok := new(big.Int).SetString(text, 10)
		if !ok {
			if len(value) > 4300 {
				message = "Invalid installer integer"
			}
			continue
		}
		if key == "deployment_health_timeout" {
			if new(big.Int).Abs(number).Cmp(big.NewInt(9223372036)) > 0 {
				message = "Invalid deployment health timeout"
			}
		} else if number.Sign() < 0 || number.Cmp(big.NewInt(65535)) > 0 {
			message = "Invalid Agent port"
		}
	}

	if message == "" && target.Path == "/api/agents/install-script" && query.Get("install_core") == "true" {
		raw, present := query["port"]
		if present {
			old, err := parsePythonJSON(python.body)
			if object, ok := old.(*orderedObject); err == nil && ok && python.status == 400 && object.GetString("message") == fmt.Sprintf("invalid literal for int() with base 10: %s", pyStringRepr(raw[0])) {
				message = "Invalid installer integer"
			}
		}
	}
	if message == "" {
		return false
	}
	value, err := parsePythonJSON(native.body)
	if err != nil || !jsonEqual(value, oObj("success", false, "message", message)) {
		return false
	}
	if python.status == 200 {
		return true
	}
	old, err := parsePythonJSON(python.body)
	if err != nil {
		return false
	}
	object, ok := old.(*orderedObject)
	return ok && python.status == 400 && !object.GetBool("success") && object.GetString("message") != "" && (nonASCII || len(query.Get("port")) > 4300 || len(query.Get("deployment_health_timeout")) > 4300 || strings.HasPrefix(object.GetString("message"), "invalid literal for int() with base 10: ") || strings.HasPrefix(object.GetString("message"), "Exceeds the limit (4300 digits) for integer string conversion: value has "))
}
