package webdav

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
)

func testWorker(t *testing.T, delay time.Duration, upload UploadFunc) *Worker {
	t.Helper()
	r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	r.Start(context.Background())
	w := newWorker(delay, upload, workerOptions{Spawner: r})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = w.Close(ctx)
		r.Shutdown(context.Background())
	})
	return w
}

type memorySource struct {
	mu    sync.Mutex
	value Snapshot
	err   error
	reads int
}

func (s *memorySource) Snapshot(context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	return s.value, s.err
}
func (s *memorySource) set(value string, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value.Document = json.RawMessage(value)
	s.value.Settings.Auto = enabled
}
func waitWorker(t *testing.T, w *Worker) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := w.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestWorkerDebounceReadsLatestAndDisablingCancelsPending(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "latest", false: "disabled"}[enabled], func(t *testing.T) {
			source := &memorySource{}
			source.set(`{"save":0}`, true)
			uploaded := []string{}
			worker := testWorker(t, 30*time.Millisecond, func(_ context.Context, snapshot Snapshot) error {
				uploaded = append(uploaded, string(snapshot.Document))
				return nil
			})
			for i := range 20 {
				source.set(`{"save":`+fmt.Sprint(i)+`}`, true)
				worker.Schedule(source)
			}
			if !enabled {
				source.set(`{"save":19}`, false)
			}
			waitWorker(t, worker)
			want := 0
			if enabled {
				want = 1
			}
			if len(uploaded) != want {
				t.Fatalf("uploads %v", uploaded)
			}
			if enabled && uploaded[0] != `{"save":19}` {
				t.Fatal("did not read fresh snapshot")
			}
		})
	}
}
func TestWorkerCoalescesChangesDuringSlowUploadAndCanRestartAfterFailure(t *testing.T) {
	source := &memorySource{}
	source.set(`{"save":0}`, true)
	started, release := make(chan struct{}), make(chan struct{})
	uploaded := []string{}
	worker := testWorker(t, 15*time.Millisecond, func(_ context.Context, snapshot Snapshot) error {
		uploaded = append(uploaded, string(snapshot.Document))
		if len(uploaded) == 1 {
			close(started)
			<-release
			return errors.New("remote failure")
		}
		return nil
	})
	worker.Schedule(source)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upload not started")
	}
	for i := 1; i < 30; i++ {
		source.set(`{"save":`+fmt.Sprint(i)+`}`, true)
		worker.Schedule(source)
	}
	close(release)
	waitWorker(t, worker)
	if len(uploaded) != 2 || uploaded[1] != `{"save":29}` {
		t.Fatalf("uploads %v", uploaded)
	}
	source.set(`{"save":30}`, true)
	worker.Schedule(source)
	waitWorker(t, worker)
	if len(uploaded) != 3 {
		t.Fatal("later commit did not restart idle worker")
	}
}
func TestConfigurationChangeIgnoresOnlyRuntimeAgentFields(t *testing.T) {
	before := []byte(`{"shared":{},"profiles":{},"system":{"system_config":{},"backup":{},"agents":[{"id":"a","host":"example.com","status":"offline","token":"synthetic"}]}}`)
	runtime := []byte(`{"profiles":{},"shared":{},"system":{"backup":{},"system_config":{},"agents":[{"id":"a","token":"synthetic","host":"example.com","status":"online","last_heartbeat":"later","latest_deployment":{},"applied_content":{}}]}}`)
	changed, err := configurationChanged(before, runtime)
	if err != nil || changed {
		t.Fatalf("runtime changed=%t err=%v", changed, err)
	}
	settings := bytes.Replace(runtime, []byte(`"host":"example.com"`), []byte(`"host":"other.example.com"`), 1)
	changed, err = configurationChanged(before, settings)
	if err != nil || !changed {
		t.Fatalf("settings changed=%t err=%v", changed, err)
	}
}
func TestCommitHookRunsOutsideLockAndUploadFailureCannotRollback(t *testing.T) {
	f, server := newDAV(t)
	f.failMethod = "PUT"
	f.failStatus = 403
	service := NewService(server.Client())
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	worker := testWorker(t, 35*time.Millisecond, func(ctx context.Context, snapshot Snapshot) error {
		_, err := service.Run(ctx, snapshot, true)
		return err
	})
	r.OnConfigurationCommitted(commitObserver(worker))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = worker.Close(ctx)
	})
	ctx := context.Background()
	backupRaw, err := json.Marshal(map[string]any{
		"webdav_url": server.URL, "webdav_username": "fixture-user", "webdav_password": "fixture-secret",
		"webdav_path": "/one/two/", "auto_backup": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		return system.SetJSON("backup", backupRaw)
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("mihomo", json.RawMessage(`{"custom_config":"saved despite upload failure"}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	waitWorker(t, worker)
	profile, err := r.Profile(ctx, "default")
	if err != nil {
		t.Fatalf("committed state changed: %v", err)
	}
	profileRaw, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("committed state changed: %v", err)
	}
	parsedProfile, err := testjson.Parse(profileRaw)
	if err != nil {
		t.Fatalf("committed state changed: %v", err)
	}
	if got, _ := testjson.Path(parsedProfile, "mihomo", "custom_config"); got != "saved despite upload failure" {
		t.Fatalf("committed state changed: %v", got)
	}
	_, err = r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("mihomo", json.RawMessage(`{"custom_config":"next saved"}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	waitWorker(t, worker)
	snap, err := notifications.New(notifications.Options{Repo: r}).TaskStatus(ctx, r.DataDir()).Read()
	if err != nil {
		t.Fatal(err)
	}
	receipt := snap.Entry("webdav_backup")
	var runs int64
	if receipt != nil {
		if c, cerr := taskstatus.Count(receipt.RunCount); cerr == nil && c.IsInt64() {
			runs = c.Int64()
		}
	}
	if receipt == nil || receipt.LastStatus == nil || *receipt.LastStatus != "failed" || runs < 2 {
		t.Fatalf("receipt %#v", receipt)
	}
}
