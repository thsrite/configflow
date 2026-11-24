package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// newProductionApp builds the production assembly (server.New with a
// repository, as cmd/configflow does) on a temporary data directory.
func newProductionApp(t *testing.T) *App {
	t.Helper()
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
	return app
}

// nilDependencies walks v (a handler or a Deps struct) and returns the paths
// of nil pointer, interface, func, map and chan fields. It descends into
// structs and into pointers to types of the api packages (sub-handlers), not
// into the injected services themselves.
func nilDependencies(path string, v reflect.Value, out *[]string) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			*out = append(*out, path)
			return
		}
		if pkg := v.Type().Elem().PkgPath(); strings.HasPrefix(pkg, "configflow/internal/api/") || pkg == "configflow/internal/server" {
			nilDependencies(path, v.Elem(), out)
		}
	case reflect.Interface, reflect.Func, reflect.Map, reflect.Chan:
		if v.IsNil() {
			*out = append(*out, path)
		}
	case reflect.Struct:
		if !strings.HasPrefix(v.Type().PkgPath(), "configflow/") {
			return // sync.Mutex and other library values
		}
		for i := 0; i < v.NumField(); i++ {
			nilDependencies(path+"."+v.Type().Field(i).Name, v.Field(i), out)
		}
	}
}

// nilByDesign lists the handler fields that stay nil after server.New:
// test seams whose nil value is the production behaviour, and the D5
// two-phase web listener that cmd/configflow sets after New.
var nilByDesign = map[string]string{
	"api.w10b.rules.service.pinnedCheck":        "test seam; nil = rulesync pinned check",
	"api.w10b.rulelibrary.service.replaceCache": "test seam; nil = rulefetch default replace",
	"api.w10c.settings.wl":                      "D5: SetWebListener after New (cmd/configflow)",
	"api.w10c.logs.LogFilePath":                 "test seam; nil = logs.LogFilePath",
	"api.w10c.logs.OpenFile":                    "test seam; nil = os.Open",
	"api.w10c.stats.Now":                        "test seam; nil = stats.Now clock",
}

// TestProductionHandlersHaveNoNilDependencies checks that every dependency
// field of the mounted handlers is set by server.New, so the transitional
// "nil field falls back to a locator" helpers (kept for NewApp and package
// tests until W11) are never taken in production.
func TestProductionHandlersHaveNoNilDependencies(t *testing.T) {
	app := newProductionApp(t)
	var nils []string
	nilDependencies("api", reflect.ValueOf(app.api), &nils)
	for _, n := range nils {
		if _, ok := nilByDesign[n]; !ok {
			t.Errorf("%s is nil in the production assembly", n)
		}
	}
}

