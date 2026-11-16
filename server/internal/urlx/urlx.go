// Package urlx renders URLs for log lines without credentials. It is the port
// of backend/utils/url_utils.py and reproduces the parts of Python's
// urllib.parse (3.11) that function relies on, quirk for quirk, because the
// redaction result must not depend on which implementation parsed the URL.
package urlx

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

const (
	redacted         = "[REDACTED]"
	invalid          = "[INVALID URL REDACTED]"
	maxDecodePasses  = 8
	hexDigits        = "0123456789abcdefABCDEF"
	uppercaseHex     = "0123456789ABCDEF"
	whatwgC0OrSpace  = "\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\x0c\r\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f "
	schemeChars      = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+-."
	alwaysSafeQuoter = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.-~"
)

var sensitiveKeyParts = []string{"token", "authorization", "auth", "key", "secret", "password", "passwd", "credential"}

var usesNetloc = map[string]bool{
	"": true, "ftp": true, "http": true, "gopher": true, "nntp": true, "telnet": true, "imap": true, "wais": true,
	"file": true, "mms": true, "https": true, "shttp": true, "snews": true, "prospero": true, "rtsp": true,
	"rtsps": true, "rtspu": true, "rsync": true, "svn": true, "svn+ssh": true, "sftp": true, "nfs": true,
	"git": true, "git+ssh": true, "ws": true, "wss": true,
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]`)
var ipvFuture = regexp.MustCompile(`\Av[a-fA-F0-9]+\..+\z`)
var exceptionNameChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

var errInvalid = errors.New("invalid URL")

// hasControl is _has_control: C0 characters and DEL.
func hasControl(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 32 || value[i] == 127 {
			return true
		}
	}
	return false
}

// badPercentEscape is _BAD_PERCENT_ESCAPE.search: a "%" not followed by two hex digits.
func badPercentEscape(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] != '%' {
			continue
		}
		if i+2 >= len(value) || !isHex(value[i+1]) || !isHex(value[i+2]) {
			return true
		}
	}
	return false
}

// Unquote is urllib.parse.unquote (utf-8, errors="replace").
func Unquote(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var raw []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			raw = append(raw, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
			continue
		}
		raw = append(raw, s[i])
	}
	return decodeUTF8Replace(raw)
}

// UnquotePlus is urllib.parse.unquote_plus.
func UnquotePlus(s string) string {
	return Unquote(strings.ReplaceAll(s, "+", " "))
}

// UnquotePlusURLQuote is unquote(s.replace("+", " "), errors="werkzeug.url_quote"),
// how Werkzeug 3 decodes query names and values (request.args): the bytes of
// every maximal invalid UTF-8 subpart are re-quoted as upper-case %XX
// instead of becoming U+FFFD.
func UnquotePlusURLQuote(s string) string {
	s = strings.ReplaceAll(s, "+", " ")
	if !strings.Contains(s, "%") {
		return s
	}
	var raw []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			raw = append(raw, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
			continue
		}
		raw = append(raw, s[i])
	}
	if utf8.Valid(raw) {
		return string(raw)
	}
	var out strings.Builder
	for i := 0; i < len(raw); {
		r, size := utf8.DecodeRune(raw[i:])
		if r != utf8.RuneError || size > 1 {
			out.Write(raw[i : i+size])
			i += size
			continue
		}
		n := invalidSubpartLength(raw[i:])
		for _, c := range raw[i : i+n] {
			out.WriteByte('%')
			out.WriteByte(uppercaseHex[c>>4])
			out.WriteByte(uppercaseHex[c&15])
		}
		i += n
	}
	return out.String()
}

func isHex(c byte) bool { return strings.IndexByte(hexDigits, c) >= 0 }

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// decodeUTF8Replace is bytes.decode("utf-8", "replace"): every maximal
// invalid subpart becomes one U+FFFD.
func decodeUTF8Replace(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || size > 1 {
			out.WriteRune(r)
			i += size
			continue
		}
		out.WriteRune(utf8.RuneError)
		i += invalidSubpartLength(b[i:])
	}
	return out.String()
}

// invalidSubpartLength returns how many bytes of the invalid sequence at the
// start of b form one maximal subpart of a well-formed sequence.
func invalidSubpartLength(b []byte) int {
	lead := b[0]
	var need int
	lo, hi := byte(0x80), byte(0xBF)
	switch {
	case lead >= 0xC2 && lead <= 0xDF:
		need = 1
	case lead == 0xE0:
		need, lo = 2, 0xA0
	case lead >= 0xE1 && lead <= 0xEC, lead == 0xEE, lead == 0xEF:
		need = 2
	case lead == 0xED:
		need, hi = 2, 0x9F
	case lead == 0xF0:
		need, lo = 3, 0x90
	case lead >= 0xF1 && lead <= 0xF3:
		need = 3
	case lead == 0xF4:
		need, hi = 3, 0x8F
	default:
		return 1
	}
	n := 1
	for n <= need && n < len(b) {
		if b[n] < lo || b[n] > hi {
			break
		}
		lo, hi = 0x80, 0xBF
		n++
	}
	return n
}

// Quote is urllib.parse.quote with the given extra safe characters.
func quote(s, safe string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(alwaysSafeQuoter, c) >= 0 || strings.IndexByte(safe, c) >= 0 {
			out.WriteByte(c)
		} else {
			out.WriteByte('%')
			out.WriteByte(uppercaseHex[c>>4])
			out.WriteByte(uppercaseHex[c&15])
		}
	}
	return out.String()
}

// Quote is urllib.parse.quote(s, safe=safe) of a UTF-8 string.
func Quote(s, safe string) string { return quote(s, safe) }

// QuotePlus is urllib.parse.quote_plus (safe="").
func QuotePlus(s string) string {
	if !strings.Contains(s, " ") {
		return quote(s, "")
	}
	return strings.ReplaceAll(quote(s, " "), " ", "+")
}

type pair struct{ key, value string }

// parseQSL is parse_qsl(qs, keep_blank_values=True).
func parseQSL(qs string) []pair {
	if qs == "" {
		return nil
	}
	var out []pair
	for _, nameValue := range strings.Split(qs, "&") {
		if nameValue == "" {
			continue
		}
		name, value, _ := strings.Cut(nameValue, "=")
		out = append(out, pair{UnquotePlus(name), UnquotePlus(value)})
	}
	return out
}

func urlencode(query []pair) string {
	parts := make([]string, len(query))
	for i, p := range query {
		parts[i] = QuotePlus(p.key) + "=" + QuotePlus(p.value)
	}
	return strings.Join(parts, "&")
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

type splitResult struct {
	scheme, netloc, path, query, fragment string
}

// hostinfo is SplitResult._hostinfo.
func (r splitResult) hostinfo() (hostname, port string) {
	hostinfo := r.netloc
	if i := strings.LastIndex(hostinfo, "@"); i >= 0 {
		hostinfo = hostinfo[i+1:]
	}
	if _, bracketed, ok := strings.Cut(hostinfo, "["); ok {
		hostname, port, _ = strings.Cut(bracketed, "]")
		_, port, _ = strings.Cut(port, ":")
	} else {
		hostname, port, _ = strings.Cut(hostinfo, ":")
	}
	return hostname, port
}

// Hostname is SplitResult.hostname ("" for None).
func (r splitResult) hostname() string {
	hostname, _ := r.hostinfo()
	if hostname == "" {
		return ""
	}
	name, zone, found := strings.Cut(hostname, "%")
	hostname = pyLower(name)
	if found {
		hostname += "%" + zone
	}
	return hostname
}

// pyLower is str.lower() (CPython 3.11 tables, full case mapping, Final_Sigma).
func pyLower(s string) string { return pystr.Lower(s) }

// port is SplitResult.port; hasPort is false for None.
func (r splitResult) port() (port int, hasPort bool, err error) {
	_, text := r.hostinfo()
	if text == "" {
		return 0, false, nil
	}
	if !isASCII(text) || !allDigits(text) {
		return 0, false, fmt.Errorf("Port could not be cast to integer value as %q", text)
	}
	value, convErr := strconv.Atoi(text)
	if convErr != nil || value < 0 || value > 65535 {
		return 0, false, errors.New("Port out of range 0-65535")
	}
	return value, true, nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

func checkBracketedHost(hostname string) error {
	if strings.HasPrefix(hostname, "v") {
		if !ipvFuture.MatchString(hostname) {
			return errors.New("IPvFuture address is invalid")
		}
		return nil
	}
	// ipaddress.ip_address: IPv4Address, then IPv6Address (which refuses an
	// empty scope id and one containing "%"), else its ValueError text.
	notAddress := fmt.Errorf("%s does not appear to be an IPv4 or IPv6 address", jsonx.Repr(hostname))
	if _, zone, found := strings.Cut(hostname, "%"); found && (zone == "" || strings.Contains(zone, "%")) {
		return notAddress
	}
	addr, err := netip.ParseAddr(hostname)
	if err != nil {
		return notAddress
	}
	if addr.Is4() {
		return errors.New("An IPv4 address cannot be in brackets")
	}
	return nil
}

func checkBracketedNetloc(netloc string) error {
	hostnameAndPort := netloc
	if i := strings.LastIndex(netloc, "@"); i >= 0 {
		hostnameAndPort = netloc[i+1:]
	}
	var hostname string
	if before, bracketed, ok := strings.Cut(hostnameAndPort, "["); ok {
		if before != "" {
			return errors.New("Invalid IPv6 URL")
		}
		var port string
		hostname, port, _ = strings.Cut(bracketed, "]")
		if port != "" && !strings.HasPrefix(port, ":") {
			return errors.New("Invalid IPv6 URL")
		}
	} else {
		hostname, _, _ = strings.Cut(hostnameAndPort, ":")
	}
	return checkBracketedHost(hostname)
}

func checkNetloc(netloc string) error {
	if netloc == "" || isASCII(netloc) {
		return nil
	}
	for _, r := range netloc {
		if netlocUnsafe[r] {
			return fmt.Errorf("netloc '%s' contains invalid characters under NFKC normalization", netloc)
		}
	}
	return nil
}

// urlsplit is urllib.parse.urlsplit(url) for str (Python 3.11.17).
func urlsplit(url string) (splitResult, error) {
	url = strings.TrimLeft(url, whatwgC0OrSpace)
	for _, c := range []string{"\t", "\r", "\n"} {
		url = strings.ReplaceAll(url, c, "")
	}
	var r splitResult
	if i := strings.Index(url, ":"); i > 0 && isASCIILetter(url[0]) {
		ok := true
		for j := 0; j < i; j++ {
			if strings.IndexByte(schemeChars, url[j]) < 0 {
				ok = false
				break
			}
		}
		if ok {
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
		open, close := strings.Contains(r.netloc, "["), strings.Contains(r.netloc, "]")
		if open != close {
			return r, errors.New("Invalid IPv6 URL")
		}
		if open && close {
			if err := checkBracketedNetloc(r.netloc); err != nil {
				return r, err
			}
		}
	}
	if i := strings.Index(url, "#"); i >= 0 {
		url, r.fragment = url[:i], url[i+1:]
	}
	if i := strings.Index(url, "?"); i >= 0 {
		url, r.query = url[:i], url[i+1:]
	}
	if err := checkNetloc(r.netloc); err != nil {
		return r, err
	}
	r.path = url
	return r, nil
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// urlunsplit is urllib.parse.urlunsplit.
func urlunsplit(scheme, netloc, url, query, fragment string) string {
	if netloc != "" || (scheme != "" && usesNetloc[scheme]) || strings.HasPrefix(url, "//") {
		if url != "" && url[0] != '/' {
			url = "/" + url
		}
		url = "//" + netloc + url
	}
	if scheme != "" {
		url = scheme + ":" + url
	}
	if query != "" {
		url += "?" + query
	}
	if fragment != "" {
		url += "#" + fragment
	}
	return url
}

func decodeLayers(value string, plus bool) (string, error) {
	decoder := Unquote
	if plus {
		decoder = UnquotePlus
	}
	for i := 0; i < maxDecodePasses; i++ {
		if hasControl(value) || badPercentEscape(value) {
			return "", errors.New("unsafe encoded value")
		}
		decoded := decoder(value)
		if decoded == value {
			return value, nil
		}
		value = decoded
	}
	if decoder(value) != value {
		return "", errors.New("too many encoding layers")
	}
	if hasControl(value) || badPercentEscape(value) {
		return "", errors.New("unsafe decoded value")
	}
	return value, nil
}

func isSensitiveKey(key string) (bool, error) {
	decoded, err := decodeLayers(key, false)
	if err != nil {
		return false, err
	}
	normalized := nonAlnum.ReplaceAllString(pystr.Lower(decoded), "")
	for _, part := range sensitiveKeyParts {
		if strings.Contains(normalized, part) {
			return true, nil
		}
	}
	return false, nil
}

func containsSensitiveExpression(value string) (bool, error) {
	for _, component := range splitAny(value, "?&;") {
		key, _, found := strings.Cut(component, "=")
		if found {
			sensitive, err := isSensitiveKey(pyStrip(key))
			if err != nil {
				return false, err
			}
			if sensitive {
				return true, nil
			}
		}
	}
	return false, nil
}

// splitAny is re.split("[...]", value) for single-character classes.
func splitAny(value, separators string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(value); i++ {
		if strings.IndexByte(separators, value[i]) >= 0 {
			parts = append(parts, value[start:i])
			start = i + 1
		}
	}
	return append(parts, value[start:])
}

// pyStrip is str.strip() (Unicode whitespace).
func pyStrip(s string) string {
	return strings.TrimFunc(s, isPySpace)
}

func isPySpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\x0b', '\x0c', 0x1c, 0x1d, 0x1e, 0x1f, 0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func sanitizeNestedValue(value string, depth int) (string, error) {
	decoded, err := decodeLayers(value, true)
	if err != nil {
		return "", err
	}
	sensitive, err := containsSensitiveExpression(decoded)
	if err != nil {
		return "", err
	}
	if sensitive {
		return redacted, nil
	}
	if depth < 2 && (strings.Contains(decoded, "://") || strings.HasPrefix(decoded, "/")) && strings.Contains(decoded, "?") {
		nested, err := safeURL(decoded, depth+1)
		if err != nil {
			return "", err
		}
		if strings.Contains(nested, redacted) {
			return redacted, nil
		}
		return nested, nil
	}
	return decoded, nil
}

func safeURL(value string, depth int) (string, error) {
	if value == "" || hasControl(value) {
		return "", errInvalid
	}
	if badPercentEscape(value) {
		return "", errors.New("invalid percent escape")
	}
	decoded, err := decodeLayers(value, false)
	if err != nil {
		return "", err
	}
	parsed, err := urlsplit(decoded)
	if err != nil {
		return "", err
	}
	hostname := parsed.hostname()
	if (parsed.scheme == "http" || parsed.scheme == "https") && hostname == "" {
		return "", errors.New("URL has no host")
	}
	if parsed.scheme == "" && !strings.HasPrefix(decoded, "/") {
		return "", errors.New("URL is neither absolute nor relative")
	}
	if strings.Contains(hostname, ":") && !strings.HasPrefix(hostname, "[") {
		hostname = "[" + hostname + "]"
	}
	port, hasPort, err := parsed.port()
	if err != nil {
		return "", err
	}
	netloc := hostname
	if hasPort {
		netloc += ":" + strconv.Itoa(port)
	}

	var query []pair
	for _, p := range parseQSL(parsed.query) {
		decodedKey, err := decodeLayers(p.key, false)
		if err != nil {
			return "", err
		}
		sensitive, err := isSensitiveKey(decodedKey)
		if err != nil {
			return "", err
		}
		if sensitive {
			query = append(query, pair{decodedKey, redacted})
			continue
		}
		clean, err := sanitizeNestedValue(p.value, depth)
		if err != nil {
			return "", err
		}
		query = append(query, pair{decodedKey, clean})
	}
	// Fragments are commonly used for credentials by subscription providers;
	// they are dropped entirely.
	return urlunsplit(parsed.scheme, netloc, parsed.path, urlencode(query), ""), nil
}

// SafeURLForLog is safe_url_for_log: a log-safe URL, failing closed for
// malformed input. User information and fragments are removed, sensitive
// query keys keep their names but lose their values, and several percent
// encoding layers are decoded before inspection so encoding cannot bypass
// the redaction.
func SafeURLForLog(url string) string {
	out, err := safeURL(url, 0)
	if err != nil {
		return invalid
	}
	return out
}

// StatusError lets SafeExceptionDetails report an HTTP status, like the
// `response.status_code` attribute of requests exceptions.
type StatusError interface {
	error
	HTTPStatus() int
}

// PythonClassError is an error that knows the Python exception class it
// stands for (yamlx.Error, rulefetch's fetch errors, ...). SafeExceptionDetails
// prints that class name instead of the Go type, like type(error).__name__.
type PythonClassError interface {
	error
	PythonClass() string
}

// SafeExceptionDetails is safe_exception_details: bounded exception metadata
// without the error's message or URL. The type name is the PythonClass() of
// the first error in the chain that has one, else Go's %T ("*url.Error",
// cleaned to "url.Error"), cleaned and cut to 80 characters like Python's.
func SafeExceptionDetails(err error) string {
	typeName := fmt.Sprintf("%T", err)
	var classed PythonClassError
	if errors.As(err, &classed) {
		typeName = classed.PythonClass()
	}
	name := exceptionNameChars.ReplaceAllString(typeName, "")
	if len(name) > 80 {
		name = name[:80]
	}
	if name == "" {
		name = "Exception"
	}
	details := "exception_type=" + name
	var status StatusError
	for current := err; current != nil; current = errors.Unwrap(current) {
		if s, ok := current.(StatusError); ok {
			status = s
			break
		}
	}
	if status != nil {
		details += " status=" + strconv.Itoa(status.HTTPStatus())
	}
	return details
}
