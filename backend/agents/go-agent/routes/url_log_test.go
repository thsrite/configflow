package routes

import (
	"agent/internal/logx"
	"strings"
	"testing"
)

func TestRoutesRedactURLForLogRedactsActualReviewNestedCredentials(t *testing.T) {
	for _, raw := range []string{
		"https://example.test/p?next=https%253A%252F%252Fnested.test%252Fp%253Ftoken%253Dnested-url-secret",
		"https://example.test/p?next=token%253Dnested-expression-secret",
		"https://example.test/p?next=api%252Bkey%253Dplus-layer-secret",
	} {
		got := logx.RedactURL(raw)
		if !strings.Contains(got, "next=[REDACTED]") {
			t.Fatalf("nested credential value was not redacted: raw=%q got=%q", raw, got)
		}
		for _, secret := range []string{"nested-url-secret", "nested-expression-secret", "plus-layer-secret"} {
			if strings.Contains(got, secret) {
				t.Fatalf("nested credential leaked %q: raw=%q got=%q", secret, raw, got)
			}
		}
	}
}

func TestRoutesRedactURLForLogFailsClosedWhenDecodingRevealsC0(t *testing.T) {
	for _, encodedControl := range []string{"%250d%250a", "%2500", "%251f", "%257f"} {
		raw := "https://example.test/p?next=ok" + encodedControl + "token=secret"
		if got := logx.RedactURL(raw); got != "[INVALID URL REDACTED]" {
			t.Fatalf("decoded control did not fail closed: raw=%q got=%q", raw, got)
		}
	}
}
