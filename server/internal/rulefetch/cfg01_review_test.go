package rulefetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// cfg01Review is testdata/cfg01_review.json, written by gen_cfg01_review.py
// from real Python (request_rule against loopback fakes, is_internal_rule_url).
type cfg01Review struct {
	Network []struct {
		Name, Start, Family string
		SourceLocations     []string `json:"source_locations"`
		ExternalLocations   []string `json:"external_locations"`
		ServerDomain        string   `json:"server_domain"`
		Status              int
		Python              struct {
			Refused    bool
			ErrorClass string `json:"error_class"`
			Status     int
			Body       string
			Forbidden  int
		}
	}
	Classify []struct {
		Name, URL      string
		ServerDomain   string `json:"server_domain"`
		PythonInternal *bool  `json:"python_internal"`
	}
}

func loadCFG01Review(t *testing.T) cfg01Review {
	t.Helper()
	data, err := os.ReadFile("testdata/cfg01_review.json")
	if err != nil {
		t.Fatal(err)
	}
	var review cfg01Review
	if err := json.Unmarshal(data, &review); err != nil {
		t.Fatal(err)
	}
	return review
}

// Python accepted these, Go refuses them under the normalized API boundary
// (security review of CFG-01; each is listed in review-cfg-c14-security.md).
var cfg01ReviewGoStricter = map[string]bool{
	"redir-encoded-mixed":             true, // Python requested /api/../landing
	"redir-encoded-slash":             true,
	"redir-encoded-backslash":         true,
	"redir-backslash-traversal":       true, // escapes the API if "\\" is a separator
	"redir-backslash-scheme-relative": true,
	"redir-http-colon-relative":       true, // Location "http:ok": a relative path only to urljoin
	"prefix-dot-out-no-redirect":      true, // the API URL itself leaves /cf/api/
}

// Host aliases neither Python nor Go recognizes as the panel (a syntactic
// origin check cannot); both treat them as ordinary external sources.
var cfg01ReviewPythonUnprotected = map[string]bool{
	"alias-ip-for-localhost": true,
	"v6-expanded":            true,
}

func TestCFG01ReviewNetworkAgainstPython(t *testing.T) {
	review := loadCFG01Review(t)
	if len(review.Network) < 60 {
		t.Fatal("missing review payloads")
	}
	for _, tt := range review.Network {
		t.Run(tt.Name, func(t *testing.T) {
			py := tt.Python
			if py.Refused && py.ErrorClass != "ValueError" {
				t.Fatalf("Python reference refused outside the URL boundary: %s", py.ErrorClass)
			}
			var mu sync.Mutex
			forbidden := 0
			counts := map[bool]int{}
			var values map[string]string
			fill := func(text string) string {
				for _, key := range []string{"origin", "external", "hostname", "host", "ehost", "port"} {
					text = strings.ReplaceAll(text, "{"+key+"}", values[key])
				}
				return text
			}
			handler := func(external bool) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					locations := tt.SourceLocations
					if external {
						locations = tt.ExternalLocations
					}
					mu.Lock()
					index := counts[external]
					counts[external]++
					landing := strings.HasSuffix(r.URL.EscapedPath(), "/landing")
					if landing {
						forbidden++
					}
					mu.Unlock()
					if !landing && index < len(locations) {
						w.Header().Set("Location", fill(locations[index]))
						w.WriteHeader(tt.Status)
					}
					fmt.Fprint(w, "ok")
				}
			}
			source := httptest.NewUnstartedServer(handler(false))
			hostname := "127.0.0.1"
			if tt.Family == "v6" {
				listener, err := net.Listen("tcp6", "[::1]:0")
				if err != nil {
					t.Skipf("no IPv6 loopback: %v", err)
				}
				source.Listener.Close()
				source.Listener = listener
				hostname = "[::1]"
			}
			source.Start()
			defer source.Close()
			external := httptest.NewServer(handler(true))
			defer external.Close()
			_, port, _ := net.SplitHostPort(source.Listener.Addr().String())
			values = map[string]string{"hostname": hostname, "port": port, "host": hostname + ":" + port,
				"ehost": strings.TrimPrefix(external.URL, "http://")}
			values["origin"] = "http://" + values["host"]
			values["external"] = external.URL

			limit := 8
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			client := &Client{Transport: HTTPTransport{}, Sleep: func(time.Duration) {}}
			response, err := client.Download(ctx, fill(tt.Start), DownloadRequest{Config: sysConfig("server_domain", fill(tt.ServerDomain)),
				MaxRedirects: &limit, ConnectTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second})
			var body []byte
			if response != nil {
				body, _ = response.Content()
				response.Close()
			}
			mu.Lock()
			hits := forbidden
			mu.Unlock()

			switch {
			case cfg01ReviewPythonUnprotected[tt.Name]:
				if py.Refused || err != nil || hits != py.Forbidden {
					t.Fatalf("alias outcome changed: Python refused=%v forbidden=%d, Go err=%v forbidden=%d", py.Refused, py.Forbidden, err, hits)
				}
			case py.Refused || cfg01ReviewGoStricter[tt.Name]:
				if err == nil {
					t.Fatalf("Go accepted a URL outside the API boundary (status %d)", response.StatusCode)
				}
				if hits != 0 {
					t.Fatalf("forbidden destination requested %d times", hits)
				}
				// The route answers 400 Invalid remote URL only for ValueError.
				if class := errorClass(err); class != "ValueError" {
					t.Fatalf("refusal is %s, not a URL error: %v", class, err)
				}
			default:
				if py.Forbidden != 0 {
					t.Fatal("unclassified Python forbidden request")
				}
				if err != nil || response.StatusCode != py.Status || string(body) != py.Body || hits != 0 {
					t.Fatalf("valid source changed: err=%v body=%q forbidden=%d", err, body, hits)
				}
			}
		})
	}
}

