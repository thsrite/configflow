package rulefetch

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
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

// NormalizeMihomoRuleset adapts the unported deploy caller.
func NormalizeMihomoRuleset(content string, behavior, format any) (string, []any, error) {
	result, problems, err := NormalizeRuleset(content, legacyText(behavior), legacyFormat(format))
	return result, legacyProblems(problems), err
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
