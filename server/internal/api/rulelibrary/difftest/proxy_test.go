package difftest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

func TestManualCacheUsesGlobalForwardProxy(t *testing.T) {
	var direct atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { direct.Add(1); w.WriteHeader(502) }))
	defer origin.Close()
	var lock sync.Mutex
	requests := []string{}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil || len(raw) != 0 {
			t.Error("GET sent a request body", err, string(raw))
		}
		if r.Method != "GET" || r.Header.Get("Content-Type") != "" || r.Header.Get("Authorization") != "" || r.RequestURI != origin.URL+"/source" {
			t.Errorf("unexpected raw source request: %s %s headers=%v", r.Method, r.RequestURI, r.Header)
		}
		lock.Lock()
		requests = append(requests, r.RequestURI)
		lock.Unlock()
		w.Write([]byte("+.proxy-source.example\n"))
	}))
	defer proxy.Close()
	setup := func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		_, err := r.UpdateSystemTransaction(ctx, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.GetObject("system_config").Set("rule_fetch_proxy", proxy.URL)
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	w := newWorld(t, setup)
	w.call("POST", "/api/rule-library", body(map[string]string{"id": "proxied", "name": "Proxied", "source_type": "url", "behavior": "domain", "format": "text", "url": origin.URL + "/source"}), true)
	w.call("POST", "/api/rule-library/cache", `{"rule_ids":["proxied"]}`, true)
	lock.Lock()
	count := len(requests)
	lock.Unlock()
	if direct.Load() != 0 || count != 4 {
		t.Fatalf("proxy path not exercised: direct=%d, proxy=%d", direct.Load(), count)
	}
}
func TestUnavailableRefreshKeepsRawCacheAndPreferences(t *testing.T) {
	var broken atomic.Bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if broken.Load() || r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte("+.retained.example\n"))
	}))
	defer origin.Close()
	w := newWorld(t, seed(t))
	created := w.call("POST", "/api/rule-library", body(map[string]string{"id": "retained", "name": "Retained", "source_type": "url", "behavior": "domain", "format": "text", "url": origin.URL + "/source"}), true)
	// Creation runs sequentially on the two real servers. Set the fixture's
	// physical cache timestamp equally before testing its retained-cache notice.
	// The notice, raw file bytes and every other stored field still compare exactly.
	stamp := fixedClock{}.Now()
	for _, root := range []string{w.srv.DataDir, w.pyDir} {
		name := rules.RuleCacheFilename(jsonx.Obj("id", "retained", "source_type", "url", "url", origin.URL+"/source", "behavior", "domain", "format", "text"))
		if root == w.pyDir {
			matched := false
			for python, native := range created.CachePaths {
				if native == name {
					name = python
					matched = true
					break
				}
			}
			if !matched {
				t.Fatal("independent Python filename missing")
			}
		}
		cache := filepath.Join(root, "shared", "rules", name)
		if err := os.Chtimes(cache, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	broken.Store(true)
	reply := w.call("POST", "/api/rule-library/cache", `{"rule_ids":["retained"]}`, true)
	result := reply.Body.(map[string]any)["results"].([]any)[0].(map[string]any)
	if result["success"] != false || result["cache_retained"] != true || !strings.Contains(result["error"].(string), "404") {
		t.Fatal("failed refresh did not retain validated cache", result)
	}
	w.call("POST", "/api/rule-library/test", `{}`, true)
}
