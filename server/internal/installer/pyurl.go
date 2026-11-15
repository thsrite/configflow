package installer

import (
	"net/netip"
	"regexp"
	"strings"
)

// splitResult is urllib.parse.SplitResult.
type splitResult struct {
	scheme, netloc, path, query, fragment string
}

// nfkcSpecial are the characters whose NFKC form (Unicode 14.0, CPython
// 3.11) contains one of "/?#@:"; _checknetloc refuses a non-ASCII netloc
// holding one of them.
var nfkcSpecial = []rune{0x2047, 0x2048, 0x2049, 0x2100, 0x2101, 0x2105, 0x2106, 0x2a74,
	0xfe13, 0xfe16, 0xfe55, 0xfe56, 0xfe5f, 0xfe6b, 0xff03, 0xff0f, 0xff1a, 0xff1f, 0xff20}

var ipvFuture = regexp.MustCompile(`\Av[a-fA-F0-9]+\.(?s:.+)\z`)

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// pyURLSplit is urllib.parse.urlsplit(url) of CPython 3.11.17.
func pyURLSplit(url string) (splitResult, error) {
	url = strings.TrimLeft(url, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f"+
		"\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f ")
	for _, b := range []string{"\t", "\r", "\n"} {
		url = strings.ReplaceAll(url, b, "")
	}
	var r splitResult
	if i := strings.IndexByte(url, ':'); i > 0 {
		c := url[0]
		if c < 0x80 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') && matchesClass(url[:i], "+-.") {
			r.scheme, url = strings.ToLower(url[:i]), url[i+1:]
		}
	}
	if strings.HasPrefix(url, "//") {
		delim := len(url)
		for _, c := range "/?#" {
			if w := strings.IndexRune(url[2:], c); w >= 0 && w+2 < delim {
				delim = w + 2
			}
		}
		r.netloc, url = url[2:delim], url[delim:]
		open, closed := strings.Contains(r.netloc, "["), strings.Contains(r.netloc, "]")
		if open != closed {
			return r, valueError("Invalid IPv6 URL")
		}
		if open && closed {
			if err := checkBracketedNetloc(r.netloc); err != nil {
				return r, err
			}
		}
	}
	if i := strings.IndexByte(url, '#'); i >= 0 {
		url, r.fragment = url[:i], url[i+1:]
	}
	if i := strings.IndexByte(url, '?'); i >= 0 {
		url, r.query = url[:i], url[i+1:]
	}
	r.path = url
	if r.netloc != "" && !isASCII(r.netloc) {
		for _, c := range nfkcSpecial {
			if strings.ContainsRune(r.netloc, c) {
				return r, valueError("netloc '" + r.netloc + "' contains invalid characters under NFKC normalization")
			}
		}
	}
	return r, nil
}

func rpartitionAt(netloc string) (userinfo string, have bool, hostinfo string) {
	if i := strings.LastIndexByte(netloc, '@'); i >= 0 {
		return netloc[:i], true, netloc[i+1:]
	}
	return "", false, netloc
}

func partition(s, sep string) (string, bool, string) {
	before, after, found := strings.Cut(s, sep)
	return before, found, after
}

func checkBracketedNetloc(netloc string) error {
	_, _, hostAndPort := rpartitionAt(netloc)
	before, haveOpen, bracketed := partition(hostAndPort, "[")
	var hostname string
	if haveOpen {
		if before != "" {
			return valueError("Invalid IPv6 URL")
		}
		var port string
		hostname, _, port = partition(bracketed, "]")
		if port != "" && !strings.HasPrefix(port, ":") {
			return valueError("Invalid IPv6 URL")
		}
	} else {
		hostname, _, _ = partition(hostAndPort, ":")
	}
	if strings.HasPrefix(hostname, "v") {
		if !ipvFuture.MatchString(hostname) {
			return valueError("IPvFuture address is invalid")
		}
		return nil
	}
	if _, err := parseIPv4(hostname); err == nil {
		return valueError("An IPv4 address cannot be in brackets")
	}
	if !isPyIPv6(hostname) {
		return valueError(reprText(hostname) + " does not appear to be an IPv4 or IPv6 address")
	}
	return nil
}

// isPyIPv6 approximates ipaddress.IPv6Address(text) (scope ids allowed).
func isPyIPv6(text string) bool {
	address, _, _ := strings.Cut(text, "%")
	if strings.Contains(text, "%") {
		_, zone, _ := strings.Cut(text, "%")
		if zone == "" {
			return false
		}
	}
	if !strings.Contains(address, ":") {
		return false
	}
	ip, err := netip.ParseAddr(address)
	return err == nil && ip.Is6()
}

// hostname is SplitResult.hostname being truthy.
func (r splitResult) hasHostname() bool {
	_, _, hostinfo := rpartitionAt(r.netloc)
	_, haveOpen, bracketed := partition(hostinfo, "[")
	var hostname string
	if haveOpen {
		hostname, _, _ = partition(bracketed, "]")
	} else {
		hostname, _, _ = partition(hostinfo, ":")
	}
	return hostname != ""
}

// credentials reports whether SplitResult.username or .password is truthy.
func (r splitResult) hasCredentials() bool {
	userinfo, have, _ := rpartitionAt(r.netloc)
	if !have {
		return false
	}
	username, _, password := partition(userinfo, ":")
	return username != "" || password != ""
}
