// TODO(port): replace with package #17 (backend/utils/webdav_backup.py::RETENTION_LIMITS,
// retention_settings). rule_fetch.normalize_rule_fetch_proxy is rulefetch.NormalizeRuleFetchProxy (C14).

package settings

import (
	"unicode"

	"configflow/internal/jsonx"
	"configflow/internal/rulefetch"
)

// RetentionLimits is webdav_backup.RETENTION_LIMITS, in its key order.
var retentionKeys = []string{"keep_latest", "keep_daily"}
var retentionLimits = map[string]int64{"keep_latest": 1000, "keep_daily": 3650}

const (
	defaultKeepLatest = 30
	defaultKeepDaily  = 30
)

// retentionSettings is webdav_backup.retention_settings: the saved values,
// or the defaults for missing or invalid ones (not ints in range).
func retentionSettings(settings *jsonx.Object) (any, any) {
	values := []any{}
	for _, item := range []struct {
		key      string
		fallback int64
	}{{"keep_latest", defaultKeepLatest}, {"keep_daily", defaultKeepDaily}} {
		value, present := settings.Get(item.key)
		n, isNumber := value.(jsonx.Number)
		v, fits := n.Int64()
		if present && isNumber && fits && v >= 0 && v <= retentionLimits[item.key] {
			values = append(values, value)
		} else {
			values = append(values, jsonx.Int(item.fallback))
		}
	}
	return values[0], values[1]
}

func pyIsSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0x1c, 0x1d, 0x1e, 0x1f, 0x85:
		return true
	}
	return r > 0x7f && unicode.IsSpace(r)
}

// normalizeRuleFetchProxy is rule_fetch.normalize_rule_fetch_proxy; ok
// false where Python raises ValueError.
func normalizeRuleFetchProxy(value any) (string, bool) {
	proxy, err := rulefetch.NormalizeRuleFetchProxy(value)
	return proxy, err == nil
}
