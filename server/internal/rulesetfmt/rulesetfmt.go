// Package rulesetfmt shares rule-set format inference between the matcher and
// the client converters without coupling either package to the other.
package rulesetfmt

import (
	"strings"
	"unicode"

	"configflow/internal/jsonx"
	"configflow/internal/weburl"
)

// URLTypeError reports a non-string URL in a stored rule-set record.
type URLTypeError struct{ Value any }

func (e *URLTypeError) Error() string { return "rule-set URL must be a string" }

// LibraryEntryTypeError reports a non-object encountered before a matching
// rule-library entry. Python's ruleset_format calls .get on each entry and
// stops as soon as one matches.
type LibraryEntryTypeError struct{ Value any }

func (e *LibraryEntryTypeError) Error() string { return "rule-library entry must be an object" }

// URLParseError retains the URL parse error while exposing a format inferred
// from the original text to callers that can continue without a parsed URL.
type URLParseError struct {
	Text string
	Err  error
}

func (e *URLParseError) Error() string { return e.Err.Error() }
func (e *URLParseError) Unwrap() error { return e.Err }

// HasYAMLPayload reports content that looks like a YAML rule set: a flow
// mapping, or a line starting with a (quoted) payload key.
func HasYAMLPayload(content string) bool {
	probe := strings.TrimPrefix(content, "\ufeff")
	firstContent := true
	for _, line := range strings.Split(probe, "\n") {
		rest := strings.TrimLeftFunc(line, unicode.IsSpace)
		if rest == "" || strings.HasPrefix(rest, "#") || strings.TrimSpace(rest) == "---" {
			continue
		}
		if firstContent {
			firstContent = false
			if strings.HasPrefix(rest, "{") {
				return true
			}
		}
		for _, key := range []string{`payload`, `"payload"`, `'payload'`} {
			if after, ok := strings.CutPrefix(rest, key); ok && strings.HasPrefix(strings.TrimLeftFunc(after, unicode.IsSpace), ":") {
				return true
			}
		}
	}
	return false
}

// Format determines the declared or inferred format of a rule-set reference.
// library is the rule_library list; sourceURL overrides the rule URL when the
// matched library entry has no URL. Malformed URL types and parse errors are
// returned so the converter can report invalid stored input.
func Format(rule *jsonx.Object, library []any, sourceURL string) (string, error) {
	return format(rule, library, sourceURL, false)
}

// FormatSkippingInvalidEntries is the tolerant form used by the matcher,
// whose previous implementation ignored malformed library entries.
func FormatSkippingInvalidEntries(rule *jsonx.Object, library []any, sourceURL string) string {
	format, _ := format(rule, library, sourceURL, true)
	return format
}

func format(rule *jsonx.Object, library []any, sourceURL string, skipInvalid bool) (string, error) {
	found := jsonx.NewObject()
	libraryID := rule.Value("library_rule_id")
	for _, entry := range library {
		item, ok := entry.(*jsonx.Object)
		if !ok {
			if !skipInvalid {
				return "", &LibraryEntryTypeError{Value: entry}
			}
			continue
		}
		match := jsonx.Truthy(libraryID) && jsonx.Equal(item.Value("id"), libraryID)
		if !jsonx.Truthy(libraryID) {
			match = jsonx.Equal(item.Value("name"), rule.Value("name"))
		}
		if match {
			found = item
			break
		}
	}
	for _, source := range []*jsonx.Object{found, rule} {
		if declared, ok := source.Value("format").(string); ok && oneOf(declared, "yaml", "text", "mrs") {
			return declared, nil
		}
	}
	source := found
	if found.Len() == 0 {
		source = rule
	}
	if source.Value("source_type") == "content" {
		if content, ok := source.Value("content").(string); ok && HasYAMLPayload(content) {
			return "yaml", nil
		}
		return "text", nil
	}
	var address any
	switch {
	case jsonx.Truthy(found.Value("url")):
		address = found.Value("url")
	case sourceURL != "":
		address = sourceURL
	case jsonx.Truthy(rule.Value("url")):
		address = rule.Value("url")
	default:
		address = ""
	}
	text, ok := address.(string)
	if !ok {
		return "text", &URLTypeError{Value: address}
	}
	path := text
	if parsed, err := weburl.Parse(text); err == nil {
		path = weburl.RawPath(parsed)
	} else {
		return inferPath(path), &URLParseError{Text: text, Err: err}
	}
	return inferPath(path), nil
}

func inferPath(path string) string {
	path = strings.ToLower(path)
	switch {
	case strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".yml"):
		return "yaml"
	case strings.HasSuffix(path, ".mrs"):
		return "mrs"
	default:
		return "text"
	}
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
