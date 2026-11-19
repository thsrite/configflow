package weburl_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/weburl"
)

type nativeClassError struct {
	class  string
	status int
}

func (e nativeClassError) Error() string {
	return "private request https://user:secret@example.com/?token=hidden"
}
func (e nativeClassError) ErrorClass() string { return e.class }
func (e nativeClassError) HTTPStatus() int    { return e.status }

type legacyClassError struct{}

func (legacyClassError) Error() string       { return "private legacy message" }
func (legacyClassError) PythonClass() string { return "RuleFetchError" }

type dualClassError struct{ nativeClassError }

func (dualClassError) PythonClass() string { return "LegacyLabel" }

var _ weburl.ClassError = nativeClassError{}

func TestSafeErrorDetailsClassMigration(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"native", nativeClassError{"RuleFetchError", 502}, "exception_type=RuleFetchError status=502"},
		{"legacy fallback", legacyClassError{}, "exception_type=RuleFetchError"},
		{"native first", dualClassError{nativeClassError{"RuleFetchError", 503}}, "exception_type=RuleFetchError status=503"},
		{"wrapped native", fmt.Errorf("outer: %w", nativeClassError{"HTTPError", 504}), "exception_type=HTTPError status=504"},
		{"wrapped legacy", fmt.Errorf("outer: %w", legacyClassError{}), "exception_type=RuleFetchError"},
		{"native deeper than legacy", errors.Join(legacyClassError{}, nativeClassError{"HTTPError", 502}), "exception_type=HTTPError status=502"},
		{"sanitize", nativeClassError{"Type=https://user:secret@example.com?token=hidden", 400}, "exception_type=Typehttpsusersecretexample.comtokenhidden status=400"},
		{"bounded", nativeClassError{strings.Repeat("A", 100), 500}, "exception_type=" + strings.Repeat("A", 80) + " status=500"},
		{"empty", nativeClassError{"", 500}, "exception_type=Exception status=500"},
		{"ordinary", errors.New("private message"), "exception_type=errors.errorString"},
		{"nil", nil, "exception_type=nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := weburl.SafeErrorDetails(tt.err); got != tt.want {
				t.Fatalf("got %q; want %q", got, tt.want)
			}
		})
	}
}
