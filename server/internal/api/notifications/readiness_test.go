package notifications_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	apinotifications "configflow/internal/api/notifications"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

func notificationServer(t *testing.T) *httpxtest.Server {
	t.Helper()
	for _, slot := range []string{tasks.SlotSubscriptions, tasks.SlotRuleSources, tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains, tasks.SlotBackup, tasks.SlotMetrics} {
		t.Cleanup(tasks.SetRowBuilder(slot, func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }))
	}
	t.Cleanup(notifications.SetInputs(notifications.Inputs{
		Rows: tasks.Available, MissingTasks: tasks.Missing,
		Agents: func(context.Context) ([]model.Agent, error) { return nil, nil },
	}))
	return httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apinotifications.Register)})
}

func TestMissingTaskSlotsCannotRetireRouterOrAgentFailures(t *testing.T) {
	s := notificationServer(t)
	t.Cleanup(tasks.SetRowBuilder(tasks.SlotRouters, nil))
	t.Cleanup(tasks.SetRowBuilder(tasks.SlotAgents, nil))
	if err := notifications.UpdateItems(s.Context(), s.DataDir, func(items *notifications.Items) error {
		for _, key := range []string{"task:router_rule_sync:default:r", "task:agent_rule_sync:a", "deployment:a"} {
			var item notifications.Item
			if err := json.Unmarshal([]byte(`{"state":"pending","source":"task","version":"v","read":false}`), &item); err != nil {
				return err
			}
			items.Set(key, &item)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.DataDir, "notifications.json")
	before, _ := os.ReadFile(path)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := s.Do(method, "/api/notifications", httpxtest.Body([]byte(`{"versions":{}}`), "application/json")).WantStatus(503)
		if method == http.MethodGet {
			var body struct {
				MissingSlots []string `json:"missing_slots"`
			}
			if err := json.Unmarshal(r.Body, &body); err != nil || len(body.MissingSlots) != 2 {
				t.Fatal(r.Text())
			}
		}
		if method == http.MethodHead && r.Text() != "" {
			t.Fatal("HEAD body", r.Text())
		}
	}
	if err := notifications.RefreshTasks(s.Context(), "subscription:s"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("incomplete observation retired old failures")
	}
}

func TestHeadProjectsWithoutMigratingOrWritingReceipts(t *testing.T) {
	s := notificationServer(t)
	path := filepath.Join(s.DataDir, "notifications.json")
	before := []byte(`{"unknown":"keep","items":{"old":{"state":"resolved","read":true}}}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	stat, _ := os.Stat(path)
	head := s.Do(http.MethodHead, "/api/notifications", httpxtest.Body([]byte(`{"versions":{"old":"v"}}`), "application/json")).WantStatus(200)
	after, _ := os.ReadFile(path)
	next, _ := os.Stat(path)
	if head.Text() != "" || !bytes.Equal(before, after) || !os.SameFile(stat, next) || !stat.ModTime().Equal(next.ModTime()) {
		t.Fatal("HEAD committed or returned a body")
	}
	get := s.Get("/api/notifications").WantStatus(200)
	for _, key := range []string{"Cache-Control", "Content-Type"} {
		if get.Header.Get(key) != head.Header.Get(key) {
			t.Fatal(key)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.Do(http.MethodHead, "/api/notifications").WantStatus(200)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("HEAD created store", err)
	}
}

func TestReadBindingRejectsMalformedAndWrongShapes(t *testing.T) {
	s := notificationServer(t)
	for _, body := range []string{"{", "[]", "null", "true", `{"versions":null}`, `{"versions":{"a":null}}`, `{"Versions":{}}`, `{"versions":{"a":12}}`, `{"versions":{},"ignored":NaN}`} {
		s.Post("/api/notifications/read", httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
	}
	s.Post("/api/notifications/read", httpxtest.Body([]byte(`{"versions":{}}`), "application/json")).WantStatus(200)
}

func TestInvalidSavedHistoryReturnsUnavailableWithoutReplacingReceipts(t *testing.T) {
	s := notificationServer(t)
	t.Cleanup(tasks.SetRowBuilder(tasks.SlotSubscriptions, func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) {
		var receipt taskstatus.Receipt
		_ = json.Unmarshal([]byte(`{"run_count":"bad"}`), &receipt)
		_, err := tasks.ProjectHistory(&receipt)
		return nil, err
	}))
	path := filepath.Join(s.DataDir, "notifications.json")
	before := []byte(`{"items":{"old":{"state":"pending","source":"task","version":"v"}}}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		s.Do(method, "/api/notifications").WantStatus(503)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("bad history replaced notification state", err)
	}
}
