package reqproxy

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

type referenceCase struct {
	Env    [][2]string `json:"env"`
	URL    string      `json:"url"`
	Result struct {
		Proxy *string `json:"proxy"`
		Auth  *string `json:"auth"`
		Error string  `json:"error"`
	} `json:"result"`
}

func errorClass(err error) string {
	switch {
	case errors.Is(err, ErrInvalidURL):
		return "InvalidURL"
	case errors.Is(err, ErrInvalidProxyURL):
		return "InvalidProxyURL"
	case errors.Is(err, ErrInvalidSchema):
		return "InvalidSchema"
	}
	class, _, _ := strings.Cut(err.Error(), ":")
	return class
}

// TestReferenceEnvironmentProxies replays the agents corpus of real requests
// 2.31 / urllib3 2.8 results (lowercase variables winning, REQUEST_METHOD,
// ALL_PROXY, NO_PROXY suffix/CIDR/host:port/'*', no loopback bypass, proxy
// URL forms, ports, Proxy-Authorization) against the shared package.
func TestReferenceEnvironmentProxies(t *testing.T) {
	data, err := os.ReadFile("../agents/testdata/proxy_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []referenceCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 900 {
		t.Fatalf("only %d cases", len(cases))
	}
	for _, c := range cases {
		environ := make([]string, len(c.Env))
		for i, pair := range c.Env {
			environ[i] = pair[0] + "=" + pair[1]
		}
		proxy, err := FromEnvironment(environ, c.URL)
		var got, want string
		switch {
		case err != nil:
			got = "error " + errorClass(err)
		case proxy == nil:
			got = "direct"
		default:
			got = proxy.String() + " auth=" + proxy.Auth
		}
		switch {
		case c.Result.Error != "":
			want = "error " + c.Result.Error
		case c.Result.Proxy == nil:
			want = "direct"
		default:
			auth := ""
			if c.Result.Auth != nil {
				auth = *c.Result.Auth
			}
			want = *c.Result.Proxy + " auth=" + auth
		}
		if got != want {
			t.Errorf("env %q url %q: got %s, want %s", c.Env, c.URL, got, want)
		}
	}
}

func TestInetAtonExact(t *testing.T) {
	for in, want := range map[string]string{"127.1": "127.0.0.1", "0x7f.0.0.1": "127.0.0.1", "2130706433": "127.0.0.1",
		"0177.0.0.1": "127.0.0.1", "1.2.3.4.5": "", "08.1.1.1": "", "a.b": "", "": ""} {
		got, ok := InetAtonExact(in)
		if !ok {
			got = ""
		}
		if got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
