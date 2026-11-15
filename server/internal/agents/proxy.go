package agents

// The proxy choice of requests 2.31 for a Session with trust_env=True (every
// manager call; monitor/panel requests set NoProxy): merge_environment_settings
// → get_environ_proxies(url) = should_bypass_proxies(url) ? {} :
// urllib.request.getproxies_environment(), then HTTPAdapter.get_connection →
// select_proxy, prepend_scheme_if_needed, urllib3 parse_url and
// proxy_manager_for (Proxy-Authorization from the proxy's userinfo). Unlike
// Go's http.ProxyFromEnvironment there is no automatic bypass of localhost or
// loopback addresses, ALL_PROXY counts, lowercase variables win, NO_PROXY is
// matched both by requests' own rules (suffixes, CIDR for IPv4 hosts, host:port)
// and by urllib's proxy_bypass_environment, and the environment is read on
// every request. Reference: testdata/gen_proxy_reference.py (real requests 2.31
// / urllib3 2.8 on CPython 3.11).

import (
	"encoding/base64"
	"errors"
	"math/big"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/urlx"
)

// environ is os.environ (a variable for tests).
var environ = os.Environ

// envPairs is os.environ in its order; a repeated name keeps its first
// value (CPython builds os.environ with PyDict_SetDefault).
func envPairs() [][2]string {
	var out [][2]string
	seen := map[string]bool{}
	for _, entry := range environ() {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, [2]string{name, value})
	}
	return out
}

func envGet(pairs [][2]string, name string) (string, bool) {
	for _, pair := range pairs {
		if pair[0] == name {
			return pair[1], true
		}
	}
	return "", false
}

// getproxiesEnvironment is urllib.request.getproxies_environment (3.11): every
// variable ending in _proxy (any case) in a first pass, then the lowercase
// ones again, an empty lowercase value removing the entry; HTTP_PROXY is
// ignored when REQUEST_METHOD is set (CGI).
func getproxiesEnvironment(pairs [][2]string) map[string]string {
	proxies := map[string]string{}
	for _, pair := range pairs {
		name := pyLower(pair[0])
		if pair[1] != "" && strings.HasSuffix(name, "_proxy") {
			proxies[name[:len(name)-6]] = pair[1]
		}
	}
	if _, ok := envGet(pairs, "REQUEST_METHOD"); ok {
		delete(proxies, "http")
	}
	for _, pair := range pairs {
		if strings.HasSuffix(pair[0], "_proxy") {
			name := pyLower(pair[0])
			if pair[1] != "" {
				proxies[name[:len(name)-6]] = pair[1]
			} else {
				delete(proxies, name[:len(name)-6])
			}
		}
	}
	return proxies
}

// lowerForMatch lowercases a NO_PROXY entry for comparison with an ASCII
// host: str.lower() maps only U+212A (Kelvin sign) from outside ASCII into
// ASCII; every other non-ASCII character stays non-ASCII and can never match,
// so it is kept as it is.
func lowerForMatch(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r + 32
		case r == 0x212a:
			return 'k'
		}
		return r
	}, s)
}

// urlHost is urllib.parse.urlparse(url).hostname / .port of a prepared URL:
// the netloc after the userinfo, brackets removed, lowercased up to a "%"
// zone; ok is false when there is no hostname.
func urlHost(raw string) (scheme, hostname, port string, ok bool) {
	if i := strings.Index(raw, ":"); i > 0 {
		scheme = strings.ToLower(raw[:i])
		raw = raw[i+1:]
	}
	if !strings.HasPrefix(raw, "//") {
		return scheme, "", "", false
	}
	netloc := raw[2:]
	if i := strings.IndexAny(netloc, "/?#"); i >= 0 {
		netloc = netloc[:i]
	}
	if i := strings.LastIndex(netloc, "@"); i >= 0 {
		netloc = netloc[i+1:]
	}
	if _, bracketed, found := strings.Cut(netloc, "["); found {
		var rest string
		hostname, rest, _ = strings.Cut(bracketed, "]")
		_, port, _ = strings.Cut(rest, ":")
	} else {
		hostname, port, _ = strings.Cut(netloc, ":")
	}
	if hostname == "" {
		return scheme, "", "", false
	}
	name, zone, percent := strings.Cut(hostname, "%")
	hostname = pyLower(name)
	if percent {
		hostname += "%" + zone
	}
	// Not reproduced: requests' prepare_url sends the IDNA form of a
	// non-ASCII host, so NO_PROXY entries would be compared with its
	// punycode (golang.org/x/net/idna needs x/text, not a dependency).
	return scheme, hostname, port, true
}

