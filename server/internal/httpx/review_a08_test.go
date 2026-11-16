package httpx_test

import (
	"net/http/httptest"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
)

// TestQueryInvalidUTF8BecomesReplacementCharacter: query names and values
// are percent-decoded (with "+" as space); invalid UTF-8 becomes U+FFFD
// per maximal invalid sequence (Werkzeug re-quoted it as %XX; accepted N2
// difference, garbage input only)
// and a "%" that starts no escape stays as it is.
func TestQueryInvalidUTF8BecomesReplacementCharacter(t *testing.T) {
	cases := map[string][][2]string{
		"a=%c0%af&b=%e4%b8&c=%FE+x&d=%e4%b8%ad%ff": {{"a", "\uFFFD\uFFFD"}, {"b", "\uFFFD"}, {"c", "\uFFFD x"}, {"d", "中\uFFFD"}},
		"%ff=1&k%e4=%e4a":                          {{"\uFFFD", "1"}, {"k\uFFFD", "\uFFFDa"}},
		"q=%zz%e4&r=%%ff&s=%41+%2B":                {{"q", "%zz\uFFFD"}, {"r", "%\uFFFD"}, {"s", "A +"}},
	}
	for raw, want := range cases {
		got := httpx.ParseQuery(raw)
		if len(got) != len(want) {
			t.Errorf("%q: %q want %q", raw, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%q pair %d: %q want %q", raw, i, got[i], want[i])
			}
		}
	}
}

// TestHostAndURLRootFollowWerkzeug: request.host is Werkzeug 3.1's get_host
// (default port removed, invalid characters → "") and request.url_root goes
// through uri_to_iri (host lower-cased, port as a number, 0 dropped).
// Expectations from Werkzeug 3.1.8 Request(environ) on the test server; the
// install-script route falls back to url_root (found by the A08 review).
func TestHostAndURLRootFollowWerkzeug(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/api/host", "test.host", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.Text(200, c.Host()+" "+c.URLRoot()+" "+c.HostURL()), nil
		})
	})})
	cases := [][3]string{
		{"panel.test:5001", "panel.test:5001", "http://panel.test:5001/"},
		{"panel.test:80", "panel.test", "http://panel.test/"},
		{"PANEL.Test", "PANEL.Test", "http://panel.test/"},
		{"x_y.test:80", "", "http:///"},
		{"[fd00::5]:5001", "[fd00::5]:5001", "http://[fd00::5]:5001/"},
		{"[FD00::5]:80", "[FD00::5]", "http://[fd00::5]/"},
		{"h:1", "h:1", "http://h:1/"},
		{"h:8080", "h:8080", "http://h:8080/"},
		{"1.2.3.4:443", "1.2.3.4:443", "http://1.2.3.4:443/"},
		{"h:80:80", "h:80", "http://h:80/"},
		{"-.", "-.", "http://-./"},
		{"h:0080", "h:0080", "http://h:80/"},
		{"h:0", "h:0", "http://h/"},
		{"h:65535", "h:65535", "http://h:65535/"},
		{"[::FFFF:1.2.3.4]:443", "[::FFFF:1.2.3.4]:443", "http://[::ffff:1.2.3.4]:443/"},
		{"h.:5", "h.:5", "http://h.:5/"},
		{"K.test", "", "http:///"},
		{"\xe4.test", "", "http:///"},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/api/host", nil)
		req.Host = c[0]
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if want := c[1] + " " + c[2] + " " + c[2]; w.Body.String() != want {
			t.Errorf("Host %q: %q want %q", c[0], w.Body.String(), want)
		}
	}
}
