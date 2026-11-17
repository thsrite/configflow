package rulefetch

import (
	"context"
	"errors"
	"io"

	"configflow/internal/localhttp"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCFG01InternalRedirectBoundary(t *testing.T) {
	data, err := os.ReadFile("testdata/cfg01.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Start string
		Locations   []string
		Refused     bool
		Python      struct {
			Refused    bool
			Status     int
			Body       string
			Forbidden  int
			ErrorClass string `json:"error_class"`
		}
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 20 {
		t.Fatal("missing Python boundary controls")
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			if tt.Python.Refused && tt.Python.ErrorClass != "ValueError" {
				t.Fatal("Python reference failed outside the URL boundary")
			}
			// Four Python-accepted ambiguous paths are intentionally refused under the
			// same normalized API-boundary policy; no general comparison exemption.
			if tt.Refused != tt.Python.Refused && tt.Name != "redirect-encoded-dot" && tt.Name != "redirect-double-encoded-dot" && tt.Name != "redirect-backslash" && tt.Name != "encoded-api-dot" {
				t.Fatal("unclassified Python outcome")
			}
			var mu sync.Mutex
			sourceCount, forbidden := 0, 0
			var source, external *httptest.Server
			handler := func(destination bool) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					index := sourceCount
					if !destination {
						sourceCount++
					}
					blocked := strings.HasSuffix(r.URL.EscapedPath(), "/landing")
					if blocked {
						forbidden++
					}
					mu.Unlock()
					if !destination && !blocked && index < len(tt.Locations) {
						location := strings.NewReplacer("{origin}", source.URL, "{external}", external.URL).Replace(tt.Locations[index])
						w.Header().Set("Location", location)
						w.WriteHeader(http.StatusFound)
						return
					}
					fmt.Fprint(w, "ok")
				}
			}
			source = httptest.NewServer(handler(false))
			defer source.Close()
			external = httptest.NewServer(handler(true))
			defer external.Close()
			start := strings.NewReplacer("{origin}", source.URL, "{external}", external.URL).Replace(tt.Start)
			limit := 8
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client := &Client{Transport: HTTPTransport{}, Sleep: func(time.Duration) {}}
			response, err := client.RequestRule(ctx, start, Request{Config: sysConfig("server_domain", source.URL), MaxRedirects: &limit, ConnectTimeout: time.Second, ReadTimeout: time.Second})
			if (err != nil) != tt.Refused {
				if response != nil {
					response.Close()
				}
				t.Fatalf("boundary outcome differs: error=%v refused=%v", err, tt.Refused)
			}
			if tt.Refused && !errors.Is(err, ErrInternalRedirect) {
				t.Fatalf("not an API-boundary refusal: %v", err)
			}
			if !tt.Refused {
				body, readErr := response.Content()
				response.Close()
				if readErr != nil || response.StatusCode != tt.Python.Status || string(body) != tt.Python.Body {
					t.Fatalf("valid source changed: %v %q", readErr, body)
				}
			}
			mu.Lock()
			hits := forbidden
			mu.Unlock()
			if hits != 0 {
				t.Fatalf("forbidden destination requested %d times", hits)
			}
		})
	}
}

func TestCFG01ValidatesBeforeLocalCallback(t *testing.T) {
	tests := []struct {
		path    string
		allowed bool
	}{
		{"/api/rules", true}, {"/api/first/../rules", true}, {"/api/rules?value=%252e%252e", true},
		{"/api/../landing", false}, {"/api/%2e%2e/landing", false}, {"/api/%252e%252e/landing", false},
		{"/api/..\\landing", false}, {"/%61pi/../landing", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			calls := 0
			transport := &fakeTransport{reply: func(int, TransportRequest) (*Response, error) {
				t.Fatal("internal request escaped to transport")
				return nil, nil
			}}
			client := &Client{Transport: transport, Sleep: func(time.Duration) {}, Local: func(_ context.Context, req localhttp.Request) (*localhttp.Response, error) {
				calls++
				if strings.Contains(req.URL, "/../") {
					t.Fatal("callback received unnormalized path")
				}
				return &localhttp.Response{StatusCode: 200, URL: req.URL, Body: io.NopCloser(strings.NewReader("ok"))}, nil
			}}
			response, err := client.RequestRule(context.Background(), "http://panel.example.com"+tt.path, Request{Config: sysConfig("server_domain", "http://panel.example.com")})
			if response != nil {
				response.Close()
			}
			wantCalls := 0
			if tt.allowed {
				wantCalls = 1
			}
			if (err == nil) != tt.allowed || calls != wantCalls {
				t.Fatalf("callback boundary: calls=%d error=%v", calls, err)
			}
			if !tt.allowed && !errors.Is(err, ErrInternalRedirect) {
				t.Fatalf("lost boundary cause: %v", err)
			}
		})
	}
}