// inetAton is socket.inet_aton (glibc inet_aton): the numeric forms of
// inetAtonExact, anything after ASCII whitespace ignored.
func inetAton(s string) (uint32, bool) {
	if i := strings.IndexAny(s, " \t\n\v\f\r"); i >= 0 {
		s = s[:i]
	}
	text, ok := inetAtonExact(s)
	if !ok {
		return 0, false
	}
	parts := strings.Split(text, ".")
	var address uint32
	for _, part := range parts {
		n, _ := strconv.Atoi(part)
		address = address<<8 | uint32(n)
	}
	return address, true
}

// isValidCIDR is requests.utils.is_valid_cidr.
func isValidCIDR(network string) (uint32, uint32, bool) {
	if strings.Count(network, "/") != 1 {
		return 0, 0, false
	}
	address, bits, _ := strings.Cut(network, "/")
	mask, ok := pyIntText(bits)
	if !ok || mask.Sign() < 1 || mask.Cmp(bigInt32) > 0 {
		return 0, 0, false
	}
	value, ok := inetAton(address)
	if !ok {
		return 0, 0, false
	}
	return value, ^uint32(0) << (32 - mask.Uint64()), true
}

var bigInt32 = big.NewInt(32)

// shouldBypassProxies is requests.utils.should_bypass_proxies(url, None).
func shouldBypassProxies(pairs [][2]string, prepared string) bool {
	_, hostname, port, ok := urlHost(prepared)
	if !ok {
		return true
	}
	noProxy, _ := envGet(pairs, "no_proxy")
	if noProxy == "" {
		noProxy, _ = envGet(pairs, "NO_PROXY")
	}
	if noProxy != "" {
		var entries []string
		for _, host := range strings.Split(strings.ReplaceAll(noProxy, " ", ""), ",") {
			if host != "" {
				entries = append(entries, host)
			}
		}
		if ip, isIPv4 := inetAton(hostname); isIPv4 {
			for _, entry := range entries {
				if network, mask, valid := isValidCIDR(entry); valid {
					if ip&mask == network&mask {
						return true
					}
				} else if hostname == entry {
					return true
				}
			}
		} else {
			hostWithPort := hostname
			if n, err := strconv.Atoi(port); err == nil && n != 0 {
				hostWithPort += ":" + strconv.Itoa(n)
			}
			for _, entry := range entries {
				if strings.HasSuffix(hostname, entry) || strings.HasSuffix(hostWithPort, entry) {
					return true
				}
			}
		}
	}
	return proxyBypassEnvironment(getproxiesEnvironment(pairs), hostname)
}

// proxyBypassEnvironment is urllib.request.proxy_bypass_environment(host).
func proxyBypassEnvironment(proxies map[string]string, host string) bool {
	noProxy, ok := proxies["no"]
	if !ok {
		return false
	}
	if noProxy == "*" {
		return true
	}
	host = lowerForMatch(host)
	hostOnly := host
	// _splitport: (.*):([0-9]*) — the part before the last ":" when only
	// ASCII digits follow it.
	if i := strings.LastIndex(host, ":"); i >= 0 && strings.Trim(host[i+1:], "0123456789") == "" {
		hostOnly = host[:i]
	}
	for _, name := range strings.Split(noProxy, ",") {
		name = pyStrip(name)
		if name == "" {
			continue
		}
		name = lowerForMatch(strings.TrimLeft(name, "."))
		if hostOnly == name || host == name {
			return true
		}
		name = "." + name
		if strings.HasSuffix(hostOnly, name) || strings.HasSuffix(host, name) {
			return true
		}
	}
	return false
}

