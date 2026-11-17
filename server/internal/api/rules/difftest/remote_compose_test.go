package difftest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComposeRemoteLibraryAtomicValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.ContentLength > 0 {
			t.Error("unexpected download request")
		}
		switch r.URL.Path {
		case "/valid":
			fmt.Fprint(w, "+.remote.example\n")
		case "/invalid":
			fmt.Fprint(w, "IP-CIDR,192.0.2.0/24\n")
		default:
			http.Error(w, "synthetic unavailable", 503)
		}
	}))
	defer server.Close()
	for _, path := range []string{"valid", "invalid", "unavailable"} {
		t.Run(path, func(t *testing.T) {
			w := newWorld(t, seed(t))
			request := map[string]any{"source": map[string]any{"kind": "new_library", "library": map[string]any{"name": "Remote source", "source_type": "url", "url": server.URL + "/" + path, "behavior": "domain", "format": "text"}}, "rule": map[string]string{"policy": "DIRECT"}, "dns": "direct"}
			reply := w.call("POST", "/api/rules/compose?profile=other", body(request), true)
			expected := 200
			if path == "invalid" {
				expected = 400
			} else if path == "unavailable" {
				expected = 502
			}
			if reply.Status != expected {
				t.Fatalf("remote %s status%d", path, reply.Status)
			}
			w.call("GET", "/api/profiles/other/rules", "", true)
			if path == "valid" {
				w.call("GET", "/api/profiles/other/rules/local/Remote%20source?target=mihomo", "", false)
			}
		})
	}
}
