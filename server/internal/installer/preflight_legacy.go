package installer

import (
	"configflow/internal/jsonx"
	"encoding/json"
)

// ParsePreflightReport adapts the remaining request facade to EvaluatePreflight.
func ParsePreflightReport(output, networkMode any) (*jsonx.Object, error) {
	mode, _ := networkMode.(string)
	if mode != "host" && mode != "macvlan" {
		return nil, valueError("network_mode 须为 host 或 macvlan")
	}
	text, ok := output.(string)
	if !ok {
		return nil, valueError("output 须为检查命令的文本输出")
	}
	report, err := EvaluatePreflight(text, mode)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	return value.(*jsonx.Object), nil
}
