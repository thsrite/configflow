package rulefetch

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func legacyText(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return "\x00"
}
func legacyFormat(value any) string {
	if value == nil {
		return ""
	}
	return legacyText(value)
}
func legacyProblems(problems []Problem) []any {
	result := make([]any, len(problems))
	for i, problem := range problems {
		result[i] = legacyObject(problem)
	}
	return result
}

// MetadataError validates the untyped request boundary before typed inspection.
func MetadataError(behavior, format, source any) string {
	return metadataError(legacyText(behavior), legacyFormat(format), legacyText(source))
}

// ContentDigest adapts the archived content-type discriminator.
func ContentDigest(value any) string {
	content, err := legacyContent(value)
	if err != nil {
		return ""
	}
	prefix := "b"
	if content.Text {
		prefix = "s"
	}
	return prefix + dataDigest(content.Data)
}

// InspectDomainContent decodes the explicit editor request before inspection.
func InspectDomainContent(value, behavior, format any) (*jsonx.Object, error) {
	content, ok := value.(string)
	if !ok {
		return nil, validationError(msgContentText)
	}
	result, err := Inspect(content, legacyText(behavior), legacyFormat(format))
	if err != nil {
		return nil, err
	}
	return legacyObject(result), nil
}

// NormalizeClassicalLines adapts typed delivery diagnostics.
func NormalizeClassicalLines(content string) (string, []any, error) {
	result, problems, err := NormalizeClassical(content)
	return result, legacyProblems(problems), err
}

// NormalizeMihomoYAML adapts typed delivery diagnostics.
func NormalizeMihomoYAML(content string) (string, []any) {
	result, problems := NormalizeYAML(content)
	return result, legacyProblems(problems)
}

// NormalizeMihomoRuleset adapts the unported deploy caller.
func NormalizeMihomoRuleset(content string, behavior, format any) (string, []any, error) {
	result, problems, err := NormalizeRuleset(content, legacyText(behavior), legacyFormat(format))
	return result, legacyProblems(problems), err
}

// ValidateDomainContent adapts stored editor records.
func ValidateDomainContent(rule, previous *jsonx.Object) error {
	typed := legacyRule(rule)
	var before *model.RuleConfig
	if previous != nil && previous.Len() > 0 {
		value := legacyRule(previous)
		before = &value
	}
	return ValidateContent(typed, before)
}

// ValidateRemoteRuleContent adapts a complete text/binary value and a narrow
// rules library. The native validator owns format inference and the cache key.
func ValidateRemoteRuleContent(value any, rule, config *jsonx.Object, format string) error {
	content, err := legacyContent(value)
	typed := legacyRule(rule)
	if err != nil {
		return NewRemoteError(typed, format, []Problem{remoteProblem(0, "", msgContentBytes)})
	}
	var library []model.RuleLibraryItem
	if config != nil {
		_ = decodeLegacy(config.GetList("rule_library"), &library)
	}
	return ValidateRemote(content, typed, library, format)
}

// ReadRulePayloadAny adapts the unported connection API to a YAML Node handle.
func ReadRulePayloadAny(content string) ([]string, any, error) {
	lines, node, err := ReadRulePayload(content)
	if node == nil {
		return lines, nil, err
	}
	return lines, node, err
}

// AppendRulePayloadAny validates the legacy opaque handle before appending.
func AppendRulePayloadAny(content, line string, payload any) (string, error) {
	node, ok := payload.(*yamlx.Node)
	if payload != nil && !ok {
		return content, validationError(msgUnreadablePayload)
	}
	return AppendRulePayload(content, line, node)
}
