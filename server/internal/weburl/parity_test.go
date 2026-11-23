package weburl

import (
	"net/http"
	"testing"
)

// TestO17ParityPrepare verifies byte-for-byte parity of Prepare (URL string and
// HTTP request target) across all caller patterns and edge-case corpora:
// CJK, spaces, stray/malformed percent escapes, +, #, ?, &, =, IPv6 + zone,
// userinfo, pre-escaped strings, emoji, control characters, and punctuation.
func TestO17ParityPrepare(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		params    [][2]string
		wantURL   string
		wantURI   string // optional: checked via http.NewRequest when non-empty
		wantError bool
	}{
		// --- Subscription / Sub-Store / Airport URLs ---
		{
			name:    "airport standard",
			raw:     "https://sub.example.com/api/v1/client/subscribe?token=abc123&flag=clash",
			wantURL: "https://sub.example.com/api/v1/client/subscribe?token=abc123&flag=clash",
			wantURI: "/api/v1/client/subscribe?token=abc123&flag=clash",
		},
		{
			name:    "airport cjk path and query with pre-escaped plus",
			raw:     "https://sub.example.com/订阅/link?name=机场 一&token=a%2Bb",
			wantURL: "https://sub.example.com/%E8%AE%A2%E9%98%85/link?name=%E6%9C%BA%E5%9C%BA%20%E4%B8%80&token=a%2Bb",
			wantURI: "/%E8%AE%A2%E9%98%85/link?name=%E6%9C%BA%E5%9C%BA%20%E4%B8%80&token=a%2Bb",
		},
		{
			name:    "substore collection cjk",
			raw:     "http://127.0.0.1:3001/download/collection/机场?target=ClashMeta",
			wantURL: "http://127.0.0.1:3001/download/collection/%E6%9C%BA%E5%9C%BA?target=ClashMeta",
			wantURI: "/download/collection/%E6%9C%BA%E5%9C%BA?target=ClashMeta",
		},
		{
			name:    "nested url in query with cjk",
			raw:     "https://sub.example.com/api?url=https://x.example.org/a?b=1&c=中",
			wantURL: "https://sub.example.com/api?url=https://x.example.org/a?b=1&c=%E4%B8%AD",
			wantURI: "/api?url=https://x.example.org/a?b=1&c=%E4%B8%AD",
		},
		{
			name:    "substore append ordered params",
			raw:     "http://127.0.0.1:3001/api/sub?z=1",
			params:  [][2]string{{"b c", "d&e=f+g"}, {"a", "中文🚀"}},
			wantURL: "http://127.0.0.1:3001/api/sub?z=1&b+c=d%26e%3Df%2Bg&a=%E4%B8%AD%E6%96%87%F0%9F%9A%80",
			wantURI: "/api/sub?z=1&b+c=d%26e%3Df%2Bg&a=%E4%B8%AD%E6%96%87%F0%9F%9A%80",
		},
		{
			name:    "append params to queryless url",
			raw:     "https://sub.example.com",
			params:  [][2]string{{"target", "ClashMeta"}, {"emoji", "1"}},
			wantURL: "https://sub.example.com/?target=ClashMeta&emoji=1",
			wantURI: "/?target=ClashMeta&emoji=1",
		},

		// --- Rulefetch / Redirect / LocalHTTP ---
		{
			name:    "rulefetch url requote",
			raw:     "http://127.0.0.1:8080/p a\"t%7eh?q=a b&r=%zz",
			wantURL: "http://127.0.0.1:8080/p%20a%22t%7eh?q=a%20b&r=%25zz",
			wantURI: "/p%20a%22t%7eh?q=a%20b&r=%25zz",
		},
		{
			name:    "rulefetch redirect requote",
			raw:     "http://127.0.0.1:8080/a b\"c<d>|e^f`g{h}%7Ei%41%zz",
			wantURL: "http://127.0.0.1:8080/a%20b%22c%3Cd%3E%7Ce%5Ef%60g%7Bh%7D%7Ei%41%25zz",
			wantURI: "/a%20b%22c%3Cd%3E%7Ce%5Ef%60g%7Bh%7D%7Ei%41%25zz",
		},
		{
			name:    "dot segments resolved",
			raw:     "http://h/a/../b?q=a b&c=机",
			wantURL: "http://h/b?q=a%20b&c=%E6%9C%BA",
		},
		{
			name:    "escaped dot segments kept",
			raw:     "http://h/%2e%2e/x",
			wantURL: "http://h/%2e%2e/x",
		},
		{
			name:    "escaped slash with cjk",
			raw:     "https://sub.example.com/a%2Fb/中文?q=1",
			wantURL: "https://sub.example.com/a%2Fb/%E4%B8%AD%E6%96%87?q=1",
			wantURI: "/a%2Fb/%E4%B8%AD%E6%96%87?q=1",
		},
		{
			name:    "pre-escaped utf8 and slash",
			raw:     "https://sub.example.com/x%20y/%E4%B8%AD%2Fz",
			wantURL: "https://sub.example.com/x%20y/%E4%B8%AD%2Fz",
			wantURI: "/x%20y/%E4%B8%AD%2Fz",
		},
		{
			name:    "double slash preserved",
			raw:     "http://config.test/api/x//y",
			wantURL: "http://config.test/api/x//y",
		},

		// --- Malformed / Stray Percent Escapes ---
		{
			name:    "stray percent in path and query",
			raw:     "https://sub.example.com/s/abc%zz?x=1%",
			wantURL: "https://sub.example.com/s/abc%25zz?x=1%25",
			wantURI: "/s/abc%25zz?x=1%25",
		},
		{
			name:    "stray percent followed by valid escape",
			raw:     "http://h/%%41/%2%42/%",
			wantURL: "http://h/%25%41/%252%42/%25",
			wantURI: "/%25%41/%252%42/%25",
		},
		{
			name:    "already escaped percent not double escaped",
			raw:     "http://h/%252F?k=%2520",
			wantURL: "http://h/%252F?k=%2520",
			wantURI: "/%252F?k=%2520",
		},

		// --- Punctuation: +, #, ?, &, =, [], (), !, ', *, ;, :, @, ~ ---
		{
			name:    "plus in path and query preserved",
			raw:     "https://h/a+b/c?x=1+2&y=a+b",
			wantURL: "https://h/a+b/c?x=1+2&y=a+b",
			wantURI: "/a+b/c?x=1+2&y=a+b",
		},
		{
			name:    "fragment preserved",
			raw:     "https://h/path?a=1&b=2#sec/1?x=中",
			wantURL: "https://h/path?a=1&b=2#sec/1?x=%E4%B8%AD",
		},
		{
			name:    "brackets in query escaped, in path preserved",
			raw:     "http://h/p/[1]?x=[1]&y=a(2)!~*'();:@,$-_.=",
			wantURL: "http://h/p/[1]?x=%5B1%5D&y=a(2)!~*'();:@,$-_.=",
		},
		{
			name:    "unsafe chars in path and query escaped",
			raw:     "https://sub.example.com/x\\y\"z<w>?a=`^|{}",
			wantURL: "https://sub.example.com/x%5Cy%22z%3Cw%3E?a=%60%5E%7C%7B%7D",
			wantURI: "/x%5Cy%22z%3Cw%3E?a=%60%5E%7C%7B%7D",
		},

		// --- Emoji ---
		{
			name:    "emoji in path query and fragment",
			raw:     "https://h/节点🚀/list?tag=🎉&k=a+b#🌍",
			wantURL: "https://h/%E8%8A%82%E7%82%B9%F0%9F%9A%80/list?tag=%F0%9F%8E%89&k=a+b#%F0%9F%8C%8D",
			wantURI: "/%E8%8A%82%E7%82%B9%F0%9F%9A%80/list?tag=%F0%9F%8E%89&k=a+b",
		},

		// --- IPv6 Hosts and Zones ---
		{
			name:    "ipv6 loopback with port",
			raw:     "http://[::1]:8080/api/v1?a=1",
			wantURL: "http://[::1]:8080/api/v1?a=1",
			wantURI: "/api/v1?a=1",
		},
		{
			name:    "ipv6 with bare zone",
			raw:     "http://[fe80::1%eth0]:8080/x",
			wantURL: "http://[fe80::1%25eth0]:8080/x",
		},
		{
			name:    "ipv6 with hex-like bare zone",
			raw:     "http://[fe80::1%ab0]:8080/x",
			wantURL: "http://[fe80::1%25ab0]:8080/x",
		},
		{
			name:    "ipv6 with pre-escaped zone",
			raw:     "http://[fe80::1%25eth0]:8080/x",
			wantURL: "http://[fe80::1%25eth0]:8080/x",
		},
		{
			name:      "ipv6 unbracketed rejected",
			raw:       "http://2001:db8::1/x",
			wantError: true,
		},
		{
			name:      "ipv6 unbalanced bracket rejected",
			raw:       "http://[::1/x",
			wantError: true,
		},

		// --- Userinfo ---
		{
			name:    "userinfo special chars and unicode",
			raw:     "https://用户:pa^ss|w{}@sub.example.com/x?a=1",
			wantURL: "https://%E7%94%A8%E6%88%B7:pa%5Ess%7Cw%7B%7D@sub.example.com/x?a=1",
		},
		{
			name:    "userinfo pre-escaped slash and at",
			raw:     "https://u:p%2Fw%40rd@sub.example.com/x",
			wantURL: "https://u:p%2Fw%40rd@sub.example.com/x",
		},
		{
			name:    "userinfo literal at and stray percent",
			raw:     "https://u:a@b100%@sub.example.com/x",
			wantURL: "https://u:a%40b100%25@sub.example.com/x",
		},
		{
			name:    "userinfo emoji",
			raw:     "https://u:🔑@sub.example.com/x",
			wantURL: "https://u:%F0%9F%94%91@sub.example.com/x",
		},

		// --- WHATWG Control Characters ---
		{
			name:    "leading controls and spaces trimmed, embedded tab/cr/lf stripped",
			raw:     " \x00\x01\x1f\t\r\nhttps://h/a\tb\rc\nd?x=1\n2",
			wantURL: "https://h/abcd?x=12",
		},
		{
			name:      "non-tab control char in path rejected by url.Parse",
			raw:       "https://h/a\x00b",
			wantError: true,
		},
		{
			name:      "DEL control char in query rejected by url.Parse",
			raw:       "https://h/a?x=\x7f",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := Prepare(tt.raw, tt.params)
			if tt.wantError {
				if err == nil {
					t.Fatalf("Prepare(%q) succeeded with %q, want error", tt.raw, u.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("Prepare(%q) unexpected error: %v", tt.raw, err)
			}
			if got := u.String(); got != tt.wantURL {
				t.Errorf("Prepare(%q) = %q, want %q", tt.raw, got, tt.wantURL)
			}
			if tt.wantURI != "" {
				req, err := http.NewRequest("GET", u.String(), nil)
				if err != nil {
					t.Fatalf("http.NewRequest(%q): %v", u.String(), err)
				}
				if got := req.URL.RequestURI(); got != tt.wantURI {
					t.Errorf("RequestURI(%q) = %q, want %q", tt.raw, got, tt.wantURI)
				}
			}
		})
	}
}

