package convert

import (
	"net/url"
	"strings"
	"testing"
)

func TestC05ReviewAppendURLQueryBracketedHosts(t *testing.T) {
	tests := []struct {
		name, input string
		reject      bool
	}{
		{"name in brackets", "https://[not-an-ip]/config", true},
		{"IPv4 in brackets", "https://[192.0.2.1]/config", true},
		{"missing bracket", "https://[2001:db8::1/config", true},
		{"IPvFuture", "https://[v1.example]/config", true},
		{"IPv6", "https://[2001:db8::1]:443/config", false},
		{"mapped IPv6", "https://[::ffff:192.0.2.1]/config", false},
		{"zone", "https://[fe80::1%25synthetic]/config", false},
		{"userinfo", "https://user:synthetic@[2001:db8::1]/config", false},
		{"rule path is opaque", "https://panel.example/rules/a://[not-an-ip].yaml", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := AppendQuery(test.input, [][2]string{{"token", "synthetic+a&b"}})
			if test.reject {
				if err == nil || got != "" {
					t.Fatalf("invalid host emitted URL %q, error %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(got, test.input+"?") {
				t.Fatalf("URL path changed: %q", got)
			}
			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Query().Get("token") != "synthetic+a&b" {
				t.Fatal("query credential changed")
			}
		})
	}
}
