//go:build configflow_proddiff

package main

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/provider"
	"configflow/internal/store"
)

func TestProddiffWaitsForFreshProvider(t *testing.T) {
	t.Cleanup(provider.SetResponseSeconds(0))
	t.Setenv("PRODDIFF_ROUTES", filepath.Join(t.TempDir(), "routes.tsv"))
	if err := writeTestManifest(httpx.New()); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/provider", "synthetic.provider", func(c *httpx.Ctx) (*httpx.Response, error) {
			return provider.Serve(c, "aggregation", "synthetic", "default", &store.Head{StoreID: "manifest"}, func(context.Context) (*httpx.Response, error) {
				close(started)
				<-release
				return httpx.Bytes(200, "text/yaml; charset=utf-8", []byte("proxies: [{name: Fresh}]\n")), nil
			})
		})
	})})
	if _, err := srv.Repo.WriteProfileText(srv.Context(), "default", "providers/synthetic.yaml", []byte("proxies: [{name: Old}]\n")); err != nil {
		t.Fatal(err)
	}
	type result struct{ body, marker string }
	done := make(chan result, 1)
	go func() {
		reply := srv.Get("/provider")
		done <- result{reply.Text(), reply.Header.Get("X-ConfigFlow-Provider")}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("render did not start")
	}
	select {
	case reply := <-done:
		t.Fatalf("comparison served a stale response: %+v", reply)
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	select {
	case reply := <-done:
		if reply.body != "proxies: [{name: Fresh}]\n" || reply.marker != "" {
			t.Fatalf("wrong completed response: %+v", reply)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("render did not finish")
	}
}
