// Shared retention limits and the native rule-download proxy validator.

package settings

import (
	"configflow/internal/rulefetch"
	"configflow/internal/webdav"
)

// Retention fields keep the API's stable validation order.
var retentionKeys = []string{"keep_latest", "keep_daily"}
var retentionLimits = map[string]int64{"keep_latest": webdav.MaxKeepLatest, "keep_daily": webdav.MaxKeepDaily}

// normalizeRuleFetchProxy is rule_fetch.normalize_rule_fetch_proxy; ok
// false where Python raises ValueError.
func normalizeRuleFetchProxy(value string) (string, bool) {
	proxy, err := rulefetch.NormalizeProxy(value)
	return proxy, err == nil
}
