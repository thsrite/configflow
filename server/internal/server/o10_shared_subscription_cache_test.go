package server

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"configflow/internal/model"
	"configflow/internal/subs"
	"configflow/internal/substore"
)

// o10AirportYAML is a synthetic airport document whose nodes use a relay
// group through dialer-proxy and whose hosts block maps node servers, so a
// generated mihomo configuration reads both the hosts and the dialers
// metadata of the stored cache.
func o10AirportYAML(id, hostTarget string) string {
	var b strings.Builder
	b.WriteString("proxies:\n")
	for i := range 6 {
		name := fmt.Sprintf("Relay %02d | %s", i, id)
		server := fmt.Sprintf("relay%02d.%s.example.com", i, id)
		dialer := ""
		if i >= 2 {
			dialer = ", dialer-proxy: Transit"
		}
		fmt.Fprintf(&b, "  - {name: %q, type: ss, server: %s, port: %d, cipher: aes-128-gcm, password: synthetic-%d%s}\n", name, server, 20000+i, i, dialer)
	}
	fmt.Fprintf(&b, "proxy-groups:\n  - {name: Transit, type: select, proxies: [%q, %q]}\n",
		"Relay 00 | "+id, "Relay 01 | "+id)
	b.WriteString("hosts:\n")
	for i := range 6 {
		fmt.Fprintf(&b, "  relay%02d.%s.example.com: %s\n", i, id, hostTarget)
	}
	return b.String()
}

func o10SaveAirportCache(t *testing.T, f *o10Fixture, id, hostTarget string) {
	t.Helper()
	ctx := f.srv.Context()
	text := o10AirportYAML(id, hostTarget)
	proxies, err := substore.ParseProxyNodes(text)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := substore.ProxyNodes(proxies)
	if err != nil {
		t.Fatal(err)
	}
	dialers, err := subs.ExtractDialers(text)
	if err != nil || dialers == nil {
		t.Fatalf("dialers: %v %v", dialers, err)
	}
	hosts := subs.ExtractHosts(text)
	if hosts == nil {
		t.Fatal("hosts not extracted")
	}
	if _, err := substore.SaveCache(ctx, f.srv.Repo, id, nodes, &substore.CacheMetadata{URL: new(o10FeedURL(id)), SubscriptionName: id, Hosts: hosts, Dialers: dialers}); err != nil {
		t.Fatal(err)
	}
}

// TestO10SharedSubscriptionCacheStaysReadOnly checks the batch-47 zero-copy
// path (LoadSubscriptionCacheShared): generating mihomo configurations, one
// after another and concurrently, never modifies the shared decoded cache
// record (its model encoding is unchanged and the same pointer is still
// served), every generation gives the same document, and a rewritten cache
// is visible on the next generation.
func TestO10SharedSubscriptionCacheStaysReadOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the full fixture")
	}
	f := newO10Fixture(t)
	ctx := f.srv.Context()
	r := f.srv.Repo
	o10SaveAirportCache(t, f, o10SubA, "198.51.100.7")

	const route = "/api/config/default/mihomo"
	get := func() string {
		res := f.do("GET", route, "", nil)
		if res.Code != 200 {
			t.Errorf("GET %s: %d %.300s", route, res.Code, res.Body.String())
			return ""
		}
		return res.Body.String()
	}
	shared, err := substore.LoadCacheShared(ctx, r, o10SubA)
	if err != nil || shared == nil {
		t.Fatalf("shared cache: %v", err)
	}
	before, err := model.Marshal(shared)
	if err != nil {
		t.Fatal(err)
	}
	first := get()
	for _, want := range []string{"Feed A/Transit", "198.51.100.7"} {
		if !strings.Contains(first, want) {
			t.Fatalf("generated config does not use the cache's %q:\n%.2000s", want, first)
		}
	}
	if second := get(); second != first {
		t.Fatal("second generation differs from the first")
	}
	var wg sync.WaitGroup
	bodies := make([]string, 8)
	for i := range bodies {
		wg.Go(func() { bodies[i] = get() })
	}
	wg.Wait()
	for i, body := range bodies {
		if body != first {
			t.Fatalf("concurrent generation %d differs", i)
		}
	}
	again, err := substore.LoadCacheShared(ctx, r, o10SubA)
	if err != nil {
		t.Fatal(err)
	}
	if again != shared {
		t.Fatal("the shared record was replaced although the file did not change")
	}
	after, err := model.Marshal(shared)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("generation modified the shared cache record:\nbefore %.600s\nafter  %.600s", before, after)
	}

	// A rewritten cache is used by the next generation.
	o10SaveAirportCache(t, f, o10SubA, "198.51.100.8")
	changed := get()
	if !strings.Contains(changed, "198.51.100.8") || strings.Contains(changed, "198.51.100.7") {
		t.Fatalf("generation still shows the previous hosts:\n%.2000s", changed)
	}
}
