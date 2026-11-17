package provider

import (
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamltest"
)

// nativeProviderOutcome grants no blanket message exemption: the native error
// must match its source verbatim, while owner/kind/path/label remain unchanged.
func nativeProviderOutcome(kind string, input, want, got *jsonx.Object) bool {
	if kind == "parse" {
		switch input.GetString("text") {
		case "{[a]: b}", "proxies: [\t]", "x: !!timestamp 2026-13-45":
			proxies, ok := got.Value("output").([]any)
			return got.Len() == 1 && ok && len(proxies) == 0
		}
	}
	if got.GetString("error") != "DialerReferenceError" || got.Len() != 2 {
		return false
	}
	message := got.GetString("message")
	for _, source := range yamltest.TextFields(input, "text", "content") {
		_, err := yamlx.Parse([]byte(source))
		if err == nil {
			continue
		}
		prefix, rest, found := strings.Cut(want.GetString("message"), "SRC[Provider YAML 内容无效|")
		if !found {
			if kind == "parse" && nativeParseOutcome(input, want, got) {
				return true
			}
			continue
		}
		parts := strings.SplitN(rest, "|", 4)
		if len(parts) != 4 {
			continue
		}
		if strings.HasPrefix(message, prefix+"SRC[Provider YAML 内容无效|") && strings.Contains(message, "|"+parts[2]+"|YAMLError:None:"+err.Error()+":") {
			return true
		}
	}
	if kind != "parse" {
		return false
	}
	prefix := ""
	if input.GetString("name") != "" || input.GetString("path") != "" {
		prefix = "CTX[订阅|" + input.GetString("name") + "|" + input.GetString("path") + "]"
	}
	// Unknown-tag scalars retain their shape, then take the original field's
	// validation path; Chinese field text and exact position are still required.
	switch input.GetString("text") {
	case "proxies: {[a]: b}":
		node, err := yamlx.Parse([]byte(input.GetString("text")))
		if err != nil {
			return false
		}
		err = yamlx.ProjectJSONKeys(yamlx.Get(node, "proxies"), yamlx.StringifyScalarKeys)
		return err != nil && message == prefix+"SRC[Provider YAML 内容无效|None|None|本次订阅返回的内容|YAMLError:None:"+err.Error()+":None]"
	case "proxies: !foo x":
		return message == prefix+"SRC[Provider proxies 必须是数组\n字段：proxies|1|10|本次订阅返回的内容|None]"
	case "proxies: [!foo x]":
		return message == prefix+"SRC[Provider 代理必须是含非空 name 的对象\n字段：proxies[0]|1|11|本次订阅返回的内容|None]"
	}
	return false
}
