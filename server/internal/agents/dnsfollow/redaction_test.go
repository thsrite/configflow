package dnsfollow

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"configflow/internal/agents/agentfake"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

func TestOutcomeRedactsCredentials(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.editAgent(t, "target", func(a *model.Agent) {
		a.Name = "target synthetic-source-token"
		_ = model.SetStoredMember(&a, "dns_follow_job", []byte(`{"password":"synthetic-private-password"}`))
	})
	var logs bytes.Buffer
	root, err := logx.Setup(logx.Config{Level: logx.Debug, Console: &logs})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	defer func() { _, _ = logx.Setup(logx.Config{Level: logx.Info}) }()
	secretMessage := "synthetic-source-token synthetic-target-token synthetic-private-password https://user:synthetic-url-password@dns.example/a?token=synthetic-query-key"
	_, _, err = f.s.saveState(f.ctx, "target", "failed", secretMessage, statePatch{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.s.Options(f.ctx, "target")
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(view)
	taskBytes, err := json.Marshal(taskRowMust(t, f))
	if err != nil {
		t.Fatal(err)
	}
	all := string(public) + string(taskBytes) + logs.String() + f.s.FailureMessage(f.ctx, errors.New(secretMessage))
	// The job's undeclared "password" member is dropped on load (O18 T1b) and
	// is no credential any more.
	for _, secret := range []string{"synthetic-source-token", "synthetic-target-token", "synthetic-url-password", "synthetic-query-key"} {
		if strings.Contains(all, secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
}
func taskRowMust(t *testing.T, f *fixture) *tasks.Task {
	t.Helper()
	a, err := f.m.ReadAgent(f.ctx, "target", true)
	if err != nil {
		t.Fatal(err)
	}
	row, err := agentTaskRow(f.ctx, a, taskstatus.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	return row
}
