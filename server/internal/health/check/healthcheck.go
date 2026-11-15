// Package check holds the pure, repository-free parts of C16: the
// health-check defaults and validation (backend/utils/health_check.py), the
// region check service definitions, verdicts and target choice
// (backend/utils/region_check.py) and the domain probe verdicts
// (backend/utils/domain_probe.py), with their per-Agent JSON stores built on
// a directory.
//
// It imports no repository code so that internal/repo (validate_health) and
// internal/rulefetch (region_check.SERVICES) can use it. The parts that need
// the repository (node latency, node DNS, store locations, wiring) are in
// internal/health.
package check

import (
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/urlx"
)

// ValueError is the ValueError validate_health raises; Error() is str(exc).
type ValueError struct{ Message string }

func (e *ValueError) Error() string { return e.Message }

func valueError(message string) error { return &ValueError{message} }

// ProviderDefaults is a fresh copy of PROVIDER_DEFAULTS.
func ProviderDefaults() *jsonx.Object {
	return jsonx.Obj("enable", true, "url", "https://www.gstatic.com/generate_204", "interval", jsonx.Int(300),
		"timeout", jsonx.Int(5000), "lazy", true, "expected-status", "204")
}

// GroupHealthTypes is GROUP_HEALTH_TYPES.
var GroupHealthTypes = []string{"url-test", "fallback", "load-balance"}

// GroupDefaults is a fresh copy of GROUP_DEFAULTS.
func GroupDefaults() *jsonx.Object {
	return jsonx.Obj("url", "https://www.gstatic.com/generate_204", "interval", jsonx.Int(60),
		"timeout", jsonx.Int(5000), "lazy", false, "expected-status", "204")
}

// IsGroupHealthType is `value in GROUP_HEALTH_TYPES` (tuple membership by ==).
func IsGroupHealthType(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, kind := range GroupHealthTypes {
		if kind == text {
			return true
		}
	}
	return false
}

// InitializeGroupHealth is initialize_group_health: creation defaults only.
func InitializeGroupHealth(group *jsonx.Object) {
	if !IsGroupHealthType(group.Value("type")) {
		return
	}
	for key, value := range GroupDefaults().All() {
		group.SetDefault(key, value)
	}
	if jsonx.Equal(group.Value("type"), "url-test") {
		group.SetDefault("tolerance", jsonx.Int(50))
	}
}

// InitializeProviderHealth is initialize_provider_health: creation defaults,
// keeping the legacy API's explicit probe URL.
func InitializeProviderHealth(source *jsonx.Object) {
	if source.Has("health_check") {
		return
	}
	defaults := ProviderDefaults()
	source.Set("health_check", defaults)
	if url := source.Value("health_check_url"); jsonx.Truthy(url) {
		defaults.Set("url", url)
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

// ValidateHealth is validate_health(values, provider=provider): persisted
// engine fields are validated without coercion. Python walks
// `allowed & values.keys()` in set order (string hashing is randomised per
// process); here it is the object's key order. Only the message differs, and
// only when several fields are invalid.
func ValidateHealth(values any, provider bool) error {
	object, ok := values.(*jsonx.Object)
	if !ok || object == nil {
		return valueError("健康检查必须是对象")
	}
	allowed := groupKeys
	if provider {
		allowed = providerKeys
		for key := range object.All() {
			if !containsKey(allowed, key) {
				return valueError("健康检查包含未知字段")
			}
		}
	}
	for key, value := range object.All() {
		if !containsKey(allowed, key) {
			continue
		}
		if err := validateField(key, value); err != nil {
			return err
		}
	}
	return nil
}

func validateField(key string, value any) error {
	switch key {
	case "enable", "lazy":
		if _, ok := value.(bool); !ok {
			return valueError(key + " 必须为布尔值")
		}
	case "interval", "timeout", "tolerance":
		minimum, maximum := int64(1), int64(600000)
		if key == "tolerance" {
			minimum = 0
		}
		if key == "interval" {
			maximum = 86400
		}
		number, ok := value.(jsonx.Number)
		n, fits := number.Int64()
		if !ok || !number.IsInt() || !fits || n < minimum || n > maximum {
			return valueError(key + " 必须为 " + strconv.FormatInt(minimum, 10) + "–" +
				strconv.FormatInt(maximum, 10) + " 的整数")
		}
	case "url":
		text, ok := value.(string)
		if !ok {
			return valueError("探测 URL 必须是有效 HTTP(S) 地址")
		}
		for _, r := range text {
			if pystr.IsSpace(r) || r < 32 {
				return valueError("探测 URL 必须是有效 HTTP(S) 地址")
			}
		}
		if !validProbeURL(text) {
			return valueError("探测 URL 必须是无凭据的 HTTP(S) 地址")
		}
	case "expected-status":
		text, ok := value.(string)
		if !ok || !expectedStatusPattern.MatchString(text) {
			return valueError("expected-status 格式应为 204 或 200-299/304")
		}
		for _, item := range strings.Split(text, "/") {
			bounds := strings.Split(item, "-")
			first, _ := strconv.Atoi(bounds[0])
			last, _ := strconv.Atoi(bounds[len(bounds)-1])
			for _, n := range []int{first, last} {
				if n < 100 || n > 599 {
					return valueError("expected-status 范围必须在 100–599 内且递增")
				}
			}
			if first > last {
				return valueError("expected-status 范围必须在 100–599 内且递增")
			}
		}
	}
	return nil
}

// validProbeURL is the urlsplit condition of validate_health.
func validProbeURL(text string) bool {
	parsed, err := urlx.Split(text)
	if err != nil {
		return false
	}
	port, hasPort, err := parsed.Port()
	if err != nil {
		return false
	}
	_, hasUser, _, hasPassword := UserInfo(parsed.Netloc)
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" &&
		!hasUser && !hasPassword && parsed.Fragment == "" && (!hasPort || (1 <= port && port <= 65535))
}

// UserInfo is SplitResult.username / .password of a netloc: each with
// whether it is not None.
func UserInfo(netloc string) (username string, hasUsername bool, password string, hasPassword bool) {
	at := strings.LastIndex(netloc, "@")
	if at < 0 {
		return "", false, "", false
	}
	username, password, hasPassword = strings.Cut(netloc[:at], ":")
	return username, true, password, hasPassword
}

// ProviderHealthCheck is provider_health_check(source): the validated
// health_check over PROVIDER_DEFAULTS. A missing health_check is Python's
// KeyError.
func ProviderHealthCheck(source *jsonx.Object) (*jsonx.Object, error) {
	value, ok := source.Get("health_check")
	if !ok {
		return nil, &KeyError{Key: "health_check"}
	}
	if err := ValidateHealth(value, true); err != nil {
		return nil, err
	}
	result := ProviderDefaults()
	for key, item := range value.(*jsonx.Object).All() {
		result.Set(key, item)
	}
	return result, nil
}

// KeyError is Python's KeyError for a missing key.
type KeyError struct{ Key string }

func (e *KeyError) Error() string { return jsonx.Repr(e.Key) }
