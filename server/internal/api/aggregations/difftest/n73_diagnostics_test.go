package difftest

import (
	"errors"
	"fmt"

	"configflow/internal/yamlx"
)

const nativeProviderInvalidYAML = "proxies: ["

// nativeProviderSyntaxBody replaces only the five approved diagnostic fields.
// Both owners, the source label, source text, Python reference diagnosis and
// native facade diagnosis remain exact; status/other fields/storage stay strict.
func nativeProviderSyntaxBody(method, path, source string, python answer, nativeStatus int) (any, error) {
	if source != nativeProviderInvalidYAML || python.Status != 400 || nativeStatus != 400 {
		return nil, fmt.Errorf("N7-3c provider diagnostic requires the approved source and two 400 responses")
	}
	var prefix string
	switch {
	case method == "GENERATE" && path == "a":
		prefix = "订阅「First」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n"
	case method == "GET" && (path == "/api/aggregations/a/preview" || path == "/api/profiles/default/aggregations/a/provider?token=c31-pull"):
		prefix = "订阅聚合「Aggregate」准备失败：订阅「Feed」准备失败：Provider YAML 内容无效\n文件：./providers/Aggregate.yaml\n内容来源：本次订阅返回的内容\n"
	default:
		return nil, fmt.Errorf("N7-3c provider diagnostic outside the five approved comparisons")
	}
	body, ok := jsonValue(python.Body).(map[string]any)
	if !ok || body["success"] != false || body["message"] != prefix+"位置：第 1 行，第 11 列\n出错内容："+source+"\n原始诊断：while parsing a flow node: expected the node content, but found '<stream end>'" {
		return nil, fmt.Errorf("N7-3c Python provider owner/source/diagnostic changed")
	}
	_, parseErr := yamlx.Parse([]byte(source))
	var syntax *yamlx.SyntaxError
	if !errors.Is(parseErr, yamlx.ErrSyntax) || !errors.As(parseErr, &syntax) || syntax.Line != 1 {
		return nil, fmt.Errorf("N7-3c native provider syntax diagnostic missing: %v", parseErr)
	}
	// Error() contains yamlx's shared Chinese syntax-prefix constant; do not
	// duplicate its text or reduce this check to a substring/prefix exemption.
	body["message"] = prefix + fmt.Sprintf("位置：第 %d 行，第 %d 列\n出错内容：%s\n原始诊断：%s", syntax.Line, max(1, syntax.Column), source, parseErr.Error())
	return body, nil
}
