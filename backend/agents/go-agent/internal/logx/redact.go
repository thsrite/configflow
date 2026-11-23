package logx

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	nonAlphaNumeric = regexp.MustCompile(`[^a-z0-9]`)

	// PEM private key blocks (multiline or single-line, closed or truncated).
	pemPrivateKeyBlock = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	pemPrivateKeyHead  = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[^\r\n]*`)

	// Embedded HTTP(S) URLs inside arbitrary log messages.
	embeddedHTTPURL = regexp.MustCompile(`https?://[^\s"'<>]+`)

	// Authorization headers and Bearer/Basic schemes.
	authHeaderSchemeRe = regexp.MustCompile(`(?i)\b(authorization\s*[:=]\s*)(?:bearer|basic)\s+[^\s,;'"}\]]+`)
	authHeaderValueRe  = regexp.MustCompile(`(?i)\b(authorization\s*[:=]\s*)[^\s,;'"}\]]+`)
	bearerBasicRe      = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{6,}`)
	sensitiveHeaderRe  = regexp.MustCompile(`(?i)\b(x-api-key|x-auth-token|cookie|set-cookie)\s*[:=]\s*[^\r\n,;]+`)

	// Sensitive JSON and key=value / key: value fields.
	sensitiveJSONFieldRe = regexp.MustCompile(`(?i)("(?:[a-z0-9_-]*(?:token|password|passwd|pwd|secret|api_?key|private_?key|privkey|credential|authorization)[a-z0-9_-]*)"\s*:\s*)"[^"]*"`)
	sensitiveKVRe        = regexp.MustCompile(`(?i)\b([a-z0-9_-]*(?:token|password|passwd|pwd|secret|api_?key|private_?key|privkey|credential)[a-z0-9_-]*\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;'"}\]]+)`)
)

func isSensitiveKey(key string) bool {
	normalized := nonAlphaNumeric.ReplaceAllString(strings.ToLower(key), "")
	for _, part := range []string{"token", "authorization", "auth", "key", "secret", "password", "passwd", "pwd", "credential", "privkey", "privatekey"} {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

// SensitiveURLQueryKey reports whether a URL query parameter or field name
// carries credentials or tokens that must be redacted in logs and diagnostics.
func SensitiveURLQueryKey(key string) bool {
	normalized := nonAlphaNumeric.ReplaceAllString(strings.ToLower(key), "")
	for _, part := range []string{"token", "authorization", "auth", "key", "secret", "password", "passwd", "credential"} {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

func hasControlChar(value string) bool {
	for _, r := range value {
		if r < 32 || r == 127 {
			return true
		}
	}
	return false
}

func decodeURLLogLayers(value string) (string, error) {
	for range 8 {
		if hasControlChar(value) {
			return "", fmt.Errorf("control character")
		}
		next, err := url.QueryUnescape(value)
		if err != nil {
			return "", err
		}
		if next == value {
			return value, nil
		}
		value = next
	}
	next, err := url.QueryUnescape(value)
	if err != nil || next != value || hasControlChar(value) {
		return "", fmt.Errorf("unsafe encoded value")
	}
	return value, nil
}

func urlLogValueHasSensitiveExpression(value string) bool {
	for _, component := range strings.FieldsFunc(value, func(r rune) bool {
		return r == '?' || r == '&' || r == ';'
	}) {
		parts := strings.SplitN(component, "=", 2)
		if len(parts) == 2 && SensitiveURLQueryKey(strings.TrimSpace(parts[0])) {
			return true
		}
	}
	return false
}

// RedactURL returns a URL suitable for logs without userinfo, fragment, or
// credential-bearing query parameters (including multi-layer percent-encoded
// nested parameters). Malformed or control-character-bearing URLs fail closed.
func RedactURL(raw string) string {
	decoded, err := decodeURLLogLayers(raw)
	if err != nil {
		return "[INVALID URL REDACTED]"
	}
	parsed, err := url.Parse(decoded)
	if err != nil || (parsed.Scheme != "" && parsed.Host == "") {
		return "[INVALID URL REDACTED]"
	}
	parsed.User = nil
	parsed.Fragment = ""
	parsed.RawFragment = ""
	query := parsed.Query()
	for key, values := range query {
		if SensitiveURLQueryKey(key) {
			query.Set(key, "[REDACTED]")
			continue
		}
		for _, value := range values {
			decodedValue, decodeErr := decodeURLLogLayers(value)
			if decodeErr != nil {
				return "[INVALID URL REDACTED]"
			}
			if urlLogValueHasSensitiveExpression(decodedValue) {
				query.Set(key, "[REDACTED]")
				break
			}
		}
	}
	parsed.RawQuery = query.Encode()
	return strings.ReplaceAll(parsed.String(), "%5BREDACTED%5D", "[REDACTED]")
}

type safeWrappedError struct {
	message string
	cause   error
}

func (e *safeWrappedError) Error() string { return e.message }
func (e *safeWrappedError) Unwrap() error { return e.cause }

// SafeURLFailure returns an error whose text uses the redacted URL while
// preserving cause for errors.Is / errors.As.
func SafeURLFailure(action, rawURL string, cause error) error {
	return &safeWrappedError{
		message: action + " " + RedactURL(rawURL),
		cause:   cause,
	}
}

// Redact scrubs private keys, credential-bearing URLs, Authorization headers,
// Bearer/Basic tokens, passwords, and sensitive key-value pairs from a log
// message, and neutralizes control characters that could forge log lines.
func Redact(text string) string {
	if text == "" {
		return ""
	}
	text = pemPrivateKeyBlock.ReplaceAllString(text, "[PRIVATE KEY REDACTED]")
	text = pemPrivateKeyHead.ReplaceAllString(text, "[PRIVATE KEY REDACTED]")
	text = embeddedHTTPURL.ReplaceAllStringFunc(text, func(match string) string {
		trimmed := strings.TrimRight(match, ".,;:)]}")
		suffix := match[len(trimmed):]
		return RedactURL(trimmed) + suffix
	})
	text = authHeaderSchemeRe.ReplaceAllString(text, "${1}[REDACTED]")
	text = authHeaderValueRe.ReplaceAllString(text, "${1}[REDACTED]")
	text = bearerBasicRe.ReplaceAllString(text, "${1} [REDACTED]")
	text = sensitiveHeaderRe.ReplaceAllString(text, "${1}: [REDACTED]")
	text = sensitiveJSONFieldRe.ReplaceAllString(text, `${1}"[REDACTED]"`)
	text = sensitiveKVRe.ReplaceAllString(text, "${1}[REDACTED]")
	return sanitizeControls(text)
}

func sanitizeControls(text string) string {
	if !hasControlChar(text) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			b.WriteByte(' ')
		case r < 32 || r == 127:
			b.WriteByte('?')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
