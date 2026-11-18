package routers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apiconfig "configflow/internal/api/config"
	apiprofiles "configflow/internal/api/profiles"
	api "configflow/internal/api/routers"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	core "configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

func saveFixtureConfig(t *testing.T, s *httpxtest.Server, cfg any) {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var fields jsonx.Object
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repo.UpdateProfileTransaction(s.Context(), "default", func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		for key, value := range fields.All() {
			current.Set(key, value)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func addGateway(t *testing.T, s *httpxtest.Server) {
	t.Helper()
	_, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		current.Set("agents", []any{jsonx.Obj("id", "gateway", "name", "Synthetic gateway", "host", "192.0.2.4", "port", 9100, "service_type", "mihomo", "profile_id", "default", "enabled", true, "status", "offline", "token", "synthetic-agent-token")})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestR15PublishHTTPDisconnectAndShutdownAfterWrite(t *testing.T) {
	for _, mode := range []string{"disconnect", "shutdown", "shutdown_before"} {
		t.Run(mode, func(t *testing.T) {
			seed := deviceState()
			seed.Tables["/ip/route"] = seed.Tables["/ip/route"][:1]
			fake := routerfake.New(nil)
			fake.Reset(seed)
			entered, release, stopped, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			var arm atomic.Bool
			device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if arm.Load() && r.URL.Path == "/rest/ip/route" && (mode == "shutdown_before" && r.Method == "GET" || mode != "shutdown_before" && r.Method == "PUT") {
					if mode != "shutdown_before" {
						fake.ServeHTTP(w, r)
					}
					close(entered)
					select {
					case <-r.Context().Done():
						close(stopped)
					case <-release:
					}
					return
				}
				fake.ServeHTTP(w, r)
			}))
			defer device.Close()
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
			runtime.Start(t.Context())
			defer runtime.Shutdown(t.Context())
			s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(api.Register, apiconfig.Register, apiprofiles.Register), Setup: func(app *httpx.App) {
				app.AfterRequest(func(c *httpx.Ctx, r *httpx.Response) {
					if c.Path() == "/api/routers/router-one/apply" {
						close(finished)
					}
				})
			}})
			core.Wire()
			createSpare(t, s)
			saveFixtureConfig(t, s, inputConfig(device.URL))
			addGateway(t, s)
			plan := s.Post("/api/routers/router-one/plan")
			plan.WantStatus(200)
			if plan.Object().GetObject("plan").GetBool("empty") || len(plan.Object().GetObject("plan").GetList("blockers")) != 0 {
				t.Fatalf("not a writable plan: %s", plan.Body)
			}
			raw, _ := json.Marshal(map[string]string{"fingerprint": plan.Object().GetObject("plan").GetString("fingerprint")})
			bound := runtimeHTTP(t, s, runtime)
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			req, err := http.NewRequestWithContext(request, "POST", bound.URL+"/api/routers/router-one/apply", bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			before, _ := s.Repo.ConfigRevision(s.Context(), "default")
			arm.Store(true)
			result := make(chan int, 1)
			go func() {
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					result <- 0
					return
				}
				defer resp.Body.Close()
				_, _ = io.Copy(io.Discard, resp.Body)
				result <- resp.StatusCode
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("write not reached")
			}
			if mode == "disconnect" {
				assertBatchProtected(t, s)
				cancel()
				close(release)
			} else {
				runtime.Shutdown(t.Context())
				select {
				case <-stopped:
				case <-time.After(2 * time.Second):
					t.Fatal("write HTTP ignored shutdown")
				}
			}
			calls := len(fake.Calls())
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("publish owner did not finish")
			}
			status := <-result
			after, _ := s.Repo.ConfigRevision(s.Context(), "default")
			expectedRevision := before + 1
			if mode == "shutdown_before" {
				expectedRevision = before
			}
			if after != expectedRevision {
				t.Fatalf("publication missing: %d/%d", before, after)
			}
			profile, _ := s.Repo.GetProfile(s.Context(), "default")
			record := profile.GetObject("routers").GetList("items")[0].(*jsonx.Object)
			last := record.GetObject("last_release")
			if mode == "shutdown_before" {
				if last != nil || status == 200 || len(fake.Snapshot().Tables["/ip/route"]) != 1 || len(fake.Calls()) != calls {
					t.Fatal("pre-write shutdown was reported/committed as a successful release")
				}
				return
			}
			if last == nil || last.GetBool("ok") != (mode == "disconnect") {
				t.Fatalf("incorrect release receipt: %v", last)
			}
			if mode == "shutdown" {
				if status != 502 {
					t.Fatalf("shutdown HTTP status=%d", status)
				}
				if len(fake.Calls()) != calls {
					t.Fatal("post-shutdown device I/O")
				}
			}
			if len(fake.Snapshot().Tables["/ip/route"]) != 2 {
				t.Fatal("device write vanished")
			}
		})
	}
}
func TestR15FastCommitStopsAtRuntimeShutdown(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
	runtime.Start(t.Context())
	defer runtime.Shutdown(t.Context())
	s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(api.Register)})
	before, _ := s.Repo.ConfigRevision(s.Context(), "default")
	s.Repo.Store().BeforeCommit = func() error { runtime.Shutdown(t.Context()); return nil }
	defer func() { s.Repo.Store().BeforeCommit = nil }()
	bound := runtimeHTTP(t, s, runtime)
	raw, _ := json.Marshal(routerBody("http://192.0.2.1"))
	response, err := http.Post(bound.URL+"/api/routers", "application/json", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	after, _ := s.Repo.ConfigRevision(s.Context(), "default")
	if before != after {
		t.Fatal("shutdown committed a new router")
	}
}
