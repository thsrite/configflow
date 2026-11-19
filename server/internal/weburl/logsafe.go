package weburl

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Log-safe renderings of URLs and errors (backend/utils/url_utils.py).

const (
	redacted   = "[REDACTED]"
	invalidURL = "[INVALID URL REDACTED]"
	maxLayers  = 8
)

var sensitiveKeyParts = []string{"token", "authorization", "auth", "key", "secret", "password", "passwd", "credential"}

var (
	nonAlnum           = regexp.MustCompile(`[^a-z0-9]`)
	exceptionNameChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)
	errUnsafe          = errors.New("unsafe URL")
)

// SafeForLog renders a URL for a log line, failing closed: user information
// and the fragment are dropped, sensitive query keys keep their names but
// lose their values (also inside URLs nested in query values), and several
// percent-encoding layers are decoded first so encoding cannot hide a key.
// Anything malformed becomes "[INVALID URL REDACTED]".
func SafeForLog(raw string) string {
	out, err := safeURL(raw, 0)
	if err != nil {
		return invalidURL
	}
	return out
}

func hasControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r < 32 || r == 127 })
}

func badEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && !(i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2])) {
			return true
		}
	}
	return false
}

// decodeLayers decodes until nothing changes (at most maxLayers times),
// refusing control characters and broken escapes at every step.
func decodeLayers(value string, plus bool) (string, error) {
	decode := Unquote
	if plus {
		decode = UnquotePlus
	}
	for range maxLayers {
		if hasControl(value) || badEscape(value) {
			return "", errUnsafe
		}
		decoded := decode(value)
		if decoded == value {
			return value, nil
		}
		value = decoded
	}
	if decode(value) != value || hasControl(value) || badEscape(value) {
		return "", errUnsafe
	}
	return value, nil
}

func sensitiveKey(key string) (bool, error) {
	decoded, err := decodeLayers(key, false)
	if err != nil {
		return false, err
	}
	normalized := nonAlnum.ReplaceAllString(strings.ToLower(decoded), "")
	for _, part := range sensitiveKeyParts {
		if strings.Contains(normalized, part) {
			return true, nil
		}
	}
	return false, nil
}

// sensitiveExpression reports whether value holds "key=..." with a sensitive
// key anywhere among its ?, & or ; separated parts.
func sensitiveExpression(value string) (bool, error) {
	for _, component := range strings.FieldsFunc(value, func(r rune) bool { return r == '?' || r == '&' || r == ';' }) {
		if key, _, found := strings.Cut(component, "="); found {
			if sensitive, err := sensitiveKey(strings.TrimSpace(key)); err != nil || sensitive {
				return sensitive, err
			}
		}
	}
	return false, nil
}

func sanitizeValue(value string, depth int) (string, error) {
	decoded, err := decodeLayers(value, true)
	if err != nil {
		return "", err
	}
	if sensitive, err := sensitiveExpression(decoded); err != nil || sensitive {
		return redacted, err
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
	if value == "" || hasControl(value) || badEscape(value) {
		return "", errUnsafe
	}
	// The user information is cut before anything is decoded: an encoded
	// "/", "?" or "#" in a password would otherwise end the authority early
	// and leave the rest of the password in the port or path.
	if start, end, ok := authority(value); ok {
		if at := strings.LastIndex(value[start:end], "@"); at >= 0 {
			value = value[:start] + value[start+at+1:]
		}
	}
	decoded, err := decodeLayers(value, false)
	if err != nil {
		return "", err
	}
	u, err := Parse(decoded)
	if err != nil {
		return "", err
	}
	host := Hostname(u)
	if (u.Scheme == "http" || u.Scheme == "https") && host == "" {
		return "", errUnsafe
	}
	if u.Scheme == "" && !strings.HasPrefix(decoded, "/") {
		return "", errUnsafe
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port, ok, err := Port(u); err != nil {
		return "", err
	} else if ok {
		host += ":" + strconv.Itoa(port)
	}
	var query [][2]string
	for _, part := range strings.Split(u.RawQuery, "&") {
		if part == "" {
			continue
		}
		rawKey, rawValue, _ := strings.Cut(part, "=")
		key, err := decodeLayers(UnquotePlus(rawKey), false)
		if err != nil {
			return "", err
		}
		sensitive, err := sensitiveKey(key)
		if err != nil {
			return "", err
		}
		if sensitive {
			query = append(query, [2]string{key, redacted})
			continue
		}
		clean, err := sanitizeValue(UnquotePlus(rawValue), depth)
		if err != nil {
			return "", err
		}
		query = append(query, [2]string{key, clean})
	}
	var b strings.Builder
	if u.Scheme != "" {
		b.WriteString(u.Scheme + ":")
	}
	if host != "" || u.Scheme == "http" || u.Scheme == "https" {
		b.WriteString("//" + host)
	}
	b.WriteString(RawPath(u))
	if len(query) > 0 {
		b.WriteString("?" + EncodeQuery(query))
	}
	// The fragment is dropped: subscription providers put credentials there.
	return b.String(), nil
}

// StatusError lets SafeErrorDetails report an HTTP status.
type StatusError interface {
	error
	HTTPStatus() int
}

// ClassError names a stable error class for logging. SafeErrorDetails prints
// this label instead of the Go type, without including the error message.
type ClassError interface {
	error
	ErrorClass() string
}

// pythonClassError is a migration fallback for callers not yet using ClassError.
// Remove it once the remaining callers have adopted ErrorClass.
type pythonClassError interface {
	error
	PythonClass() string
}

// SafeErrorDetails is bounded error metadata for logs without the message
// or URL: "exception_type=<class>[ status=<code>]".
func SafeErrorDetails(err error) string {
	typeName := fmt.Sprintf("%T", err)
	var classed ClassError
	if errors.As(err, &classed) {
		typeName = classed.ErrorClass()
	} else {
		var legacy pythonClassError
		if errors.As(err, &legacy) {
			typeName = legacy.PythonClass()
		}
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
	if errors.As(err, &status) {
		details += " status=" + strconv.Itoa(status.HTTPStatus())
	}
	return details
}
