package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/provider"
)

func startGlobalPublishRuntime(t *testing.T, ctx context.Context) *lifecycle.Runtime {
	t.Helper()
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(ctx)
	t.Cleanup(func() { rt.Stop(8 * time.Second) })
	return rt
}

func durableSnapshot(t *testing.T, f *publishFixture) map[string]string {
	t.Helper()
	r := f.m.Repository()
	entry, err := r.Store().Load()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{"@database": string(raw)}
	err = filepath.WalkDir(r.DataDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(r.DataDir(), path)
		if strings.HasPrefix(rel, "configflow.sqlite3") {
			return nil
		}
		if d.IsDir() {
			out[rel] = "@directory"
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestShutdownRejectsEveryPublishCommit(t *testing.T) {
	f := newPublish(t, agentfake.Options{}, nil)
	rt := startGlobalPublishRuntime(t, f.ctx)
	ctx := rt.BindRequest(f.ctx)
	b := f.bundle(t, "closed")
	target := mustTarget(t, f)
	rt.Stop(8 * time.Second)
	before := durableSnapshot(t, f)
	calls := len(f.fake.Calls())
	checks := []struct {
		name string
		run  func() error
	}{
		{"publish-direct", func() error {
			_, e := New(Options{}).PublishDirect(ctx, f.m, "publish-agent", b, PublishOptions{})
			return e
		}},
		{"publish-pair-entry", func() error { _, e := New(Options{}).Publish(ctx, f.m, "publish-agent", b, PublishOptions{}); return e }},
		{"push-entry", func() error { _, e := New(Options{}).Push(ctx, f.m, "publish-agent", PushInput{Bundle: b}); return e }},
		{"push-http-owner", func() error {
			return New(Options{}).PushConfig(ctx, f.m, "publish-agent", PushRequest{Body: []byte(`{}`)}).Err
		}},
		{"generate-owner", func() error {
			return New(Options{}).GenerateFinal(ctx, f.m, "publish-agent", GenerateRequest{Body: []byte(`{}`)}).Err
		}},
		{"reserve", func() error { _, e := f.m.ReserveDeployment(ctx, "publish-agent", "closed"); return e }},
		{"record", func() error {
			_, e := recordUpdate(ctx, f.m, "publish-agent", "closed", Update{Status: new("uploading"), Submitted: new(true)}, agents.RecordOptions{})
			return e
		}},
		{"activate", func() error { _, e := f.m.ActivateCandidateModel(ctx, "publish-agent", "closed"); return e }},
		{"rollback", func() error { _, e := f.m.RollbackDeploymentModel(ctx, "publish-agent", "closed"); return e }},
		{"recover", func() error { _, e := f.m.RecoverDeploymentModel(ctx, "publish-agent", "closed"); return e }},
		{"receipt", func() error {
			return NewReceipts(f.m.Repository()).Capture(ctx, "publish-agent", b, f.source, "https://panel.example.com", nil)
		}},
		{"preview", func() error {
			_, e := NewPreviews(f.m.Repository().DataDir(), nil).Save(ctx, PreviewInput{Target: target, Source: f.source, Bundle: b, Members: []Target{target}})
			return e
		}},
		{"atomic-file", func() error {
			return WriteFileAtomic(ctx, filepath.Join(f.m.Repository().DataDir(), "closed.txt"), []byte("no write"))
		}},
		{"provider-cache", func() error { return provider.New(provider.Options{Repo: f.m.Repository()}).CommitWrites(ctx, nil) }},
		{"provider-artifacts", func() error {
			return provider.New(provider.Options{Repo: f.m.Repository()}).CommitArtifacts(ctx, "default", nil)
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if e := check.run(); !errors.Is(e, context.Canceled) {
				t.Fatal("shutdown was not rejected", e)
			}
			if !reflect.DeepEqual(before, durableSnapshot(t, f)) || len(f.fake.Calls()) != calls {
				t.Fatal("post-shutdown database/file/device write")
			}
		})
	}
}

func TestShutdownAtSQLiteCommitKeepsWholeDocument(t *testing.T) {
	f := newPublish(t, agentfake.Options{}, nil)
	rt := startGlobalPublishRuntime(t, f.ctx)
	ctx := rt.BindRequest(f.ctx)
	before := durableSnapshot(t, f)
	f.m.Repository().Store().BeforeCommit = func() error { rt.Stop(8 * time.Second); return nil }
	_, err := f.m.ReserveDeployment(ctx, "publish-agent", "at-commit")
	defer f.m.FinishDeploymentPreparation("publish-agent", "at-commit")
	f.m.Repository().Store().BeforeCommit = nil
	if !errors.Is(err, context.Canceled) {
		t.Error("BeforeCommit shutdown returned", err)
	}
	if !reflect.DeepEqual(before, durableSnapshot(t, f)) {
		t.Fatal("cancelled SQLite transaction committed")
	}
	rt.Start(f.ctx)
	defer rt.Stop(8 * time.Second)
	if _, err = f.m.ReserveDeployment(rt.BindRequest(f.ctx), "publish-agent", "at-commit"); err != nil {
		t.Fatal("cancelled reservation leaked", err)
	}
	f.m.FinishDeploymentPreparation("publish-agent", "at-commit")
}

type acceptedBeforeRecord struct {
	next             agents.Client
	arrived, release chan struct{}
}

func (c *acceptedBeforeRecord) Do(ctx context.Context, request agents.Request) (*agents.Response, error) {
	response, err := c.next.Do(ctx, request)
	if request.Method == http.MethodPost && strings.HasSuffix(request.URL, "/api/deployments") && err == nil {
		close(c.arrived)
		<-c.release
	}
	return response, err
}

func TestShutdownAfterAgentAcceptanceRetainsUncertainSubmission(t *testing.T) {
	f := newPublish(t, agentfake.Options{}, nil)
	rt := startGlobalPublishRuntime(t, f.ctx)
	ctx := rt.BindRequest(f.ctx)
	client := &acceptedBeforeRecord{next: agents.HTTPClient{}, arrived: make(chan struct{}), release: make(chan struct{})}
	f.m.SetClient(client)
	b := f.bundle(t, "accepted-at-stop")
	done := make(chan error, 1)
	go func() {
		_, err := New(Options{}).PublishDirect(ctx, f.m, "publish-agent", b, PublishOptions{Activate: true, SourceSnapshot: f.source, BaseURL: "https://panel.example.com"})
		done <- err
	}()
	<-client.arrived
	before := durableSnapshot(t, f)
	rt.Stop(8 * time.Second)
	close(client.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, durableSnapshot(t, f)) {
		t.Fatal("late accepted response committed after shutdown")
	}
	task, ok := f.agent(t).Deployments.Get("accepted-at-stop")
	if !ok || task.Submitted == nil || !*task.Submitted || task.Status != "uploading" || textValue(task.SourceSHA256) == "" || f.agent(t).AppliedContent != nil {
		t.Fatal("uncertain durable submission lost", task)
	}
	if len(f.fake.Journal()) != 1 {
		t.Fatal("Agent did not accept real upload")
	}
	rt.Start(f.ctx)
	defer rt.Stop(8 * time.Second)
	var last *model.Deployment
	var err error
	for i := 0; i < 4; i++ {
		last, err = f.m.GetDeploymentModel(rt.BindRequest(f.ctx), "publish-agent", "accepted-at-stop")
		if err != nil {
			t.Fatal(err)
		}
	}
	if last.Status != "succeeded" || f.agent(t).AppliedContent == nil {
		t.Fatal("restart could not resolve saved uncertain result", last)
	}
}

func TestDisconnectedBoundRequestCompletesPublicationAndArtifacts(t *testing.T) {
	f := newPublish(t, agentfake.Options{}, nil)
	rt := startGlobalPublishRuntime(t, f.ctx)
	defer rt.Stop(8 * time.Second)
	request, cancel := context.WithCancel(f.ctx)
	ctx := rt.BindRequest(request)
	cancel()
	b := f.bundle(t, "disconnected-runtime")
	answer, err := New(Options{}).PublishDirect(ctx, f.m, "publish-agent", b, PublishOptions{Activate: true, SourceSnapshot: f.source, BaseURL: "https://panel.example.com"})
	if err != nil || !resultView(fixtureTask(answer)).Submitted {
		t.Fatal(answer, err)
	}
	target := mustTarget(t, f)
	if _, err = NewPreviews(f.m.Repository().DataDir(), nil).Save(ctx, PreviewInput{Target: target, Source: f.source, Bundle: b, Members: []Target{target}}); err != nil {
		t.Fatal(err)
	}
	receipts := NewReceipts(f.m.Repository())
	if err = receipts.Capture(ctx, "publish-agent", b, f.source, "https://panel.example.com", nil); err != nil {
		t.Fatal(err)
	}
	repository := f.m.Repository()
	if _, err = os.Stat(filepath.Join(repository.DataDir(), ".generated-previews")); err != nil {
		t.Fatal(err)
	}
	if len(f.fake.Journal()) != 1 {
		t.Fatal("browser cancellation stopped upload")
	}
}

func TestNativePairOwnerTakesPrecedenceAndRuntimeGuards(t *testing.T) {
	f := newPublish(t, agentfake.Options{}, nil)
	rt := startGlobalPublishRuntime(t, f.ctx)
	nativeCalls := 0
	dp := New(Options{Pair: func(ctx context.Context, _ *agents.Manager, _ string, _ *Bundle, _ PublishOptions) (*model.Deployment, bool, error) {
		nativeCalls++
		if ctx.Err() != nil {
			t.Fatal("stopped owner was dispatched")
		}
		var result model.Deployment
		err := json.Unmarshal([]byte(`{"success":true,"deployment_id":"native-owner"}`), &result)
		return &result, true, err
	}})
	bound := rt.BindRequest(f.ctx)
	request, disconnect := context.WithCancel(bound)
	disconnect()
	result, err := dp.Publish(request, f.m, "publish-agent", nil, PublishOptions{})
	if err != nil || result.DeploymentID != "native-owner" || nativeCalls != 1 {
		t.Fatal("native owner did not run once", result, err, nativeCalls)
	}
	rt.Stop(8 * time.Second)
	if _, err = dp.Publish(bound, f.m, "publish-agent", nil, PublishOptions{}); !errors.Is(err, context.Canceled) || nativeCalls != 1 {
		t.Fatal("shutdown dispatched owner", err, nativeCalls)
	}
}
