package diag

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

var credentialKeys = strings.Fields("password passwd passphrase uuid private-key privatekey pre-shared-key secret token auth auth-str authentication authorization proxy-authorization psk obfs-password short-id api-key apikey access-key registration-key access-token client-secret cookie private-key-passphrase")
var credentialSuffixes = []string{"-password", "-passwd", "-passphrase", "-secret", "-token", "-psk", "-private-key"}
var credentialParams = strings.Fields("token access-token auth auth-token key api-key apikey password passwd pass pwd secret sign signature sig uuid code ticket credential session sid")
var opaquePart = regexp.MustCompile(`^[A-Za-z0-9_\-=~]{24,}$`)
var bareUserinfo = regexp.MustCompile(`^[^\s/:@]+:[^\s/@]+@\S+$`)

// IsCredentialKey recognizes credential field names, excluding domain/data keys.
// Callers must separately exclude mappings whose keys are data (e.g. hosts).
func IsCredentialKey(key string) bool {
	key = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(key)), "_", "-")
	return !strings.ContainsAny(key, ". ") && (slices.Contains(credentialKeys, key) || hasSuffix(key, credentialSuffixes))
}

func hasSuffix(s string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

func isOpaque(s string) bool {
	return opaquePart.MatchString(s) && strings.ContainsAny(s, "0123456789") && strings.ContainsFunc(s, func(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' })
}

// CredentialSpans returns sorted byte ranges of URL and bare user:password
// credentials. Hosts and ports remain readable. It shares FailureText's URL
// recognition; malformed URLs fail closed. Opaque path/query tokens are marked.
func CredentialSpans(text string) [][2]int {
	// Both a credential URL ("://") and bare userinfo need a ':'.
	if !strings.Contains(text, ":") {
		return [][2]int{}
	}
	var spans [][2]int
	// FieldsFunc lacks positions; advancing through each original word retains them.
	offset := 0
	for _, word := range strings.FieldsFunc(text, unicode.IsSpace) {
		start := offset + strings.Index(text[offset:], word)
		offset = start + len(word)
		if strings.Contains(word, "://") {
			for _, loc := range CredentialURLPattern.FindAllStringIndex(word, -1) {
				for _, r := range urlCredentialSpans(word[loc[0]:loc[1]]) {
					spans = append(spans, [2]int{start + loc[0] + r[0], start + loc[0] + r[1]})
				}
			}
		} else if bareUserinfo.MatchString(word) {
			spans = append(spans, [2]int{start, start + strings.LastIndex(word, "@")})
		}
	}
	slices.SortFunc(spans, func(a, b [2]int) int {
		if a[0] != b[0] {
			return a[0] - b[0]
		}
		return a[1] - b[1]
	})
	out := make([][2]int, 0, len(spans))
	for _, s := range spans {
		if s[1] <= s[0] {
			continue
		}
		if len(out) > 0 && s[0] <= out[len(out)-1][1] {
			out[len(out)-1][1] = max(s[1], out[len(out)-1][1])
		} else {
			out = append(out, s)
		}
	}
	return out
}

func urlCredentialSpans(raw string) [][2]int {
	// Go's parser also rejects a stray percent escape. Keeping such a URL secret
	// is preferable to leaking a credential when its boundary is ambiguous.
	if _, err := url.Parse(raw); err != nil {
		if proseAfterOrigin(raw) {
			return nil
		}
		return [][2]int{{0, len(raw)}}
	}
	head := strings.Index(raw, "://") + 3
	end := len(raw)
	if i := strings.IndexAny(raw[head:], "/?#"); i >= 0 {
		end = head + i
	}
	var spans [][2]int
	if i := strings.LastIndex(raw[head:end], "@"); i >= 0 {
		spans = append(spans, [2]int{head, head + i})
	}
	stop := len(raw)
	if i := strings.IndexByte(raw[end:], '#'); i >= 0 {
		stop = end + i
	}
	query := stop
	if i := strings.IndexByte(raw[end:stop], '?'); i >= 0 {
		query = end + i
	}
	pos := end
	for _, part := range strings.Split(raw[end:query], "/") {
		if isOpaque(part) {
			spans = append(spans, [2]int{pos, pos + len(part)})
		}
		pos += len(part) + 1
	}
	if query < stop {
		pos = query + 1
		for _, part := range strings.Split(raw[pos:stop], "&") {
			name, value, eq := strings.Cut(part, "=")
			decodedName, _ := url.QueryUnescape(name)
			decodedValue, _ := url.QueryUnescape(value)
			normalized := strings.ReplaceAll(strings.ToLower(decodedName), "_", "-")
			if isOpaque(decodedName) {
				spans = append(spans, [2]int{pos, pos + len(name)})
			}
			if eq && value != "" && (slices.Contains(credentialParams, normalized) || hasSuffix(normalized, []string{"-token", "-key", "-secret", "-password"}) || isOpaque(decodedValue)) {
				spans = append(spans, [2]int{pos + len(name) + 1, pos + len(part)})
			}
			pos += len(part) + 1
		}
	}
	return spans
}

// proseAfterOrigin reports a bare scheme://host[:port] directly followed by
// non-ASCII punctuation, as in Chinese messages "跳转到 https://host:8443（HTTP 302）".
// Only a credential-free origin qualifies, and the rest of the word must not
// contain '@'; anything else stays fail-closed.
func proseAfterOrigin(raw string) bool {
	cut := strings.IndexFunc(raw, func(r rune) bool { return r > unicode.MaxASCII && unicode.IsPunct(r) })
	if cut < 0 || strings.Contains(raw[cut:], "@") {
		return false
	}
	u, err := url.Parse(raw[:cut])
	return err == nil && u.Host != "" && u.User == nil && u.Opaque == "" && u.Path == "" && !u.ForceQuery && u.RawQuery == "" && u.Fragment == "" && !strings.ContainsAny(raw[:cut], "?#")
}
