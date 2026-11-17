package rulefetch

import (
	"errors"
	"net/url"
	"path"
	"strings"
	"unicode"

	"configflow/internal/jsonx"
	"configflow/internal/weburl"
)

// parseRuleURL is the URL a rule download requests for raw (weburl.Prepare:
// a stray "%" escaped, dot segments resolved), so every check below looks at
// the same URL the fetch sends.
func parseRuleURL(raw string) (*url.URL, error) { return weburl.Prepare(raw, nil) }

// joinURL resolves a redirect's Location against the URL that was requested.
func joinURL(base, location string) (string, error) {
	b, err := parseRuleURL(base)
	if err != nil {
		return "", err
	}
	ref, err := weburl.Prepare(location, nil)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(ref).String(), nil
}

// ErrInvalidURL is the ValueError of validate_rule_url.
var ErrInvalidURL = errors.New("Invalid HTTP/HTTPS URL")

// ErrInternalRedirect identifies a target outside an internal API boundary.
var ErrInternalRedirect = errors.New("Internal API redirect leaves its origin")

// internalBoundary is sticky for the whole redirect chain once a URL enters
// this server's API. It is not reclassified as external after preparation.
type internalBoundary struct {
	base   *url.URL
	origin origin
	prefix string
}

func prepareRuleTarget(raw string, config *jsonx.Object, hostURL string, boundary *internalBoundary) (*url.URL, *url.URL, *internalBoundary, error) {
	prepared, err := ValidateRuleURL(raw)
	if err != nil {
		return nil, nil, boundary, err
	}
	if boundary == nil {
		base, internal, err := internalRuleBase(raw, config, hostURL)
		if err != nil {
			return nil, nil, boundary, err
		}
		if !internal {
			base, internal, err = internalRuleBase(prepared.String(), config, hostURL)
			if err != nil {
				return nil, nil, boundary, err
			}
		}
		if internal {
			base, err = weburl.Prepare(base.String(), nil)
			if err != nil {
				return nil, nil, boundary, err
			}
			o, err := originOf(base)
			if err != nil {
				return nil, nil, boundary, err
			}
			boundary = &internalBoundary{base: base, origin: o, prefix: strings.TrimRight(base.Path, "/") + "/api/"}
		}
	}
	if boundary != nil {
		if err := boundary.validate(prepared); err != nil {
			return nil, nil, boundary, err
		}
		return prepared, boundary.base, boundary, nil
	}
	return prepared, nil, nil, nil
}

func (b *internalBoundary) validate(target *url.URL) error {
	o, err := originOf(target)
	if err != nil || o != b.origin {
		return ErrInternalRedirect
	}
	// Path is already decoded once by net/url. Check nested escaped forms too:
	// a reverse proxy must not turn an accepted encoded traversal into a path
	// outside the API. Backslashes have inconsistent separator semantics.
	candidate := target.Path
	for round := 0; round < 8; round++ {
		if strings.Contains(candidate, "\\") {
			return ErrInternalRedirect
		}
		clean := path.Clean(candidate)
		if strings.HasSuffix(candidate, "/") {
			clean += "/"
		}
		if !strings.HasPrefix(clean, b.prefix) {
			return ErrInternalRedirect
		}
		decoded, err := url.PathUnescape(candidate)
		if err != nil || decoded == candidate {
			return nil
		}
		candidate = decoded
	}
	// Do not spend unbounded work peeling adversarial encodings or accept a
	// still-ambiguous path when the bounded native checks cannot prove it safe.
	return ErrInternalRedirect
}

// ValidateRuleURL is validate_rule_url: syntax only (http/https, a host,
// a port in 1..65535, no whitespace or backslash in the authority, no
// control characters). The result is the URL as it will be requested.
func ValidateRuleURL(raw string) (*url.URL, error) {
	if strings.ContainsFunc(raw, func(r rune) bool { return r < 32 || r == 127 }) {
		return nil, ErrInvalidURL
	}
	u, err := parseRuleURL(raw)
	if err != nil {
		return nil, ErrInvalidURL
	}
	port, hasPort, err := weburl.Port(u)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || weburl.Hostname(u) == "" {
		return nil, ErrInvalidURL
	}
	if hasPort && port < 1 {
		return nil, ErrInvalidURL
	}
	// Validate the authority as written. net/url exposes percent-decoded
	// user information, but an encoded space is valid URL syntax and requests
	// sends it as Basic credentials. Looking at u.User here would reject it.
	authority := rawAuthority(raw)
	if strings.ContainsFunc(authority, unicode.IsSpace) || strings.Contains(authority, "\\") {
		return nil, ErrInvalidURL
	}
	return u, nil
}

func rawAuthority(raw string) string {
	_, rest, found := strings.Cut(raw, "://")
	if !found {
		return ""
	}
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// ErrInvalidProxy is the ValueError of normalize_rule_fetch_proxy.
var (
	ErrProxyNotString = errors.New("规则下载代理必须是 HTTP 或 HTTPS 地址")
	ErrInvalidProxy   = errors.New("规则下载代理必须是 HTTP 或 HTTPS 地址，不能包含路径、查询参数或片段")
)

// NormalizeRuleFetchProxy is normalize_rule_fetch_proxy: "" for direct,
// otherwise "scheme://authority" with the authority as written.
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
	if err != nil {
		return "", ErrInvalidProxy
	}
	// The path, query and fragment are checked as written.
	_, rest, _ := strings.Cut(text, "://")
	authority, tail := rest, ""
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		authority, tail = rest[:i], rest[i:]
	}
	tail, fragment, _ := strings.Cut(tail, "#")
	path, query, _ := strings.Cut(tail, "?")
	if (path != "" && path != "/") || query != "" || fragment != "" {
		return "", ErrInvalidProxy
	}
	return parsed.Scheme + "://" + authority, nil
}
