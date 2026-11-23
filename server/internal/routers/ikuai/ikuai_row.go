package ikuai

import (
	"encoding/json"
	"strconv"
)

// ikuaiRow is one decoded iKuai reply row: text fields plus numeric ids,
// counters and flags.
type Row map[string]any

// text reads a text or numeric field; other values are absent.
func (r Row) Text(key string) string {
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
func (r Row) set(key string) bool { return r.Text(key) != "" }

func (r Row) Is(key, value string) bool     { s, ok := r[key].(string); return ok && s == value }
func (r Row) Has(key string) bool           { _, ok := r[key]; return ok }
func (r Row) Str(key string) (string, bool) { s, ok := r[key].(string); return s, ok }

// filled: the field holds a non-empty string, a non-zero number or true.
func (r Row) filled(key string) bool { return nonEmptyText(r[key]) != "" || r[key] == true }