// urllib3 2.x util/url.py patterns.
const (
	u3IPv4Pat     = `(?:[0-9]{1,3}\.){3}[0-9]{1,3}`
	u3Hex         = `[0-9A-Fa-f]{1,4}`
	u3Ls32        = `(?:` + u3Hex + `:` + u3Hex + `|` + u3IPv4Pat + `)`
	u3IPv6Pat     = `(?:(?:` + u3Hex + `:){6}` + u3Ls32 + `|::(?:` + u3Hex + `:){5}` + u3Ls32 + `|(?:` + u3Hex + `)?::(?:` + u3Hex + `:){4}` + u3Ls32 + `|(?:(?:` + u3Hex + `:)?` + u3Hex + `)?::(?:` + u3Hex + `:){3}` + u3Ls32 + `|(?:(?:` + u3Hex + `:){0,2}` + u3Hex + `)?::(?:` + u3Hex + `:){2}` + u3Ls32 + `|(?:(?:` + u3Hex + `:){0,3}` + u3Hex + `)?::` + u3Hex + `:` + u3Ls32 + `|(?:(?:` + u3Hex + `:){0,4}` + u3Hex + `)?::` + u3Ls32 + `|(?:(?:` + u3Hex + `:){0,5}` + u3Hex + `)?::` + u3Hex + `|(?:(?:` + u3Hex + `:){0,6}` + u3Hex + `)?::)`
	u3Unreserved  = `A-Za-z0-9._\-~`
	u3ZoneIDPat   = `(?:%25|%)(?:[` + u3Unreserved + `]|%[a-fA-F0-9]{2})+`
	u3IPv6AddrZ   = `\[` + u3IPv6Pat + `(?:` + u3ZoneIDPat + `)?\]`
	u3RegNamePat  = `(?:[^\[\]%:/?#]|%[a-fA-F0-9]{2})*`
	u3HostPortPat = `(?s)^(` + u3RegNamePat + `|` + u3IPv4Pat + `|` + u3IPv6AddrZ + `)(?::0*?(|0|[1-9][0-9]{0,4}))?$`
)

var (
	u3SchemeRE      = regexp.MustCompile(`^(?:[a-zA-Z][a-zA-Z0-9+-]*:|/)`)
	u3URIRE         = regexp.MustCompile(`(?s)^(?:([a-zA-Z][a-zA-Z0-9+.-]*):)?(?://([^\\/?#]*))?([^?#]*)(?:\?([^#]*))?(?:#(.*))?$`)
	u3HostPortRE    = regexp.MustCompile(u3HostPortPat)
	u3HostInvalidRE = regexp.MustCompile(`[\x00-\x20\x7f]`)
	u3IPv4RE        = regexp.MustCompile(`^(?:0[xX][0-9a-fA-F]+|[0-9]+)(?:\.(?:0[xX][0-9a-fA-F]+|[0-9]+)){0,3}$`)
	u3IPv6AddrZRE   = regexp.MustCompile(`^` + u3IPv6AddrZ + `$`)
	u3PercentRE     = regexp.MustCompile(`%[a-fA-F0-9]{2}`)
)

// errInvalidURL, errInvalidProxyURL and errInvalidSchema are requests'
// InvalidURL (urllib3 LocationParseError / ProxySchemeUnknown in
// get_connection), InvalidProxyURL and InvalidSchema (SOCKS without PySocks).
var (
	errInvalidURL      = errors.New("InvalidURL: proxy URL could not be parsed")
	errInvalidProxyURL = errors.New("InvalidProxyURL: Please check proxy URL. It is malformed and could be missing the host.")
	errInvalidSchema   = errors.New("InvalidSchema: Missing dependencies for SOCKS support.")
)

// envProxy is the proxy requests would use: URL (scheme://host:port with
// urllib3's host text) and the Proxy-Authorization value.
type envProxy struct {
	scheme, host string
	port         int
	auth         string
}

func (p *envProxy) String() string { return p.scheme + "://" + p.host + ":" + strconv.Itoa(p.port) }

// URL is the proxy for http.Transport.
func (p *envProxy) URL() *url.URL {
	host := strings.TrimSuffix(strings.TrimPrefix(p.host, "["), "]")
	return &url.URL{Scheme: p.scheme, Host: joinHostPort(host, p.port)}
}

func joinHostPort(host string, port int) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + strconv.Itoa(port)
	}
	return host + ":" + strconv.Itoa(port)
}

// environProxy is get_environ_proxies + get_connection for a prepared URL:
// nil when the request goes direct.
func environProxy(prepared string) (*envProxy, error) {
	pairs := envPairs()
	if shouldBypassProxies(pairs, prepared) {
		return nil, nil
	}
	proxies := getproxiesEnvironment(pairs)
	scheme, hostname, _, ok := urlHost(prepared)
	var keys []string
	if ok {
		keys = []string{scheme + "://" + hostname, scheme, "all://" + hostname, "all"}
	} else {
		keys = []string{scheme, "all"}
	}
	value := ""
	for _, key := range keys {
		if v, found := proxies[key]; found {
			value = v
			break
		}
	}
	if value == "" {
		return nil, nil
	}
	return proxyFromValue(value)
}

