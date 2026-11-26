package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
)

func TestConfigFindingsFailedReceiptsCreateNotificationsBeforeAllTaskSlotsExist(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{RepoOptions: RepoOptions()})
	prod, err := New(srv.Context(), Options{Repo: srv.Repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		prod.Runtime.Shutdown(context.Background())
		prod.Agents.Close()
	})
	ctx := srv.Context()
	err = srv.Repo.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		shared.Subscriptions = []model.Subscription{
			testjson.Decode[model.Subscription](t, map[string]any{"id": "backoff", "name": "Synthetic", "url": "https://feed.example/sub", "interval": 60, "enabled": true}),
			testjson.Decode[model.Subscription](t, map[string]any{"id": "due", "name": "Due", "url": "https://feed.example/due", "interval": 60}),
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		_, err := taskstatus.New(srv.DataDir).SaveResult(ctx, refresh.SourceTaskKey("backoff"), taskstatus.Result{OK: false, Trigger: "scheduled", Error: "上游返回 HTTP 403"})
		if err != nil {
			t.Fatal(err)
		}
	}
	// A due task completes; projecting all known receipts must also expose
	// the pre-existing backoff failure without executing that source again.
	_, err = prod.Notifications.TaskStatus(ctx, srv.DataDir).SaveResult(ctx, refresh.SourceTaskKey("due"), taskstatus.Result{OK: true, Trigger: "scheduled"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(srv.DataDir, "notifications.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("notification missing: %v", err)
	}
	var doc struct {
		Items map[string]struct {
			State        string `json:"state"`
			FailureCount int64  `json:"failure_count"`
			Message      string `json:"message"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	item := doc.Items["task:subscription:backoff"]
	if item.State != "pending" || item.FailureCount != 3 || item.Message != "上游返回 HTTP 403" {
		t.Fatalf("missing failed/backoff notification: %s", data)
	}
}