// TestProductionGlobalRoutesDoNotUseRepositoryLocator serves every global GET
// route without path parameters on the production assembly twice: once with
// the process repository set to the App's repository (as cmd/configflow
// does), once with it pointing at another, empty repository and no Agent
// manager singleton. The App's repository holds data the other one lacks, so
// a handler that still resolves its repository through repo.GetRepository
// (or the Agent manager through agents.Get) answers differently. The response
// redaction in httpx still reads the process repository (W11), which is why
// the locator is redirected rather than broken.
func TestProductionGlobalRoutesDoNotUseRepositoryLocator(t *testing.T) {
	app := newProductionApp(t)
	repo.SetRepository(app.Repo)
	t.Cleanup(func() { repo.SetRepository(nil) })
	app.Runtime.RunStartup(context.Background())
	t.Cleanup(func() { app.Runtime.Stop(30 * time.Second) })
	previous := auth.Configure(auth.Config{})
	t.Cleanup(func() { auth.Configure(previous) })
	seedDistinctData(t, app.Repo)

	other, err := repo.OpenDefaultWithOptions(t.TempDir(), RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	if _, err := repo.PrepareConfig(repo.WithRepository(context.Background(), other)); err != nil {
		t.Fatal(err)
	}

	var routes []string
	for _, route := range app.HTTP.Router.Routes() {
		if !slices.Contains(route.Methods, http.MethodGet) || strings.Contains(route.Path, "<") ||
			!repo.IsGlobalRequest(repo.RequestRoute{Endpoint: route.Endpoint, Blueprint: route.Blueprint(), Path: route.Path}) {
			continue
		}
		routes = append(routes, route.Path)
	}
	serve := func() map[string]string {
		out := map[string]string{}
		for _, path := range routes {
			rec := httptest.NewRecorder()
			app.HTTP.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			out[path] = strconv.Itoa(rec.Code) + " " + timestamp.ReplaceAllString(rec.Body.String(), "<time>")
		}
		return out
	}
	want := serve()
	repo.SetRepository(other)
	agents.UseManager(nil)
	got := serve()
	if len(routes) < 20 {
		t.Fatalf("only %d global GET routes served", len(routes))
	}
	for _, path := range routes {
		if got[path] != want[path] {
			t.Errorf("GET %s depends on the process repository:\nwith App repo: %.400s\nwith other:    %.400s", path, want[path], got[path])
		}
	}
	for _, marker := range []string{"w10-profile", "w10-node", "w10-library"} {
		found := false
		for _, body := range want {
			found = found || strings.Contains(body, marker)
		}
		if !found {
			t.Errorf("no global route showed the seeded %s", marker)
		}
	}
}

func seedDistinctData(t *testing.T, r *repo.Repository) {
	t.Helper()
	ctx := context.Background()
	if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "w10-profile", Name: "w10-profile"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateShared(ctx, func(_ context.Context, s *model.Shared) error {
		s.Nodes = append(s.Nodes, model.Node{ID: "w10-node", Name: "w10-node", Type: "ss", Server: "192.0.2.1"})
		s.RuleLibrary = append(s.RuleLibrary, model.RuleLibraryItem{
			ID: "w10-library", Name: "w10-library", SourceType: new("content"), Content: new("DOMAIN,example.com"),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// timestamp matches the response clocks (checked_at, timestamp) that differ
// between the two passes on their own.
var timestamp = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(\+00:00|Z)?`)

// TestProductionAuthGateMatchesGo enables panel login and serves every route
// of the production assembly without credentials: the routes that do not
// answer 401 (public endpoints and paths, and handlers with their own token
// checks) are exactly those of branch go before O17 W10
// (testdata/auth_open_go.txt, dumped from 98a05aebd with an uncommitted
// probe running unauthenticatedSweep).
func TestProductionAuthGateMatchesGo(t *testing.T) {
	app := newProductionApp(t)
	repo.SetRepository(app.Repo)
	t.Cleanup(func() { repo.SetRepository(nil) })
	app.Runtime.RunStartup(context.Background())
	t.Cleanup(func() { app.Runtime.Stop(30 * time.Second) })
	want, err := os.ReadFile("testdata/auth_open_go.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(unauthenticatedSweep(t, app), "\n") + "\n"
	if got != string(want) {
		t.Errorf("routes answering without credentials changed:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// unauthenticatedSweep serves every route of the production assembly once
// without credentials while panel login is enabled and returns, in route
// order, "METHOD path endpoint" for the routes that did not answer 401.
func unauthenticatedSweep(t *testing.T, app *App) []string {
	t.Helper()
	previous := auth.Configure(auth.Config{AdminUsername: "admin", AdminPassword: "secret-for-tests", SecretKey: "k"})
	t.Cleanup(func() { auth.Configure(previous) })
	param := regexp.MustCompile(`<(?:[a-z]+:)?([A-Za-z_]+)>`)
	var open []string
	for _, route := range app.HTTP.Router.Routes() {
		method := route.Methods[0]
		path := param.ReplaceAllString(route.Path, "x")
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		app.HTTP.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			open = append(open, method+" "+route.Path+" "+route.Endpoint)
		}
	}
	return open
}
