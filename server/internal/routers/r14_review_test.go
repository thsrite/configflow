package routers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

// F1: the 30 s download budget also covers connecting, waiting for headers and
// the client's retries. Running out there is a transient download failure in
// Python (requests ConnectTimeout/ReadTimeout → DownloadError → failed, retried
// at the next interval). It must not become the permanent size/time limit
// error, which pauses the sync until someone publishes manually.
func TestR14ReviewPreResponseTimeoutIsTransient(t *testing.T) {
	hang := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer hang.Close()
	// The production 30 s budget is unchanged; the caller's earlier deadline
	// expires first and takes the same path as the budget would.
	downloads := &RuleSyncDownloads{Client: &rulefetch.Client{Transport: rulefetch.HTTPTransport{}, Sleep: func(time.Duration) {}}}
	source := mustPolicyJSON(map[string]string{"id": "lib", "url": hang.URL, "behavior": "classical", "format": "text", "name": "Synthetic"})
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, err := downloads.Download(ctx, source, json.RawMessage(`{"system_config":{}}`))
	var transient *syncDownloadError
	if !errors.As(err, &transient) || !strings.HasPrefix(err.Error(), messageSyncDownloadError) {
		t.Fatalf("a timeout before any response is not a transient download failure: %v", err)
	}

	s, f, r := r14Fixture(t)
	s.Download = func(ctx context.Context, raw, config json.RawMessage) (string, error) {
		short, stop := context.WithTimeout(ctx, 300*time.Millisecond)
		defer stop()
		return (&RuleSyncDownloads{Client: downloads.Client}).Download(short, json.RawMessage(strings.Replace(string(raw), "https://example.com/ips", hang.URL, 1)), config)
	}
	writes := len(f.Calls())
	status, err := s.Sync(t.Context(), "default", r.ID, "schedule")
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "failed" {
		t.Fatalf("an unreachable rule server paused the sync instead of retrying: %+v", status)
	}
	if len(f.Calls()) != writes {
		t.Fatal("device contacted after a failed download")
	}
}

// F2: Python start_job reports a busy router with "请稍后重试"; only the sync
// itself says "请稍后再试". The text reaches the task page as a 409 message.
func TestR14ReviewStartJobBusyMessage(t *testing.T) {
	s, _, r := r14Fixture(t)
	_, release, lockErr := RouterJobLock(t.Context(), "default", r.ID, r)
	if lockErr != nil {
		t.Fatal("job lock not free")
	}
	defer release()
	if err := s.StartJob(t.Context(), "default", r.ID); err == nil || err.Error() != "此路由器正在发布或同步，请稍后重试" {
		t.Fatalf("start job busy message %v", err)
	}
	if _, err := s.Sync(t.Context(), "default", r.ID, "schedule"); err == nil || err.Error() != "此路由器正在发布或同步，请稍后再试" {
		t.Fatalf("sync busy message %v", err)
	}
}

// Integration guard for O3-6b (batch-6): the repository commit guard, Sync and
// StartJob must contend for one L1 table. batch-6 registers its own
// jobLockHook on a separate "router-job" table; if R14's syncJobs survives the
// merge next to it, this test fails because Sync no longer sees the hook's lock.
func TestR14ReviewRepositoryHookSharesJobLock(t *testing.T) {
	s, _, r := r14Fixture(t)
	Wire()
	current := repo.SetHooks(repo.Hooks{})
	repo.SetHooks(current)
	if current.JobLockRecord == nil {
		t.Fatal("routers did not register the job lock hook")
	}
	var raw model.Router
	if err := json.Unmarshal([]byte(`{"id":"`+r.ID+`","type":"routeros","endpoint":"`+r.Endpoint+`/"}`), &raw); err != nil {
		t.Fatal(err)
	}
	release, ok := current.JobLockRecord(t.Context(), "default", r.ID, raw)
	if !ok {
		t.Fatal("hook could not take a free job lock")
	}
	defer release()
	if _, err := s.Sync(t.Context(), "default", r.ID, "schedule"); err == nil || err.Error() != messageSyncBusy {
		t.Fatalf("sync ignored the repository hook's job lock: %v", err)
	}
	if err := s.StartJob(t.Context(), "default", r.ID); err == nil || err.Error() != messageEditBusy {
		t.Fatalf("start job ignored the repository hook's job lock: %v", err)
	}
}