// Host spellings that cannot be dialled without DNS: Python's real
// is_internal_rule_url decides whether the API boundary applies. Go must
// refuse every URL Python treats as internal; where Python does not, the
// expected Go outcome is fixed here (true = refused before any landing).
var cfg01ReviewClassifyGo = map[string]bool{
	"idn-upper": true, "ascii-upper": true, "explicit-default-port": true, "leading-zero-default-port": true,
	"https-explicit-default-port": true, "userinfo": true, "v6-upper": true,
	// Go compares host names the way its transport dials them (IDNA for
	// non-ASCII; net/url decodes %XX in a host), so these are also the panel.
	"idn-punycode": true, "idn-fullwidth": true, "percent-host": true,
	// Neither side recognizes these aliases (see cfg01ReviewPythonUnprotected).
	"trailing-dot": false, "scheme-mismatch": false, "ipv4-decimal": false, "ipv4-octal": false,
	"ipv4-hex": false, "ipv4-short": false, "ipv4-any": false, "ipv4-mapped-v6": false,
	"localhost-for-ip": false, "v6-expanded": false,
}

func TestCFG01ReviewHostClassification(t *testing.T) {
	review := loadCFG01Review(t)
	if len(review.Classify) != len(cfg01ReviewClassifyGo) {
		t.Fatalf("classification cases %d, expectations %d", len(review.Classify), len(cfg01ReviewClassifyGo))
	}
	for _, tt := range review.Classify {
		t.Run(tt.Name, func(t *testing.T) {
			want, ok := cfg01ReviewClassifyGo[tt.Name]
			if !ok || tt.PythonInternal == nil {
				t.Fatal("unclassified case")
			}
			if *tt.PythonInternal && !want {
				t.Fatal("Python treats this URL as internal; Go must refuse it")
			}
			landing := 0
			transport := &fakeTransport{reply: func(n int, req TransportRequest) (*Response, error) {
				if strings.HasSuffix(req.URL, "/landing") {
					landing++
				}
				header := http.Header{}
				status := 200
				if n == 1 {
					header.Set("Location", "http://outside.example/landing")
					status = 302
				}
				return &Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("ok"))}, nil
			}}
			client := &Client{Transport: transport, Sleep: func(time.Duration) {}}
			response, err := client.Download(context.Background(), tt.URL, DownloadRequest{Config: sysConfig("server_domain", tt.ServerDomain)})
			if response != nil {
				response.Close()
			}
			refused := err != nil && landing == 0
			if refused != want {
				t.Fatalf("Go refused=%v (err=%v, landing=%d, calls=%v), want %v", refused, err, landing, transport.urls(), want)
			}
			if refused && len(transport.calls) != 0 {
				t.Fatalf("refused only after a request: %v", transport.urls())
			}
		})
	}
}
