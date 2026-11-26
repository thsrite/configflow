package redact

import (
	"strings"
	"testing"
)

// Review R15 F1: a credential-free origin followed directly by Chinese
// punctuation stays readable; any userinfo, path, query or later '@' keeps
// the conservative whole-URL redaction.
func TestReviewR15OriginBeforeChinesePunctuation(t *testing.T) {
	for _, tc := range []struct {
		text    string
		visible bool
	}{
		{"iKuai 要求跳转到 https://198.51.100.7:8443（HTTP 302）。请核对", true},
		{"跳转到 http://[2001:db8::1]:8080，请核对", true},
		{"跳转到 https://user:synthetic-pass@198.51.100.7（HTTP 302）", false},
		{"跳转到 https://198.51.100.7:8443（x@y）", false},
		{"跳转到 https://198.51.100.7/login?token=synthetic-token（HTTP 302）", false},
		{"跳转到 https://198.51.100.7:bad（HTTP 302）", false},
	} {
		got := string(SanitizeCredentials(tc.text, nil))
		if visible := !strings.Contains(got, Redacted); visible != tc.visible {
			t.Errorf("%q -> %s, visible=%v", tc.text, got, visible)
		}
		if strings.Contains(got, "synthetic-") {
			t.Errorf("credential leaked: %s", got)
		}
	}
}
