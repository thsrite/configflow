package weburl

import (
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
		"http://Config.TEST:080/api/x":       "http://config.test:80/api/x",
		"http://h/a/../b?q=a b&c=机":          "http://h/b?q=a%20b&c=%E6%9C%BA",
		"http://h":                           "http://h/",
		"http://h/a%zz%41":                   "http://h/a%25zzA",
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
