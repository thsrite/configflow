package substore

import (
	"fmt"
	utf8codec "golang.org/x/text/encoding/unicode"
	"strings"
	"time"
	"unicode"
)

type statusCarrier interface{ HTTPStatus() int }

// safeExceptionDetails carries only diagnostic category and HTTP status, never error text or URLs.
func safeExceptionDetails(err error) string {
	name := fmt.Sprintf("%T", err)
	switch value := err.(type) {
	case *RequestError:
		name = value.Class
	case *RuntimeUnavailable:
		name = "RuntimeUnavailable"
	case *DependencyUpdateError:
		name = "DependencyUpdateError"
	case *ReleaseError:
		name = "ReleaseError"
	}
	var b strings.Builder
	for _, r := range name {
		if r < 128 && (r == '_' || r == '.' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		}
	}
	name = b.String()
	if len(name) > 80 {
		name = name[:80]
	}
	if name == "" {
		name = "Error"
	}
	result := "exception_type=" + name
	if s, ok := err.(statusCarrier); ok && s.HTTPStatus() != 0 {
		result += fmt.Sprintf(" status=%d", s.HTTPStatus())
	}
	return result
}
func fileError(err error, path string) error { return fmt.Errorf("file %s: %w", path, err) }
func latin1(s string) string {
	out := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		out[i] = rune(s[i])
	}
	return string(out)
}
func runePrefix(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
func decodeUTF8Replace(b []byte) string {
	out, _ := utf8codec.UTF8.NewDecoder().Bytes(b)
	return string(out)
}
func isoFormat(t time.Time) string {
	t = t.UTC()
	base := t.Format("2006-01-02T15:04:05")
	if us := t.Nanosecond() / 1000; us != 0 {
		base += fmt.Sprintf(".%06d", us)
	}
	return base + "+00:00"
}
func unixTime(t time.Time) float64 {
	ns := t.UnixNano()
	if ns%1e9 == 0 {
		return float64(ns / 1e9)
	}
	return float64(ns) / 1e9
}
