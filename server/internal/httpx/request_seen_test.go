//go:build !configflow_proddiff

package httpx_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

func TestRequestSeenAfterProfileAndAuth(t *testing.T) {
	mode := os.Getenv("O33_HTTPX_SCENARIO")
	if mode == "" {
		for _, scenario := range []string{"protected", "public-heartbeat"} {
			t.Run(scenario, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRequestSeenAfterProfileAndAuth$", "-test.count=1")
				cmd.Env = append(os.Environ(), "O33_HTTPX_SCENARIO="+scenario)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("request gate process: %v\n%s", err, out)
				}
			})
		}
		return
	}
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo.SetRepository(r)
	auth.Configure(auth.Config{AdminUsername: "test-admin", AdminPassword: "test-password", SecretKey: "synthetic-signing-key"})
	reg := lifecycle.NewRegistry()
	ran := make(chan struct{}, 2)
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "request-gated", Start: lifecycle.AfterFirstRequest,
		Round: func(context.Context) error { ran <- struct{}{}; return nil }})
	if err := lifecycle.Configure(lifecycle.Options{Registry: reg}); err != nil {
		t.Fatal(err)
	}
	lifecycle.Start(context.Background())
	defer lifecycle.Stop(time.Second)
	app := httpx.New()
	auth.Install(app)
	app.Router.GET("/api/profiles/<profile_id>/protected", "test.protected", func(*httpx.Ctx) (*httpx.Response, error) { return httpx.Empty(200), nil })
	app.Router.POST("/api/agents/<agent_id>/heartbeat", "agents.agent_heartbeat", func(*httpx.Ctx) (*httpx.Response, error) { return httpx.Empty(200), nil })
	request := func(method, path, token string, status int) {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s: status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
	request("GET", "/api/profiles/missing/protected", "", 401)
	request("GET", "/api/profiles/default/protected", "", 401)
	request("GET", "/api/profiles/default/protected", "invalid-token", 401)
	select {
	case <-ran:
		t.Fatal("rejected profile/authentication started the loop")
	default:
	}
	if mode == "public-heartbeat" {
		request("POST", "/api/agents/edge/heartbeat", "", 200)
	} else {
		token, err := auth.GenerateToken("test-admin")
		if err != nil {
			t.Fatal(err)
		}
		request("GET", "/api/profiles/default/protected", token, 200)
		request("GET", "/api/profiles/default/protected", token, 200)
	}
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("accepted request did not start the loop")
	}
	lifecycle.Stop(time.Second)
	select {
	case <-ran:
		t.Fatal("later request restarted the one-shot driver")
	default:
	}
}
