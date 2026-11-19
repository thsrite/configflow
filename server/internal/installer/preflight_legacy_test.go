package installer

import (
	"encoding/json"
	"strings"

	"configflow/internal/jsonx"
)

// ParsePreflightReport adapts the remaining request facade to EvaluatePreflight.
func ParsePreflightReport(output, networkMode any) (*jsonx.Object, error) {
	mode, _ := networkMode.(string)
	if mode != "host" && mode != "macvlan" {
		return nil, valueError(msgPreflightNetwork)
	}
	text, ok := output.(string)
	if !ok {
		return nil, valueError(msgPreflightOutputType)
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

func pyRstrip(text string) string                  { return trimScriptTail(text) }
func pyStrip(text string) string                   { return strings.TrimSpace(text) }
func pySplitLines(text string) []string            { return machineLines(text) }
func b64decodeValidate(text string) ([]byte, bool) { return decodeProbeBase64(text) }
