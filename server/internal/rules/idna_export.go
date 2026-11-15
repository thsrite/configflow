package rules

// IDNAEncode is str.encode('idna').decode('ascii') (CPython's IDNA 2003
// codec with nameprep): the ASCII form, or ok=false where Python raises
// UnicodeError. Exported for internal/health (node_dns._server).
func IDNAEncode(input string) (string, bool) { return pyIDNAEncode(input) }
