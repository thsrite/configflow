package httpxtest

import (
	"net/http"
	"testing"
)

func TestO9ExpectedHeadersPreservesUnrelatedFields(t *testing.T) {
	python := http.Header{"Access-Control-Allow-Origin": {"*"}, "Access-Control-Allow-Methods": {"GET"}, "Access-Control-Allow-Headers": {"Authorization"}, "Vary": {"Origin, Accept-Encoding"}, "Cache-Control": {"no-cache"}}
	goHeader := http.Header{"Content-Type": {"text/plain"}, "X-Content-Type-Options": {"nosniff"}}
	got := O9ExpectedHeaders(python, goHeader)
	if got.Get("Access-Control-Allow-Origin") != "" || got.Get("Vary") != "Accept-Encoding" || got.Get("X-Content-Type-Options") != "nosniff" || got.Get("Cache-Control") != "no-cache" {
		t.Fatal(got)
	}
	if python.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("source headers mutated")
	}
	goHeader.Set("X-Content-Type-Options", "unsafe")
	if O9ExpectedHeaders(python, goHeader).Get("X-Content-Type-Options") != "" {
		t.Fatal("unexpected nosniff value accepted")
	}
	goHeader.Set("X-Content-Type-Options", "nosniff")
	goHeader.Set("Content-Type", "application/json")
	if O9ExpectedHeaders(python, goHeader).Get("X-Content-Type-Options") != "" {
		t.Fatal("non-text header delta accepted")
	}
}

func TestO9ProfileEchoDeltaRequiresExactUnauthorizedBody(t *testing.T) {
	body := []byte(`{"success":false,"message":"请先登录后再操作。"}`)
	if got := O9ExpectedProfileHeader("other", 401, 401, body, body); got != "" {
		t.Fatal(got)
	}
	for _, test := range []struct {
		status int
		body   []byte
	}{
		{403, body}, {401, []byte(`{"success":false,"message":"wrong"}`)},
		{401, []byte(`{"success":false,"message":"请先登录后再操作。","extra":true}`)},
	} {
		if got := O9ExpectedProfileHeader("other", 401, test.status, body, test.body); got != "other" {
			t.Fatal("unapproved response hidden")
		}
	}
}
