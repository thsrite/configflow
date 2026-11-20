package check

import (
	"errors"
	"fmt"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
)

// KeyError is the legacy missing-key error expected by unmigrated callers of
// ProviderHealthCheck (removed when convert drops legacy health helpers).
type KeyError struct{ Key string }

// Error returns the native missing-health configuration message.
func (e *KeyError) Error() string { return msgMissingHealthCheck }

// Unwrap preserves the native missing-member cause for transitional callers.
func (e *KeyError) Unwrap() error { return ErrMissingHealthCheck }

func decodeLegacy[T any](value any, dst *T) error {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(raw, dst)
}

func encodeLegacyValue(value any) (any, error) {
	raw, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(raw)
}

func encodeLegacyObject(value any) (*jsonx.Object, error) {
	parsed, err := encodeLegacyValue(value)
	if err != nil {
		return nil, err
	}
	obj, ok := parsed.(*jsonx.Object)
	if !ok {
		return nil, fmt.Errorf("expected JSON object, got %T", parsed)
	}
	return obj, nil
}

func encodeLegacySlice(value any) ([]any, error) {
	parsed, err := encodeLegacyValue(value)
	if err != nil {
		return nil, err
	}
	list, ok := parsed.([]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON array, got %T", parsed)
	}
	return list, nil
}

func replaceObjectInPlace(target, next *jsonx.Object) {
	for _, key := range target.Keys() {
		target.Delete(key)
	}
	for key, item := range next.All() {
		target.Set(key, item)
	}
}

// ProviderDefaults returns DefaultProviderHealth as a legacy *jsonx.Object.
// Removed when api/proxygroups migrates off jsonx.
func ProviderDefaults() *jsonx.Object {
	obj, _ := encodeLegacyObject(DefaultProviderHealth())
	return obj
}

// GroupDefaults returns DefaultGroupHealth as a legacy *jsonx.Object.
// Removed when api/proxygroups migrates off jsonx.
func GroupDefaults() *jsonx.Object {
	obj, _ := encodeLegacyObject(DefaultGroupHealth())
	return obj
}

// InitializeGroupHealth mutates group in place via InitGroupHealth.
// Removed when api/proxygroups migrates off jsonx.
func InitializeGroupHealth(group *jsonx.Object) {
	if group == nil {
		return
	}
	var rawObj rawJSONObject
	if err := decodeLegacy(group, &rawObj); err != nil {
		return
	}
	if err := InitGroupHealth(&rawObj.Fields); err != nil {
		return
	}
	next, err := encodeLegacyObject(rawObj)
	if err != nil {
		return
	}
	replaceObjectInPlace(group, next)
}

// ValidateHealth validates legacy jsonx values via ValidateHealthJSON.
// Removed when api/proxygroups and repo/legacy.go migrate off jsonx.
func ValidateHealth(values any, provider bool) error {
	if _, ok := values.(*jsonx.Object); !ok || values == (*jsonx.Object)(nil) {
		return valueError(msgHealthMustBeObject)
	}
	raw, err := jsonx.Compact(values)
	if err != nil {
		return valueError(msgHealthMustBeObject)
	}
	return ValidateHealthJSON(raw, provider)
}

// ProviderHealthCheck resolves source's health_check via ResolveProviderHealth.
// Removed when convert migrates off legacy ProviderHealthCheck.
func ProviderHealthCheck(source *jsonx.Object) (*jsonx.Object, error) {
	if source == nil {
		return nil, &KeyError{Key: "health_check"}
	}
	var rawObj rawJSONObject
	if err := decodeLegacy(source, &rawObj); err != nil {
		return nil, err
	}
	// Transitional callers still expose the raw request validator contract.
	if health, present := rawObj.Fields.Get("health_check"); present {
		if err := ValidateHealthJSON(health, true); err != nil {
			return nil, err
		}
	}
	resolved, err := ResolveProviderHealth(&rawObj.Fields)
	if err != nil {
		if errors.Is(err, ErrMissingHealthCheck) {
			return nil, &KeyError{Key: "health_check"}
		}
		return nil, err
	}
	return encodeLegacyObject(resolved)
}
