package oxidns

// ForwardUpstreams parses and validates forward-plugin upstreams using the same
// reader as Generate. Empty input uses defaults. The returned tree is owned by
// the caller; this function performs no I/O.
func ForwardUpstreams(text string, defaults []string) (*Node, error) {
	return forwardUpstreams(text, defaults)
}
