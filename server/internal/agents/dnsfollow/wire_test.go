package dnsfollow

import (
	"slices"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/tasks"
)

func TestC07RunnerAndManagerOwnerLifecycle(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	previous := tasks.Runner("dns_follow")
	defer tasks.SetRunner("dns_follow", previous)
	Register()
	Register()
	names := lifecycle.Names()
	count := 0
	for _, name := range names {
		if name == "dns-follow" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate/missing periodic owner", names)
	}
	if !slices.Contains(tasks.Missing(), tasks.SlotAgents) {
		t.Fatal("A17 incorrectly supplied the missing A16 task-row half")
	}
	runner := tasks.Runner("dns_follow")
	if runner == nil {
		t.Fatal("C07 DNS follow runner missing")
	}
	f.editAgent(t, "target", func(a *agents.Record) { a.Set("dns_follow_state", jsonx.Obj("status", "failed")) })
	if err := runner(f.ctx, "dns_follow", "target"); err != nil {
		t.Fatal(err)
	}
	m, err := agents.Get(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if st := state(f.agent(t)); st.Status != "waiting" || !value(st.RetryRequested) {
		t.Fatal(st)
	}
	if err := runner(f.ctx, "dns_follow", "target"); err == nil {
		t.Fatal("runner allowed waiting retry")
	}
	if ForManager(f.m) != ForManager(f.m) {
		t.Fatal("manager owner not shared")
	}
	f.m.Close()
	services.Lock()
	_, exists := services.byManager[f.m]
	services.Unlock()
	if exists {
		t.Fatal("closed manager retained")
	}
}
