package rulesetfmt

// TODO(N8-9): remove this JSON-only boundary after matcher/converter callers
// use RuleFormat. No format inference or Python value behavior lives here.
import (
	"bytes"
	"encoding/json"
	"errors"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// URLTypeError reports a non-string URL in a stored rule-set record.
type URLTypeError struct{ Value any }

func (e *URLTypeError) Error() string { return msgURLType }

// LibraryEntryTypeError reports a non-object in the legacy rule library.
type LibraryEntryTypeError struct{ Value any }

func (e *LibraryEntryTypeError) Error() string { return msgLibraryEntryType }

// Format adapts legacy records to the typed format implementation.
func Format(rule *jsonx.Object, library []any, sourceURL string) (string, error) {
	typedRule, typedLibrary, err := decodeLegacy(rule, library)
	if err != nil {
		return "", err
	}
	result, err := RuleFormat(typedRule, typedLibrary, sourceURL)
	var invalid *URLValueError
	if errors.As(err, &invalid) {
		value, decodeErr := jsonx.Parse(invalid.Raw)
		if decodeErr != nil {
			return "", decodeErr
		}
		return result, &URLTypeError{Value: value}
	}
	return result, err
}

// FormatSkippingInvalidEntries retains the matcher's no-error boundary.
// A malformed record follows typed decode validation rather than Python glue.
func FormatSkippingInvalidEntries(rule *jsonx.Object, library []any, sourceURL string) string {
	result, _ := Format(rule, library, sourceURL)
	if result == "" {
		return "text"
	}
	return result
}

func decodeLegacy(rule *jsonx.Object, library []any) (model.RuleConfig, []model.RuleLibraryItem, error) {
	var typedRule model.RuleConfig
	data, err := json.Marshal(rule)
	if err != nil {
		return typedRule, nil, err
	}
	if err := json.Unmarshal(data, &typedRule); err != nil {
		return typedRule, nil, err
	}
	typedLibrary := make([]model.RuleLibraryItem, len(library))
	for i, entry := range library {
		data, err := json.Marshal(entry)
		if err != nil {
			return typedRule, nil, err
		}
		if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
			return typedRule, nil, &LibraryEntryTypeError{Value: entry}
		}
		if err := json.Unmarshal(data, &typedLibrary[i]); err != nil {
			return typedRule, nil, &LibraryEntryTypeError{Value: entry}
		}
	}
	return typedRule, typedLibrary, nil
}
