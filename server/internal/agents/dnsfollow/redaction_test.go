package dnsfollow

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

func TestOutcomeRedactsCredentialsAndPreservesUnknownFields(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.editAgent(t, "target", func(a *agents.Record) {
		a.Set("name", "target synthetic-source-token")
		a.Set("future_agent", jsonx.Obj("n", 23))
		a.GetObject("dns_follow").Set("future_binding", jsonx.Obj("x", true))
		a.GetObject("dns_follow_state").Set("future_state", jsonx.Obj("x", 7))
		a.Set("dns_follow_job", jsonx.Obj("password", "synthetic-private-password"))
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
	raw, err := f.m.GetAgentByID(f.ctx, "target", true)
	if err != nil {
		t.Fatal(err)
	}
	if raw.GetObject("future_agent").GetInt("n") != 23 || raw.GetObject("dns_follow").GetObject("future_binding").Value("x") != true || raw.GetObject("dns_follow_state").GetObject("future_state").GetInt("x") != 7 || raw.GetObject("dns_follow_job").GetString("password") != "synthetic-private-password" {
		t.Fatal("owner rewrite lost private or future fields")
	}
}
func taskRowMust(t *testing.T, f *fixture) *jsonx.Object {
	t.Helper()
	a, err := f.m.GetAgentByID(f.ctx, "target", true)
	if err != nil {
		t.Fatal(err)
	}
	row, err := taskRow(f.ctx, a, jsonx.Obj())
	if err != nil {
		t.Fatal(err)
	}
	return row
}
