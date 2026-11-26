package rulelibrary

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/api/proxygroups"
	apirules "configflow/internal/api/rules"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

func runtimeRequest(t *testing.T, base context.Context) (*lifecycle.Runtime, context.Context, context.CancelFunc) {
	t.Helper()
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(t.Context())
	request, cancel := context.WithCancel(base)
	bound := runtime.BindRequest(request)
	t.Cleanup(func() {
		cancel()
		ctx, stop := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer stop()
		runtime.Shutdown(ctx)
	})
	return runtime, bound, cancel
}
func decodeTestJSON[T any](t *testing.T, raw string) *T {
	t.Helper()
	out := new(T)
	if err := jsondecode.Unmarshal([]byte(raw), out); err != nil {
		t.Fatal(err)
	}
	return out
}

func jsonQuote(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func draftBody(t *testing.T, token, content string) *draftRequest {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"revision": token, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	return decodeDraftRequest(raw)
}

type durableState struct {
	Document string
	Files    map[string]string
}

func persisted(t *testing.T, s *httpxtest.Server) durableState {
	t.Helper()
	out := durableState{Document: doc(t, s), Files: map[string]string{}}
	err := filepath.WalkDir(s.DataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".json", ".list", ".yaml", ".invalid", ".tmp":
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(s.DataDir, path)
		if err != nil {
			return err
		}
		out.Files[relative] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEveryWriteOwnerHonoursRuntimeShutdownAndRequestDisconnect(t *testing.T) {
	tests := []struct {
		name               string
		write              func(*testing.T, *Service, *proxygroups.Service, context.Context, string, string) error
		networkOnly, cache bool
	}{
		{name: "library-create", write: func(t *testing.T, s *Service, _ *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.Create(ctx, decodeTestJSON[libraryRequest](t, `{"id":"new","name":"New","source_type":"content","behavior":"domain","format":"text","content":"+.new.example\n"}`))
			return err
		}},
		{name: "library-put", write: func(t *testing.T, s *Service, _ *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.Mutate(ctx, "manual", decodeTestJSON[libraryRequest](t, `{"name":"Edited","source_type":"content","behavior":"domain","format":"text","content":"+.new.example\n"}`), false, "http://localhost")
			return err
		}},
		{name: "library-delete", write: func(t *testing.T, s *Service, _ *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.Mutate(ctx, "manual", nil, true, "http://localhost")
			return err
		}},
		{name: "draft", write: func(t *testing.T, s *Service, _ *proxygroups.Service, ctx context.Context, token, _ string) error {
			_, err := s.SaveDraft(ctx, "manual", draftBody(t, token, "+.new.example\n"))
			return err
		}},
		{name: "library-reorder", write: func(t *testing.T, s *Service, _ *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.Reorder(ctx, json.RawMessage(`{"ids":["remote"]}`))
			return err
		}},
		{name: "proxy-domain", write: func(t *testing.T, s *Service, _ *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.SetProxyDomains(ctx, decodeTestJSON[proxyDomainsRequest](t, `{"proxy_domains":"https://mirror.example/"}`))
			return err
		}},
		{name: "cache", cache: true, write: func(t *testing.T, s *Service, _ *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.Cache(ctx, decodeTestJSON[cacheRequest](t, `{"rule_ids":["manual"]}`))
			return err
		}},
		{name: "connectivity", write: func(_ *testing.T, s *Service, _ *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.Test(ctx, "http://localhost")
			return err
		}},
		{name: "probe", networkOnly: true, write: func(t *testing.T, s *Service, _ *proxygroups.Service, ctx context.Context, _, origin string) error {
			_, err := s.Single(ctx, decodeTestJSON[singleRequest](t, `{"url":`+jsonQuote(t, origin+"/source")+`}`))
			return err
		}},
		{name: "group-create", write: func(t *testing.T, _ *Service, s *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.Create(ctx, "default", json.RawMessage(`{"id":"new","name":"New","type":"select","manual_nodes":["DIRECT"]}`), "http://localhost")
			return err
		}},
		{name: "group-profile-create", write: func(t *testing.T, _ *Service, s *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.Create(ctx, "other", json.RawMessage(`{"id":"new","name":"New","type":"select","manual_nodes":["DIRECT"]}`), "http://localhost")
			return err
		}},
		{name: "group-put", write: func(t *testing.T, _ *Service, s *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.Mutate(ctx, "default", "group", json.RawMessage(`{"name":"Renamed"}`), false, "http://localhost")
			return err
		}},
		{name: "group-delete", write: func(_ *testing.T, _ *Service, s *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.Mutate(ctx, "default", "group", nil, true, "http://localhost")
			return err
		}},
		{name: "group-health", write: func(t *testing.T, _ *Service, s *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.BatchHealthURL(ctx, "default", healthURL(t, `{"ids":["group"],"url":"https://probe.example/new"}`))
			return err
		}},
		{name: "group-reorder", write: func(t *testing.T, _ *Service, s *proxygroups.Service, ctx context.Context, _, _ string) error {
			_, err := s.Reorder(ctx, "default", json.RawMessage(`{"ids":["group"]}`))
			return err
		}},
	}
	for _, test := range tests {
		for _, shutdown := range []bool{false, true} {
			mode := "request-disconnect"
			if shutdown {
				mode = "runtime-shutdown"
			}
			t.Run(test.name+"/"+mode, func(t *testing.T) {
				app := fixture(t)
				createManual(t, app)
				groups := &proxygroups.Service{Repo: app.Repo}
				if _, err := app.Repo.CreateProfileRecord(app.Context(), repo.ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
					t.Fatal(err)
				}

				if _, err := groups.Create(app.Context(), "default", json.RawMessage(`{"id":"group","name":"Group","type":"url-test","manual_nodes":["DIRECT"],"url":"http://probe.example/old"}`), "http://localhost"); err != nil {
					t.Fatal(err)
				}
				var calls atomic.Int32
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.Method == "HEAD" {
						w.WriteHeader(404)
						return
					}
					w.Write([]byte("+.probe.example\n"))
				}))
				defer origin.Close()
				service := &Service{repo: app.Repo, client: rulefetch.DefaultClient}
				remote := decodeTestJSON[libraryRequest](t, `{"id":"remote","name":"Remote","source_type":"url","behavior":"domain","format":"text","url":`+jsonQuote(t, origin.URL+"/source")+`}`)
				if _, err := service.Create(app.Context(), remote); err != nil {
					t.Fatal(err)
				}
				calls.Store(0)
				draft, err := service.Draft(app.Context(), "manual")
				if err != nil {
					t.Fatal(err)
				}
				if test.cache {
					item, err := service.source(app.Context(), "manual")
					if err != nil {
						t.Fatal(err)
					}
					path, err := service.cachePath(app.Context(), item)
					if err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(path, []byte("+.stale.example\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				runtime, ctx, cancel := runtimeRequest(t, app.Context())
				if shutdown {
					runtime.Shutdown(t.Context())
				} else {
					cancel()
				}
				before := persisted(t, app)
				repeats := 1
				if shutdown {
					repeats = 8
				}
				for range repeats {
					err = test.write(t, service, groups, ctx, draft.Revision, origin.URL)
					if shutdown {
						if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(before, persisted(t, app)) || calls.Load() != 0 {
							t.Fatalf("stopped owner changed durable state or sent I/O: err=%v calls=%d", err, calls.Load())
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						after := persisted(t, app)
						if test.networkOnly {
							if calls.Load() != 1 {
								t.Fatal("disconnected probe did not finish", calls.Load())
							}
						} else if reflect.DeepEqual(before, after) {
							t.Fatal("disconnected owner did not commit its effect")
						}
					}
				}
			})
		}
	}
}

func TestStoppedRuntimeCannotStageOrReplaceFiles(t *testing.T) {
	app := fixture(t)
	svc := &Service{repo: app.Repo, client: rulefetch.DefaultClient}
	createManual(t, app)
	item, err := svc.source(app.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	runtime, ctx, _ := runtimeRequest(t, app.Context())
	// Production leaf calls receive the owner's detached generation context,
	// rather than the raw request view whose Err only tracks disconnection.
	ctx = lifecycle.Detach(ctx)
	stage, err := svc.stageCache(ctx, item, []byte("+.new.example\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Shutdown(t.Context())
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("leaf fixture did not retain runtime cancellation")
	}
	before := persisted(t, app)
	if err = stage.commit(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("stopped stage committed", err)
	}
	if _, err = svc.stageCache(ctx, item, []byte("+.later.example\n"), nil); !errors.Is(err, context.Canceled) {
		t.Fatal("stopped owner staged bytes", err)
	}
	if !reflect.DeepEqual(before, persisted(t, app)) {
		t.Fatal("stopped leaf changed files")
	}
	// Cleanup happens only after a new generation admits another cache owner.
	runtime.Start(t.Context())
	fresh := runtime.BindRequest(app.Context())
	if _, err = svc.Cache(fresh, decodeTestJSON[cacheRequest](t, `{"rule_ids":["manual"]}`)); err != nil {
		t.Fatal(err)
	}
	for path := range persisted(t, app).Files {
		if filepath.Ext(path) == ".tmp" || filepath.Ext(path) == ".invalid" {
			t.Fatal("next admitted owner did not repair stopped stage", path)
		}
	}
}
func TestShutdownBetweenDBCommitAndCacheRenameIsFailClosed(t *testing.T) {
	app := fixture(t)
	createManual(t, app)
	runtime, ctx, _ := runtimeRequest(t, app.Context())
	var atShutdown durableState
	service := &Service{repo: app.Repo, client: rulefetch.DefaultClient, replaceCache: func(leaf context.Context, from, to string) error {
		runtime.Shutdown(t.Context())
		atShutdown = persisted(t, app)
		return replaceFile(leaf, from, to)
	}}
	draft, err := service.Draft(app.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveDraft(ctx, "manual", draftBody(t, draft.Revision, "+.committed.example\n")); !errors.Is(err, context.Canceled) {
		t.Fatal("rename ignored shutdown", err)
	}
	if !reflect.DeepEqual(atShutdown, persisted(t, app)) {
		t.Fatal("cache publication/cleanup wrote after shutdown")
	}
	item, err := service.source(app.Context(), "manual")
	if err != nil || model.TextValue(item.Content) != "+.committed.example\n" {
		t.Fatal("pre-shutdown committed DB was rolled back", err)
	}
	path, err := service.cachePath(app.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "+.old.example\n" {
		t.Fatalf("rename landed after shutdown: %q %v", raw, err)
	}
	if _, err = os.Stat(path + ".invalid"); err != nil {
		t.Fatal("shutdown cleared durable invalidation", err)
	}
	// Reopen the repository and start a fresh Runtime: the durable marker must
	// keep every reader on the committed inline declaration, never the old file.
	reopened := httpxtest.New(t, httpxtest.Options{DataDir: app.DataDir, Auth: true})
	apirules.New(apirules.Deps{Repo: reopened.Repo}).Register(reopened.App.Router)
	New(Deps{Repo: reopened.Repo}).Register(reopened.App.Router)
	_, fresh, _ := runtimeRequest(t, reopened.Context())
	for _, route := range []string{"/api/rule-library/content/manual", "/api/profiles/default/rule-library/content/manual", "/api/rules/local/Manual"} {
		if got := reopened.Get(route, reopened.Login()).WantStatus(200).Text(); got != "+.committed.example\n" {
			t.Fatalf("restart did not serve committed inline content at %s: %q", route, got)
		}
	}
	if raw, err = os.ReadFile(path); err != nil || string(raw) != "+.old.example\n" {
		t.Fatalf("restart reader published outside the edit protocol: %q %v", raw, err)
	}
	if _, err = os.Stat(path + ".invalid"); err != nil {
		t.Fatal("restart reader cleared invalidation", err)
	}
	freshService := &Service{repo: reopened.Repo, client: rulefetch.DefaultClient}
	draft, err = freshService.Draft(reopened.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = freshService.SaveDraft(fresh, "manual", draftBody(t, draft.Revision, "+.repaired.example\n")); err != nil {
		t.Fatal("next edit did not repair interrupted publication", err)
	}
	if raw, err = os.ReadFile(path); err != nil || string(raw) != "+.repaired.example\n" {
		t.Fatalf("repaired cache: %q %v", raw, err)
	}
	if _, err = os.Stat(path + ".invalid"); !os.IsNotExist(err) {
		t.Fatal("next edit retained invalidation", err)
	}
	temps, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".rule-edit-*.tmp"))
	if err != nil || len(temps) != 0 {
		t.Fatal("next edit retained stopped stages", temps, err)
	}
}

func healthURL(t *testing.T, raw string) *proxygroups.HealthURLRequest {
	t.Helper()
	var out proxygroups.HealthURLRequest
	if err := jsondecode.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return &out
}
