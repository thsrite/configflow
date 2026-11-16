// Legacy Unicode trimming remains for older settings routes.

package settings

import (
	"unicode"

	"configflow/internal/rulefetch"
	"configflow/internal/webdav"
)

// Retention fields keep the API's stable validation order.
var retentionKeys = []string{"keep_latest", "keep_daily"}
var retentionLimits = map[string]int64{"keep_latest": webdav.MaxKeepLatest, "keep_daily": webdav.MaxKeepDaily}

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
