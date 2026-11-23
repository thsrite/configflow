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

var _ weburl.ClassError = nativeClassError{}

func TestSafeErrorDetailsClass(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"native", nativeClassError{"RuleFetchError", 502}, "exception_type=RuleFetchError status=502"},
		{"wrapped native", fmt.Errorf("outer: %w", nativeClassError{"HTTPError", 504}), "exception_type=HTTPError status=504"},
		{"joined native", errors.Join(errors.New("plain"), nativeClassError{"HTTPError", 502}), "exception_type=HTTPError status=502"},
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

func TestHasControl(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"printable ascii", "https://example.com/path?a=1", false},
		{"space", "hello world", false},
		{"unicode", "中文配置", false},
		{"nul", "a\x00b", true},
		{"newline", "a\nb", true},
		{"cr", "a\rb", true},
		{"tab", "a\tb", true},
		{"unit separator 0x1f", "a\x1fb", true},
		{"del 0x7f", "a\x7fb", true},
		{"c1 control 0x80 not ascii", "a\u0080b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := weburl.HasControl(tt.in); got != tt.want {
				t.Fatalf("HasControl(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
