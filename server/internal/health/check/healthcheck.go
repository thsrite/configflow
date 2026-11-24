// Package check implements pure, repository-free health-check validation and
// defaults, region-check service definitions/verdicts/target selection, domain
// probe verdicts, and their per-Agent JSON stores.
//
// It does not access the configuration repository, run probe subprocesses, or
// manage HTTP/Agent lifecycles; repository-backed settings, node latency
// testing, and store wiring live in internal/health.
package check

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/weburl"
)

const (
	// DefaultProbeURL is the default HTTP(S) probe URL for providers and groups.
	DefaultProbeURL = "https://www.gstatic.com/generate_204"
	// DefaultExpectedStatus is the default expected HTTP status range.
	DefaultExpectedStatus = "204"
)

// ErrMissingHealthCheck reports that a provider source lacks health_check.
var ErrMissingHealthCheck = errors.New(msgMissingHealthCheck)

// ValidationError reports an invalid health-check configuration or input value.
type ValidationError struct{ Message string }

// Error returns the user-facing validation message.
func (e *ValidationError) Error() string { return e.Message }

func invalidInput(message string) error { return &ValidationError{Message: message} }

// ProviderHealth holds a resolved provider health-check configuration.
type ProviderHealth struct {
	Enable         bool   `json:"enable"`
	URL            string `json:"url"`
	Interval       int64  `json:"interval"`
	Timeout        int64  `json:"timeout"`
	Lazy           bool   `json:"lazy"`
	ExpectedStatus string `json:"expected-status"`
}

// GroupHealth holds default health-check settings for a proxy group.
type GroupHealth struct {
	URL            string `json:"url"`
	Interval       int64  `json:"interval"`
	Timeout        int64  `json:"timeout"`
	Lazy           bool   `json:"lazy"`
	ExpectedStatus string `json:"expected-status"`
	Tolerance      *int64 `json:"tolerance,omitempty"`
}

// DefaultProviderHealth returns the default provider health-check settings.
func DefaultProviderHealth() ProviderHealth {
	return ProviderHealth{
		Enable:         true,
		URL:            DefaultProbeURL,
		Interval:       300,
		Timeout:        5000,
		Lazy:           true,
		ExpectedStatus: DefaultExpectedStatus,
	}
}

// GroupHealthTypes lists proxy-group types that carry health-check settings.
var GroupHealthTypes = []string{"url-test", "fallback", "load-balance"}

// DefaultGroupHealth returns the default proxy-group health-check settings.
func DefaultGroupHealth() GroupHealth {
	return GroupHealth{
		URL:            DefaultProbeURL,
		Interval:       60,
		Timeout:        5000,
		Lazy:           false,
		ExpectedStatus: DefaultExpectedStatus,
	}
}

// IsGroupHealthType reports whether kind is a proxy-group type with health checks.
func IsGroupHealthType(kind string) bool {
	for _, candidate := range GroupHealthTypes {
		if candidate == kind {
			return true
		}
	}
	return false
}

// InitGroupHealth populates creation-time default health-check fields on group.
func InitGroupHealth(group *model.Fields) error {
	if group == nil {
		return nil
	}
	rawType, ok := group.Get("type")
	if !ok {
		return nil
	}
	var kind StrictString
	if err := json.Unmarshal(rawType, &kind); err != nil || !IsGroupHealthType(string(kind)) {
		return nil
	}
	defaults := DefaultGroupHealth()
	urlJSON, err := jsonresp.Encode(defaults.URL)
	if err != nil {
		return fmt.Errorf("encode group default url: %w", err)
	}
	statusJSON, err := jsonresp.Encode(defaults.ExpectedStatus)
	if err != nil {
		return fmt.Errorf("encode group default status: %w", err)
	}
	pairs := []struct {
		key   string
		value json.RawMessage
	}{
		{"url", urlJSON},
		{"interval", json.RawMessage(strconv.FormatInt(defaults.Interval, 10))},
		{"timeout", json.RawMessage(strconv.FormatInt(defaults.Timeout, 10))},
		{"lazy", json.RawMessage(strconv.FormatBool(defaults.Lazy))},
		{"expected-status", statusJSON},
	}
	for _, pair := range pairs {
		if !group.Has(pair.key) {
			if err := group.Set(pair.key, pair.value); err != nil {
				return fmt.Errorf("set group default %s: %w", pair.key, err)
			}
		}
	}
	if string(kind) == "url-test" && !group.Has("tolerance") {
		if err := group.Set("tolerance", json.RawMessage("50")); err != nil {
			return fmt.Errorf("set group default tolerance: %w", err)
		}
	}
	return nil
}

