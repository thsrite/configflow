package substore

import (
	"context"
	"strings"
	"testing"
)

func TestN82ReviewNativeEnvironmentProxy(t *testing.T) {
	proxy := newRecordingProxy(t)
	for _, tt := range []struct {
		name   string
		env    []string
		reject bool
	}{
		{"ALL fallback", []string{"ALL_PROXY=" + proxy.srv.URL}, false},
		{"lower ALL fallback", []string{"all_proxy=" + proxy.srv.URL, "ALL_PROXY=http://127.0.0.1:1"}, false},
		{"scheme preferred", []string{"HTTP_PROXY=" + proxy.srv.URL, "ALL_PROXY=http://127.0.0.1:1"}, false},
		{"lower scheme preferred", []string{"http_proxy=" + proxy.srv.URL, "HTTP_PROXY=http://127.0.0.1:1"}, false},
		{"CGI uppercase", []string{"REQUEST_METHOD=GET", "HTTP_PROXY=" + proxy.srv.URL}, true},
		{"CGI ALL", []string{"REQUEST_METHOD=GET", "ALL_PROXY=" + proxy.srv.URL}, true},
		{"CGI ALL cannot override", []string{"REQUEST_METHOD=GET", "HTTP_PROXY=" + proxy.srv.URL, "ALL_PROXY=" + proxy.srv.URL}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withEnviron(t, tt.env...)
			before := len(proxy.requests())
			response, err := (RequestsClient{}).Do(context.Background(), &Request{Method: "GET", URL: "http://substore.example:80/api/utils/env", Timeout: Seconds(3)})
			if tt.reject {
				if err == nil || !strings.Contains(err.Error(), "CGI") || len(proxy.requests()) != before {
					t.Fatal("CGI sent a request")
				}
				return
			}
			if err != nil || response.StatusCode != 200 || len(proxy.requests()) != before+1 {
				t.Fatalf("proxy request: %v", err)
			}
			if got := proxy.requests()[before]; got != "GET http://substore.example:80/api/utils/env auth=" {
				t.Fatal("proxy target/credentials changed")
			}
		})
	}
}
