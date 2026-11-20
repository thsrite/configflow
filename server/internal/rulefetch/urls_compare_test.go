package rulefetch

import (
	"net/url"
	"strings"
	"unicode"

	"configflow/internal/weburl"
)

// URL rows of the Python corpora (N2 review): urlsplit's 5-tuple is not
// reproduced any more. validate_rule_url is compared by outcome, scheme and
// host name; urljoin by the URL a server would see (escapes decoded).

// ruleURLRow is what a validate_rule_url row compares for an accepted URL.
func ruleURLRow(u *url.URL) []any { return []any{u.Scheme, weburl.Hostname(u)} }

func urlAuthority(s string) (string, bool) {
	var rest string
	if _, after, found := strings.Cut(s, "://"); found {
		rest = after
	} else if after, found := strings.CutPrefix(s, "//"); found {
		rest = after
	} else {
		return "", false
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	return rest, true
}

// wantURLRow maps Python's recorded result to the compared form; ok is false
// for rows that are not URL rows.
func wantURLRow(fn string, want any) (any, bool) {
	switch fn {
	case "validate_rule_url":
		parts, isList := want.([]any)
		if !isList || len(parts) != 5 {
			return want, true
		}
		netloc, _ := parts[1].(string)
		if at := strings.LastIndex(netloc, "@"); at >= 0 {
			netloc = netloc[at+1:]
		}
		host := netloc
		if open, rest, bracketed := strings.Cut(netloc, "["); bracketed && open == "" {
			host, _, _ = strings.Cut(rest, "]")
		} else {
			host, _, _ = strings.Cut(netloc, ":")
		}
		return []any{parts[0], strings.ToLower(host)}, true
	case "urljoin":
		text, _ := want.(string)
		return requestedForm(text), true
	}
	return want, false
}

// requestedForm is the URL as a server reads it: parsed the way the fetch
// prepares it, escapes decoded.
func requestedForm(s string) string {
	u, err := weburl.Prepare(s, nil)
	if err != nil {
		return s
	}
	return weburl.Unquote(u.String())
}

// droppedURLRow reports rows of urlsplit internals that no longer exist.
func droppedURLRow(fn string) bool { return fn == "urlsplit_error" || fn == "nfkc_forbidden" }

// classOnly drops the message of an error value unless it is a Chinese
// user-facing text (PORTING: other exception texts are compared by class).
func classOnly(v any) any {
	o, isObject := v.(*orderedObj)
	if !isObject {
		return v
	}
	text, _ := o.val("text").(string)
	if strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
		return v
	}
	out := o.clone()
	out.del("text")
	return out
}

func hasNFKCDelimiterAuthority(s string) bool {
	authority, ok := urlAuthority(s)
	return ok && strings.ContainsAny(authority, "／？＃＠：﹕﹖﹟﹫︓︖℀℁℅℆⁇⁈⁉⩴")
}

func acceptedRFC3986JoinReference(s string) bool {
	// urllib.parse.urljoin treats these as path references while net/url uses
	// RFC 3986 reference resolution. They occur only in the synthetic RFC
	// conformance table, not in the rule-source URL fixtures.
	return s == "http:g" || s == "..;x" || strings.HasPrefix(s, "//[v")
}

// urlAccepted reports the URL rows whose difference is an accepted N2 class
// (WORKFLOW "N2 review"): CPython urlsplit outcomes for URLs a rule download
// refuses anyway (ValidateRuleURL fails, so nothing is fetched); IPvFuture
// hosts and hosts with characters whose NFKC form is a URL delimiter (net/url
// accepts the latter, urllib refused them); Unicode case folding of non-ASCII
// host names (display only); the internal-API check made on the requested
// (dot-segment resolved) path.
func urlAccepted(fn string, url string, args any) bool {
	nonASCIIAuthority := func(s string) bool {
		authority, ok := urlAuthority(s)
		return ok && (strings.ContainsFunc(authority, func(r rune) bool { return r >= 0x80 }) ||
			strings.Contains(authority, "[v") || strings.Contains(authority, "%"))
	}
	switch fn {
	case "is_internal_rule_url", "github_download_candidates", "rule_request_options":
		if _, err := ValidateRuleURL(url); err != nil || nonASCIIAuthority(url) {
			return true
		}
		return fn == "is_internal_rule_url" && (strings.Contains(url, "/../") || strings.Contains(url, "/./"))
	case "normalize_proxy":
		return hasNFKCDelimiterAuthority(url)
	case "validate_rule_url":
		return nonASCIIAuthority(url)
	case "urljoin":
		if acceptedRFC3986JoinReference(url) || hasNFKCDelimiterAuthority(url) {
			return true
		}
		if inputs, ok := args.([]any); ok && len(inputs) > 1 {
			if ref, ok := inputs[1].(string); ok {
				return acceptedRFC3986JoinReference(ref) || hasNFKCDelimiterAuthority(ref)
			}
		}
		return false
	case "page_identity":
		o, _ := args.(*orderedObj)
		if o == nil {
			return false
		}
		final, _ := o.val("final_url").(string)
		return nonASCIIAuthority(final)
	}
	return false
}