// InitProviderHealth populates creation-time default health_check on source,
// preserving a legacy non-empty health_check_url when present.
func InitProviderHealth(source *model.Fields) error {
	if source == nil || source.Has("health_check") {
		return nil
	}
	defaults := DefaultProviderHealth()
	if rawURL, ok := source.Get("health_check_url"); ok {
		var legacyURL Text
		if err := json.Unmarshal(rawURL, &legacyURL); err == nil && legacyURL != "" {
			defaults.URL = string(legacyURL)
		}
	}
	encoded, err := jsonresp.Encode(defaults)
	if err != nil {
		return fmt.Errorf("encode provider default health_check: %w", err)
	}
	if err := source.Set("health_check", encoded); err != nil {
		return fmt.Errorf("set provider health_check: %w", err)
	}
	return nil
}

// ProviderHealthDefaults is the health_check a new subscription or aggregation
// gets when its request has none: the provider defaults, with a non-empty
// legacy health_check_url (text, or a number read as text, as
// InitProviderHealth reads it) as the probe URL. legacyURL is the sent member,
// nil when absent.
func ProviderHealthDefaults(legacyURL json.RawMessage) *model.ProviderHealthCheck {
	d := DefaultProviderHealth()
	var legacy Text
	if legacyURL != nil && json.Unmarshal(legacyURL, &legacy) == nil && legacy != "" {
		d.URL = string(legacy)
	}
	return &model.ProviderHealthCheck{
		Enable:         new(d.Enable),
		URL:            new(d.URL),
		Interval:       new(model.Int(d.Interval)),
		Timeout:        new(model.Int(d.Timeout)),
		Lazy:           new(d.Lazy),
		ExpectedStatus: new(d.ExpectedStatus),
	}
}

var (
	providerKeys = []string{"enable", "url", "interval", "timeout", "lazy", "expected-status"}
	groupKeys    = []string{"url", "interval", "timeout", "lazy", "expected-status", "tolerance"}
)

func containsKey(keys []string, key string) bool {
	for _, item := range keys {
		if item == key {
			return true
		}
	}
	return false
}

var expectedStatusPattern = regexp.MustCompile(`^[0-9]{3}(?:-[0-9]{3})?(?:/[0-9]{3}(?:-[0-9]{3})?)*$`)

// ValidateHealthJSON validates raw JSON health-check fields without coercion.
func ValidateHealthJSON(raw json.RawMessage, provider bool) error {
	var object rawJSONObject
	if err := json.Unmarshal(raw, &object); err != nil {
		return invalidInput(msgHealthMustBeObject)
	}
	allowed := groupKeys
	if provider {
		allowed = providerKeys
		for _, key := range object.Fields.Keys() {
			if !containsKey(allowed, key) {
				return invalidInput(msgHealthUnknownField)
			}
		}
	}
	for _, key := range object.Fields.Keys() {
		if !containsKey(allowed, key) {
			continue
		}
		value, _ := object.Fields.Get(key)
		if err := validateFieldJSON(key, value); err != nil {
			return err
		}
	}
	return nil
}

// ValidateProviderHealth validates a stored provider health-check record.
func ValidateProviderHealth(hc *model.ProviderHealthCheck) error {
	if hc == nil {
		return invalidInput(msgHealthMustBeObject)
	}
	raw, err := model.Marshal(hc)
	if err != nil {
		return fmt.Errorf("marshal provider health check: %w", err)
	}
	return validateStoredHealth(raw, healthValues{Enable: hc.Enable, URL: hc.URL, Interval: hc.Interval, Timeout: hc.Timeout, Lazy: hc.Lazy, ExpectedStatus: hc.ExpectedStatus}, true)
}

// healthValues contains the decoded values used by stored-data validation.
// The original member set supplies presence; R5 provenance is never reinterpreted.
type healthValues struct {
	Enable         *bool      `json:"enable"`
	URL            *string    `json:"url"`
	Interval       *model.Int `json:"interval"`
	Timeout        *model.Int `json:"timeout"`
	Lazy           *bool      `json:"lazy"`
	ExpectedStatus *string    `json:"expected-status"`
	Tolerance      *model.Int `json:"tolerance"`
}

