package server

import (
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/notifications"
	"configflow/internal/refresh"
	"configflow/internal/taskstatus"
)

func TestConfigFindingsFailedReceiptsCreateNotificationsBeforeAllTaskSlotsExist(t *testing.T) {
	NewApp()
	srv := httpxtest.New(t, httpxtest.Options{})
	ctx := srv.Context()
	_, err := srv.Repo.SaveShared(ctx, jsonx.Obj("subscriptions", []any{jsonx.Obj("id", "backoff", "name", "Synthetic", "url", "https://feed.example/sub", "interval", 60, "enabled", true), jsonx.Obj("id", "due", "name", "Due", "url", "https://feed.example/due", "interval", 60)}), nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		_, err := taskstatus.New(srv.DataDir).Record(refresh.SourceTaskKey("backoff"), taskstatus.Result{OK: false, Trigger: "scheduled", Error: "上游返回 HTTP 403"})
		if err != nil {
			t.Fatal(err)
		}
	}
	// A due task completes; projecting all known receipts must also expose
	// the pre-existing backoff failure without executing that source again.
	_, err = notifications.TaskStatus(ctx, srv.DataDir).Record(refresh.SourceTaskKey("due"), taskstatus.Result{OK: true, Trigger: "scheduled"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(srv.DataDir, "notifications.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("notification missing: %v", err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	item := value.(*jsonx.Object).GetObject("items").GetObject("task:subscription:backoff")
	if item.GetString("state") != "pending" || item.GetInt("failure_count") != 3 || item.GetString("message") != "上游返回 HTTP 403" {
		t.Fatalf("missing failed/backoff notification: %s", data)
	}
}
