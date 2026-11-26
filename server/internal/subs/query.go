package subs

import (
	"configflow/internal/weburl"
	"strings"
)

// ParseQueryOptions reads external subscription transport options. Only '&'
// separates options; semicolons remain part of a value. Empty assignments are
// omitted, repeated values retain source order, and the shared URL decoder
// preserves malformed percent escapes accepted by existing subscription sources.
func ParseQueryOptions(query string) (keys []string, values map[string][]string) {
	values = map[string][]string{}
	for _, option := range strings.Split(query, "&") {
		name, value, present := strings.Cut(option, "=")
		if !present || value == "" {
			continue
		}
		name, value = weburl.UnquotePlus(name), weburl.UnquotePlus(value)
		if _, seen := values[name]; !seen {
			keys = append(keys, name)
		}
		values[name] = append(values[name], value)
	}
	return keys, values
}
