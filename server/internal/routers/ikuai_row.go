package routers

import (
	"encoding/json"
	"strconv"
)

// ikuaiRow is one decoded iKuai reply row: text fields plus numeric ids,
// counters and flags.
type ikuaiRow map[string]any

// text reads a text or numeric field; other values are absent.
func (r ikuaiRow) text(key string) string {
	switch v := r[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	}
	return ""
}

// set: the field holds a value.
func (r ikuaiRow) set(key string) bool { return r.text(key) != "" }

func (r ikuaiRow) is(key, value string) bool     { s, ok := r[key].(string); return ok && s == value }
func (r ikuaiRow) has(key string) bool           { _, ok := r[key]; return ok }
func (r ikuaiRow) str(key string) (string, bool) { s, ok := r[key].(string); return s, ok }

// filled: the field holds a non-empty string, a non-zero number or true.
func (r ikuaiRow) filled(key string) bool { return nonEmptyText(r[key]) != "" || r[key] == true }
