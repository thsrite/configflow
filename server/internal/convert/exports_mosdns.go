package convert

// Exported access to Python-semantics helpers of this package for the MosDNS
// converter (internal/mosdns, C21), so it does not keep private copies.

// AppendURLQuery is profile_context.append_url_query(url, params); err is the
// ValueError urlsplit raises (as *PyError).
func AppendURLQuery(url string, params [][2]any) (out string, err error) {
	defer catch(&err)
	return appendURLQuery(url, params), nil
}

// PySplitlines is str.splitlines().
func PySplitlines(s string) []string { return pySplitlines(s) }

// PyReEscape is re.escape (Python 3.7+).
func PyReEscape(s string) string { return pyReEscape(s) }

// IsIPNetwork reports whether ipaddress.ip_network(text, strict=False)
// accepts text.
func IsIPNetwork(text string) bool {
	_, _, ok := ipNetwork(text)
	return ok
}
