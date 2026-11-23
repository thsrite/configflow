package routes

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var nonAlphaNumeric = regexp.MustCompile(`[^a-z0-9]`)

func sensitiveURLQueryKey(key string) bool {
	normalized := nonAlphaNumeric.ReplaceAllString(strings.ToLower(key), "")
	for _, part := range []string{"token", "authorization", "auth", "key", "secret", "password", "passwd", "credential"} {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

func urlLogHasControl(value string) bool {
	for _, char := range value {
		if char < 32 || char == 127 {
			return true
		}
	}
	return false
}

func decodeURLLogLayers(value string) (string, error) {
	for i := 0; i < 8; i++ {
		if urlLogHasControl(value) {
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
	if err != nil || next != value || urlLogHasControl(value) {
		return "", fmt.Errorf("unsafe encoded value")
	}
	return value, nil
}

func urlLogValueHasSensitiveExpression(value string) bool {
	for _, component := range strings.FieldsFunc(value, func(char rune) bool {
		return char == '?' || char == '&' || char == ';'
	}) {
		parts := strings.SplitN(component, "=", 2)
		if len(parts) == 2 && sensitiveURLQueryKey(strings.TrimSpace(parts[0])) {
			return true
		}
	}
	return false
}

// redactURLForLog returns a URL suitable for logs without userinfo or query credentials.
func redactURLForLog(raw string) string {
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
		if sensitiveURLQueryKey(key) {
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

func safeURLFailure(action, rawURL string, cause error) error {
	return &safeWrappedError{
		message: action + " " + redactURLForLog(rawURL),
		cause:   cause,
	}
}

func commandForLog(command string) string {
	if strings.HasPrefix(command, "http://") || strings.HasPrefix(command, "https://") {
		return redactURLForLog(command)
	}
	return "[COMMAND REDACTED]"
}

// JsonResponse 是一个辅助函数，用于统一返回JSON响应
func JsonResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if data != nil {
		if statusCode >= 400 {
			log.Printf("Returning error response: %d", statusCode)
		}
		json.NewEncoder(w).Encode(data)
	}
}
