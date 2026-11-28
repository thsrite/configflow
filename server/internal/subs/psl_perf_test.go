package subs

import "testing"

func TestExtractDomainDotForms(t *testing.T) {
	for _, private := range []bool{false, true} {
		for _, host := range []string{"Sub。Example．com｡", "Sub．Example｡com。", "Sub.Example.com."} {
			s, d, p := ExtractDomain(host, private)
			if s != "Sub" || d != "Example" || p != "com" {
				t.Fatalf("%q: (%q, %q, %q)", host, s, d, p)
			}
		}
	}
}

func BenchmarkExtractDomainASCII(b *testing.B) {
	ExtractDomain("host.example.com", false)
	b.ReportAllocs()
	for b.Loop() {
		ExtractDomain("host.example.com", false)
	}
}
