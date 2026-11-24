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

// LibraryIndex returns the first matching declaration or -1. Rules and
// library items are plain records (O18 T1c): identities of the wrong type
// were read as empty text and match nothing.
func LibraryIndex(rule model.RuleConfig, library []model.RuleLibraryItem) (int, error) {
	for i := range library {
		item := &library[i]
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
		// A matched non-empty id or name is a stored member, so the record
		// is not empty; only a match on empty names needs the encoding.
		item := &library[index]
		if item.ID != "" || item.Name != "" {
			found = item
		} else {
			data, err := model.Marshal(item)
			if err != nil {
				return "", err
			}
			var members map[string]json.RawMessage
			if err := json.Unmarshal(data, &members); err != nil {
				return "", err
			}
			if len(members) != 0 {
				found = item
			}
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
		address = text(found.URL)
	}
	if address == "" {
		address = sourceURL
	}
	if address == "" {
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
