package rulefetch

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// rareLineBreaks are the separators only Python's str.splitlines broke lines
// at; since O17 rule text breaks lines like the web editor (\n, \r\n, \r).
const rareLineBreaks = "\v\f\x1c\x1d\x1e\u0085  "

// hasRareLineBreak reports a replayed call whose string arguments contain one
// of rareLineBreaks: its recorded Python line numbers no longer apply.
func hasRareLineBreak(args any) bool {
	var value any
	if json.Unmarshal([]byte(encode(args)), &value) != nil {
		return false
	}
	var walk func(any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case string:
			if strings.ContainsAny(x, rareLineBreaks) {
				return true
			}
			// validate_remote_bytes passes the content base64-encoded.
			decoded, err := base64.StdEncoding.DecodeString(x)
			return err == nil && strings.ContainsAny(string(decoded), rareLineBreaks)
		case []any:
			for _, item := range x {
				if walk(item) {
					return true
				}
			}
		case map[string]any:
			for _, item := range x {
				if walk(item) {
					return true
				}
			}
		}
		return false
	}
	return walk(value)
}