// proxyFromValue is prepend_scheme_if_needed(value, "http"), parse_url and
// proxy_manager_for. A host that the first parse_url does not find never
// comes back from prepend_scheme_if_needed's netloc/path swap (the path then
// starts with "/"), so it is InvalidProxyURL.
func proxyFromValue(value string) (*envProxy, error) {
	raw := value
	if !u3SchemeRE.MatchString(raw) {
		raw = "//" + raw
	}
	m := u3URIRE.FindStringSubmatchIndex(raw)
	group := func(i int) (string, bool) {
		if m[2*i] < 0 {
			return "", false
		}
		return raw[m[2*i]:m[2*i+1]], true
	}
	scheme, hasScheme := group(1)
	scheme = strings.ToLower(scheme)
	normalize := !hasScheme || scheme == "http" || scheme == "https"
	authority, _ := group(2)
	if authority == "" {
		return nil, errInvalidProxyURL
	}
	auth, hostPort := "", authority
	if i := strings.LastIndex(authority, "@"); i >= 0 {
		auth, hostPort = authority[:i], authority[i+1:]
	}
	hp := u3HostPortRE.FindStringSubmatchIndex(hostPort)
	if hp == nil {
		return nil, errInvalidURL
	}
	host := hostPort[hp[2]:hp[3]]
	port := -1
	if hp[4] >= 0 && hp[5] > hp[4] {
		n, err := strconv.Atoi(hostPort[hp[4]:hp[5]])
		if err != nil || n > 65535 {
			return nil, errInvalidURL
		}
		port = n
	}
	if host != "" {
		if u3HostInvalidRE.MatchString(host) {
			return nil, errInvalidURL
		}
		if normalize {
			var err error
			if host, err = normalizeProxyHost(host); err != nil {
				return nil, err
			}
		}
	}
	if host == "" {
		return nil, errInvalidProxyURL
	}
	if !hasScheme {
		scheme = "http"
	}
	if strings.HasPrefix(scheme, "socks") {
		return nil, errInvalidSchema
	}
	if scheme != "http" && scheme != "https" {
		return nil, errInvalidURL
	}
	if port < 0 {
		port = 80
		if scheme == "https" {
			port = 443
		}
	}
	proxy := &envProxy{scheme: scheme, host: host, port: port}
	if auth != "" {
		header, err := proxyAuthorization(auth)
		if err != nil {
			return nil, err
		}
		proxy.auth = header
	}
	return proxy, nil
}

// normalizeProxyHost is urllib3's _normalize_host for http(s): bracketed IPv6
// lowercased, numeric IPv4 kept, names lowercased (IDNA for non-ASCII labels).
func normalizeProxyHost(host string) (string, error) {
	if u3IPv6AddrZRE.MatchString(host) {
		if i := strings.Index(host, "%"); i >= 0 {
			zone := host[i:]
			if strings.HasPrefix(zone, "%25") && zone != "%25]" {
				zone = "%" + zone[3:]
			}
			return strings.ToLower(host[:i]) + zone, nil
		}
		return strings.ToLower(host), nil
	}
	if u3IPv4RE.MatchString(host) {
		return host, nil
	}
	if strings.Contains(host, "%") {
		if strings.Count(host, "%") != len(u3PercentRE.FindAllString(host, -1)) {
			return "", errInvalidURL
		}
	}
	labels := strings.Split(host, ".")
	for i, label := range labels {
		if isASCII(label) {
			labels[i] = strings.ToLower(label)
			continue
		}
		// Not reproduced: urllib3 IDNA-encodes the label (strict IDNA 2008);
		// the lowercased name then fails Go's name resolution instead.
		labels[i] = pyLower(label)
	}
	return strings.Join(labels, "."), nil
}

// proxyAuthorization is HTTPAdapter.proxy_headers(proxy): get_auth_from_url of
// the rebuilt proxy URL (urllib3 percent-encodes the userinfo; when any "%"
// in it is not a valid escape every "%" is encoded, so it reads back
// literally) and _basic_auth_str (latin-1). No header without a password or
// with an empty user name.
func proxyAuthorization(auth string) (string, error) {
	user, password, hasPassword := strings.Cut(auth, ":")
	if !hasPassword {
		return "", nil
	}
	decode := urlx.Unquote
	if strings.Count(auth, "%") != len(u3PercentRE.FindAllString(auth, -1)) {
		decode = func(s string) string { return s }
	}
	user, password = decode(user), decode(password)
	if user == "" {
		return "", nil
	}
	credentials, ok := latin1(user + ":" + password)
	if !ok {
		return "", errors.New("UnicodeEncodeError: 'latin-1' codec can't encode characters in the proxy credentials")
	}
	return "Basic " + base64.StdEncoding.EncodeToString(credentials), nil
}
