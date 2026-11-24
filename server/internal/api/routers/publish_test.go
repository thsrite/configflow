package routers_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/agents"
	apiconfig "configflow/internal/api/config"
	apiprofiles "configflow/internal/api/profiles"
	api "configflow/internal/api/routers"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
	core "configflow/internal/routers"
	"configflow/internal/routers/routerfake"
	routerrulesync "configflow/internal/routers/rulesync"
)

func saveFixtureConfig(t *testing.T, s *httpxtest.Server, cfg any) {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	stored := map[string]json.RawMessage{}
	_, err = s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, current *model.Profile) error {
		for key, value := range fields {
			if err := current.SetJSON(key, value); err != nil {
				// Malformed stored records are input no writer accepts
				// (O18 T1c); the fixture writes them to the store as a
				// Python-era database would hold them.
				var typed *model.MemberTypeError
				if !errors.As(err, &typed) {
					return err
				}
				stored[key] = value
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range stored {
		storeProfileRow(t, s, key, value)
	}
}

// storeProfileRow writes one row of the default profile as given.
func storeProfileRow(t *testing.T, s *httpxtest.Server, key string, value json.RawMessage) {
	t.Helper()
	db, err := sql.Open("sqlite", s.Repo.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := db.Exec("UPDATE fields SET value=?, revision=revision+1 WHERE scope='profile' AND owner='default' AND field=?", string(value), key)
	if err != nil {
		t.Fatal(err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		if _, err := db.Exec("INSERT INTO fields(scope, owner, field, position, value, revision) VALUES('profile', 'default', ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM fields WHERE scope='profile' AND owner='default'), ?, 1)", key, string(value)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("UPDATE meta SET value=CAST(value AS INTEGER)+1 WHERE key='generation'"); err != nil {
		t.Fatal(err)
	}
}
func addGateway(t *testing.T, s *httpxtest.Server) {
	t.Helper()
	err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, current *model.System) error {
		return current.SetJSON("agents", json.RawMessage(`[{"id":"gateway","name":"Synthetic gateway","host":"192.0.2.4","port":9100,"service_type":"mihomo","profile_id":"default","enabled":true,"status":"offline","token":"synthetic-agent-token"}]`))
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
			s := httpxtest.New(t, httpxtest.Options{Setup: func(app *httpx.App) {
				app.AfterRequest(func(c *httpx.Ctx, r *httpx.Response) {
					if c.Path() == "/api/routers/router-one/apply" {
						close(finished)
					}
				})
			}})
			apiconfig.New(apiconfig.Deps{Repo: s.Repo}).Register(s.App.Router)
			apiprofiles.New(apiprofiles.Deps{Repo: s.Repo}).Register(s.App.Router)
			mgr, err := agents.New(s.Context(), s.Repo, agents.Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(mgr.Close)
			nt := notifications.New(notifications.Options{Repo: s.Repo, Summarize: core.NotificationSummary})
			rtr := core.New(s.Repo, core.Options{Notifications: nt})
			rr := routerrulesync.New(s.Repo, routerrulesync.Options{Notifications: nt})
			api.New(api.Deps{Repo: s.Repo, Agents: mgr, Routers: rtr, RuleSync: rr, Notifications: nt}).Register(s.App.Router)
			wireRouterTestHooks(t)
			createSpare(t, s)
			saveFixtureConfig(t, s, inputConfig(device.URL))
			addGateway(t, s)
			plan := s.Post("/api/routers/router-one/plan")
			plan.WantStatus(200)
			planObj := decodeJSON(t, plan)["plan"].(map[string]any)
			blockers, _ := planObj["blockers"].([]any)
			if planObj["empty"] == true || len(blockers) != 0 {
				t.Fatalf("not a writable plan: %s", plan.Body)
			}
			raw, _ := json.Marshal(map[string]string{"fingerprint": planObj["fingerprint"].(string)})
			bound := runtimeHTTP(t, s, runtime)
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			req, err := http.NewRequestWithContext(request, "POST", bound.URL+"/api/routers/router-one/apply", bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			before, _ := s.Repo.ConfigRevision(s.Context(), "default")
			headBefore, _ := s.Repo.Head(s.Context())
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
				// Lab L5-D3: the process exits when Shutdown returns, so the
				// aborted record must already be committed by then; only a
				// tracked publish owner is awaited after the cancellation.
				if mode == "shutdown" {
					profile, _ := s.Repo.Profile(s.Context(), "default")
					last := profile.Routers.Items[0].LastRelease
					if last == nil || last.OK || !strings.Contains(last.Message, "（已完成 ") {
						t.Fatalf("aborted release not recorded before shutdown returned: %+v", last)
					}
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
			headAfter, _ := s.Repo.Head(s.Context())
			if after != before {
				t.Fatalf("last_release-only publication advanced config revision: %d/%d", before, after)
			}
			expectedState := headBefore.State + 1
			if mode == "shutdown_before" {
				expectedState = headBefore.State
			}
			if headAfter.State != expectedState {
				t.Fatalf("publication state generation: %d -> %d (want %d)", headBefore.State, headAfter.State, expectedState)
			}
			profile, _ := s.Repo.Profile(s.Context(), "default")
			record := profile.Routers.Items[0]
			last := record.LastRelease
			if mode == "shutdown_before" {
				if last != nil || status == 200 || len(fake.Snapshot().Tables["/ip/route"]) != 1 || len(fake.Calls()) != calls {
					t.Fatal("pre-write shutdown was reported/committed as a successful release")
				}
				return
			}
			if last == nil || last.OK != (mode == "disconnect") {
				t.Fatalf("incorrect release receipt: %+v", last)
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
	s := httpxtest.New(t, httpxtest.Options{})
	api.New(api.Deps{Repo: s.Repo}).Register(s.App.Router)
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
