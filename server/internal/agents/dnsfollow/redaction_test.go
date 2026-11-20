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

func TestOutcomeRedactsCredentialsAndPreservesUnknownFields(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.editAgent(t, "target", func(a *model.Agent) {
		a.Name = "target synthetic-source-token"
		_ = a.Fields.Set("future_agent", json.RawMessage(`{"n":23}`))
		_ = a.DNSFollow.Fields.Set("future_binding", json.RawMessage(`{"x":true}`))
		_ = a.DNSFollowState.Fields.Set("future_state", json.RawMessage(`{"x":7}`))
		_ = a.SetJSON("dns_follow_job", []byte(`{"password":"synthetic-private-password"}`))
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
	for _, secret := range []string{"synthetic-source-token", "synthetic-target-token", "synthetic-private-password", "synthetic-url-password", "synthetic-query-key"} {
		if strings.Contains(all, secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	raw, err := f.m.ReadAgent(f.ctx, "target", true)
	if err != nil {
		t.Fatal(err)
	}
	fa, _ := raw.Fields.Get("future_agent")
	fb, _ := raw.DNSFollow.Fields.Get("future_binding")
	fs, _ := raw.DNSFollowState.Fields.Get("future_state")
	jp, _ := raw.DNSFollowJob.Fields.Get("password")
	if string(fa) != `{"n":23}` || string(fb) != `{"x":true}` || string(fs) != `{"x":7}` || string(jp) != `"synthetic-private-password"` {
		t.Fatal("owner rewrite lost private or future fields", string(fa), string(fb), string(fs), string(jp))
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
