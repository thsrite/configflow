package weburl

import (
	"net/http"
	"net/url"
	"testing"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestFromEnvironment(t *testing.T) {
	cases := []struct {
		env    []string
		target string
		want   string // "" = direct
	}{
		{nil, "http://example.com/", ""},
		{[]string{"HTTP_PROXY=http://p:3128"}, "http://127.0.0.1:8080/api", "http://p:3128"},
		{[]string{"HTTP_PROXY=http://p:3128"}, "http://localhost/", "http://p:3128"},
		{[]string{"HTTP_PROXY=http://p:3128"}, "https://example.com/", ""},
		{[]string{"HTTPS_PROXY=p:3128"}, "https://example.com/", "http://p:3128"},
		{[]string{"ALL_PROXY=http://all:1"}, "https://example.com/", "http://all:1"},
		{[]string{"HTTP_PROXY=http://upper:1", "http_proxy=http://lower:1"}, "http://x/", "http://lower:1"},
		{[]string{"HTTP_PROXY=http://p:1", "NO_PROXY=example.com"}, "http://a.example.com/", ""},
		{[]string{"HTTP_PROXY=http://p:1", "NO_PROXY=.example.com"}, "http://example.com/", ""},
		{[]string{"HTTP_PROXY=http://p:1", "NO_PROXY=example.com"}, "http://badexample.com/", "http://p:1"},
		{[]string{"HTTP_PROXY=http://p:1", "no_proxy=10.0.0.0/8"}, "http://10.1.2.3:80/", ""},
		{[]string{"HTTP_PROXY=http://p:1", "NO_PROXY=fd00::/8"}, "http://[fd00::1]/", ""},
		{[]string{"HTTP_PROXY=http://p:1", "NO_PROXY=host:8080"}, "http://host:8080/", ""},
		{[]string{"HTTP_PROXY=http://p:1", "NO_PROXY=host:8080"}, "http://host/", "http://p:1"},
		{[]string{"HTTP_PROXY=http://p:1", "NO_PROXY= * "}, "http://x/", ""},
		{[]string{"HTTP_PROXY=http://u:pw@p:1"}, "http://x/", "http://u:pw@p:1"},
	}
	for _, c := range cases {
		got, err := FromEnvironment(c.env, mustParse(t, c.target))
		text := ""
		if got != nil {
			text = got.String()
		}
		if err != nil || text != c.want {
			t.Errorf("%v %s: %q %v, want %q", c.env, c.target, text, err, c.want)
		}
	}
	if _, err := FromEnvironment([]string{"HTTP_PROXY=ftp://p:1"}, mustParse(t, "http://x/")); err == nil {
		t.Error("ftp proxy accepted")
	}
}

func TestPrepare(t *testing.T) {
	cases := map[string]string{
		"http://Config.TEST:8080/api/x":      "http://Config.TEST:8080/api/x",
		"http://h/a/../b?q=a b&c=机":          "http://h/b?q=a%20b&c=%E6%9C%BA",
		"http://h":                           "http://h/",
		"http://h/a%zz%41":                   "http://h/a%25zz%41",
		"http://h/p?x=[1]":                   "http://h/p?x=%5B1%5D",
		"https://u:p@h/x#frag":               "https://u:p@h/x#frag",
		"http://h/%2e%2e/x":                  "http://h/%2e%2e/x",
		"http://h/sub?token=abc&target=x%2F": "http://h/sub?token=abc&target=x%2F",
	}
	for in, want := range cases {
		u, err := Prepare(in, nil)
		if err != nil || u.String() != want {
			t.Errorf("%q: %v %v, want %q", in, u, err, want)
		}
	}
	u, _ := Prepare("http://h/x?a=1", [][2]string{{"b c", "d&e"}})
	if u.String() != "http://h/x?a=1&b+c=d%26e" {
		t.Errorf("params: %s", u)
	}
}

func TestSafeForLog(t *testing.T) {
	cases := map[string]string{
		"https://user:pw@Example.com:8443/sub?token=abc&x=1#secret": "https://example.com:8443/sub?token=%5BREDACTED%5D&x=1",
		"https://h/p?u=" + url.QueryEscape("https://i/?key=1"):      "https://h/p?u=%5BREDACTED%5D",
		"https://h/p?%2574oken=1":                                   "https://h/p?token=%5BREDACTED%5D",
		"https://h/p?a=%zz":                                         "[INVALID URL REDACTED]",
		"not a url":                                                 "[INVALID URL REDACTED]",
		"/api/x?password=1":                                         "/api/x?password=%5BREDACTED%5D",
	}
	for in, want := range cases {
		if got := SafeForLog(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestUnquoteIsLenient(t *testing.T) {
	if got := Unquote("a%41%zz%"); got != "aA%zz%" {
		t.Error(got)
	}
	if got := Quote("a b/+"); got != "a%20b%2F%2B" {
		t.Error(got)
	}
}

func TestParseZone(t *testing.T) {
	for in, want := range map[string]string{
		"udp://[fe80::1%eth0]:53":   "fe80::1%eth0",
		"udp://[fe80::1%25eth0]:53": "fe80::1%eth0",
		"udp://[::1]:53":            "::1",
	} {
		u, err := Parse(in)
		if err != nil || u.Hostname() != want {
			t.Errorf("%q: %v %v, want %q", in, u, err, want)
		}
	}
}

// Regression (review N2): what a server sees for realistic subscription and
// rule URLs, compared with requests 2.31 on the test server (synthetic
// values). Path and query bytes must match; escapes stay as written.
func TestPrepareRequestLine(t *testing.T) {
	cases := map[string]string{
		"https://sub.example.com/api/v1/client/subscribe?token=abc123&flag=clash": "/api/v1/client/subscribe?token=abc123&flag=clash",
		"https://sub.example.com/订阅/link?name=机场 一&token=a%2Bb":                   "/%E8%AE%A2%E9%98%85/link?name=%E6%9C%BA%E5%9C%BA%20%E4%B8%80&token=a%2Bb",
		"https://sub.example.com/s/abc%zz?x=1%":                                   "/s/abc%25zz?x=1%25",
		"https://sub.example.com/a%2Fb/中文?q=1":                                    "/a%2Fb/%E4%B8%AD%E6%96%87?q=1",
		"https://sub.example.com/x%20y/%E4%B8%AD%2Fz":                             "/x%20y/%E4%B8%AD%2Fz",
		"https://sub.example.com/x?token=abc|def&k={v}":                           "/x?token=abc%7Cdef&k=%7Bv%7D",
		"http://127.0.0.1:3001/download/collection/机场?target=ClashMeta":           "/download/collection/%E6%9C%BA%E5%9C%BA?target=ClashMeta",
		"https://sub.example.com/x\\y\"z<w>?a=`^":                                 "/x%5Cy%22z%3Cw%3E?a=%60%5E",
		"https://sub.example.com/api?url=https://x.example.org/a?b=1&c=中":         "/api?url=https://x.example.org/a?b=1&c=%E4%B8%AD",
	}
	for in, want := range cases {
		u, err := Prepare(in, nil)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil || req.URL.RequestURI() != want {
			t.Errorf("%q: request line %q (%v), want %q", in, req.URL.RequestURI(), err, want)
		}
	}
}

// Regression (review N2): credentials with characters net/url refuses in
// user information (requests sent them) still parse, as typed.
func TestParseCredentials(t *testing.T) {
	for in, want := range map[string][2]string{
		"https://user:pa^ss@sub.example.com/x":     {"user", "pa^ss"},
		"https://user:p|ss{}@sub.example.com/x":    {"user", "p|ss{}"},
		"https://user:pass word@sub.example.com/x": {"user", "pass word"},
		"https://用户:密码@sub.example.com/x":          {"用户", "密码"},
		"https://user:p%40ss@sub.example.com/x":    {"user", "p@ss"},
		"https://user:100%@sub.example.com/x":      {"user", "100%"},
		"https://u:a@b@sub.example.com/x":          {"u", "a@b"},
	} {
		u, err := Parse(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		password, _ := u.User.Password()
		if u.User.Username() != want[0] || password != want[1] || u.Hostname() != "sub.example.com" {
			t.Errorf("%q: %q %q %q, want %q", in, u.User.Username(), password, u.Hostname(), want)
		}
		if _, err := Prepare(in, nil); err != nil {
			t.Errorf("Prepare(%q): %v", in, err)
		}
	}
	proxy, err := ParseProxy("http://user:p^w@proxy.example:3128")
	if err != nil {
		t.Fatal(err)
	}
	if password, _ := proxy.User.Password(); password != "p^w" {
		t.Errorf("proxy password %q", password)
	}
}

// Regression (review N2): an encoded "/" in a password must not move the
// rest of the password into the port or path of the logged URL.
func TestSafeForLogCredentials(t *testing.T) {
	for in, want := range map[string]string{
		"https://u:12%2F34@sub.example.com/x": "https://sub.example.com/x",
		"https://u:ab%3Fcd@sub.example.com/x": "https://sub.example.com/x",
		"https://u:ab%23cd@sub.example.com/x": "https://sub.example.com/x",
		"https://u:p^w@sub.example.com/x?a=1": "https://sub.example.com/x?a=1",
		"https://u%40x:p@sub.example.com/x":   "https://sub.example.com/x",
	} {
		if got := SafeForLog(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
