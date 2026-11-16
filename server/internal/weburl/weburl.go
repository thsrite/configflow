// Package weburl holds the few URL and proxy rules the panel needs on top of
// net/url and net/http: parsing user-entered URLs, preparing a URL before it
// is requested, the proxy choice from the environment, and log-safe URL and
// error renderings (backend/utils/url_utils.py).
package weburl

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Parse is url.Parse after the clean-up browsers (and Python's urlsplit) do
// on a typed URL: leading control characters and spaces are dropped and tab,
// CR and LF removed anywhere.
func Parse(raw string) (*url.URL, error) {
	raw = strings.TrimLeft(raw, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\x0c\r\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f ")
	if strings.ContainsAny(raw, "\t\r\n") {
		raw = strings.NewReplacer("\t", "", "\r", "", "\n", "").Replace(raw)
	}
	u, err := url.Parse(escapeZone(raw))
	if err != nil {
		return nil, err
	}
	if strings.Contains(u.Host, "[") != strings.Contains(u.Host, "]") {
		return nil, errors.New("invalid IPv6 URL")
	}
	if !strings.Contains(u.Host, "[") && strings.Count(u.Host, ":") > 1 {
		return nil, errors.New("an IPv6 host must be in brackets")
	}
	password, _ := u.User.Password()
	if strings.ContainsAny(u.Host+u.User.Username()+password, nfkcDelimiters) {
		return nil, ErrAmbiguousHost
	}
	return u, nil
}

// escapeZone writes the zone of a bracketed IPv6 host typed as
// "[fe80::1%eth0]" the way net/url reads it ("%25eth0").
func escapeZone(raw string) string {
	start := strings.Index(raw, "://")
	if start < 0 {
		return raw
	}
	start += 3
	end := len(raw)
	if i := strings.IndexAny(raw[start:], "/?#"); i >= 0 {
		end = start + i
	}
	open, close := strings.LastIndex(raw[start:end], "["), strings.LastIndex(raw[start:end], "]")
	if open < 0 || close < open {
		return raw
	}
	percent := strings.Index(raw[start+open:start+close], "%")
	if percent < 0 || strings.HasPrefix(raw[start+open+percent:], "%25") {
		return raw
	}
	at := start + open + percent + 1
	return raw[:at] + "25" + raw[at:]
}

// ErrAmbiguousHost is Parse's error for a host or user name holding a
// character that normalises to a URL delimiter (see nfkcDelimiters).
var ErrAmbiguousHost = errors.New("URL authority contains characters that normalise to URL delimiters")

// nfkcDelimiters are the characters whose NFKC form contains one of
// "/?#@:" (full-width slash, ...): a host holding one could be read as a
// different host after IDNA normalisation, so such URLs are refused.
const nfkcDelimiters = "／？＃＠：﹕﹖﹟﹫︓︖℀℁℅℆⁇⁈⁉⩴"

// Hostname is u.Hostname() with ASCII letters lower-cased (non-ASCII names
// are left to IDNA, so "İ" never folds into an ASCII host name).
func Hostname(u *url.URL) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, u.Hostname())
}

// Port is the explicit port of u: ok is false without one, err is set for a
// port above 65535 (url.Parse already refuses non-digits).
func Port(u *url.URL) (port int, ok bool, err error) {
	text := u.Port()
	if text == "" {
		return 0, false, nil
	}
	port, err = strconv.Atoi(text)
	if err != nil || port > 65535 {
		return 0, false, errors.New("port out of range 0-65535")
	}
	return port, true, nil
}

// HasCredentials reports whether u carries a non-empty user name or password.
func HasCredentials(u *url.URL) bool {
	if u.User == nil {
		return false
	}
	password, _ := u.User.Password()
	return u.User.Username() != "" || password != ""
}

// RawPath is the path of u as it was written (not percent-decoded).
func RawPath(u *url.URL) string {
	if u.RawPath != "" {
		return u.RawPath
	}
	return u.EscapedPath()
}

// Quote percent-encodes every byte except letters, digits and "-._~"
// (a path segment or query value that must stay one opaque piece).
func Quote(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

// Unquote decodes the valid %XX escapes of s and leaves anything else as it
// is; invalid UTF-8 becomes U+FFFD. Unlike url.PathUnescape it never fails,
// which redaction and display code need.
func Unquote(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	decoded := b.String()
	if utf8.ValidString(decoded) {
		return decoded
	}
	// Each maximal invalid subpart (a cut-off sequence counts once) becomes
	// one U+FFFD, the usual UTF-8 decoder rule.
	var out strings.Builder
	for i := 0; i < len(decoded); {
		r, size := utf8.DecodeRuneInString(decoded[i:])
		if r != utf8.RuneError || size > 1 {
			out.WriteString(decoded[i : i+size])
			i += size
			continue
		}
		out.WriteRune(utf8.RuneError)
		i += invalidSubpart(decoded[i:])
	}
	return out.String()
}

// invalidSubpart is the length of the invalid UTF-8 sequence at the start of
// s: a valid lead byte and the continuation bytes that still fit it.
func invalidSubpart(s string) int {
	lo, hi, need := byte(0x80), byte(0xBF), 0
	switch c := s[0]; {
	case c >= 0xC2 && c <= 0xDF:
		need = 1
	case c == 0xE0:
		need, lo = 2, 0xA0
	case c == 0xED:
		need, hi = 2, 0x9F
	case c >= 0xE1 && c <= 0xEF:
		need = 2
	case c == 0xF0:
		need, lo = 3, 0x90
	case c == 0xF4:
		need, hi = 3, 0x8F
	case c >= 0xF1 && c <= 0xF3:
		need = 3
	}
	n := 1
	for n <= need && n < len(s) && s[n] >= lo && s[n] <= hi {
		lo, hi = 0x80, 0xBF
		n++
	}
	return n
}

// UnquotePlus is Unquote after "+" → " " (form encoding).
func UnquotePlus(s string) string { return Unquote(strings.ReplaceAll(s, "+", " ")) }

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// EncodeQuery is the form encoding of ordered key/value pairs.
func EncodeQuery(pairs [][2]string) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = url.QueryEscape(p[0]) + "=" + url.QueryEscape(p[1])
	}
	return strings.Join(parts, "&")
}

