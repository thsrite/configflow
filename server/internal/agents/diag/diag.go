// Package diag holds the pure text helpers of backend/agents/diagnostics.py
// (failure_text) and backend/agents/operation_events.py (event_message,
// MESSAGE_LIMIT). It is a leaf package so that both internal/agents and
// internal/notifications use the one implementation (internal/agents imports
// internal/notifications, so notifications cannot import agents).
package diag

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"configflow/internal/weburl"
)

// Limits (UTF-8 bytes): diagnostics.DETAIL_LIMIT and
// operation_events.MESSAGE_LIMIT.
const (
	DetailLimit  = 16 * 1024
	MessageLimit = 1024
)

// PyWS is what Python's \s matches in a str pattern, for use inside a
// character class.
const PyWS = `\t\n\x0b\f\r \x{1c}-\x{1f}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}`

// Under re.IGNORECASE Python also matches "i" with U+0130 (its lowercase is
// "i") and U+0131 (sre's equivalence table); RE2's case folding does not, so
// every "i" of the keywords is spelled [iİı]. (k/K/U+212A and s/S/U+017F fold
// the same way in both.)
var (
	secretKey     = foldI(`(?:password|passwd|passphrase|secret|token|api[_-]?key|private[_-]?key|access[_-]?key|authorization|credential)`)
	bearerOrBasic = foldI(`(Bearer|Basic)`)
)

func foldI(pattern string) string { return strings.ReplaceAll(pattern, "i", "[iİı]") }

const urlStart = `[a-zA-Z][a-zA-Z0-9+.-]*://[^` + PyWS + `<>"'`

// The Python patterns use \b, which is Unicode-aware there and ASCII-only in
// RE2. The \b of _AUTH and _SECRET stands directly before the keyword (after
// an optional quote), so it is dropped from the patterns and checked by
// replaceAtWordStart with Python's definition of a word character.
var (
	// URLPattern is diagnostics._URL / operation_events._URL.
	URLPattern = regexp.MustCompile(urlStart + `\\]+`)
	// CredentialURLPattern includes backslashes in literal configuration URLs.
	// Diagnostics stop at them to avoid swallowing an escaped quote; configuration
	// strings have already been decoded and their complete credentials must be marked.
	CredentialURLPattern = regexp.MustCompile(urlStart + `]+`)
	ansiPattern          = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	// (?i)\b(Bearer|Basic)\s+[^\s,"'<>]+ without the \b.
	authPattern = regexp.MustCompile(`(?i)` + bearerOrBasic + `[` + PyWS + `]+[^` + PyWS + `,"'<>]+`)
	blockSecret = regexp.MustCompile(`(?i)^([ \t]*)(-[` + PyWS + `]+)?["']?` + secretKey + `["']?[` + PyWS + `]*:[ \t]*[|>][0-9+-]*[ \t]*(?:#[^\n]*)?$`)
	pemPattern  = regexp.MustCompile(`(?s)-----BEGIN [^-]*PRIVATE KEY-----.*?(?:-----END [^-]*PRIVATE KEY-----|$)`)
	// _SECRET without the \b; group 2 is the optional opening quote.
	secretPattern = regexp.MustCompile(`(?i)((["']?)` + secretKey + `["']?[` + PyWS + `]*[:=][ \t]*)` +
		`(?:` + bearerOrBasic + `[` + PyWS + `]+)?(?:"(?:\\.|[^"\\])*"|'(?:''|\\.|[^'\\])*'|[^` + PyWS + `,;}&]+)`)
)

// IsPySpace is Python's str.isspace() for one character (also what str.strip()
// removes and what \s matches).
func IsPySpace(r rune) bool {
	switch r {
	case 0x1c, 0x1d, 0x1e, 0x1f, 0x85:
		return true
	}
	return unicode.IsSpace(r)
}

// PyStrip is str.strip() without arguments.
func PyStrip(text string) string { return strings.TrimFunc(text, IsPySpace) }

// isPyWord is a word character of Python's Unicode \w and \b: str.isalnum()
// or "_".
func isPyWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// atWordStart is Python's \b directly before a word character at byte offset
// i: true at the start of the text or after a non-word character.
func atWordStart(text string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:i])
	return !isPyWord(r)
}

