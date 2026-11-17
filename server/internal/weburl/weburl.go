// Package weburl holds the few URL and proxy rules the panel needs on top of
// net/url and net/http: parsing user-entered URLs, preparing a URL before it
// is requested, the proxy choice from the environment, and log-safe URL and
// error renderings (backend/utils/url_utils.py).
package weburl

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"

	"golang.org/x/net/http/httpproxy"
)

// Parse is url.Parse after the clean-up browsers do on a typed URL (WHATWG
// URL standard): leading control characters and spaces are dropped, tab, CR
// and LF removed anywhere, and a "%" that starts no escape stands for itself
// ("%25"). Characters net/url refuses in the user
// information (a password such as "pa^ss", "p|w", non-ASCII) are
// percent-encoded first, so u.User still yields them as typed. Unbalanced
// brackets and an unbracketed IPv6 host are refused.
func Parse(raw string) (*url.URL, error) {
	raw = strings.TrimLeft(raw, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\x0c\r\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f ")
	if strings.ContainsAny(raw, "\t\r\n") {
		raw = strings.NewReplacer("\t", "", "\r", "", "\n", "").Replace(raw)
	}
	u, err := url.Parse(escapeZone(escapeUserinfo(escapeStrayPercent(raw))))
	if err != nil {
		return nil, err
	}
	if strings.Contains(u.Host, "[") != strings.Contains(u.Host, "]") {
		return nil, errors.New("invalid IPv6 URL")
	}
	if !strings.Contains(u.Host, "[") && strings.Count(u.Host, ":") > 1 {
		return nil, errors.New("an IPv6 host must be in brackets")
	}
	return u, nil
}

// authority locates the authority ("user:pass@host:port") of raw: the text
// after "scheme://" (or a leading "//") up to the first "/", "?" or "#".
func authority(raw string) (start, end int, ok bool) {
	switch i := strings.Index(raw, "//"); {
	case i == 0:
		start = 2
	case i > 0 && raw[i-1] == ':' && !strings.ContainsAny(raw[:i-1], "/?#"):
		start = i + 2
	default:
		return 0, 0, false
	}
	end = len(raw)
	if j := strings.IndexAny(raw[start:], "/?#"); j >= 0 {
		end = start + j
	}
	return start, end, true
}

// escapeUserinfo percent-encodes the bytes of raw's user information that
// RFC 3986 does not allow there (net/url refuses them), keeping escapes.
func escapeUserinfo(raw string) string {
	start, end, ok := authority(raw)
	if !ok {
		return raw
	}
	at := strings.LastIndex(raw[start:end], "@")
	if at < 0 {
		return raw
	}
	userinfo := raw[start : start+at]
	escaped := escapeExcept(userinfo, "-._~!$&'()*+,;=:%")
	if escaped == userinfo {
		return raw
	}
	return raw[:start] + escaped + raw[start+at:]
}

// escapeZone writes the zone of a bracketed IPv6 host typed as
// "[fe80::1%eth0]" the way net/url reads it ("%25eth0").
func escapeZone(raw string) string {
	start, end, ok := authority(raw)
	if !ok {
		return raw
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
	return strings.ToValidUTF8(b.String(), "\uFFFD")
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
// Sub-Store address) plus extra query params (form-encoded, appended). It
// makes a typed URL requestable without changing what it addresses (after
// Parse's clean-up): bytes that may not appear in a
// path or query (non-ASCII, space, "|", ...) are percent-encoded while
// existing escapes stay as written (so "%2F" is never turned into "/"), and
// for http(s) dot segments are resolved (RFC 3986 §5.2.4; requests to the
// panel itself are routed without a server in front) and an empty path is
// "/".
func Prepare(raw string, params [][2]string) (*url.URL, error) {
	u, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.RawPath != "" {
		// RawPath is set when the written path differs from Go's own
		// encoding of the decoded path; keep it, made valid.
		u.RawPath = escapeExcept(u.RawPath, uriChars)
	}
	if (u.Scheme == "http" || u.Scheme == "https") && u.Opaque == "" {
		u = u.ResolveReference(&url.URL{})
		if u.Path == "" {
			u.Path = "/"
		}
	}
	u.RawQuery = escapeExcept(u.RawQuery, queryChars)
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

// uriChars are the ASCII punctuation characters RFC 3986 allows in a path or
// query (besides letters and digits), "%" for existing escapes.
const uriChars = "-._~!$&'()*+,;=:@/?%[]"

// queryChars follows requests' requote_uri treatment of square brackets in
// an existing query; brackets are quoted even though they are valid in the
// RFC 3986 query grammar.
const queryChars = "-._~!$&'()*+,;=:@/?%"

// escapeExcept percent-encodes every byte of s that is not an ASCII letter,
// digit or one of allowed.
func escapeExcept(s, allowed string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
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

// ParseProxy parses a configured proxy value ("host:port" means http://);
// its user name and password become Proxy-Authorization.
func ParseProxy(value string) (*url.URL, error) {
	raw := value
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil, ErrInvalidProxy
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
		return u, nil
	}
	return nil, ErrInvalidProxy
}

// FromEnvironment selects the proxy using Go's native environment rules.
// Localhost and loopback addresses go direct; HTTP_PROXY is refused in CGI.
// The environment is read for each call, including redirect hops.
func FromEnvironment(target *url.URL) (*url.URL, error) {
	config := httpproxy.FromEnvironment()
	all := os.Getenv("all_proxy")
	if all == "" {
		all = os.Getenv("ALL_PROXY")
	}
	// ALL_PROXY remains a user-facing fallback. CGI checks, bypasses and
	// parsing are still performed by the native ProxyFunc.
	if config.HTTPProxy == "" {
		config.HTTPProxy = all
	}
	if config.HTTPSProxy == "" {
		config.HTTPSProxy = all
	}
	return config.ProxyFunc()(target)
}
