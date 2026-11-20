package rulefetch

// Deferred cases of backend/test_local_http.py that go through rule_fetch:
// same-origin API URLs are dispatched in process (localhttp), redirects keep
// the API boundary, external or non-API URLs use the network transport.
// test_real_rule_url_check_works_with_one_http_worker is about Waitress's
// worker pool and has no Go counterpart (the server is not pool-bound).

import (
	"context"
	"net/http"
	"testing"
	"time"

	"configflow/internal/localhttp"
)

func TestLocalDispatchAndRedirectBoundary(t *testing.T) {
	localhttp.Register(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a":
			http.Redirect(w, r, "/api/b", http.StatusFound)
		case "/api/out":
			http.Redirect(w, r, "http://other.test/api/rules", http.StatusFound)
		default:
			w.Write([]byte("local " + r.URL.Path))
		}
	}), localTestSpawner{})
	t.Cleanup(func() { localhttp.Register(nil, nil) })
	fake := &fakeTransport{reply: func(int, TransportRequest) (*Response, error) { return reply(200, "network"), nil }}
	client := &Client{Transport: fake, Local: localhttp.Do, Sleep: func(time.Duration) {}}
	config := sysConfig("server_domain", "http://config.test", "rule_fetch_proxy", "http://proxy.test:1")
	get := func(url string, r DownloadRequest) (string, error) {
		if r.Config == nil {
			r.Config = config
		}
		response, err := client.Download(context.Background(), url, r)
		if err != nil {
			return "", err
		}
		content, err := response.Content()
		return string(content), err
	}
	if body, err := get("http://config.test/api/rules", DownloadRequest{}); err != nil || body != "local /api/rules" || len(fake.calls) != 0 {
		t.Fatal(body, err)
	}
	if body, err := get("http://config.test/api/a", DownloadRequest{}); err != nil || body != "local /api/b" || len(fake.calls) != 0 {
		t.Fatal(body, err)
	}
	if _, err := get("http://config.test/api/out", DownloadRequest{}); err == nil || err.Error() != "Internal API redirect leaves its origin" || len(fake.calls) != 0 {
		t.Fatal(err)
	}
	for _, url := range []string{"http://other.test/api/rules", "http://config.test/elsewhere"} {
		fake.calls = nil
		if body, err := get(url, DownloadRequest{}); err != nil || body != "network" || len(fake.calls) != 1 || fake.calls[0].req.Proxy != "http://proxy.test:1" {
			t.Fatal(url, body, err)
		}
	}
	// The current request's host URL counts as this server too.
	fake.calls = nil
	if body, err := get("http://h.test/api/x", DownloadRequest{Config: sysConfig(), HostURL: "http://h.test/"}); err != nil || body != "local /api/x" || len(fake.calls) != 0 {
		t.Fatal(body, err)
	}
}

type localTestSpawner struct{}

func (localTestSpawner) Spawn(ctx context.Context, _ string, fn func(context.Context)) error {
	go fn(context.WithoutCancel(ctx))
	return nil
}
