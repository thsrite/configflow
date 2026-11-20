package installer

import (
	"encoding/json"
	"strings"
)

// ParsePreflightReport adapts test inputs to EvaluatePreflight.
func ParsePreflightReport(output, networkMode any) (map[string]any, error) {
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
	return parseJSONMap(raw)
}

func pyRstrip(text string) string                  { return trimScriptTail(text) }
func pyStrip(text string) string                   { return strings.TrimSpace(text) }
func pySplitLines(text string) []string            { return machineLines(text) }
func b64decodeValidate(text string) ([]byte, bool) { return decodeProbeBase64(text) }