// matchesAtWordStart is pattern.finditer(text) for a Python pattern of the
// form \b<pattern> (or ["']?\b<pattern> when quoteGroup > 0 names the group of
// the optional quote): a match whose keyword does not start at a word
// boundary is rejected and the search resumes one character later, as Python
// does when \b fails at a start position. Each result holds absolute submatch
// byte offsets.
func matchesAtWordStart(text string, pattern *regexp.Regexp, quoteGroup int) [][]int {
	var out [][]int
	for pos := 0; pos <= len(text); {
		loc := pattern.FindStringSubmatchIndex(text[pos:])
		if loc == nil {
			break
		}
		for i := range loc {
			if loc[i] >= 0 {
				loc[i] += pos
			}
		}
		start, end := loc[0], loc[1]
		keyword := start
		if quoteGroup > 0 && loc[2*quoteGroup] >= 0 {
			keyword = loc[2*quoteGroup+1]
		}
		if !atWordStart(text, keyword) {
			_, size := utf8.DecodeRuneInString(text[start:])
			pos = start + max(size, 1)
			continue
		}
		out = append(out, loc)
		if end == start {
			_, size := utf8.DecodeRuneInString(text[end:])
			pos = end + max(size, 1)
		} else {
			pos = end
		}
	}
	return out
}

// replaceAtWordStart is pattern.sub(repl, text) for the patterns of
// matchesAtWordStart.
func replaceAtWordStart(text string, pattern *regexp.Regexp, quoteGroup int, repl func(groups []string) string) string {
	var out strings.Builder
	last := 0
	for _, loc := range matchesAtWordStart(text, pattern, quoteGroup) {
		groups := make([]string, len(loc)/2)
		for g := range groups {
			if loc[2*g] >= 0 {
				groups[g] = text[loc[2*g]:loc[2*g+1]]
			}
		}
		out.WriteString(text[last:loc[0]])
		out.WriteString(repl(groups))
		last = loc[1]
	}
	out.WriteString(text[last:])
	return out.String()
}

// SecretSpans returns the byte spans [start, end) of the key=value
// credentials FailureText redacts (diagnostics._SECRET.finditer). A value
// may continue on a later line, e.g. "authorization: Bearer" followed by the
// token, so callers excerpting one line check whether a span crosses it.
func SecretSpans(text string) [][2]int {
	var out [][2]int
	for _, loc := range matchesAtWordStart(text, secretPattern, 2) {
		out = append(out, [2]int{loc[0], loc[1]})
	}
	return out
}

// HasCredentialText reports inline credentials, standalone Bearer/Basic values
// and PEM private keys recognized by FailureText. This keeps public metadata
// redaction on the same patterns as diagnostic redaction.
func HasCredentialText(text string) bool {
	// Every pattern needs one of these: secretPattern a literal ':' or '=',
	// authPattern a b/B (Bearer/Basic only fold to ASCII), pemPattern "BEGIN".
	if !strings.ContainsAny(text, ":=bB") {
		return false
	}
	return len(SecretSpans(text)) > 0 || len(matchesAtWordStart(text, authPattern, 0)) > 0 || pemPattern.MatchString(text)
}

// FailureText is diagnostics.failure_text(value, secrets, limit): ANSI
// escapes and control characters removed, private keys, YAML secret blocks,
// the given secrets (also URL-encoded), URL credentials, Authorization values
// and key=value secrets redacted, stripped, and cut to limit UTF-8 bytes with
// a "[输出过长，已截断]" marker. Non-string values give "". limit <= 0 means
// DetailLimit (Python's default).
func FailureText(value any, secrets []any, limit int) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	var names []string
	for _, value := range secrets {
		if secret, ok := value.(string); ok {
			names = append(names, secret)
		}
	}
	return DiagnosticText(text, names, limit)
}