// TestO17ParityQuoteUnquoteEncodeQuery verifies byte-for-byte parity of Quote,
// Unquote, UnquotePlus, and EncodeQuery across edge-case corpora.
func TestO17ParityQuoteUnquoteEncodeQuery(t *testing.T) {
	t.Run("Quote", func(t *testing.T) {
		tests := []struct {
			in, want string
		}{
			{"", ""},
			{"abcXYZ0129-._~", "abcXYZ0129-._~"},
			{"a b/+", "a%20b%2F%2B"},
			{"a?b&c=d#e%f", "a%3Fb%26c%3Dd%23e%25f"},
			{"中文 机场🚀", "%E4%B8%AD%E6%96%87%20%E6%9C%BA%E5%9C%BA%F0%9F%9A%80"},
			{"a\x00\t\n\x7fb", "a%00%09%0A%7Fb"},
			{"!$'()*+,;:@[]{}", "%21%24%27%28%29%2A%2B%2C%3B%3A%40%5B%5D%7B%7D"},
		}
		for _, tt := range tests {
			if got := Quote(tt.in); got != tt.want {
				t.Errorf("Quote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		}
	})

	t.Run("Unquote", func(t *testing.T) {
		tests := []struct {
			in, want string
		}{
			{"", ""},
			{"plain+text", "plain+text"},
			{"a%20b%2F%2B", "a b/+"},
			{"a%41%zz%", "aA%zz%"},
			{"%25", "%"},
			{"%252F", "%2F"},
			{"%%41", "%A"},
			{"%2%41", "%2A"},
			{"%E4%B8%AD%E6%96%87%F0%9F%9A%80", "中文🚀"},
			{"%e4%be%8b", "例"},
			{"bad%FFutf8%E4%B8", "bad\uFFFDutf8\uFFFD"},
			{"ctrl%00%0a%7f", "ctrl\x00\n\x7f"},
			{"no-percent-\xff", "no-percent-\xff"},
		}
		for _, tt := range tests {
			if got := Unquote(tt.in); got != tt.want {
				t.Errorf("Unquote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		}
	})

	t.Run("UnquotePlus", func(t *testing.T) {
		tests := []struct {
			in, want string
		}{
			{"", ""},
			{"a+b+c", "a b c"},
			{"a+b%2Bc%20d", "a b+c d"},
			{"a+%41+%zz+%", "a A %zz %"},
			{"%E4%B8%AD+%F0%9F%9A%80", "中 🚀"},
			{"bad+%FF", "bad \uFFFD"},
			{"plus-invalid-\xff+x", "plus-invalid-\xff x"},
		}
		for _, tt := range tests {
			if got := UnquotePlus(tt.in); got != tt.want {
				t.Errorf("UnquotePlus(%q) = %q, want %q", tt.in, got, tt.want)
			}
		}
	})

	t.Run("EncodeQuery", func(t *testing.T) {
		tests := []struct {
			name  string
			pairs [][2]string
			want  string
		}{
			{"empty", nil, ""},
			{"ordered preserved", [][2]string{{"z", "1"}, {"a", "2"}, {"z", "3"}}, "z=1&a=2&z=3"},
			{"mosdns rule proxy", [][2]string{
				{"url", "https://example.com/a b/规则.yaml?k=1&v=2"},
				{"source_id", "rule-1"},
				{"part", "domain"},
				{"token", "t+o/k=e&n"},
			}, "url=https%3A%2F%2Fexample.com%2Fa+b%2F%E8%A7%84%E5%88%99.yaml%3Fk%3D1%26v%3D2&source_id=rule-1&part=domain&token=t%2Bo%2Fk%3De%26n"},
			{"emoji and blank", [][2]string{{"k 🚀", ""}, {"", "v#1"}}, "k+%F0%9F%9A%80=&=v%231"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := EncodeQuery(tt.pairs); got != tt.want {
					t.Errorf("EncodeQuery = %q, want %q", got, tt.want)
				}
			})
		}
	})
}

// TestO17ParitySafeForLog verifies SafeForLog parity across edge cases.
func TestO17ParitySafeForLog(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://user:pw@Example.com:8443/sub?token=abc&x=1#secret", "https://example.com:8443/sub?token=%5BREDACTED%5D&x=1"},
		{"https://h/p?%2574oken=1&k=中🚀", "https://h/p?token=%5BREDACTED%5D&k=%E4%B8%AD%F0%9F%9A%80"},
		{"https://h/p?a=%zz", "[INVALID URL REDACTED]"},
		{"https://u:100%@h/p", "[INVALID URL REDACTED]"},
		{"https://h/p?a=1\nb", "[INVALID URL REDACTED]"},
		{"https://h/p?a=%00", "[INVALID URL REDACTED]"},
		{"https://h/p?a=%25%25%25%25%25%25%25%25%2541", "[INVALID URL REDACTED]"},
		{"https://[fe80::1%eth0]:8443/api?key=1&ok=2", "[INVALID URL REDACTED]"},
		{"https://[::1]:8443/api?key=1&ok=2", "https://[::1]:8443/api?key=%5BREDACTED%5D&ok=2"},
		{"/api/v1?x=1&auth=secret&y=2", "/api/v1?x=1&auth=%5BREDACTED%5D&y=2"},
	}
	for _, tt := range tests {
		if got := SafeForLog(tt.in); got != tt.want {
			t.Errorf("SafeForLog(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
