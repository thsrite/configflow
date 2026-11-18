// Package rulesetfmt shares rule-set format inference between the matcher and
// the client converters without coupling either package to the other.
package rulesetfmt

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"configflow/internal/model"
	"configflow/internal/weburl"
)

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

// LibraryIndex returns the first matching declaration or -1. It validates
// reference metadata only as far as the first match, including an empty record.
func LibraryIndex(rule model.RuleConfig, library []model.RuleLibraryItem) (int, error) {
	if len(library) > 0 && model.IssueUnder(model.Issues(rule), "library_rule_id") {
		return -1, ErrReferenceIDType
	}
	for i := range library {
		item := &library[i]
		if rule.LibraryRuleID != "" {
			if model.IssueUnder(model.Issues(item), "id") {
				return -1, ErrReferenceIDType
			}
		} else if model.IssueUnder(model.Issues(item), "name") || model.IssueUnder(model.Issues(rule), "name") {
			return -1, ErrReferenceNameType
		}
		if rule.LibraryRuleID != "" && item.ID == rule.LibraryRuleID || rule.LibraryRuleID == "" && item.Name == rule.Name {
			return i, nil
		}
	}
	return -1, nil
}

// RuleFormat determines format using typed stored records. Malformed members
// either follow model R5 or produce a typed validation error when inspected.
func RuleFormat(rule model.RuleConfig, library []model.RuleLibraryItem, sourceURL string) (string, error) {
	index, err := LibraryIndex(rule, library)
	if err != nil {
		return "", err
	}
	var found *model.RuleLibraryItem
	if index >= 0 {
		data, err := model.Marshal(library[index])
		if err != nil {
			return "", err
		}
		var members map[string]json.RawMessage
		if err := json.Unmarshal(data, &members); err != nil {
			return "", err
		}
		if len(members) != 0 {
			found = &library[index]
		}
	}
	if found != nil && validFormat(text(found.Format)) {
		return text(found.Format), nil
	}
	if validFormat(text(rule.Format)) {
		return text(rule.Format), nil
	}
	sourceType, content := text(rule.SourceType), text(rule.Content)
	if found != nil {
		sourceType, content = text(found.SourceType), text(found.Content)
	}
	if sourceType == "content" {
		if HasYAMLPayload(content) {
			return "yaml", nil
		}
		return "text", nil
	}
	address := ""
	if found != nil {
		if model.IssueUnder(model.Issues(found), "url") {
			data, err := model.Marshal(found)
			if err != nil {
				return "", err
			}
			return "text", invalidURL(data)
		}
		address = text(found.URL)
	}
	if address == "" {
		address = sourceURL
	}
	if address == "" {
		if model.IssueUnder(model.Issues(rule), "url") {
			data, err := model.Marshal(rule)
			if err != nil {
				return "", err
			}
			return "text", invalidURL(data)
		}
		address = text(rule.URL)
	}
	if parsed, err := weburl.Parse(address); err == nil {
		return inferPath(weburl.RawPath(parsed)), nil
	} else {
		return inferPath(address), &URLParseError{Text: address, Err: err}
	}
}

func text(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func validFormat(value string) bool { return value == "yaml" || value == "text" || value == "mrs" }

// ErrURLType identifies a non-string URL encountered during format inference.
var ErrURLType = errors.New(msgURLType)

// ErrReferenceIDType identifies invalid stored rule-library reference IDs.
var ErrReferenceIDType = errors.New(msgReferenceIDType)

// ErrReferenceNameType identifies invalid stored rule-library names.
var ErrReferenceNameType = errors.New(msgReferenceNameType)

// URLValueError preserves opaque invalid-field evidence for a legacy boundary.
type URLValueError struct{ Raw json.RawMessage }

func (e *URLValueError) Error() string { return msgURLType }
func (e *URLValueError) Unwrap() error { return ErrURLType }
func invalidURL(data []byte) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return err
	}
	return &URLValueError{Raw: members["url"]}
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
