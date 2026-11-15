package rules

// Exported line helpers of backend/utils/ruleset_content.py. The port of
// that module is internal/rulefetch, which imports rules; the matcher here
// needs the same helpers, so they live in this package (compat.go) and
// rulefetch and convert call these.

// ValidPattern is ruleset_content._valid_pattern.
func ValidPattern(value string) bool { return validPattern(value) }

// ClassicalLine is ruleset_content._classical_line: the explicit Classical
// rule for a bare domain/IP line, the line itself for comments, blank and
// typed lines, or ErrClassicalLine.
func ClassicalLine(value string) (string, error) { return classicalLine(value) }

// ClassicalLineError is ruleset_content.classical_line_error ("" for None).
func ClassicalLineError(value string) string { return classicalLineError(value) }

// HasYAMLPayload is ruleset_content._has_yaml_payload.
func HasYAMLPayload(content string) bool { return hasYAMLPayload(content) }

// PyUpper is Python's str.upper().
func PyUpper(s string) string { return pyUpper(s) }

// PyStrip is Python's str.strip() (Unicode whitespace).
func PyStrip(s string) string { return pyStrip(s) }

// PyIsSpace is Python's str.isspace() for one character.
func PyIsSpace(r rune) bool { return pyIsSpace(r) }
