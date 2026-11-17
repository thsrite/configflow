package agents

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestN82ReviewNativeEnvironmentProxy(t *testing.T) {
	var requests atomic.Int64
	proxy := listenHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Host != "agent.example:80" {
			t.Error("wrong proxy target")
		}
		w.Write([]byte("proxy"))
	}))
	for _, tt := range []struct {
		name   string
		env    [][2]string
		reject bool
	}{
		{"ALL fallback", [][2]string{{"ALL_PROXY", "http://" + proxy}}, false},
		{"lower ALL fallback", [][2]string{{"all_proxy", "http://" + proxy}, {"ALL_PROXY", "http://127.0.0.1:1"}}, false},
		{"scheme preferred", [][2]string{{"HTTP_PROXY", "http://" + proxy}, {"ALL_PROXY", "http://127.0.0.1:1"}}, false},
		{"lower scheme preferred", [][2]string{{"http_proxy", "http://" + proxy}, {"HTTP_PROXY", "http://127.0.0.1:1"}}, false},
		{"CGI uppercase", [][2]string{{"REQUEST_METHOD", "GET"}, {"HTTP_PROXY", "http://" + proxy}}, true},
		{"CGI ALL", [][2]string{{"REQUEST_METHOD", "GET"}, {"ALL_PROXY", "http://" + proxy}}, true},
		{"CGI ALL cannot override", [][2]string{{"REQUEST_METHOD", "GET"}, {"HTTP_PROXY", "http://" + proxy}, {"ALL_PROXY", "http://" + proxy}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withEnviron(t, tt.env)
			before := requests.Load()
			response, err := (HTTPClient{}).Do(context.Background(), Request{Method: "GET", URL: "http://agent.example:80/status", Timeout: Seconds(3)})
			if tt.reject {
				if err == nil || !strings.Contains(err.Error(), "CGI") || requests.Load() != before {
					t.Fatal("CGI sent a request")
				}
				return
			}
			if err != nil || response.StatusCode != 200 || string(response.Body) != "proxy" || requests.Load() != before+1 {
				t.Fatalf("proxy request: %v", err)
			}
		})
	}
}