func validateStoredHealth(raw json.RawMessage, values healthValues, provider bool) error {
	var original rawJSONObject
	if err := json.Unmarshal(raw, &original); err != nil {
		return err
	}
	canonical, err := jsonresp.Encode(values)
	if err != nil {
		return err
	}
	var decoded rawJSONObject
	if err := json.Unmarshal(canonical, &decoded); err != nil {
		return err
	}
	allowed := groupKeys
	if provider {
		allowed = providerKeys
	}
	var fields model.Fields
	for key, rawValue := range original.Fields.All() {
		if containsKey(allowed, key) {
			rawValue, _ = decoded.Fields.Get(key)
		}
		if err := fields.Set(key, rawValue); err != nil {
			return err
		}
	}
	encoded, err := fields.MarshalJSON()
	if err != nil {
		return err
	}
	return ValidateHealthJSON(encoded, provider)
}

func validateFieldJSON(key string, raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	switch key {
	case "enable", "lazy":
		if !bytes.Equal(trimmed, []byte("true")) && !bytes.Equal(trimmed, []byte("false")) {
			return invalidInput(fmt.Sprintf(msgHealthBool, key))
		}
	case "interval", "timeout", "tolerance":
		minimum, maximum := int64(1), int64(600000)
		if key == "tolerance" {
			minimum = 0
		}
		if key == "interval" {
			maximum = 86400
		}
		if !isJSONIntLiteral(trimmed) {
			return invalidInput(fmt.Sprintf(msgHealthIntRange, key, minimum, maximum))
		}
		n, err := strconv.ParseInt(string(trimmed), 10, 64)
		if err != nil || n < minimum || n > maximum {
			return invalidInput(fmt.Sprintf(msgHealthIntRange, key, minimum, maximum))
		}
	case "url":
		var text StrictString
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return invalidInput(msgHealthInvalidURL)
		}
		for _, r := range string(text) {
			if unicode.IsSpace(r) || r < 32 {
				return invalidInput(msgHealthInvalidURL)
			}
		}
		if !validProbeURL(string(text)) {
			return invalidInput(msgHealthURLNoCredentials)
		}
	case "expected-status":
		var text StrictString
		if err := json.Unmarshal(trimmed, &text); err != nil || !expectedStatusPattern.MatchString(string(text)) {
			return invalidInput(msgHealthStatusFormat)
		}
		for _, item := range strings.Split(string(text), "/") {
			bounds := strings.Split(item, "-")
			first, _ := strconv.Atoi(bounds[0])
			last, _ := strconv.Atoi(bounds[len(bounds)-1])
			if first < 100 || first > 599 || last < 100 || last > 599 || first > last {
				return invalidInput(msgHealthStatusRange)
			}
		}
	}
	return nil
}

// validProbeURL checks the URL condition of ValidateHealthJSON: http(s), host,
// no credentials or fragment, and port in 1..65535.
func validProbeURL(text string) bool {
	parsed, err := weburl.Parse(text)
	if err != nil {
		return false
	}
	port, hasPort, err := weburl.Port(parsed)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" &&
		parsed.User == nil && parsed.Fragment == "" && (!hasPort || port >= 1)
}

// ResolveProviderHealth validates source's health_check and overlays it onto
// DefaultProviderHealth. Missing health_check returns ErrMissingHealthCheck.
func ResolveProviderHealth(source *model.Fields) (ProviderHealth, error) {
	if source == nil {
		return ProviderHealth{}, ErrMissingHealthCheck
	}
	raw, ok := source.Get("health_check")
	if !ok {
		return ProviderHealth{}, ErrMissingHealthCheck
	}
	var overlay model.ProviderHealthCheck
	if err := json.Unmarshal(raw, &overlay); err != nil {
		return ProviderHealth{}, invalidInput(msgHealthMustBeObject)
	}
	if err := ValidateProviderHealth(&overlay); err != nil {
		return ProviderHealth{}, err
	}
	result := DefaultProviderHealth()
	if overlay.Enable != nil {
		result.Enable = *overlay.Enable
	}
	if overlay.URL != nil {
		result.URL = *overlay.URL
	}
	if overlay.Interval != nil {
		result.Interval = int64(*overlay.Interval)
	}
	if overlay.Timeout != nil {
		result.Timeout = int64(*overlay.Timeout)
	}
	if overlay.Lazy != nil {
		result.Lazy = *overlay.Lazy
	}
	if overlay.ExpectedStatus != nil {
		result.ExpectedStatus = *overlay.ExpectedStatus
	}
	return result, nil
}
