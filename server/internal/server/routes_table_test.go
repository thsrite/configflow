package server

import (
	"context"
	"os"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

// TestProductionRouteTableMatchesGo checks that the production assembly
// (server.New with a repository) registers exactly the routes it registered
// on branch go before O17 W10: same methods, path patterns and endpoint
// names, in the same order, each api package once. testdata/routes_go.tsv was
// dumped from branch go (98a05aebd) with a temporary probe that was not
// committed.
func TestProductionRouteTableMatchesGo(t *testing.T) {
	want, err := os.ReadFile("testdata/routes_go.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rt := lifecycle.NewProcessRuntime(lifecycle.Options{})
	t.Cleanup(func() {
		if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
			t.Error(err)
		}
	})
	r, err := repo.OpenDefaultWithOptions(t.TempDir(), RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	app, err := New(context.Background(), Options{Repo: r, Runtime: rt})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(app.Agents.Close)

	var got []string
	for _, route := range app.HTTP.Router.Routes() {
		got = append(got, strings.Join(route.Methods, ",")+"\t"+route.Path+"\t"+route.Endpoint)
	}
	wantLines := strings.Split(strings.TrimRight(string(want), "\n"), "\n")
	if len(got) != len(wantLines) {
		t.Errorf("route count = %d, want %d", len(got), len(wantLines))
	}
	for i := 0; i < len(got) || i < len(wantLines); i++ {
		var g, w string
		if i < len(got) {
			g = got[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Fatalf("route %d = %q, want %q", i, g, w)
		}
	}
}
