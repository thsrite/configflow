package rulefetch

import "configflow/internal/jsonx"

// Archived fixtures use the original quoting when constructing source text.
func jsonDumps(value string, ascii bool) string {
	out, _ := jsonx.Encode(value, jsonx.Options{EnsureASCII: ascii})
	return string(out)
}
