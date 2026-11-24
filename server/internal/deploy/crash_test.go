package deploy

import (
	"bytes"
	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/testjson"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestPublishChild is re-executed only by TestPublishProcessRecovery.
func TestPublishChild(t *testing.T) {
	dir := os.Getenv("A14_CHILD_DATA")
	if dir == "" {
		t.Skip("process helper")
	}
	r, e := repo.OpenDefault(dir)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	m, e := agents.New(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	target, found, e := targetByID(ctx, m, "publish-agent")
	if e != nil || !found {
		t.Fatal(e)
	}
	source := testSource(t, map[string]any{"profile_id": "default", "rule_library": []any{}, "mihomo": map[string]any{}})
	main := "mode: rule\n"
	if os.Getenv("A14_CHILD_PHASE") == "preparing" {
		main = fmt.Sprintf("mode: rule\nrules: ['GEOIP,CN,DIRECT']\ngeox-url: {mmdb: http://%s:%s/api/geo}\n", target.Host, string(target.Port))
	}
	b, e := Build(ctx, BuildInput{Target: target, Source: source, ConfigContent: main, DeploymentID: "crash"})
	if e != nil {
		t.Fatal(e)
	}
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(context.Background())
	boundCtx := rt.BindRequest(ctx)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	defer signal.Stop(sig)
	done := make(chan error, 1)
	go func() {
		answer, e := New(Options{}).PublishDirect(boundCtx, m, "publish-agent", b, PublishOptions{Activate: true})
		if e == nil {
			t.Logf("child outcome: %+v", resultView(fixtureTask(answer)))
		}
		done <- e
	}()
	select {
	case <-sig:
		rt.Stop(8 * time.Second)
		if e = <-done; !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	}
}
func TestPublishProcessRecovery(t *testing.T) {
	for _, phase := range []string{"preparing", "uploading", "accepted"} {
		for _, sig := range []syscall.Signal{syscall.SIGKILL, syscall.SIGTERM} {
			t.Run(phase+"-"+sig.String(), func(t *testing.T) {
				arrived := make(chan struct{}, 1)
				release := make(chan struct{})
				var blocked atomic.Bool
				blocked.Store(true)
				f := newPublish(t, agentfake.Options{}, func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						match := (phase == "preparing" && r.URL.Path == "/api/geo") || (phase != "preparing" && r.URL.Path == "/api/deployments")
						if match && blocked.Load() {
							if phase == "accepted" {
								rec := httptest.NewRecorder()
								next.ServeHTTP(rec, r)
							}
							select {
							case arrived <- struct{}{}:
							default:
							}
							select {
							case <-r.Context().Done():
							case <-release:
							}
							return
						}
						next.ServeHTTP(w, r)
					})
				})
				cmd := exec.Command(os.Args[0], "-test.run=^TestPublishChild$", "-test.count=1", "-test.v")
				var output bytes.Buffer
				cmd.Stdout = &output
				cmd.Stderr = &output
				cmd.Env = append(os.Environ(), "A14_CHILD_DATA="+f.m.Repository().DataDir(), "A14_CHILD_PHASE="+phase)
				if e := cmd.Start(); e != nil {
					t.Fatal(e)
				}
				waited := false
				t.Cleanup(func() {
					if !waited {
						_ = cmd.Process.Kill()
						_ = cmd.Wait()
					}
				})
				select {
				case <-arrived:
				case <-time.After(20 * time.Second):
					t.Fatal("child never reached phase", output.String())
				}
				beforeDep, _ := f.agent(t).Deployments.Get("crash")
				before, _ := json.Marshal(beforeDep)
				if e := cmd.Process.Signal(sig); e != nil {
					t.Fatal(e)
				}
				e := cmd.Wait()
				waited = true
				if sig == syscall.SIGTERM && e != nil {
					t.Fatal("graceful child", e, output.String())
				}
				afterDep, _ := f.agent(t).Deployments.Get("crash")
				after, _ := json.Marshal(afterDep)
				if !testjson.Equal(before, after) {
					t.Fatal("shutdown wrote a false outcome", string(before), string(after))
				}
				blocked.Store(false)
				close(release)
				answer, e := f.m.GetDeploymentModel(f.ctx, "publish-agent", "crash")
				want := "failed"
				if phase == "accepted" {
					want = "succeeded"
				}
				if e != nil || resultView(fixtureTask(answer)).Status != want {
					t.Fatal(answer, e)
				}
				// A process-local preparation key cannot survive death; the next task starts.
				_, e = New(Options{}).PublishDirect(f.ctx, f.m, "publish-agent", f.bundle(t, "after-crash"), PublishOptions{Activate: false})
				if e != nil {
					t.Fatal(e)
				}
			})
		}
	}
}

func TestPublishKeepsProductionUploadTimeout(t *testing.T) {
	// A real HTTP read timeout, with the production 10s connect / 120s read pair.
	f := newPublish(t, agentfake.Options{Script: []agentfake.Fault{{Path: "/api/deployments", DelayMS: 121000, Code: 202, Body: []byte(`{"success":true,"status":"verifying"}`)}}}, nil)
	client := &observedPublishClient{next: agents.HTTPClient{}}
	f.m.SetClient(client)
	started := time.Now()
	answer, e := New(Options{}).PublishDirect(f.ctx, f.m, "publish-agent", f.bundle(t, "timeout"), PublishOptions{})
	if e != nil || resultView(fixtureTask(answer)).Status != "unknown" || !resultView(fixtureTask(answer)).Submitted {
		t.Fatal(answer, e)
	}
	if time.Since(started) < 119*time.Second {
		t.Fatal("production read timeout was shortened")
	}
	if client.uploadTimeout != agents.Pair(10, 120) {
		t.Fatal("upload timeout", client.uploadTimeout)
	}
}

type observedPublishClient struct {
	next          agents.Client
	uploadTimeout agents.Timeout
}

func (c *observedPublishClient) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	if r.Method == "POST" && strings.HasSuffix(r.URL, "/api/deployments") {
		c.uploadTimeout = r.Timeout
	}
	return c.next.Do(ctx, r)
}