// DiagnosticText bounds readable failure details and removes credential values.
func DiagnosticText(text string, secrets []string, limit int) string {
	if limit <= 0 {
		limit = DetailLimit
	}
	text = ansiPattern.ReplaceAllString(text, "")
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || (r >= 32 && r != 127) {
			return r
		}
		return -1
	}, text)
	text = hideBlockSecrets(pemPattern.ReplaceAllString(text, "[PRIVATE KEY REDACTED]"))
	var names []string
	seen := map[string]bool{}
	for _, secret := range secrets {
		if secret != "" && !seen[secret] {
			seen[secret] = true
			names = append(names, secret)
		}
	}
	// Python sorts a set, so the order of equally long secrets is arbitrary
	// there; here it is the given order.
	sort.SliceStable(names, func(i, j int) bool {
		return utf8.RuneCountInString(names[i]) > utf8.RuneCountInString(names[j])
	})
	for _, secret := range names {
		// {secret, quote(secret, safe=''), quote_plus(secret)}
		for _, variant := range []string{secret, strings.ReplaceAll(url.QueryEscape(secret), "+", "%20"), url.QueryEscape(secret)} {
			text = strings.ReplaceAll(text, variant, "[REDACTED]")
		}
	}
	text = URLPattern.ReplaceAllStringFunc(text, safeDiagnosticURL)
	text = replaceAtWordStart(text, authPattern, 0, func(groups []string) string { return groups[1] + " [REDACTED]" })
	text = replaceAtWordStart(text, secretPattern, 2, func(groups []string) string { return groups[1] + "[REDACTED]" })
	text = PyStrip(text)
	if len(text) > limit {
		suffix := messageTruncated
		if limit < len(suffix) {
			cut := []byte(suffix)[:limit]
			for len(cut) > 0 && !utf8.Valid(cut) {
				cut = cut[:len(cut)-1]
			}
			return string(cut)
		}
		cut := []byte(text)[:limit-len(suffix)]
		for len(cut) > 0 && !utf8.Valid(cut) {
			cut = cut[:len(cut)-1]
		}
		text = string(cut) + suffix
	}
	return text
}

// EventMessage is operation_events.event_message(value, secrets): download
// addresses hidden, then FailureText with MessageLimit.
func EventMessage(value any, secrets []any) string {
	if text, ok := value.(string); ok {
		value = URLPattern.ReplaceAllString(text, messageHiddenDownload)
	}
	return FailureText(value, secrets, MessageLimit)
}

func safeDiagnosticURL(match string) string {
	// Proxy URI payloads can contain complete encoded credentials rather than a host.
	scheme, _, _ := strings.Cut(match, ":")
	switch strings.ToLower(scheme) {
	case "http", "https", "tcp", "udp", "tls", "quic", "h3":
		return weburl.SafeForLog(match)
	}
	return "[REDACTED URL]"
}

// hideBlockSecrets is _hide_block_secrets: hides a YAML block value without
// swallowing the next diagnostic line.
func hideBlockSecrets(text string) string {
	lines := SplitLinesKeepEnds(text)
	var out strings.Builder
	for index := 0; index < len(lines); {
		line := lines[index]
		match := blockSecret.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
		index++
		if match == nil {
			out.WriteString(line)
			continue
		}
		out.WriteString(line[:strings.Index(line, ":")+1] + " [REDACTED]\n")
		indent := utf8.RuneCountInString(match[1]) + utf8.RuneCountInString(match[2])
		for index < len(lines) {
			child := lines[index]
			stripped := strings.TrimLeft(child, " \t")
			if PyStrip(child) != "" && utf8.RuneCountInString(child)-utf8.RuneCountInString(stripped) <= indent {
				break
			}
			index++
		}
	}
	return out.String()
}

// SplitLinesKeepEnds is str.splitlines(keepends=True).
func SplitLinesKeepEnds(text string) []string {
	var lines []string
	runes := []rune(text)
	start := 0
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '\r':
			if i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
		case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		default:
			continue
		}
		lines = append(lines, string(runes[start:i+1]))
		start = i + 1
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

// EventText exposes the shared diagnostic visitor to typed notification producers.
func EventText(text string, secrets ...string) string {
	return DiagnosticText(URLPattern.ReplaceAllString(text, messageHiddenDownload), secrets, MessageLimit)
}
