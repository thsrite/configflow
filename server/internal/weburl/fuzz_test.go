package weburl

import (
	"net/url"
	"strings"
	"testing"
)

func referenceUnquote(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			hi := refUnhex(s[i+1])
			lo := refUnhex(s[i+2])
			b.WriteByte(hi<<4 | lo)
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return strings.ToValidUTF8(b.String(), "\uFFFD")
}

func refUnhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

func FuzzUnquoteParity(f *testing.F) {
	seeds := []string{
		"",
		"plain+text",
		"a%41%zz%",
		"%25",
		"%252F",
		"%%41",
		"%2%41",
		"%E4%B8%AD%E6%96%87%F0%9F%9A%80",
		"bad%FFutf8%E4%B8",
		"a+b%2Bc%20d",
		"plus-invalid-\xff+x",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := Unquote(s), referenceUnquote(s); got != want {
			t.Fatalf("Unquote(%q) = %q, want %q", s, got, want)
		}
		wantPlus := referenceUnquote(strings.ReplaceAll(s, "+", " "))
		if got := UnquotePlus(s); got != wantPlus {
			t.Fatalf("UnquotePlus(%q) = %q, want %q", s, got, wantPlus)
		}
	})
}

func FuzzParseAndPrepare(f *testing.F) {
	seeds := []string{
		"https://sub.example.com/api/v1/client/subscribe?token=abc123&flag=clash",
		"https://sub.example.com/订阅/link?name=机场 一&token=a%2Bb",
		"https://sub.example.com/s/abc%zz?x=1%",
		"https://sub.example.com/a%2Fb/中文?q=1",
		"http://h/p/[1]?x=[1]",
		"http://[fe80::1%eth0]:8080/x",
		"http://[fe80::1%ab0]:8080/x",
		"https://用户:pa^ss|w{}@sub.example.com/x#frag",
		" \x00\t\r\nhttps://h/a\tb?x=1\n2",
		"https://h/节点🚀?tag=🎉",
		"http://::1/x",
		"http://[::1/x",
	}
	for _, s := range seeds {
		f.Add(s, "k", "v")
	}
	f.Fuzz(func(t *testing.T, raw, pk, pv string) {
		u, err := Parse(raw)
		if err == nil && u != nil {
			_ = Hostname(u)
			_, _, _ = Port(u)
			_ = HasCredentials(u)
			_ = RawPath(u)
		}
		p, err := Prepare(raw, nil)
		if err == nil && p != nil {
			s := p.String()
			// Idempotence: when net/url itself accepts the formatted URL,
			// preparing it a second time must keep the same string.
			if _, stdErr := url.Parse(s); stdErr == nil {
				p2, err2 := Prepare(s, nil)
				if err2 != nil {
					t.Fatalf("Prepare(Prepare(%q) = %q) failed: %v", raw, s, err2)
				}
				if p2.String() != s {
					t.Fatalf("Prepare not idempotent for %q: first=%q second=%q", raw, s, p2.String())
				}
			}
		}
		if pk != "" || pv != "" {
			_, _ = Prepare(raw, [][2]string{{pk, pv}})
		}
	})
}

func FuzzSafeForLog(f *testing.F) {
	seeds := []string{
		"",
		"https://user:pw@Example.com:8443/sub?token=abc&x=1#secret",
		"https://h/p?%2574oken=1",
		"https://h/p?a=%zz",
		"https://u:12%2F34@sub.example.com/x",
		"https://[::1]:8443/api?key=1&ok=2",
		"/api/x?password=1",
		"https://h/p?a=%25%25%25%25%25%25%25%25%2541",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		out := SafeForLog(raw)
		if HasControl(out) {
			t.Fatalf("SafeForLog(%q) returned control char: %q", raw, out)
		}
	})
}
