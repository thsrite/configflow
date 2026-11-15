package rulefetch

import (
	"errors"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/rules"
)

// URL is urllib.parse.SplitResult (CPython 3.11) for a str URL.
type URL struct {
	Scheme, Netloc, Path, Query, Fragment string
}

var errInvalidIPv6 = errors.New("Invalid IPv6 URL")
var errNetloc = errors.New("netloc contains invalid characters under NFKC normalization")

const urlSchemeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+-."

// Characters whose NFKC form contains one of "/?#@:" (urllib's _checknetloc).
const netlocNFKCForbidden = "／？＃＠：﹕﹖﹟﹫︓︖℀℁℅℆⁇⁈⁉⩴"

// SplitURL is urlsplit(url, scheme=defaultScheme); the error is its
// ValueError.
func SplitURL(url, defaultScheme string) (URL, error) {
	result := URL{Scheme: defaultScheme}
	url = strings.TrimLeft(url, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\x0c\r\x0e\x0f"+
		"\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f ")
	for _, unsafe := range []string{"\t", "\r", "\n"} {
		url = strings.ReplaceAll(url, unsafe, "")
	}
	if i := strings.Index(url, ":"); i > 0 && isASCIILetter(url[0]) {
		valid := true
		for _, r := range url[:i] {
			if !strings.ContainsRune(urlSchemeChars, r) {
				valid = false
				break
			}
		}
		if valid {
			result.Scheme, url = strings.ToLower(url[:i]), url[i+1:]
		}
	}
	if strings.HasPrefix(url, "//") {
		delimiter := len(url)
		for _, c := range "/?#" {
			if at := strings.IndexRune(url[2:], c); at >= 0 && at+2 < delimiter {
				delimiter = at + 2
			}
		}
		result.Netloc, url = url[2:delimiter], url[delimiter:]
		open, closed := strings.Contains(result.Netloc, "["), strings.Contains(result.Netloc, "]")
		if open != closed {
			return result, errInvalidIPv6
		}
		if open && closed && !validBracketedNetloc(result.Netloc) {
			return result, errors.New("Invalid IPv6 URL")
		}
	}
	if before, after, found := strings.Cut(url, "#"); found {
		url, result.Fragment = before, after
	}
	if before, after, found := strings.Cut(url, "?"); found {
		url, result.Query = before, after
	}
	result.Path = url
	if !validNetloc(result.Netloc) {
		return result, errNetloc
	}
	return result, nil
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

var ipvFuture = regexp.MustCompile(`^v[a-fA-F0-9]+\..+$`)

func validBracketedNetloc(netloc string) bool {
	hostinfo := netloc
	if at := strings.LastIndex(hostinfo, "@"); at >= 0 {
		hostinfo = hostinfo[at+1:]
	}
	before, bracketed, found := strings.Cut(hostinfo, "[")
	if !found {
		hostname, _, _ := strings.Cut(hostinfo, ":")
		return validBracketedHost(hostname)
	}
	if before != "" {
		return false
	}
	hostname, port, _ := strings.Cut(bracketed, "]")
	if port != "" && !strings.HasPrefix(port, ":") {
		return false
	}
	return validBracketedHost(hostname)
}

func validBracketedHost(host string) bool {
	if strings.HasPrefix(host, "v") {
		return ipvFuture.MatchString(host) && !strings.Contains(host, "\n")
	}
	address, zone, scoped := strings.Cut(host, "%")
	if scoped && (zone == "" || strings.Contains(zone, "%")) {
		return false
	}
	parsed, err := netip.ParseAddr(address)
	return err == nil && parsed.Is6()
}

func validNetloc(netloc string) bool {
	for i := 0; i < len(netloc); i++ {
		if netloc[i] >= 0x80 {
			return !strings.ContainsAny(netloc, netlocNFKCForbidden)
		}
	}
	return true
}

func (u URL) hostinfo() (string, string, bool) {
	hostinfo := u.Netloc
	if at := strings.LastIndex(hostinfo, "@"); at >= 0 {
		hostinfo = hostinfo[at+1:]
	}
	var hostname, port string
	if _, bracketed, found := strings.Cut(hostinfo, "["); found {
		var rest string
		hostname, rest, _ = strings.Cut(bracketed, "]")
		_, port, _ = strings.Cut(rest, ":")
	} else {
		hostname, port, _ = strings.Cut(hostinfo, ":")
	}
	return hostname, port, port != ""
}

// Hostname is SplitResult.hostname ("" for None).
func (u URL) Hostname() string {
	hostname, _, _ := u.hostinfo()
	if hostname == "" {
		return ""
	}
	name, zone, found := strings.Cut(hostname, "%")
	if found {
		return strings.ToLower(name) + "%" + zone
	}
	return strings.ToLower(name)
}

// Port is SplitResult.port: the number, whether there is one, and the
// ValueError for a port that is not a number in 0..65535.
func (u URL) Port() (int, bool, error) {
	_, port, present := u.hostinfo()
	if !present {
		return 0, false, nil
	}
	for i := 0; i < len(port); i++ {
		if port[i] < '0' || port[i] > '9' {
			return 0, false, errors.New("Port could not be cast to integer value as " + quoteRepr(port))
		}
	}
	digits := strings.TrimLeft(port, "0")
	if len(digits) > 5 {
		return 0, false, errors.New("Port out of range 0-65535")
	}
	value, _ := strconv.Atoi("0" + digits)
	if value > 65535 {
		return 0, false, errors.New("Port out of range 0-65535")
	}
	return value, true, nil
}

func quoteRepr(s string) string { return "'" + s + "'" }

// userinfo returns SplitResult.username / password ("" and false for None).
func (u URL) userinfo() (user string, hasUser bool, password string, hasPassword bool) {
	at := strings.LastIndex(u.Netloc, "@")
	if at < 0 {
		return "", false, "", false
	}
	user, password, hasPassword = strings.Cut(u.Netloc[:at], ":")
	return user, true, password, hasPassword
}

// String is SplitResult.geturl() (urlunsplit).
func (u URL) String() string { return urlunsplit(u.Scheme, u.Netloc, u.Path, u.Query, u.Fragment) }

var usesNetloc = map[string]bool{
	"": true, "ftp": true, "http": true, "gopher": true, "nntp": true, "telnet": true, "imap": true, "wais": true,
	"file": true, "mms": true, "https": true, "shttp": true, "snews": true, "prospero": true, "rtsp": true,
	"rtsps": true, "rtspu": true, "rsync": true, "svn": true, "svn+ssh": true, "sftp": true, "nfs": true,
	"git": true, "git+ssh": true, "ws": true, "wss": true,
}

var usesRelative = map[string]bool{
	"": true, "ftp": true, "http": true, "gopher": true, "nntp": true, "imap": true, "wais": true, "file": true,
	"https": true, "shttp": true, "mms": true, "prospero": true, "rtsp": true, "rtspu": true, "sftp": true,
	"svn": true, "svn+ssh": true, "ws": true, "wss": true,
}

var usesParams = map[string]bool{
	"": true, "ftp": true, "hdl": true, "prospero": true, "http": true, "imap": true, "https": true,
	"shttp": true, "rtsp": true, "rtspu": true, "sip": true, "sips": true, "mms": true, "sftp": true, "tel": true,
}

func urlunsplit(scheme, netloc, url, query, fragment string) string {
	if netloc != "" || (scheme != "" && usesNetloc[scheme] && !strings.HasPrefix(url, "//")) {
		if url != "" && !strings.HasPrefix(url, "/") {
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

type parsedURL struct {
	URL
	params string
}

// urlparse is urllib.parse.urlparse(url, scheme).
func urlparse(url, scheme string) (parsedURL, error) {
	split, err := SplitURL(url, scheme)
	if err != nil {
		return parsedURL{}, err
	}
	result := parsedURL{URL: split}
	if usesParams[split.Scheme] && strings.Contains(split.Path, ";") {
		path := split.Path
		var i int
		if slash := strings.LastIndex(path, "/"); slash >= 0 {
			i = strings.Index(path[slash:], ";")
			if i >= 0 {
				i += slash
			}
		} else {
			i = strings.Index(path, ";")
		}
		if i >= 0 {
			result.Path, result.params = path[:i], path[i+1:]
		}
	}
	return result, nil
}

func urlunparse(p parsedURL) string {
	url := p.Path
	if p.params != "" {
		url += ";" + p.params
	}
	return urlunsplit(p.Scheme, p.Netloc, url, p.Query, p.Fragment)
}

// URLJoin is urllib.parse.urljoin(base, url).
func URLJoin(base, url string) (string, error) {
	if base == "" {
		return url, nil
	}
	if url == "" {
		return base, nil
	}
	b, err := urlparse(base, "")
	if err != nil {
		return "", err
	}
	u, err := urlparse(url, b.Scheme)
	if err != nil {
		return "", err
	}
	if u.Scheme != b.Scheme || !usesRelative[u.Scheme] {
		return url, nil
	}
	if usesNetloc[u.Scheme] {
		if u.Netloc != "" {
			return urlunparse(u), nil
		}
		u.Netloc = b.Netloc
	}
	if u.Path == "" && u.params == "" {
		u.Path, u.params = b.Path, b.params
		if u.Query == "" {
			u.Query = b.Query
		}
		return urlunparse(u), nil
	}
	baseParts := strings.Split(b.Path, "/")
	if baseParts[len(baseParts)-1] != "" {
		baseParts = baseParts[:len(baseParts)-1]
	}
	var segments []string
	if strings.HasPrefix(u.Path, "/") {
		segments = strings.Split(u.Path, "/")
	} else {
		segments = append(append([]string{}, baseParts...), strings.Split(u.Path, "/")...)
		if len(segments) > 2 {
			middle := []string{}
			for _, s := range segments[1 : len(segments)-1] {
				if s != "" {
					middle = append(middle, s)
				}
			}
			segments = append(append([]string{segments[0]}, middle...), segments[len(segments)-1])
		}
	}
	var resolved []string
	for _, seg := range segments {
		switch seg {
		case "..":
			if len(resolved) > 0 {
				resolved = resolved[:len(resolved)-1]
			}
		case ".":
		default:
			resolved = append(resolved, seg)
		}
	}
	if last := segments[len(segments)-1]; last == "." || last == ".." {
		resolved = append(resolved, "")
	}
	u.Path = strings.Join(resolved, "/")
	if u.Path == "" {
		u.Path = "/"
	}
	return urlunparse(u), nil
}

// ErrInvalidURL is the ValueError of validate_rule_url.
var ErrInvalidURL = errors.New("Invalid HTTP/HTTPS URL")

// ValidateRuleURL is validate_rule_url: syntax only (http/https, a host,
// a port in 1..65535, no whitespace or backslash in the netloc, no control
// characters).
func ValidateRuleURL(url string) (URL, error) {
	for _, r := range url {
		if r < 32 || r == 127 {
			return URL{}, ErrInvalidURL
		}
	}
	parsed, err := SplitURL(url, "")
	if err != nil {
		return parsed, ErrInvalidURL
	}
	port, hasPort, err := parsed.Port()
	if err != nil {
		return parsed, ErrInvalidURL
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return parsed, ErrInvalidURL
	}
	if hasPort && (port < 1 || port > 65535) {
		return parsed, ErrInvalidURL
	}
	for _, r := range parsed.Netloc {
		if rules.PyIsSpace(r) {
			return parsed, ErrInvalidURL
		}
	}
	if strings.Contains(parsed.Netloc, "\\") {
		return parsed, ErrInvalidURL
	}
	return parsed, nil
}

// ErrInvalidProxy is the ValueError of normalize_rule_fetch_proxy.
var (
	ErrProxyNotString = errors.New("规则下载代理必须是 HTTP 或 HTTPS 地址")
	ErrInvalidProxy   = errors.New("规则下载代理必须是 HTTP 或 HTTPS 地址，不能包含路径、查询参数或片段")
)

// NormalizeRuleFetchProxy is normalize_rule_fetch_proxy: "" for direct,
// otherwise scheme://netloc.
func NormalizeRuleFetchProxy(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", ErrProxyNotString
	}
	text = pyStrip(text)
	if text == "" {
		return "", nil
	}
	parsed, err := ValidateRuleURL(text)
	if err != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.Query != "" || parsed.Fragment != "" {
		return "", ErrInvalidProxy
	}
	parsed.Path = ""
	return parsed.String(), nil
}
