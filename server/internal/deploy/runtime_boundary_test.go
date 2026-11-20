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
	"configflow/internal/repo"
)

func startGlobalPublishRuntime(t *testing.T, ctx context.Context) {
	t.Helper()
	lifecycle.Stop(8 * time.Second)
	if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	lifecycle.Start(ctx)
	t.Cleanup(func() {
		lifecycle.Stop(8 * time.Second)
		if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
			t.Error("restore idle test Runtime", err)
		}
	})
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
	startGlobalPublishRuntime(t, f.ctx)
	ctx := lifecycle.BindRequest(f.ctx)
	b := f.bundle(t, "closed")
	target := mustTarget(t, f)
	lifecycle.Stop(8 * time.Second)
	before := durableSnapshot(t, f)
	calls := len(f.fake.Calls())
	checks := []struct {
		name string
		run  func() error
	}{
		{"publish-direct", func() error { _, e := PublishDirect(ctx, f.m, "publish-agent", b, PublishOptions{}); return e }},
		{"publish-pair-entry", func() error { _, e := Publish(ctx, f.m, "publish-agent", b, PublishOptions{}); return e }},
		{"push-entry", func() error { _, e := Push(ctx, f.m, "publish-agent", PushInput{Bundle: b}); return e }},
		{"push-http-owner", func() error { return PushConfig(ctx, f.m, "publish-agent", PushRequest{Body: []byte(`{}`)}).Err }},
		{"generate-owner", func() error { return GenerateFinal(ctx, f.m, "publish-agent", GenerateRequest{Body: []byte(`{}`)}).Err }},
		{"reserve", func() error { _, e := f.m.BeginDeployment(ctx, "publish-agent", "closed"); return e }},
		{"record", func() error {
			_, e := recordUpdate(ctx, f.m, "publish-agent", "closed", Update{Status: ptr("uploading"), Submitted: ptr(true)}, agents.RecordOptions{})
			return e
		}},
		{"activate", func() error { _, e := f.m.ActivateCandidateModel(ctx, "publish-agent", "closed"); return e }},
		{"rollback", func() error { _, e := f.m.RollbackDeploymentModel(ctx, "publish-agent", "closed"); return e }},
		{"recover", func() error { _, e := f.m.RecoverDeploymentModel(ctx, "publish-agent", "closed"); return e }},
		{"receipt", func() error {
			return NewReceipts(f.m.Repository().DataDir()).Capture(ctx, "publish-agent", b, f.source, "https://panel.example.com", nil)
		}},
		{"preview", func() error {
			_, e := NewPreviews(f.m.Repository().DataDir(), nil).Save(ctx, PreviewInput{Target: target, Source: f.source, Bundle: b, Members: []Target{target}})
			return e
		}},
		{"atomic-file", func() error {
			return WriteFileAtomic(ctx, filepath.Join(f.m.Repository().DataDir(), "closed.txt"), []byte("no write"))
		}},
		{"provider-cache", func() error { return provider.CommitCacheUpdates(ctx, nil) }},
		{"provider-artifacts", func() error { return provider.CommitProviderBundle(ctx, "default", nil) }},
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
	startGlobalPublishRuntime(t, f.ctx)
	ctx := lifecycle.BindRequest(f.ctx)
	before := durableSnapshot(t, f)
	f.m.Repository().Store().BeforeCommit = func() error { lifecycle.Stop(8 * time.Second); return nil }
	_, err := f.m.BeginDeployment(ctx, "publish-agent", "at-commit")
	defer f.m.FinishDeploymentPreparation("publish-agent", "at-commit")
	f.m.Repository().Store().BeforeCommit = nil
	if !errors.Is(err, context.Canceled) {
		t.Error("BeforeCommit shutdown returned", err)
	}
	if !reflect.DeepEqual(before, durableSnapshot(t, f)) {
		t.Fatal("cancelled SQLite transaction committed")
	}
	lifecycle.Start(f.ctx)
	defer lifecycle.Stop(8 * time.Second)
	if _, err = f.m.BeginDeployment(lifecycle.BindRequest(f.ctx), "publish-agent", "at-commit"); err != nil {
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
	startGlobalPublishRuntime(t, f.ctx)
	ctx := lifecycle.BindRequest(f.ctx)
	client := &acceptedBeforeRecord{next: agents.HTTPClient{}, arrived: make(chan struct{}), release: make(chan struct{})}
	f.m.SetClient(client)
	b := f.bundle(t, "accepted-at-stop")
	done := make(chan error, 1)
	go func() {
		_, err := PublishDirect(ctx, f.m, "publish-agent", b, PublishOptions{Activate: true, SourceSnapshot: f.source, BaseURL: "https://panel.example.com"})
		done <- err
	}()
	<-client.arrived
	before := durableSnapshot(t, f)
	lifecycle.Stop(8 * time.Second)
	close(client.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, durableSnapshot(t, f)) {
		t.Fatal("late accepted response committed after shutdown")
	}
	task := f.agent(t).GetObject("deployments").GetObject("accepted-at-stop")
	if !task.GetBool("submitted") || task.GetString("status") != "uploading" || task.GetString("source_sha256") == "" || f.agent(t).Has("applied_content") {
		t.Fatal("uncertain durable submission lost", task)
	}
	if len(f.fake.Journal()) != 1 {
		t.Fatal("Agent did not accept real upload")
	}
	lifecycle.Start(f.ctx)
	defer lifecycle.Stop(8 * time.Second)
	var last *model.Deployment
	var err error
	for i := 0; i < 4; i++ {
		last, err = f.m.GetDeploymentModel(lifecycle.BindRequest(f.ctx), "publish-agent", "accepted-at-stop")
		if err != nil {
			t.Fatal(err)
		}
	}
	if last.Status != "succeeded" || !f.agent(t).Has("applied_content") {
		t.Fatal("restart could not resolve saved uncertain result", last)
	}
}

func TestDisconnectedBoundRequestCompletesPublicationAndArtifacts(t *testing.T) {
	f := newPublish(t, agentfake.Options{}, nil)
	startGlobalPublishRuntime(t, f.ctx)
	defer lifecycle.Stop(8 * time.Second)
	request, cancel := context.WithCancel(f.ctx)
	ctx := lifecycle.BindRequest(request)
	cancel()
	b := f.bundle(t, "disconnected-runtime")
	answer, err := PublishDirect(ctx, f.m, "publish-agent", b, PublishOptions{Activate: true, SourceSnapshot: f.source, BaseURL: "https://panel.example.com"})
	if err != nil || !resultView(fixtureTask(answer)).Submitted {
		t.Fatal(answer, err)
	}
	target := mustTarget(t, f)
	if _, err = NewPreviews(f.m.Repository().DataDir(), nil).Save(ctx, PreviewInput{Target: target, Source: f.source, Bundle: b, Members: []Target{target}}); err != nil {
		t.Fatal(err)
	}
	receipts := NewReceipts(f.m.Repository().DataDir())
	if err = receipts.Capture(ctx, "publish-agent", b, f.source, "https://panel.example.com", nil); err != nil {
		t.Fatal(err)
	}
	repository, _ := repo.GetRepository(f.ctx)
	if _, err = os.Stat(filepath.Join(repository.DataDir(), ".generated-previews")); err != nil {
		t.Fatal(err)
	}
	if len(f.fake.Journal()) != 1 {
		t.Fatal("browser cancellation stopped upload")
	}
}

func TestNativePairOwnerTakesPrecedenceAndRuntimeGuards(t *testing.T) {
	f := newPublish(t, agentfake.Options{}, nil)
	startGlobalPublishRuntime(t, f.ctx)
	nativeCalls, legacyCalls := 0, 0
	restoreLegacy := SetPairPublication(func(context.Context, *agents.Manager, string, *Bundle, PublishOptions) (*model.Deployment, bool, error) {
		legacyCalls++
		return nil, true, nil
	})
	defer restoreLegacy()
	restoreNative := SetPairPublication(func(ctx context.Context, _ *agents.Manager, _ string, _ *Bundle, _ PublishOptions) (*model.Deployment, bool, error) {
		nativeCalls++
		if ctx.Err() != nil {
			t.Fatal("stopped owner was dispatched")
		}
		var result model.Deployment
		err := json.Unmarshal([]byte(`{"success":true,"deployment_id":"native-owner"}`), &result)
		return &result, true, err
	})
	defer restoreNative()
	bound := lifecycle.BindRequest(f.ctx)
	request, disconnect := context.WithCancel(bound)
	disconnect()
	result, err := Publish(request, f.m, "publish-agent", nil, PublishOptions{})
	if err != nil || result.DeploymentID != "native-owner" || nativeCalls != 1 || legacyCalls != 0 {
		t.Fatal("native owner did not run once", result, err, nativeCalls, legacyCalls)
	}
	lifecycle.Stop(8 * time.Second)
	if _, err = Publish(bound, f.m, "publish-agent", nil, PublishOptions{}); !errors.Is(err, context.Canceled) || nativeCalls != 1 || legacyCalls != 0 {
		t.Fatal("shutdown dispatched owner", err, nativeCalls, legacyCalls)
	}
}
