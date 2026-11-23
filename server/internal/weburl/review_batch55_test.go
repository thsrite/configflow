package weburl

import (
	"strings"
	"testing"
)

// TestO17ReviewEdgePrepare pins boundary inputs added in the batch-55 review.
// Every expectation was produced by the pre-1.13 implementation (branch go
// before the batch) and is unchanged by the net/url rewrite.
func TestO17ReviewEdgePrepare(t *testing.T) {
	long := "https://sub.example.com/" + strings.Repeat("中a%2F%zz", 2000) + "?q=" + strings.Repeat("%E4%B8+ ", 3000)
	longWant := "https://sub.example.com/" + strings.Repeat("%E4%B8%ADa%2F%25zz", 2000) + "?q=" + strings.Repeat("%E4%B8+%20", 3000)
	tests := []struct {
		name, raw, want  string
		wantUser, wantPw string
	}{
		{name: "%u escape is not an escape (path and query)", raw: "https://h/a%u00e9/b?a=%u00E9", want: "https://h/a%25u00e9/b?a=%25u00E9"},
		{name: "plus and %2B stay distinct in path and query", raw: "https://h/a+b%2Bc?x=a+b%2Bc&y=%2b", want: "https://h/a+b%2Bc?x=a+b%2Bc&y=%2b"},
		{name: "%2f and %2F keep their case and are not decoded", raw: "https://h/a%2fb%2Fc?u=%2f%2F", want: "https://h/a%2fb%2Fc?u=%2f%2F"},
		{name: "only a percent in path", raw: "https://h/%", want: "https://h/%25"},
		{name: "only a percent in query", raw: "https://h/?%", want: "https://h/?%25"},
		{name: "only a percent in fragment", raw: "https://h/#%", want: "https://h/#%25"},
		{name: "empty path becomes slash", raw: "https://h", want: "https://h/"},
		{name: "raw non-UTF-8 bytes are escaped as bytes", raw: "https://h/\xff?x=\xfe#\xfd", want: "https://h/%FF?x=%FE#%FD"},
		{name: "escaped non-UTF-8 bytes kept as written", raw: "https://h/%ff?x=%FE", want: "https://h/%ff?x=%FE"},
		{name: "userinfo: last @ ends it, first : splits it", raw: "https://u@ser:p@ss:w@h/x", want: "https://u%40ser:p%40ss%3Aw@h/x", wantUser: "u@ser", wantPw: "p@ss:w"},
		{name: "userinfo: colon in password", raw: "https://u:a:b@c@h:8443/x?k=%2B", want: "https://u:a%3Ab%40c@h:8443/x?k=%2B", wantUser: "u", wantPw: "a:b@c"},
		{name: "userinfo: pre-escaped colon and at", raw: "https://u%3Ap:pw%40x@h/x", want: "https://u%3Ap:pw%40x@h/x", wantUser: "u:p", wantPw: "pw@x"},
		{name: "very long URL", raw: long, want: longWant},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := Prepare(tt.raw, nil)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if got := u.String(); got != tt.want {
				t.Errorf("Prepare = %q, want %q", got, tt.want)
			}
			if tt.wantUser != "" {
				pw, _ := u.User.Password()
				if u.User.Username() != tt.wantUser || pw != tt.wantPw {
					t.Errorf("user = %q:%q, want %q:%q", u.User.Username(), pw, tt.wantUser, tt.wantPw)
				}
			}
		})
	}
}

func TestO17ReviewEdgeUnquote(t *testing.T) {
	tests := []struct{ in, want, wantPlus string }{
		{"", "", ""},
		{"%", "%", "%"},
		{"%u00e9", "%u00e9", "%u00e9"},
		{"%2f%2F", "//", "//"},
		{"%2B+", "++", "+ "},
		{"a\x80b%80", "a�b�", "a�b�"},
		{"%C3", "�", "�"},
		{strings.Repeat("%", 3000), strings.Repeat("%", 3000), strings.Repeat("%", 3000)},
		{strings.Repeat("%41", 5000), strings.Repeat("A", 5000), strings.Repeat("A", 5000)},
	}
	for _, tt := range tests {
		if got := Unquote(tt.in); got != tt.want {
			t.Errorf("Unquote(%.40q) = %.40q, want %.40q", tt.in, got, tt.want)
		}
		if got := UnquotePlus(tt.in); got != tt.wantPlus {
			t.Errorf("UnquotePlus(%.40q) = %.40q, want %.40q", tt.in, got, tt.wantPlus)
		}
	}
}

// TestO17ReviewSafeForLogInvalidUTF8 pins the one change of the 1.13 rewrite:
// raw invalid UTF-8 (without any "%") is now replaced by U+FFFD like an
// escaped invalid byte already was, instead of being logged as "%FF".
func TestO17ReviewSafeForLogInvalidUTF8(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://h/p?a=\xff", "https://h/p?a=%EF%BF%BD"},
		{"https://h/p?a=%ff%fe", "https://h/p?a=%EF%BF%BD"},
		{"https://h/a%u00e9", "[INVALID URL REDACTED]"},
		{"https://u:p%3A%40@h/x?token=1", "https://h/x?token=%5BREDACTED%5D"},
	}
	for _, tt := range tests {
		if got := SafeForLog(tt.in); got != tt.want {
			t.Errorf("SafeForLog(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