// Prepare is the URL the panel requests for raw (a subscription, rule or
// Sub-Store address) plus extra query params, normalised as RFC 3986 §6
// allows: a "%" that starts no escape is sent as "%25", escapes of
// unreserved characters (but ".") are decoded, characters that may not appear in a
// query are percent-encoded; for http(s) the host is lower-cased, a port's
// leading zeros dropped, dot segments removed and an empty path is "/".
func Prepare(raw string, params [][2]string) (*url.URL, error) {
	u, err := Parse(decodeUnreserved(escapeStrayPercent(raw)))
	if err != nil {
		return nil, err
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		u.Host = strings.ToLower(u.Host)
		if port := u.Port(); strings.HasPrefix(port, "0") {
			u.Host = strings.TrimSuffix(u.Host[:len(u.Host)-len(port)], ":")
			if trimmed := strings.TrimLeft(port, "0"); trimmed != "" {
				u.Host += ":" + trimmed
			}
		}
		if u.Opaque == "" {
			u = u.ResolveReference(&url.URL{})
			if u.Path == "" {
				u.Path = "/"
			}
		}
	}
	u.RawQuery = escapeQuery(u.RawQuery)
	if len(params) > 0 {
		if u.RawQuery != "" {
			u.RawQuery += "&"
		}
		u.RawQuery += EncodeQuery(params)
	}
	return u, nil
}

func escapeStrayPercent(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteByte(s[i])
		if s[i] == '%' && !(i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2])) {
			b.WriteString("25")
		}
	}
	return b.String()
}

// decodeUnreserved decodes %XX escapes of letters, digits and "-_~" ("%2E"
// stays, so an encoded dot never becomes a dot segment).
func decodeUnreserved(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			if c := unhex(s[i+1])<<4 | unhex(s[i+2]); c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_~", c) >= 0 {
				b.WriteByte(c)
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// escapeQuery percent-encodes the bytes that may not appear in a query
// (RFC 3986 query = pchar / "/" / "?"), leaving existing escapes alone.
func escapeQuery(q string) string {
	const allowed = "-._~!$&'()*+,;=:@/?%[]"
	var b strings.Builder
	for i := 0; i < len(q); i++ {
		c := q[i]
		if c < 0x80 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(allowed, c) >= 0) {
			b.WriteByte(c)
			continue
		}
		const hex = "0123456789ABCDEF"
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

// ErrInvalidProxy is returned for a proxy value that is not a usable
// http(s):// or socks5:// URL with a host.
var ErrInvalidProxy = errors.New("invalid proxy URL")

// ParseProxy parses a configured proxy value ("host:port" means http://).
func ParseProxy(value string) (*url.URL, error) {
	raw := value
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil, ErrInvalidProxy
	}
	if _, ok := u.User.Password(); !ok || u.User.Username() == "" {
		u.User = nil // no Proxy-Authorization without a user name and password
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
		return u, nil
	}
	return nil, ErrInvalidProxy
}

// FromEnvironment is the proxy for target from environ (os.Environ() form):
// <scheme>_proxy, then ALL_PROXY, lower-case names first, unless NO_PROXY
// matches the target. nil means direct. Unlike http.ProxyFromEnvironment,
// localhost and loopback targets are not exempt (an Agent on 127.0.0.1 goes
// through the proxy unless NO_PROXY names it) and ALL_PROXY counts, as with
// the requests library the panel used before.
func FromEnvironment(environ []string, target *url.URL) (*url.URL, error) {
	get := func(names ...string) string {
		for _, name := range names {
			for _, entry := range environ {
				if key, value, ok := strings.Cut(entry, "="); ok && key == name && value != "" {
					return value
				}
			}
		}
		return ""
	}
	scheme := strings.ToLower(target.Scheme)
	value := get(scheme+"_proxy", strings.ToUpper(scheme)+"_PROXY", "all_proxy", "ALL_PROXY")
	if value == "" || target.Hostname() == "" || NoProxyMatches(get("no_proxy", "NO_PROXY"), target) {
		return nil, nil
	}
	return ParseProxy(value)
}

// NoProxyMatches reports whether a NO_PROXY list names target's host: "*",
// a domain (with or without a leading dot; it covers its subdomains), an IP
// address, a CIDR range, each optionally with ":port".
func NoProxyMatches(noProxy string, target *url.URL) bool {
	host := strings.ToLower(target.Hostname())
	port := target.Port()
	addr, addrErr := netip.ParseAddr(host)
	for _, entry := range strings.Split(noProxy, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if entry == "*" {
			return true
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			if addrErr == nil && prefix.Contains(addr.Unmap()) {
				return true
			}
			continue
		}
		entryHost, entryPort := entry, ""
		if h, p, err := net.SplitHostPort(entry); err == nil {
			entryHost, entryPort = h, p
		}
		if entryPort != "" && entryPort != port {
			continue
		}
		entryHost = strings.TrimPrefix(strings.Trim(entryHost, "[]"), ".")
		if host == entryHost || strings.HasSuffix(host, "."+entryHost) {
			return true
		}
	}
	return false
}
