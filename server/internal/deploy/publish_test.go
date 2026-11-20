package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/model"
	"configflow/internal/repo"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type publishFixture struct {
	ctx    context.Context
	m      *agents.Manager
	fake   *agentfake.Agent
	source *Source
}

func newPublish(t *testing.T, o agentfake.Options, wrap func(http.Handler) http.Handler) *publishFixture {
	t.Helper()
	if o.AgentID == "" {
		o.AgentID = "publish-agent"
	}
	if o.ServiceType == "" {
		o.ServiceType = "mihomo"
	}
	o.Token = "synthetic-publish-token"
	fake := agentfake.New(o)
	var handler http.Handler = fake
	if wrap != nil {
		handler = wrap(handler)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	n, _ := strconv.Atoi(port)
	ctx := testContext(t)
	r, _ := repo.GetRepository(ctx)
	e := r.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		enabled := true
		sys.Agents = []model.Agent{{
			ID:          o.AgentID,
			Name:        "Synthetic",
			Host:        host,
			Port:        model.Int(n),
			Token:       o.Token,
			ServiceType: o.ServiceType,
			ProfileID:   "default",
			Version:     "1.8.60-go",
			Enabled:     &enabled,
		}}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	m, e := agents.New(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	cfg := map[string]any{
		"profile_id":    "default",
		"system_config": map[string]any{"server_domain": "https://panel.example.com"},
		"nodes":         []any{},
		"subscriptions": []any{},
		"proxy_groups":  []any{},
		"rule_configs":  []any{},
		"rule_library":  []any{},
		"mihomo":        map[string]any{"custom_config": "mode: rule\n"},
		"mosdns":        map[string]any{},
		"oxidns":        map[string]any{},
	}
	t.Cleanup(m.Close)
	return &publishFixture{ctx, m, fake, testSource(t, cfg)}
}
func (f *publishFixture) bundle(t *testing.T, id string) *Bundle {
	t.Helper()
	text := "mode: rule\n"
	service := "mihomo"
	target, _, e := targetByID(f.ctx, f.m, "publish-agent")
	if e != nil {
		t.Fatal(e)
	}
	service = target.ServiceType
	if service == "oxidns" {
		text = "plugins: []\n"
	}
	b, e := Build(f.ctx, BuildInput{Target: target, Source: f.source, ConfigContent: text, DeploymentID: id})
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func (f *publishFixture) agent(t *testing.T) *model.Agent {
	t.Helper()
	a, e := f.m.ReadAgent(f.ctx, "publish-agent", true)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestPublishBoundary(t *testing.T) {
	f := newPublish(t, agentfake.Options{ConfigPath: "installed.yaml"}, nil)
	b := f.bundle(t, "stage")
	got, e := PublishDirect(f.ctx, f.m, "publish-agent", b, PublishOptions{Activate: false, SourceSnapshot: f.source, BaseURL: "https://panel.example.com", ConfigRevision: &Revision{Value: 7}})
	if e != nil || resultView(fixtureTask(got)).Status != "verifying" {
		t.Fatal(got, e)
	}
	if f.agent(t).AppliedContent != nil {
		t.Fatal("accepted upload advanced baseline")
	}
	got, e = f.m.GetDeploymentModel(f.ctx, "publish-agent", "stage")
	if e != nil || resultView(fixtureTask(got)).Status != "ready" {
		t.Fatal(got, e)
	}
	got, e = f.m.ActivateCandidateModel(f.ctx, "publish-agent", "stage")
	if e != nil || resultView(fixtureTask(got)).Status != "succeeded" {
		t.Fatal(got, e)
	}
	a := f.agent(t)
	applied := a.AppliedContent
	if applied == nil || applied.DeploymentID != "stage" || applied.ConfigPath != "installed.yaml" {
		t.Fatal(a)
	}
	receipt, e := NewReceipts(f.m.Repository().DataDir()).LoadAgent(f.ctx, a, true)
	if e != nil || receipt == nil {
		t.Fatal("receipt", e)
	}
	calls := f.fake.Calls()
	uploads := 0
	for _, c := range calls {
		if c.Path == "/api/deployments" {
			uploads++
			if c.Headers.Get("Authorization") != "Bearer synthetic-publish-token" || c.Headers.Get("Content-Type") != "application/gzip" || c.Headers.Get("X-Activate") != "false" || c.Headers.Get("X-Deployment-ID") != "stage" || c.Headers.Get("X-Content-SHA256") != digest(c.Body) || c.Manifest.ConfigPath != "installed.yaml" {
				t.Fatal("wire contract", c)
			}
		} else if c.Path != "/api/config/final-preview" && (len(c.Body) != 0 || c.Headers.Get("Content-Type") != "") {
			t.Fatal("body on bodyless request", c)
		}
	}
	journal := f.fake.Journal()["stage"]
	if journal.Digest == "" || journal.Digest != calls[1].Headers.Get("X-Content-SHA256") {
		t.Fatal("Agent journal digest does not match accepted bytes", journal)
	}
	if uploads != 1 {
		t.Fatal("upload count", uploads)
	}
	got, e = f.m.RollbackDeploymentModel(f.ctx, "publish-agent", "stage")
	if e != nil || resultView(fixtureTask(got)).Status != "rolled_back" {
		t.Fatal(got, e)
	}
	if f.agent(t).AppliedContent != nil {
		t.Fatal("manual rollback retained baseline")
	}
}
func TestSubmittedBeforeNetworkAndLostResponse(t *testing.T) {
	var f *publishFixture
	f = newPublish(t, agentfake.Options{Script: []agentfake.Fault{{Path: "/api/deployments", Method: "POST", LoseResponse: true}}}, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/deployments" {
				a := f.agent(t)
				v, ok := a.Deployments.Get("lost")
				if !ok || v.Status != "uploading" || v.Submitted == nil || !*v.Submitted || textValue(v.SHA256) == "" || v.PreparedContent == nil {
					t.Error("POST before durable submitted", a)
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	got, e := PublishDirect(f.ctx, f.m, "publish-agent", f.bundle(t, "lost"), PublishOptions{Activate: true})
	if e != nil || resultView(fixtureTask(got)).Status != "unknown" || !resultView(fixtureTask(got)).Submitted {
		t.Fatal(got, e)
	}
	if f.agent(t).AppliedContent != nil {
		t.Fatal("uncertain upload advanced baseline")
	}
	got, e = f.m.GetDeploymentModel(f.ctx, "publish-agent", "lost")
	if e != nil || resultView(fixtureTask(got)).Status != "succeeded" {
		t.Fatal(got, e)
	}
	if f.agent(t).AppliedContent == nil {
		t.Fatal("confirmed poll did not advance baseline")
	}
}
func TestLateUploadCannotRegressPoll(t *testing.T) {
	arrived, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f := newPublish(t, agentfake.Options{Progress: []string{"checking", "succeeded"}}, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/deployments" {
				rec := httptest.NewRecorder()
				next.ServeHTTP(rec, r)
				once.Do(func() { close(arrived) })
				<-release
				for k, v := range rec.Header() {
					w.Header()[k] = v
				}
				w.WriteHeader(rec.Code)
				_, _ = w.Write(rec.Body.Bytes())
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	// Worker is tracked by RunDetached; the test's goroutine only waits for it.
	done := make(chan error, 1)
	b := f.bundle(t, "late")
	go func() {
		_, e := PublishDirect(f.ctx, f.m, "publish-agent", b, PublishOptions{Activate: true})
		done <- e
	}()
	<-arrived
	got, e := f.m.GetDeploymentModel(f.ctx, "publish-agent", "late")
	if e != nil || resultView(fixtureTask(got)).Status != "checking" {
		t.Fatal(got, e)
	}
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if f.agent(t).LatestDeployment == nil || f.agent(t).LatestDeployment.Status != "checking" {
		t.Fatal("late accepted receipt regressed confirmation")
	}
}
func TestPublishRefusalsAndJournalLoss(t *testing.T) {
	for _, tc := range []struct {
		name  string
		caps  json.RawMessage
		fault []agentfake.Fault
		want  string
		code  int
	}{
		{"old-agent", json.RawMessage(`{"service_type":"mihomo"}`), nil, "failed", 409},
		{"http-refusal", nil, []agentfake.Fault{{Path: "/api/deployments", Code: 403, Body: json.RawMessage(`{"success":false,"message":"refused"}`)}}, "failed", 403},
		{"invalid-answer", nil, []agentfake.Fault{{Path: "/api/deployments", Code: 202, Body: json.RawMessage(`[]`)}}, "unknown", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublish(t, agentfake.Options{Capabilities: tc.caps, Script: tc.fault}, nil)
			r, e := PublishDirect(f.ctx, f.m, "publish-agent", f.bundle(t, "refuse"), PublishOptions{})
			if e != nil || resultView(fixtureTask(r)).Status != tc.want || resultView(fixtureTask(r)).HTTPStatus != tc.code {
				t.Fatal(r, e)
			}
			if f.agent(t).AppliedContent != nil {
				t.Fatal("failure advanced baseline")
			}
		})
	}
	f := newPublish(t, agentfake.Options{}, nil)
	b := f.bundle(t, "restart")
	_, e := PublishDirect(f.ctx, f.m, "publish-agent", b, PublishOptions{})
	if e != nil {
		t.Fatal(e)
	}
	f.fake.Restart(true)
	r, e := f.m.GetDeploymentModel(f.ctx, "publish-agent", "restart")
	if e != nil || resultView(fixtureTask(r)).Status != "failed" || !strings.Contains(textValue(r.Message), "no record") {
		t.Fatal(r, e)
	}
}
