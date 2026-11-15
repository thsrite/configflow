package convert

import "configflow/internal/rules"

// hasYAMLPayload is ruleset_content._has_yaml_payload (ported in
// rules.HasYAMLPayload next to the matcher that also needs it).
func hasYAMLPayload(content string) bool { return rules.HasYAMLPayload(content) }
