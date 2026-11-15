package urlx

import (
	"regexp"
	"strconv"
	"strings"
)

// Copied from internal/substore (C13, verified there against requests on a
// corpus) so that localhttp can use it; substore can switch to this one.

// RequestsPrepareURL is requests' URL preparation (PreparedRequest.prepare_url
// with urllib3 2.x parse_url): scheme and host lowercased, dot segments
// removed, invalid characters of path/query/fragment percent-encoded and
// existing escapes upper-cased, an empty path made "/", params appended to
// the query (urlencode), then requote_uri. A URL with a colon that does not
// start with "http" is left as it is (requests skips preparation there).
// IDNA encoding of non-ASCII hosts is not reproduced.
func RequestsPrepareURL(raw string, params [][2]string) (string, error) {
	if strings.Contains(raw, ":") && !strings.HasPrefix(asciiLower(raw), "http") {
		return raw, nil
	}
	if !schemeRE.MatchString(raw) {
		raw = "//" + raw
	}
	m := uriRE.FindStringSubmatchIndex(raw)
	group := func(i int) (string, bool) {
		if m[2*i] < 0 {
			return "", false
		}
		return raw[m[2*i]:m[2*i+1]], true
	}
	scheme, _ := group(1)
	authority, hasAuthority := group(2)
	path, _ := group(3)
	query, _ := group(4)
	fragment, _ := group(5)
	normalize := scheme == "" || asciiLower(scheme) == "http" || asciiLower(scheme) == "https"
	scheme = asciiLower(scheme)
	netloc := ""
	if hasAuthority && authority != "" {
		auth, hostPort := "", authority
		if i := strings.LastIndex(authority, "@"); i >= 0 {
			auth, hostPort = authority[:i], authority[i+1:]
		}
		host, port := hostPort, ""
		if strings.HasPrefix(hostPort, "[") {
			if i := strings.Index(hostPort, "]"); i >= 0 {
				host, port = hostPort[:i+1], strings.TrimPrefix(hostPort[i+1:], ":")
			}
		} else if i := strings.LastIndex(hostPort, ":"); i >= 0 {
			host, port = hostPort[:i], hostPort[i+1:]
		}
		if auth != "" && normalize {
			auth = encodeInvalidURLChars(auth, userinfoChars)
		}
		if normalize {
			host = asciiLower(host)
		}
		if auth != "" {
			netloc = auth + "@"
		}
		netloc += host
		// urllib3 parses the port as an int; requests drops port 0.
		if n, err := strconv.Atoi(port); err == nil && n > 0 {
			netloc += ":" + strconv.Itoa(n)
		} else if err != nil && port != "" {
			netloc += ":" + port
		}
	}
	if normalize && path != "" {
		path = encodeInvalidURLChars(removeDotSegmentsURLLib3(path), pathChars)
	}
	if normalize && query != "" {
		query = encodeInvalidURLChars(query, pathChars+"?")
	}
	if normalize && fragment != "" {
		fragment = encodeInvalidURLChars(fragment, pathChars+"?")
	}
	if path == "" {
		path = "/"
	}
	if len(params) > 0 {
		encoded := make([]string, len(params))
		for i, p := range params {
			encoded[i] = QuotePlus(p[0]) + "=" + QuotePlus(p[1])
		}
		if query != "" {
			query += "&" + strings.Join(encoded, "&")
		} else {
			query = strings.Join(encoded, "&")
		}
	}
	out := ""
	if scheme != "" {
		out = scheme + ":"
	}
	if netloc != "" || scheme == "http" || scheme == "https" {
		out += "//" + netloc
	}
	out += path
	if query != "" {
		out += "?" + query
	}
	if fragment != "" {
		out += "#" + fragment
	}
	return requoteURI(out), nil
}

var (
	schemeRE  = regexp.MustCompile(`^(?:[a-zA-Z][a-zA-Z0-9+-]*:|/)`)
	uriRE     = regexp.MustCompile(`(?s)^(?:([a-zA-Z][a-zA-Z0-9+.-]*):)?(?://([^\\/?#]*))?([^?#]*)(?:\?([^#]*))?(?:#(.*))?$`)
	percentRE = regexp.MustCompile(`%[a-fA-F0-9]{2}`)
)

const (
	userinfoChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-~!$&'()*+,;=:"
	pathChars     = userinfoChars + "@/"
)

// encodeInvalidURLChars is urllib3's _encode_invalid_chars.
func encodeInvalidURLChars(component, allowed string) string {
	count := 0
	component = percentRE.ReplaceAllStringFunc(component, func(s string) string { count++; return strings.ToUpper(s) })
	isPercentEncoded := count == strings.Count(component, "%")
	var b strings.Builder
	for i := 0; i < len(component); i++ {
		c := component[i]
		if (isPercentEncoded && c == '%') || (c < 128 && strings.IndexByte(allowed, c) >= 0) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(uppercaseHex[c>>4])
			b.WriteByte(uppercaseHex[c&15])
		}
	}
	return b.String()
}

// removeDotSegmentsURLLib3 is urllib3's _remove_path_dot_segments.
func removeDotSegmentsURLLib3(path string) string {
	var output []string
	for _, segment := range strings.Split(path, "/") {
		switch {
		case segment == ".":
		case segment != "..":
			output = append(output, segment)
		case len(output) > 0:
			output = output[:len(output)-1]
		}
	}
	if strings.HasPrefix(path, "/") && (len(output) == 0 || output[0] != "") {
		output = append([]string{""}, output...)
	}
	if strings.HasSuffix(path, "/.") || strings.HasSuffix(path, "/..") {
		output = append(output, "")
	}
	return strings.Join(output, "/")
}



func isASCIIAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// requoteURI is requests.utils.requote_uri.
func requoteURI(uri string) string {
	if unquoted, ok := unquoteUnreserved(uri); ok {
		return quoteSafe(unquoted, "!#$%&'()*+,/:;=?@[]~")
	}
	return quoteSafe(uri, "!#$&'()*+,/:;=?@[]~")
}

// unquoteUnreserved is requests.utils.unquote_unreserved (false: InvalidURL).
func unquoteUnreserved(uri string) (string, bool) {
	parts := strings.Split(uri, "%")
	for i := 1; i < len(parts); i++ {
		h := parts[i]
		if len(h) >= 2 && isASCIIAlnum(h[0]) && isASCIIAlnum(h[1]) {
			if !isHex(h[0]) || !isHex(h[1]) {
				return "", false
			}
			c := unhex(h[0])<<4 | unhex(h[1])
			if isASCIIAlnum(c) || strings.IndexByte("-._~", c) >= 0 {
				parts[i] = string(rune(c)) + h[2:]
			} else {
				parts[i] = "%" + h
			}
		} else {
			parts[i] = "%" + h
		}
	}
	return strings.Join(parts, ""), true
}


func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// quoteSafe is urllib.parse.quote(s, safe) on the UTF-8 bytes.
func quoteSafe(s, safe string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isASCIIAlnum(c) || strings.IndexByte("_.-~", c) >= 0 || strings.IndexByte(safe, c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(uppercaseHex[c>>4])
			b.WriteByte(uppercaseHex[c&15])
		}
	}
	return b.String()
}
